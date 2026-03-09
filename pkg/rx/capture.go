package rx

import (
	"context"
	"fmt"
	"sync"

	"github.com/gopacket/gopacket"
)

// CaptureMode determines how packets are captured from interfaces.
type CaptureMode int

const (
	// CaptureModeDedicated creates one AF_PACKET handle per consumer.
	// Uses TPACKET_V3 mmap ring buffer with Retire_blk_tov=1 for low latency.
	// Simpler, better isolation, but more kernel overhead.
	CaptureModeDedicated CaptureMode = iota

	// CaptureModeShared creates one AF_PACKET handle per interface, demuxes in userspace.
	// Uses TPACKET_V3 mmap ring buffer with Retire_blk_tov=1 for low latency.
	// Lower latency, less kernel overhead, but requires coordination.
	CaptureModeShared

	// CaptureModeLibpcap uses gopacket/pcap (libpcap) matching vendor wfb-ng exactly:
	// - pcap_set_immediate_mode(1) for low latency
	// - pcap_set_timeout(-1) for blocking
	// - BPF filter: ether[0x0a:2]==0x5742 && ether[0x0c:4]==channel_id
	// This is for testing/debugging to match vendor behavior.
	// Requires build tag: -tags libpcap
	CaptureModeLibpcap
)

// String returns the string representation of the capture mode.
func (m CaptureMode) String() string {
	switch m {
	case CaptureModeDedicated:
		return "dedicated"
	case CaptureModeShared:
		return "shared"
	case CaptureModeLibpcap:
		return "libpcap"
	default:
		return fmt.Sprintf("unknown(%d)", int(m))
	}
}

// PacketSource provides packets from a capture source.
type PacketSource interface {
	// ReadPacket blocks until a packet is available.
	ReadPacket() (data []byte, ci gopacket.CaptureInfo, err error)
	// Close releases this consumer's resources.
	Close() error
}

// CaptureManager manages packet capture handles.
// In dedicated mode, each consumer gets its own handle.
// In shared mode, one handle per interface is shared among consumers.
type CaptureManager struct {
	mu      sync.Mutex
	mode    CaptureMode
	shared  map[string]*SharedCapture // interface -> shared capture
	started bool
}

// NewCaptureManager creates a new capture manager.
func NewCaptureManager(mode CaptureMode) *CaptureManager {
	return &CaptureManager{
		mode:   mode,
		shared: make(map[string]*SharedCapture),
	}
}

// GetSource returns a packet source for the given interface and channel.
// In dedicated mode, creates a new AF_PACKET handle.
// In shared mode, returns a consumer for the shared handle.
// In libpcap mode, creates a new libpcap handle matching vendor wfb-ng.
func (cm *CaptureManager) GetSource(iface string, channelID uint32, wlanIdx uint8) (PacketSource, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	switch cm.mode {
	case CaptureModeDedicated:
		return newDedicatedSource(iface, channelID, wlanIdx)

	case CaptureModeLibpcap:
		return NewLibpcapSource(iface, channelID, wlanIdx, 0)

	case CaptureModeShared:
		// Shared mode - get or create shared capture for this interface
		sc, ok := cm.shared[iface]
		if !ok {
			var err error
			sc, err = newSharedCapture(iface)
			if err != nil {
				return nil, err
			}
			cm.shared[iface] = sc
		}
		return sc.AddConsumer(channelID, wlanIdx), nil

	default:
		return newDedicatedSource(iface, channelID, wlanIdx)
	}
}

// Start begins capture on all shared handles.
// No-op in dedicated mode (Receivers own their sources).
func (cm *CaptureManager) Start() {
	if cm.mode == CaptureModeDedicated {
		return
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.started {
		return
	}
	cm.started = true

	for _, sc := range cm.shared {
		go sc.Run()
	}
}

// Close stops all shared captures and releases resources.
// No-op in dedicated mode (Receivers own their sources).
func (cm *CaptureManager) Close() error {
	if cm.mode == CaptureModeDedicated {
		return nil
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	for _, sc := range cm.shared {
		sc.Close()
	}
	cm.shared = make(map[string]*SharedCapture)
	cm.started = false

	return nil
}

// Mode returns the current capture mode.
func (cm *CaptureManager) Mode() CaptureMode {
	return cm.mode
}

// dedicatedSource wraps an AF_PACKET handle for dedicated mode.
type dedicatedSource struct {
	source    *AFPacketSource
	channelID uint32
	wlanIdx   uint8
}

func newDedicatedSource(iface string, channelID uint32, wlanIdx uint8) (*dedicatedSource, error) {
	source, err := NewAFPacketSource(AFPacketConfig{
		Interface:   iface,
		Promiscuous: true,
		BPFFilter:   BuildWFBFilter(channelID), // Kernel-level filtering
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open AF_PACKET on %s: %w", iface, err)
	}

	return &dedicatedSource{
		source:    source,
		channelID: channelID,
		wlanIdx:   wlanIdx,
	}, nil
}

func (ds *dedicatedSource) ReadPacket() ([]byte, gopacket.CaptureInfo, error) {
	return ds.source.ReadPacket()
}

func (ds *dedicatedSource) Close() error {
	return ds.source.Close()
}

// SharedCapture manages a single AF_PACKET handle shared by multiple consumers.
type SharedCapture struct {
	mu             sync.RWMutex
	iface          string
	source         *AFPacketSource
	consumers      map[uint32]*sharedConsumer // channelID -> consumer
	ctx            context.Context
	cancel         context.CancelFunc
	extractFailed  uint64 // Count of packets where channel ID extraction failed
}

func newSharedCapture(iface string) (*SharedCapture, error) {
	ctx, cancel := context.WithCancel(context.Background())

	source, err := NewAFPacketSource(AFPacketConfig{
		Interface:   iface,
		Promiscuous: true,
		BPFFilter:   BuildWFBMagicFilter(), // Filter for WFB magic, demux channel in userspace
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to open AF_PACKET on %s: %w", iface, err)
	}

	return &SharedCapture{
		iface:     iface,
		source:    source,
		consumers: make(map[uint32]*sharedConsumer),
		ctx:       ctx,
		cancel:    cancel,
	}, nil
}

// AddConsumer adds a consumer for the given channel ID.
func (sc *SharedCapture) AddConsumer(channelID uint32, wlanIdx uint8) *sharedConsumer {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	// Check if consumer for this channel already exists
	if consumer, ok := sc.consumers[channelID]; ok {
		return consumer
	}

	consumer := &sharedConsumer{
		channelID: channelID,
		wlanIdx:   wlanIdx,
		packets:   make(chan capturedPacket, 256), // Buffer to avoid blocking capture loop
		ctx:       sc.ctx,
	}
	sc.consumers[channelID] = consumer

	return consumer
}

// Run starts the capture loop, dispatching packets to consumers.
func (sc *SharedCapture) Run() {
	for {
		select {
		case <-sc.ctx.Done():
			return
		default:
		}

		data, ci, err := sc.source.ReadPacket()
		if err != nil {
			select {
			case <-sc.ctx.Done():
				return
			default:
				continue
			}
		}

		// Extract channel ID from packet (offset 12-16 in 802.11 header after radiotap)
		// This requires parsing radiotap to find the right offset
		channelID := sc.extractChannelID(data)
		if channelID == 0 {
			continue
		}

		// Dispatch to matching consumer
		sc.mu.RLock()
		consumer, ok := sc.consumers[channelID]
		sc.mu.RUnlock()

		if ok {
			// Non-blocking send - drop packet if consumer is slow
			select {
			case consumer.packets <- capturedPacket{data: data, ci: ci}:
			default:
				// Consumer buffer full, drop packet
			}
		}
	}
}

// extractChannelID parses the packet to extract the WFB channel ID.
func (sc *SharedCapture) extractChannelID(data []byte) uint32 {
	// Need at least radiotap header length field (4 bytes) + some data
	if len(data) < 4 {
		return 0
	}

	// Radiotap header length is at offset 2-3 (little endian)
	rtLen := int(data[2]) | int(data[3])<<8
	if len(data) < rtLen+24 { // radiotap + 802.11 header
		return 0
	}

	// Channel ID is at offset 12-16 in the 802.11 header (Address 2, bytes 2-6)
	// 802.11 header starts after radiotap
	ieee80211Start := rtLen
	channelIDOffset := ieee80211Start + 12 // Skip frame control (2) + duration (2) + addr1 (6) + addr2[0:2] (2)

	if len(data) < channelIDOffset+4 {
		return 0
	}

	// Channel ID is big-endian in the packet
	return uint32(data[channelIDOffset])<<24 |
		uint32(data[channelIDOffset+1])<<16 |
		uint32(data[channelIDOffset+2])<<8 |
		uint32(data[channelIDOffset+3])
}

// Close stops the capture and closes all consumers.
func (sc *SharedCapture) Close() {
	sc.cancel()

	sc.mu.Lock()
	defer sc.mu.Unlock()

	for _, consumer := range sc.consumers {
		close(consumer.packets)
	}
	sc.consumers = make(map[uint32]*sharedConsumer)

	sc.source.Close()
}

// sharedConsumer receives packets from a SharedCapture for a specific channel.
type sharedConsumer struct {
	channelID uint32
	wlanIdx   uint8
	packets   chan capturedPacket
	ctx       context.Context
	closed    bool
	mu        sync.Mutex
}

type capturedPacket struct {
	data []byte
	ci   gopacket.CaptureInfo
}

func (c *sharedConsumer) ReadPacket() ([]byte, gopacket.CaptureInfo, error) {
	select {
	case pkt, ok := <-c.packets:
		if !ok {
			return nil, gopacket.CaptureInfo{}, fmt.Errorf("consumer closed")
		}
		return pkt.data, pkt.ci, nil
	case <-c.ctx.Done():
		return nil, gopacket.CaptureInfo{}, c.ctx.Err()
	}
}

func (c *sharedConsumer) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Just mark as closed - the SharedCapture owns the channel
	c.closed = true
	return nil
}
