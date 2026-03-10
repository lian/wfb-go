package rx

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lian/wfb-go/pkg/crypto"
	"github.com/lian/wfb-go/pkg/protocol"
)

// Debug enables verbose packet tracing (set via WFB_DEBUG=1 environment variable)
var Debug = os.Getenv("WFB_DEBUG") == "1"

func debugLog(format string, args ...interface{}) {
	if Debug {
		fmt.Fprintf(os.Stderr, "[AGG] "+format+"\n", args...)
	}
}

var (
	ErrNoSession      = errors.New("rx: no session established")
	ErrDecryptFailed  = errors.New("rx: decryption failed")
	ErrInvalidPacket  = errors.New("rx: invalid packet")
	ErrOldEpoch       = errors.New("rx: old epoch")
	ErrWrongChannel   = errors.New("rx: wrong channel ID")
	ErrUnsupportedFEC = errors.New("rx: unsupported FEC type")
)

// OutputFunc is called for each reassembled packet.
type OutputFunc func(data []byte) error

// Stats holds receiver statistics (snapshot).
type Stats struct {
	PacketsAll      uint64 // All received packets (count_p_all)
	BytesAll        uint64 // All received bytes
	PacketsUniq     uint64 // Unique data packets stored (count_p_uniq)
	PacketsSession  uint64 // Session packets
	PacketsData     uint64 // Data packets
	PacketsDecErr   uint64 // Decryption errors (count_p_dec_err)
	PacketsBad      uint64 // Malformed packets (count_p_bad)
	PacketsFECRec   uint64 // FEC recovered packets (count_p_fec_recovered)
	PacketsLost     uint64 // Lost packets (count_p_lost)
	PacketsOverride uint64 // Ring overflows (count_p_override)
	PacketsOutgoing uint64 // Output packets
	BytesOutgoing   uint64 // Output bytes (count_b_all)

	// Session info from TX (0 if no session received yet)
	Epoch uint64
	FecK  int
	FecN  int

	// Per-antenna statistics (keyed by wlanIdx<<8 | antennaIdx)
	AntennaStats map[uint32]*AntennaStats
}

// AntennaStats holds per-antenna statistics.
type AntennaStats struct {
	WlanIdx   uint8
	Antenna   uint8
	Freq      uint16
	MCSIndex  uint8
	Bandwidth uint8

	PacketsReceived uint64
	RSSIMin         int8
	RSSIMax         int8
	RSSISum         int64 // For calculating average
	SNRMin          int8
	SNRMax          int8
	SNRSum          int64 // For calculating average
}

// atomicStats holds atomic counters for lock-free statistics.
type atomicStats struct {
	packetsAll      atomic.Uint64
	bytesAll        atomic.Uint64
	packetsUniq     atomic.Uint64
	packetsSession  atomic.Uint64
	packetsData     atomic.Uint64
	packetsDecErr   atomic.Uint64
	packetsBad      atomic.Uint64
	packetsFECRec   atomic.Uint64
	packetsLost     atomic.Uint64
	packetsOverride atomic.Uint64
	packetsOutgoing atomic.Uint64
	bytesOutgoing   atomic.Uint64
}

func (s *atomicStats) snapshot() Stats {
	return Stats{
		PacketsAll:      s.packetsAll.Load(),
		BytesAll:        s.bytesAll.Load(),
		PacketsUniq:     s.packetsUniq.Load(),
		PacketsSession:  s.packetsSession.Load(),
		PacketsData:     s.packetsData.Load(),
		PacketsDecErr:   s.packetsDecErr.Load(),
		PacketsBad:      s.packetsBad.Load(),
		PacketsFECRec:   s.packetsFECRec.Load(),
		PacketsLost:     s.packetsLost.Load(),
		PacketsOverride: s.packetsOverride.Load(),
		PacketsOutgoing: s.packetsOutgoing.Load(),
		BytesOutgoing:   s.bytesOutgoing.Load(),
	}
}

func (s *atomicStats) reset() {
	s.packetsAll.Store(0)
	s.bytesAll.Store(0)
	s.packetsUniq.Store(0)
	s.packetsSession.Store(0)
	s.packetsData.Store(0)
	s.packetsDecErr.Store(0)
	s.packetsBad.Store(0)
	s.packetsFECRec.Store(0)
	s.packetsLost.Store(0)
	s.packetsOverride.Store(0)
	s.packetsOutgoing.Store(0)
	s.bytesOutgoing.Store(0)
}

// outputPacket is a packet ready for output.
type outputPacket struct {
	buf  *[]byte // Pooled buffer (return to pool after use)
	data []byte  // Slice of buf with actual data
	seq  uint32
}

// outputBufPool reduces allocations for output packet buffers.
var outputBufPool = sync.Pool{
	New: func() interface{} {
		// Allocate buffer for max payload (MTU can be up to 4096)
		buf := make([]byte, 4096)
		return &buf
	},
}


// Aggregator handles session management, decryption, and FEC recovery.
type Aggregator struct {
	mu sync.Mutex

	// Channel
	channelID uint32
	epoch     uint64

	// Keypair (RX secret key + TX public key)
	rxSecretKey [32]byte
	txPublicKey [32]byte

	// Current session
	sessionKey  [crypto.KeySize]byte
	sessionHash [32]byte
	hasSession  bool
	aead        *crypto.AEAD

	// FEC
	ring       *Ring
	fecK       int
	fecN       int
	decryptBuf []byte // reused buffer for decryption

	// Output (async via channel to avoid blocking FEC path)
	outputFn   OutputFunc
	outputChan chan outputPacket

	// Sequence tracking (like vendor: seq = block_idx * fec_k + fragment_idx)
	lastBlockIdx uint64
	lastSeq      uint32 // last output packet sequence number

	// Stats (atomic, lock-free access)
	stats atomicStats

	// Per-antenna stats (written by background processor only)
	antMu        sync.Mutex // Only for writes
	antennaStats map[uint32]*AntennaStats

	// Published antenna stats snapshot (lock-free reads via atomic)
	antSnapshot atomic.Pointer[map[uint32]*AntennaStats]

	// Async antenna stats channel (non-blocking updates from hot path)
	antStatsChan chan RXInfo

	// Stop signal for background goroutines
	stopChan  chan struct{}
	closeOnce sync.Once
}

// AggregatorConfig holds aggregator configuration.
type AggregatorConfig struct {
	KeyData   []byte     // RX key data (64 bytes: secret key + peer public key)
	Epoch     uint64     // Minimum epoch to accept
	ChannelID uint32     // Expected channel ID
	OutputFn  OutputFunc // Output callback
}

// NewAggregator creates a new aggregator.
func NewAggregator(cfg AggregatorConfig) (*Aggregator, error) {
	// Parse keys
	rxKey, err := crypto.ParseRXKey(cfg.KeyData)
	if err != nil {
		return nil, err
	}

	a := &Aggregator{
		channelID:    cfg.ChannelID,
		epoch:        cfg.Epoch,
		outputFn:     cfg.OutputFn,
		fecK:         8,  // Will be updated by session packet
		fecN:         12, // Will be updated by session packet
		decryptBuf:   make([]byte, protocol.MAX_FEC_PAYLOAD),
		antennaStats: make(map[uint32]*AntennaStats),
		outputChan:   make(chan outputPacket, 256), // Buffered for non-blocking output
		antStatsChan: make(chan RXInfo, 256),       // Buffered for non-blocking stats
		stopChan:     make(chan struct{}),
	}

	copy(a.rxSecretKey[:], rxKey.SecretKey[:])
	copy(a.txPublicKey[:], rxKey.TXPubKey[:])

	// Start background processors
	go a.runOutputProcessor()
	go a.runAntennaStatsProcessor()

	return a, nil
}

// runOutputProcessor sends packets from the output channel.
// Runs in background to avoid blocking the FEC path.
func (a *Aggregator) runOutputProcessor() {
	var lastSeq uint32
	for {
		select {
		case <-a.stopChan:
			return
		case pkt := <-a.outputChan:
			// Count lost packets via sequence gaps
			if pkt.seq > lastSeq+1 && lastSeq > 0 {
				lostCount := pkt.seq - lastSeq - 1
				a.stats.packetsLost.Add(uint64(lostCount))
			}
			lastSeq = pkt.seq

			// Send to output
			if a.outputFn != nil {
				a.outputFn(pkt.data)
			}

			a.stats.packetsOutgoing.Add(1)
			a.stats.bytesOutgoing.Add(uint64(len(pkt.data)))

			// Return buffer to pool
			if pkt.buf != nil {
				outputBufPool.Put(pkt.buf)
			}
		}
	}
}

// runAntennaStatsProcessor processes antenna stats updates from the channel.
// Runs in background to avoid blocking the hot path.
// Periodically publishes a snapshot for lock-free reads.
func (a *Aggregator) runAntennaStatsProcessor() {
	updateCount := 0
	for {
		select {
		case <-a.stopChan:
			return
		case info := <-a.antStatsChan:
			a.processAntennaStats(&info)
			updateCount++
			// Publish snapshot every 100 updates
			if updateCount >= 100 {
				a.publishAntennaSnapshot()
				updateCount = 0
			}
		}
	}
}

// publishAntennaSnapshot creates a copy of antenna stats for lock-free reads.
func (a *Aggregator) publishAntennaSnapshot() {
	a.antMu.Lock()
	if len(a.antennaStats) == 0 {
		a.antMu.Unlock()
		return
	}
	snapshot := make(map[uint32]*AntennaStats, len(a.antennaStats))
	for k, v := range a.antennaStats {
		snapshot[k] = &AntennaStats{
			WlanIdx:         v.WlanIdx,
			Antenna:         v.Antenna,
			Freq:            v.Freq,
			MCSIndex:        v.MCSIndex,
			Bandwidth:       v.Bandwidth,
			PacketsReceived: v.PacketsReceived,
			RSSIMin:         v.RSSIMin,
			RSSIMax:         v.RSSIMax,
			RSSISum:         v.RSSISum,
			SNRMin:          v.SNRMin,
			SNRMax:          v.SNRMax,
			SNRSum:          v.SNRSum,
		}
	}
	a.antMu.Unlock()
	a.antSnapshot.Store(&snapshot)
}

// Close stops the aggregator's background goroutines.
// Safe to call multiple times.
func (a *Aggregator) Close() {
	a.closeOnce.Do(func() {
		close(a.stopChan)
	})
}

// Flush drains pending async operations for testing.
// Stops background processors and processes remaining items synchronously.
// After Flush(), the aggregator should not be used further.
func (a *Aggregator) Flush() {
	// Stop background processors first to avoid race
	a.Close()

	// Small sleep to let goroutines see stopChan and exit their select
	time.Sleep(10 * time.Millisecond)

	// Drain antenna stats channel and process synchronously
	for {
		select {
		case info := <-a.antStatsChan:
			a.processAntennaStats(&info)
		default:
			goto doneAntenna
		}
	}
doneAntenna:

	// Publish antenna snapshot
	a.publishAntennaSnapshot()

	// Drain output channel
	for {
		select {
		case pkt := <-a.outputChan:
			// Count stats like the output processor would
			a.stats.packetsOutgoing.Add(1)
			a.stats.bytesOutgoing.Add(uint64(len(pkt.data)))
			// Call output function
			if a.outputFn != nil {
				a.outputFn(pkt.data)
			}
			// Return buffer to pool
			if pkt.buf != nil {
				outputBufPool.Put(pkt.buf)
			}
		default:
			return
		}
	}
}

// ProcessPacket processes a received WFB packet.
func (a *Aggregator) ProcessPacket(data []byte) error {
	a.stats.packetsAll.Add(1)
	a.stats.bytesAll.Add(uint64(len(data)))

	if len(data) == 0 {
		return ErrInvalidPacket
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	switch data[0] {
	case protocol.WFB_PACKET_SESSION:
		return a.processSessionPacket(data)
	case protocol.WFB_PACKET_DATA:
		return a.processDataPacket(data)
	default:
		a.stats.packetsBad.Add(1)
		return ErrInvalidPacket
	}
}

// processSessionPacket handles session key announcements.
func (a *Aggregator) processSessionPacket(data []byte) error {
	minSize := protocol.SESSION_HDR_SIZE + protocol.SessionDataSize + protocol.BOX_MAC_SIZE
	if len(data) < minSize {
		a.stats.packetsBad.Add(1)
		return ErrInvalidPacket
	}

	// Parse header
	hdr, err := protocol.UnmarshalSessionHeader(data)
	if err != nil {
		a.stats.packetsBad.Add(1)
		return err
	}

	// Compute keyed hash of encrypted portion to detect duplicates
	// Uses BLAKE2b with session nonce as key, matching C++ crypto_generichash
	newHash := crypto.KeyedHash(data[protocol.SESSION_HDR_SIZE:], hdr.SessionNonce[:])

	// Check if this is a duplicate session
	if a.hasSession && bytes.Equal(a.sessionHash[:], newHash[:]) {
		a.stats.packetsSession.Add(1)
		return nil
	}

	// Decrypt session data
	var nonce [24]byte
	copy(nonce[:], hdr.SessionNonce[:])

	ciphertext := data[protocol.SESSION_HDR_SIZE:]

	plaintext, ok := crypto.BoxOpen(
		ciphertext,
		&nonce,
		&a.txPublicKey,
		&a.rxSecretKey,
	)
	if !ok {
		a.stats.packetsDecErr.Add(1)
		return ErrDecryptFailed
	}

	// Parse session data
	sessionData, err := protocol.UnmarshalSessionData(plaintext)
	if err != nil {
		a.stats.packetsDecErr.Add(1)
		return err
	}

	// Validate epoch
	if sessionData.Epoch < a.epoch {
		a.stats.packetsDecErr.Add(1)
		return ErrOldEpoch
	}

	// Validate channel ID
	if sessionData.ChannelID != a.channelID {
		a.stats.packetsDecErr.Add(1)
		return ErrWrongChannel
	}

	// Validate FEC type
	if sessionData.FECType != protocol.WFB_FEC_VDM_RS {
		a.stats.packetsDecErr.Add(1)
		return ErrUnsupportedFEC
	}

	// Validate FEC parameters
	k := int(sessionData.K)
	n := int(sessionData.N)
	if k < 1 || n < 1 || k > n {
		a.stats.packetsDecErr.Add(1)
		return ErrInvalidPacket
	}

	a.stats.packetsSession.Add(1)

	// Check if session key changed
	if !bytes.Equal(a.sessionKey[:], sessionData.SessionKey[:]) {
		debugLog("SESSION_NEW epoch=%d fec=%d/%d (old fec=%d/%d)", sessionData.Epoch, k, n, a.fecK, a.fecN)

		// Update session
		a.epoch = sessionData.Epoch
		copy(a.sessionKey[:], sessionData.SessionKey[:])

		// Create new AEAD cipher
		aead, err := crypto.NewAEAD(a.sessionKey[:])
		if err != nil {
			return err
		}
		a.aead = aead

		// Reset sequence tracking on new session
		a.lastSeq = 0

		// Update FEC parameters and reset ring
		// IMPORTANT: Always reset ring on session change to clear stale block indices
		if k != a.fecK || n != a.fecN || a.ring == nil {
			a.fecK = k
			a.fecN = n

			// Create new ring with new parameters
			ring, err := NewRing(k, n)
			if err != nil {
				return err
			}
			a.ring = ring
			debugLog("SESSION_NEW ring created k=%d n=%d", k, n)
		} else {
			// Same FEC params but new session - reset ring state
			// This clears lastKnown so new blocks aren't rejected
			if err := a.ring.Reset(k, n); err != nil {
				return err
			}
			debugLog("SESSION_NEW ring reset k=%d n=%d", k, n)
		}

		a.hasSession = true
	}

	// Cache session hash
	copy(a.sessionHash[:], newHash[:])

	return nil
}

// processDataPacket handles encrypted data packets.
func (a *Aggregator) processDataPacket(data []byte) error {
	if !a.hasSession {
		a.stats.packetsDecErr.Add(1)
		return ErrNoSession
	}

	minSize := protocol.BLOCK_HDR_SIZE + protocol.CHACHA_TAG_SIZE + protocol.PACKET_HDR_SIZE
	if len(data) < minSize {
		a.stats.packetsBad.Add(1)
		return ErrInvalidPacket
	}

	// Parse block header
	blockHdr, err := protocol.UnmarshalBlockHeader(data)
	if err != nil {
		a.stats.packetsBad.Add(1)
		return err
	}

	// Decrypt BEFORE updating ring state. A corrupt packet with garbage blockIdx
	// could poison lastKnown, causing all subsequent valid packets to be rejected
	// as "too old". We must verify the packet is authentic before trusting its header.
	nonceBytes := crypto.NonceFromUint64(blockHdr.DataNonce)
	plaintext, err := a.aead.Open(a.decryptBuf[:0], nonceBytes[:], data[protocol.BLOCK_HDR_SIZE:], data[:protocol.BLOCK_HDR_SIZE])
	if err != nil {
		a.stats.packetsDecErr.Add(1)
		return ErrDecryptFailed
	}

	// NOW it's safe to parse block/fragment indices - packet is authenticated
	blockIdx, fragmentIdx := protocol.ParseDataNonce(blockHdr.DataNonce)

	debugLog("DATA_RX block=0x%x frag=%d", blockIdx, fragmentIdx)

	// Sanity check block index (matches wfb-ng)
	if blockIdx > protocol.MAX_BLOCK_IDX {
		a.stats.packetsBad.Add(1)
		return ErrInvalidPacket
	}

	// Get ring slot for this block
	ringIdx, evicted, valid := a.ring.GetBlockRingIdx(blockIdx)
	if !valid {
		// Block is too old
		debugLog("DATA_OLD block=0x%x (rejected)", blockIdx)
		return nil
	}

	// Flush any evicted blocks (ring overflow)
	for _, evictedIdx := range evicted {
		a.flushEvictedBlock(evictedIdx)
	}

	// Get fragment buffer and copy decrypted data
	fragBuf := a.ring.GetFragmentBuffer(ringIdx, fragmentIdx)
	if fragBuf == nil {
		// Already have this fragment (duplicate)
		a.stats.packetsData.Add(1)
		return nil
	}

	// Copy authenticated plaintext into ring buffer
	copy(fragBuf, plaintext)

	a.stats.packetsData.Add(1)
	a.stats.packetsUniq.Add(1) // Non-duplicate fragment successfully stored

	// Mark fragment as filled
	complete := a.ring.SetFragmentSize(ringIdx, fragmentIdx, len(plaintext))

	// Optimization: if this is the front block, send fragments immediately if no gaps
	if ringIdx == a.ring.Front() {
		if err := a.sendContiguousFragments(ringIdx); err != nil {
			return err
		}
		// If all k fragments sent without gaps, advance ring
		if a.ring.IsComplete(ringIdx) {
			a.ring.Advance()
			return nil
		}
	}

	// If block has k fragments and can be FEC recovered
	if complete {
		debugLog("BLOCK_COMPLETE block=0x%x ringIdx=%d", blockIdx, ringIdx)

		// Flush all older incomplete blocks first
		if err := a.flushOlderBlocks(ringIdx); err != nil {
			return err
		}

		// Now process this block with FEC if needed
		if err := a.processCompleteBlock(ringIdx); err != nil {
			return err
		}

		// Send all fragments from this block
		if err := a.sendAllFragments(ringIdx); err != nil {
			return err
		}

		// Advance past this block
		a.ring.Advance()
	}

	return nil
}

// processCompleteBlock handles a block that has enough fragments.
func (a *Aggregator) processCompleteBlock(ringIdx int) error {
	// Check if FEC recovery is needed
	if a.ring.NeedsRecovery(ringIdx) {
		recovered, err := a.ring.Recover(ringIdx)
		if err != nil {
			debugLog("FEC_ERROR ringIdx=%d err=%v", ringIdx, err)
			return err
		}
		if recovered > 0 {
			debugLog("FEC_RECOVERED ringIdx=%d count=%d", ringIdx, recovered)
		}
		a.stats.packetsFECRec.Add(uint64(recovered))
	}

	return nil
}

// sendContiguousFragments sends fragments from the front block that have no gaps.
// This is an optimization to reduce latency - we don't wait for FEC if fragments arrive in order.
func (a *Aggregator) sendContiguousFragments(ringIdx int) error {
	blockIdx := a.ring.GetBlockIdx(ringIdx)
	for {
		fragIdx, ok := a.ring.PeekNextToSend(ringIdx)
		if !ok {
			break
		}

		// Check if this fragment is available
		fragData, fragSize := a.ring.GetFragment(ringIdx, fragIdx)
		if fragSize == 0 {
			// Gap - stop sending contiguous fragments
			break
		}

		// Send the fragment (with sequence tracking for lost packet counting)
		if err := a.outputFragment(fragData, blockIdx, fragIdx); err != nil {
			return err
		}

		// Advance the send pointer
		a.ring.AdvanceFragmentToSend(ringIdx)
	}
	return nil
}

// sendAllFragments sends all k data fragments from a block (after FEC recovery if needed).
func (a *Aggregator) sendAllFragments(ringIdx int) error {
	blockIdx := a.ring.GetBlockIdx(ringIdx)
	for {
		fragIdx, ok := a.ring.GetNextToSend(ringIdx)
		if !ok {
			break
		}

		fragData, fragSize := a.ring.GetFragment(ringIdx, fragIdx)
		if fragSize > 0 {
			if err := a.outputFragment(fragData, blockIdx, fragIdx); err != nil {
				return err
			}
		}
	}
	return nil
}

// flushOlderBlocks sends whatever fragments are available from blocks older than ringIdx.
// This is called when a newer block becomes FEC-recoverable.
func (a *Aggregator) flushOlderBlocks(targetRingIdx int) error {
	for {
		frontIdx := a.ring.Front()
		if frontIdx == targetRingIdx {
			// Reached the target block
			break
		}

		if err := a.flushSingleBlock(frontIdx); err != nil {
			return err
		}
	}
	return nil
}

// flushEvictedBlock sends remaining fragments from a block being evicted.
// Called when ring overflow forces eviction of an incomplete block.
func (a *Aggregator) flushEvictedBlock(ringIdx int) {
	blockIdx := a.ring.GetBlockIdx(ringIdx)
	debugLog("RING_OVERFLOW block=0x%x ringIdx=%d", blockIdx, ringIdx)

	a.stats.packetsOverride.Add(1)

	// Send any remaining fragments (like wfb-ng rx_ring_push)
	fecK := a.ring.FecK()
	for fragIdx := uint8(0); fragIdx < uint8(fecK); fragIdx++ {
		fragData, fragSize := a.ring.GetFragment(ringIdx, fragIdx)
		if fragSize > 0 && !a.ring.WasSent(ringIdx, fragIdx) {
			a.outputFragment(fragData, blockIdx, fragIdx)
		}
	}
}

// flushSingleBlock sends available fragments from a block and advances the ring.
// Used by flushOlderBlocks when a newer block becomes complete.
func (a *Aggregator) flushSingleBlock(ringIdx int) error {
	blockIdx := a.ring.GetBlockIdx(ringIdx)
	debugLog("FLUSH_BLOCK block=0x%x ringIdx=%d", blockIdx, ringIdx)

	// Send any available fragments from this incomplete block
	fecK := a.ring.FecK()
	for fragIdx := uint8(0); fragIdx < uint8(fecK); fragIdx++ {
		fragData, fragSize := a.ring.GetFragment(ringIdx, fragIdx)
		if fragSize > 0 && !a.ring.WasSent(ringIdx, fragIdx) {
			if err := a.outputFragment(fragData, blockIdx, fragIdx); err != nil {
				return err
			}
		}
	}

	// Advance to next block
	a.ring.Advance()
	return nil
}

// outputFragment extracts the actual packet from a fragment and queues it for output.
// Uses non-blocking channel send to avoid stalling the FEC path.
func (a *Aggregator) outputFragment(fragment []byte, blockIdx uint64, fragIdx uint8) error {
	if len(fragment) < protocol.PACKET_HDR_SIZE {
		return nil
	}

	// Parse packet header
	pktHdr, err := protocol.UnmarshalPacketHeader(fragment)
	if err != nil {
		return nil
	}

	// Check for FEC-only packet (padding, no actual data)
	if pktHdr.Flags&protocol.WFB_PACKET_FEC_ONLY != 0 {
		return nil
	}

	// Extract payload
	payloadSize := int(pktHdr.PacketSize)
	if payloadSize == 0 {
		return nil
	}

	if protocol.PACKET_HDR_SIZE+payloadSize > len(fragment) {
		return nil
	}

	// Get buffer from pool and copy payload (fragment buffer may be reused)
	bufPtr := outputBufPool.Get().(*[]byte)
	buf := *bufPtr
	copy(buf, fragment[protocol.PACKET_HDR_SIZE:protocol.PACKET_HDR_SIZE+payloadSize])

	// Compute packet sequence (matches wfb-ng: seq = block_idx * fec_k + fragment_idx)
	packetSeq := uint32(blockIdx)*uint32(a.fecK) + uint32(fragIdx)

	// Non-blocking send to output channel
	select {
	case a.outputChan <- outputPacket{buf: bufPtr, data: buf[:payloadSize], seq: packetSeq}:
	default:
		// Channel full - output is slower than FEC processing
		outputBufPool.Put(bufPtr) // Return buffer since we're dropping
		a.stats.packetsLost.Add(1)
		log.Printf("[AGG] WARNING: output channel full, dropping packet seq=%d", packetSeq)
	}

	return nil
}

// Stats returns current statistics (completely lock-free).
func (a *Aggregator) Stats() Stats {
	stats := a.stats.snapshot()

	// Add session info from current session
	stats.Epoch = a.epoch
	stats.FecK = a.fecK
	stats.FecN = a.fecN

	// Read antenna stats from atomic snapshot (lock-free)
	if snap := a.antSnapshot.Load(); snap != nil {
		stats.AntennaStats = *snap
	}

	return stats
}

// StatsWithAntennas returns statistics including fresh per-antenna data.
// This acquires a lock - use only at shutdown for final stats.
func (a *Aggregator) StatsWithAntennas() Stats {
	stats := a.stats.snapshot()

	// Add session info from current session
	stats.Epoch = a.epoch
	stats.FecK = a.fecK
	stats.FecN = a.fecN

	// Get fresh antenna stats under lock
	a.antMu.Lock()
	if len(a.antennaStats) > 0 {
		stats.AntennaStats = make(map[uint32]*AntennaStats, len(a.antennaStats))
		for k, v := range a.antennaStats {
			stats.AntennaStats[k] = &AntennaStats{
				WlanIdx:         v.WlanIdx,
				Antenna:         v.Antenna,
				Freq:            v.Freq,
				MCSIndex:        v.MCSIndex,
				Bandwidth:       v.Bandwidth,
				PacketsReceived: v.PacketsReceived,
				RSSIMin:         v.RSSIMin,
				RSSIMax:         v.RSSIMax,
				RSSISum:         v.RSSISum,
				SNRMin:          v.SNRMin,
				SNRMax:          v.SNRMax,
				SNRSum:          v.SNRSum,
			}
		}
	}
	a.antMu.Unlock()

	return stats
}

// ResetStats resets statistics counters.
func (a *Aggregator) ResetStats() {
	a.stats.reset()
}

// HasSession returns whether a session has been established.
func (a *Aggregator) HasSession() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.hasSession
}

// FEC returns current FEC parameters.
func (a *Aggregator) FEC() (k, n int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.fecK, a.fecN
}

// UpdateAntennaStats queues an antenna stats update from the hot path.
// Non-blocking: drops the update if the channel is full.
func (a *Aggregator) UpdateAntennaStats(info *RXInfo) {
	if info == nil {
		return
	}

	// Non-blocking send to avoid stalling the hot path
	select {
	case a.antStatsChan <- *info:
	default:
		// Channel full, drop this update (stats are best-effort)
	}
}

// processAntennaStats updates per-antenna statistics from RX info.
// Called by the background processor goroutine.
func (a *Aggregator) processAntennaStats(info *RXInfo) {
	a.antMu.Lock()
	defer a.antMu.Unlock()

	// Process each antenna in the RXInfo
	// Antennas with RSSI of 0 are considered inactive
	for i := 0; i < len(info.RSSI); i++ {
		rssi := info.RSSI[i]
		if rssi == 0 {
			continue // Skip inactive antennas
		}

		antIdx := info.Antenna[i]
		noise := info.Noise[i]
		snr := rssi - noise
		if noise == 0 {
			snr = 0 // Can't calculate SNR without noise floor
		}

		// Key: wlanIdx << 8 | antennaIdx
		key := uint32(info.WlanIdx)<<8 | uint32(antIdx)

		ant, ok := a.antennaStats[key]
		if !ok {
			ant = &AntennaStats{
				WlanIdx:   info.WlanIdx,
				Antenna:   antIdx,
				Freq:      info.Freq,
				MCSIndex:  info.MCSIndex,
				Bandwidth: info.Bandwidth,
				RSSIMin:   rssi,
				RSSIMax:   rssi,
				SNRMin:    snr,
				SNRMax:    snr,
			}
			a.antennaStats[key] = ant
		}

		ant.PacketsReceived++
		ant.RSSISum += int64(rssi)
		ant.SNRSum += int64(snr)

		if rssi < ant.RSSIMin {
			ant.RSSIMin = rssi
		}
		if rssi > ant.RSSIMax {
			ant.RSSIMax = rssi
		}
		if snr < ant.SNRMin {
			ant.SNRMin = snr
		}
		if snr > ant.SNRMax {
			ant.SNRMax = snr
		}

		// Update radio info
		ant.Freq = info.Freq
		ant.MCSIndex = info.MCSIndex
		ant.Bandwidth = info.Bandwidth
	}
}
