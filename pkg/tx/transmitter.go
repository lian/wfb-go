// Package tx implements the WFB transmitter.
package tx

import (
	"errors"
	"log"
	"sync"
	"time"

	"github.com/lian/wfb-go/pkg/crypto"
	"github.com/lian/wfb-go/pkg/fec"
	"github.com/lian/wfb-go/pkg/protocol"
)

const (
	// MaxBlockIdx is the maximum block index before session key rotation
	MaxBlockIdx = (1 << 56) - 1

	// SessionKeyAnnounceMsec is the interval for session key announcements
	SessionKeyAnnounceMsec = 1000
)

var (
	ErrPacketTooLarge = errors.New("tx: packet too large")
	ErrNoInjector     = errors.New("tx: no injector configured")
)

// Injector is the interface for packet injection.
type Injector interface {
	// Inject sends a packet over the air.
	Inject(data []byte) error
}

// Stats holds transmitter statistics.
type Stats struct {
	PacketsInjected  uint64
	BytesInjected    uint64
	PacketsDropped   uint64
	PacketsTruncated uint64 // Packets that exceeded MAX_PAYLOAD_SIZE and were truncated
	FECTimeouts      uint64
	SessionsStarted  uint64
}

// Transmitter handles FEC encoding and packet transmission.
type Transmitter struct {
	mu sync.Mutex

	// FEC parameters
	fecK    int
	fecN    int
	encoder *fec.Encoder

	// Block management
	blockIdx    uint64
	fragmentIdx uint8
	block       [][]byte // FEC block buffers
	maxPktSize  int      // max packet size in current block

	// Session
	epoch       uint64
	channelID   uint32
	sessionKey  [crypto.KeySize]byte
	sessionPkt  []byte // pre-built session packet
	aead        *crypto.AEAD

	// Keys
	txSecretKey [32]byte
	rxPublicKey [32]byte

	// Injection
	injector Injector

	// Stats
	stats Stats

	// FEC timing
	fecDelay   time.Duration
	fecTimeout time.Duration

	// FEC timeout management
	lastPacketTime time.Time  // Time of last packet sent (for timeout)
	timeoutStop    chan struct{}
	timeoutWg      sync.WaitGroup
}

// Config holds transmitter configuration.
type Config struct {
	FecK       int           // FEC data shards (default: 8)
	FecN       int           // FEC total shards (default: 12)
	Epoch      uint64        // Session epoch
	ChannelID  uint32        // (link_id << 8) | port
	FecDelay   time.Duration // Delay between FEC packets
	FecTimeout time.Duration // Timeout to close incomplete blocks (0=disabled)
	KeyData    []byte        // TX key data (64 bytes: secret key + peer public key)
}

// New creates a new transmitter.
func New(cfg Config, injector Injector) (*Transmitter, error) {
	if cfg.FecK <= 0 {
		cfg.FecK = 8
	}
	if cfg.FecN <= 0 {
		cfg.FecN = 12
	}

	// Parse keys
	txKey, err := crypto.ParseTXKey(cfg.KeyData)
	if err != nil {
		return nil, err
	}

	t := &Transmitter{
		fecK:       cfg.FecK,
		fecN:       cfg.FecN,
		epoch:      cfg.Epoch,
		channelID:  cfg.ChannelID,
		fecDelay:   cfg.FecDelay,
		fecTimeout: cfg.FecTimeout,
		injector:   injector,
	}

	copy(t.txSecretKey[:], txKey.SecretKey[:])
	copy(t.rxPublicKey[:], txKey.RXPubKey[:])

	// Initialize session
	if err := t.initSession(); err != nil {
		return nil, err
	}

	// Start FEC timeout handler if enabled
	if t.fecTimeout > 0 {
		t.timeoutStop = make(chan struct{})
		t.timeoutWg.Add(1)
		go t.fecTimeoutLoop()
	}

	return t, nil
}

// initSession initializes a new session with fresh keys.
func (t *Transmitter) initSession() error {
	// Create FEC encoder
	encoder, err := fec.NewEncoder(t.fecK, t.fecN)
	if err != nil {
		return err
	}
	t.encoder = encoder

	// Allocate FEC blocks
	t.block = make([][]byte, t.fecN)
	for i := 0; i < t.fecN; i++ {
		t.block[i] = make([]byte, protocol.MAX_FEC_PAYLOAD)
	}

	// Reset block counters
	t.blockIdx = 0
	t.fragmentIdx = 0
	t.maxPktSize = 0

	// Generate session key
	var err2 error
	t.sessionKey, err2 = crypto.GenerateSessionKey()
	if err2 != nil {
		return err2
	}

	// Create AEAD cipher
	t.aead, err2 = crypto.NewAEAD(t.sessionKey[:])
	if err2 != nil {
		return err2
	}

	// Build session packet
	t.sessionPkt, err2 = t.buildSessionPacket()
	if err2 != nil {
		return err2
	}

	t.stats.SessionsStarted++
	return nil
}

// buildSessionPacket creates the encrypted session announcement packet.
func (t *Transmitter) buildSessionPacket() ([]byte, error) {
	// Create session data
	sessionData := protocol.SessionData{
		Epoch:     t.epoch,
		ChannelID: t.channelID,
		FECType:   protocol.WFB_FEC_VDM_RS,
		K:         uint8(t.fecK),
		N:         uint8(t.fecN),
	}
	copy(sessionData.SessionKey[:], t.sessionKey[:])

	// Marshal session data
	plaintext := sessionData.Marshal()

	// Generate nonce for crypto_box
	nonce, err := crypto.GenerateBoxNonce()
	if err != nil {
		return nil, err
	}

	// Encrypt with crypto_box
	ciphertext := crypto.BoxSeal(plaintext, &nonce, &t.rxPublicKey, &t.txSecretKey)

	// Build session header
	hdr := protocol.SessionHeader{
		PacketType: protocol.WFB_PACKET_SESSION,
	}
	copy(hdr.SessionNonce[:], nonce[:])

	// Combine header + ciphertext
	pkt := make([]byte, protocol.SESSION_HDR_SIZE+len(ciphertext))
	copy(pkt[:protocol.SESSION_HDR_SIZE], hdr.Marshal())
	copy(pkt[protocol.SESSION_HDR_SIZE:], ciphertext)

	return pkt, nil
}

// SendSessionKey sends the session key announcement packet.
func (t *Transmitter) SendSessionKey() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.sendSessionKeyLocked()
}

func (t *Transmitter) sendSessionKeyLocked() error {
	if t.injector == nil {
		return ErrNoInjector
	}
	return t.injector.Inject(t.sessionPkt)
}

// SendPacket sends a data packet through the FEC encoder.
// Returns true if more packets can be added to the current block.
func (t *Transmitter) SendPacket(data []byte) (bool, error) {
	return t.sendPacketWithFlags(data, 0)
}

// SendFECOnly sends FEC-only packets to close an incomplete block.
// Returns true if a block was closed.
func (t *Transmitter) SendFECOnly() (bool, error) {
	return t.sendPacketWithFlags(nil, protocol.WFB_PACKET_FEC_ONLY)
}

func (t *Transmitter) sendPacketWithFlags(data []byte, flags uint8) (bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sendPacketWithFlagsLocked(data, flags)
}

func (t *Transmitter) sendPacketWithFlagsLocked(data []byte, flags uint8) (bool, error) {
	// FEC-only packets are only for closing already opened blocks
	isFECOnly := (flags & protocol.WFB_PACKET_FEC_ONLY) != 0
	if t.fragmentIdx == 0 && isFECOnly {
		return false, nil
	}

	payloadSize := len(data)
	if payloadSize > protocol.MAX_PAYLOAD_SIZE {
		log.Printf("[TX] WARNING: packet truncated from %d to %d bytes", payloadSize, protocol.MAX_PAYLOAD_SIZE)
		payloadSize = protocol.MAX_PAYLOAD_SIZE
		data = data[:payloadSize]
		t.stats.PacketsTruncated++
	}

	// Build packet header
	pktHdr := protocol.PacketHeader{
		Flags:      flags,
		PacketSize: uint16(payloadSize),
	}

	// Copy header + data into block buffer
	buf := t.block[t.fragmentIdx]
	copy(buf[:protocol.PACKET_HDR_SIZE], pktHdr.Marshal())
	if payloadSize > 0 {
		copy(buf[protocol.PACKET_HDR_SIZE:], data)
	}

	// Zero-pad the rest
	totalSize := protocol.PACKET_HDR_SIZE + payloadSize
	for i := totalSize; i < protocol.MAX_FEC_PAYLOAD; i++ {
		buf[i] = 0
	}

	// Send this fragment
	if err := t.sendBlockFragment(totalSize); err != nil {
		return false, err
	}

	// Track max packet size for FEC
	if totalSize > t.maxPktSize {
		t.maxPktSize = totalSize
	}
	t.fragmentIdx++

	// Update last packet time for FEC timeout (only for real data packets)
	if !isFECOnly {
		t.lastPacketTime = time.Now()
	}

	// If block not full, we can add more
	if t.fragmentIdx < uint8(t.fecK) {
		return true, nil
	}

	// Block is full - encode FEC and send parity packets
	if err := t.encodeFECAndSend(); err != nil {
		return false, err
	}

	return true, nil
}

// sendBlockFragment encrypts and sends a single fragment.
func (t *Transmitter) sendBlockFragment(packetSize int) error {
	if t.injector == nil {
		return ErrNoInjector
	}

	// Build block header
	nonce := (t.blockIdx << 8) | uint64(t.fragmentIdx)
	blockHdr := protocol.BlockHeader{
		PacketType: protocol.WFB_PACKET_DATA,
		DataNonce:  nonce,
	}

	// Encrypt payload
	plaintext := t.block[t.fragmentIdx][:packetSize]
	hdrBytes := blockHdr.Marshal()

	// Use nonce as 8-byte value for ChaCha20-Poly1305
	nonceBytes := crypto.NonceFromUint64(nonce)
	ciphertext := t.aead.Seal(nil, nonceBytes[:], plaintext, hdrBytes)

	// Build final packet: header + ciphertext
	pkt := make([]byte, protocol.BLOCK_HDR_SIZE+len(ciphertext))
	copy(pkt[:protocol.BLOCK_HDR_SIZE], hdrBytes)
	copy(pkt[protocol.BLOCK_HDR_SIZE:], ciphertext)

	// Inject
	if err := t.injector.Inject(pkt); err != nil {
		t.stats.PacketsDropped++
		return err
	}

	t.stats.PacketsInjected++
	t.stats.BytesInjected += uint64(len(pkt))
	return nil
}

// encodeFECAndSend generates and sends FEC parity packets.
func (t *Transmitter) encodeFECAndSend() error {
	// Prepare shards for encoding (all padded to maxPktSize)
	shards := make([][]byte, t.fecN)
	for i := 0; i < t.fecK; i++ {
		shards[i] = t.block[i][:t.maxPktSize]
	}
	for i := t.fecK; i < t.fecN; i++ {
		shards[i] = t.block[i][:t.maxPktSize]
	}

	// Encode
	if err := t.encoder.EncodeInPlace(shards); err != nil {
		return err
	}

	// Send parity packets
	for t.fragmentIdx < uint8(t.fecN) {
		if t.fecDelay > 0 {
			time.Sleep(t.fecDelay)
		}

		if err := t.sendBlockFragment(t.maxPktSize); err != nil {
			return err
		}
		t.fragmentIdx++
	}

	// Advance to next block
	t.blockIdx++
	t.fragmentIdx = 0
	t.maxPktSize = 0

	// Rotate session key if needed
	if t.blockIdx > MaxBlockIdx {
		if err := t.initSession(); err != nil {
			return err
		}
		// Send session key multiple times for redundancy
		for i := 0; i < t.fecN-t.fecK+1; i++ {
			if err := t.sendSessionKeyLocked(); err != nil {
				return err
			}
		}
	}

	return nil
}

// Stats returns current transmitter statistics.
func (t *Transmitter) Stats() Stats {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.stats
}

// SetFEC updates FEC parameters and restarts the session.
func (t *Transmitter) SetFEC(k, n int) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if k < 1 || n < 1 || k > n || n > 255 {
		return errors.New("tx: invalid FEC parameters")
	}

	// Close current block if open
	for t.fragmentIdx > 0 {
		if _, err := t.sendPacketWithFlagsLocked(nil, protocol.WFB_PACKET_FEC_ONLY); err != nil {
			return err
		}
	}

	t.fecK = k
	t.fecN = n

	// Reinitialize session
	if err := t.initSession(); err != nil {
		return err
	}

	// Announce new session key
	for i := 0; i < t.fecN-t.fecK+1; i++ {
		if err := t.sendSessionKeyLocked(); err != nil {
			return err
		}
	}

	return nil
}

// BlockInfo returns current block state.
func (t *Transmitter) BlockInfo() (blockIdx uint64, fragmentIdx uint8) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.blockIdx, t.fragmentIdx
}

// ChannelID returns the configured channel ID.
func (t *Transmitter) ChannelID() uint32 {
	return t.channelID
}

// FEC returns the current FEC parameters.
func (t *Transmitter) FEC() (k, n int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.fecK, t.fecN
}

// Close stops the FEC timeout handler and releases resources.
func (t *Transmitter) Close() {
	if t.timeoutStop != nil {
		close(t.timeoutStop)
		t.timeoutWg.Wait()
	}
}

// fecTimeoutLoop runs in a goroutine and closes incomplete FEC blocks after timeout.
func (t *Transmitter) fecTimeoutLoop() {
	defer t.timeoutWg.Done()

	ticker := time.NewTicker(t.fecTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-t.timeoutStop:
			return
		case <-ticker.C:
			t.checkFECTimeout()
		}
	}
}

// checkFECTimeout checks if the current block has timed out and closes it.
func (t *Transmitter) checkFECTimeout() {
	t.mu.Lock()
	defer t.mu.Unlock()

	// No open block
	if t.fragmentIdx == 0 {
		return
	}

	// Check if timeout has elapsed since last packet
	if time.Since(t.lastPacketTime) < t.fecTimeout {
		return
	}

	// Close the block by sending FEC-only packets
	for t.fragmentIdx > 0 && t.fragmentIdx < uint8(t.fecK) {
		if _, err := t.sendPacketWithFlagsLocked(nil, protocol.WFB_PACKET_FEC_ONLY); err != nil {
			return
		}
		t.stats.FECTimeouts++
	}
}
