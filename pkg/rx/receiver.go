package rx

import (
	"context"
	"encoding/binary"
	"errors"
	"log"
	"sync"

	"github.com/gopacket/gopacket"

	"github.com/lian/wfb-go/pkg/protocol"
	"github.com/lian/wfb-go/pkg/wifi/frame"
	"github.com/lian/wfb-go/pkg/wifi/radiotap"
)

var (
	ErrNotMonitorMode = errors.New("rx: interface not in monitor mode")
	ErrPcapOpen       = errors.New("rx: failed to open pcap")
)

// RXInfo holds metadata about a received packet.
type RXInfo struct {
	WlanIdx   uint8
	Antenna   [protocol.RX_ANT_MAX]uint8
	RSSI      [protocol.RX_ANT_MAX]int8
	Noise     [protocol.RX_ANT_MAX]int8
	Freq      uint16
	MCSIndex  uint8
	Bandwidth uint8
}

// PacketHandler is called for each received WFB packet.
type PacketHandler func(payload []byte, info *RXInfo) error

// Receiver captures packets from a WiFi interface in monitor mode.
type Receiver struct {
	mu sync.Mutex

	iface     string
	wlanIdx   uint8
	channelID uint32
	source    PacketSource // Packet source (from CaptureManager)
	handler   PacketHandler
	ctx       context.Context
	cancel    context.CancelFunc

	// Stats
	packetsReceived uint64
	packetsFiltered uint64
	packetsError    uint64
}

// ReceiverConfig holds receiver configuration.
type ReceiverConfig struct {
	Interface string        // WiFi interface name
	WlanIdx   uint8         // Interface index (for multi-interface setups)
	ChannelID uint32        // Channel ID to filter
	Handler   PacketHandler // Packet handler callback
	Source    PacketSource  // Packet source (from CaptureManager)
}

// NewReceiver creates a new packet receiver.
func NewReceiver(cfg ReceiverConfig) (*Receiver, error) {
	if cfg.Source == nil {
		return nil, errors.New("rx: packet source is required")
	}

	ctx, cancel := context.WithCancel(context.Background())

	r := &Receiver{
		iface:     cfg.Interface,
		wlanIdx:   cfg.WlanIdx,
		channelID: cfg.ChannelID,
		source:    cfg.Source,
		handler:   cfg.Handler,
		ctx:       ctx,
		cancel:    cancel,
	}

	return r, nil
}

// Run starts the packet capture loop. Blocks until Close() is called.
func (r *Receiver) Run() error {
	for {
		select {
		case <-r.ctx.Done():
			return nil
		default:
		}

		data, ci, err := r.source.ReadPacket()
		if err != nil {
			// Check if closed
			select {
			case <-r.ctx.Done():
				return nil
			default:
			}

			r.packetsError++
			continue
		}

		r.packetsReceived++

		// Process packet
		if err := r.processPacket(data, ci); err != nil {
			r.packetsError++
		}
	}
}

// processPacket parses a captured packet and extracts the WFB payload.
func (r *Receiver) processPacket(data []byte, ci gopacket.CaptureInfo) error {
	if len(data) < radiotap.MinHeaderSize {
		return errors.New("packet too short for radiotap")
	}

	// Parse radiotap header
	rtHdr, rtLen, err := radiotap.Parse(data)
	if err != nil {
		return err
	}

	// Skip radiotap header
	data = data[rtLen:]

	// Check for bad FCS - drop these packets
	if rtHdr.HasBadFCS() {
		return errors.New("packet has bad FCS")
	}

	// Skip self-injected packets (our own transmissions looped back)
	if rtHdr.IsSelfInjected() {
		return nil
	}

	// Strip FCS if present (4 bytes at end of frame)
	if rtHdr.HasFCS() {
		if len(data) < 4 {
			return errors.New("packet too short to have FCS")
		}
		data = data[:len(data)-4]
	}

	if len(data) < frame.HeaderSize {
		return errors.New("packet too short for 802.11")
	}

	// Parse 802.11 header
	ieee80211Hdr, err := frame.Unmarshal(data)
	if err != nil {
		return err
	}

	// Safety check: verify WFB packet (should be filtered by BPF, but check anyway)
	if !ieee80211Hdr.IsWFBPacket() {
		r.packetsFiltered++
		log.Printf("[RX] WARNING: non-WFB packet passed BPF filter on %s (filtered=%d)", r.iface, r.packetsFiltered)
		return nil
	}

	// Extract payload (after 802.11 header)
	payload := data[frame.HeaderSize:]

	// Safety check: verify channel ID (should be filtered by BPF/demux, but check anyway)
	pktChannelID := ieee80211Hdr.GetChannelID()
	if pktChannelID != r.channelID {
		r.packetsFiltered++
		log.Printf("[RX] WARNING: wrong channel ID passed filter on %s (got=0x%08x want=0x%08x filtered=%d)", r.iface, pktChannelID, r.channelID, r.packetsFiltered)
		return nil
	}

	// Build RX info from radiotap header
	info := &RXInfo{
		WlanIdx:   r.wlanIdx,
		MCSIndex:  uint8(rtHdr.GetMCSIndex()),
		Bandwidth: uint8(rtHdr.Bandwidth()),
	}

	// Copy frequency
	if rtHdr.HasChannel {
		info.Freq = rtHdr.ChannelFreq
	}

	// Initialize antenna arrays with default values
	for i := 0; i < protocol.RX_ANT_MAX; i++ {
		info.Antenna[i] = 0xFF // Unused
		info.RSSI[i] = 0       // 0 = inactive (used as filter in aggregator)
		info.Noise[i] = 0
	}

	// Copy per-antenna info from extended radiotap bitmaps (rtl8812au style)
	if rtHdr.AntennaCount > 0 {
		for i := 0; i < rtHdr.AntennaCount && i < protocol.RX_ANT_MAX; i++ {
			if rtHdr.Antennas[i].Valid {
				info.Antenna[i] = rtHdr.Antennas[i].Antenna
				info.RSSI[i] = rtHdr.Antennas[i].DBMSignal
				info.Noise[i] = rtHdr.Antennas[i].DBMNoise
			}
		}
	} else {
		// Fall back to primary radiotap fields (single antenna)
		if rtHdr.HasDBMSignal {
			info.RSSI[0] = rtHdr.DBMSignal
		}
		if rtHdr.HasAntenna {
			info.Antenna[0] = rtHdr.Antenna
		}
		if rtHdr.HasDBMNoise {
			info.Noise[0] = rtHdr.DBMNoise
		}
	}

	// Call handler
	if r.handler != nil {
		return r.handler(payload, info)
	}

	return nil
}

// Close stops the receiver and releases resources.
func (r *Receiver) Close() error {
	r.cancel()

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.source != nil {
		r.source.Close()
		r.source = nil
	}

	return nil
}

// Stats returns receiver statistics.
func (r *Receiver) Stats() (received, filtered, errors uint64) {
	return r.packetsReceived, r.packetsFiltered, r.packetsError
}

// Forwarder receives packets and forwards them via UDP with RX metadata.
type Forwarder struct {
	receivers      []*Receiver
	agg            *Aggregator
	captureManager *CaptureManager
	ownsManager    bool // true if we created the manager (should close it)
}

// ForwarderConfig holds forwarder configuration.
type ForwarderConfig struct {
	Interfaces     []string        // WiFi interfaces
	ChannelID      uint32          // Channel ID to filter
	KeyData        []byte          // RX key data (64 bytes: secret key + peer public key)
	Epoch          uint64          // Minimum epoch
	OutputFn       OutputFunc
	CaptureManager *CaptureManager // Shared capture manager (nil = create based on CaptureMode)
	CaptureMode    CaptureMode     // Capture mode (used when CaptureManager is nil)
}

// NewForwarder creates a new forwarder that aggregates from multiple interfaces.
func NewForwarder(cfg ForwarderConfig) (*Forwarder, error) {
	// Create aggregator
	aggCfg := AggregatorConfig{
		KeyData:   cfg.KeyData,
		Epoch:     cfg.Epoch,
		ChannelID: cfg.ChannelID,
		OutputFn:  cfg.OutputFn,
	}

	agg, err := NewAggregator(aggCfg)
	if err != nil {
		return nil, err
	}

	// Use provided capture manager or create one based on CaptureMode
	captureMgr := cfg.CaptureManager
	ownsManager := false
	if captureMgr == nil {
		captureMgr = NewCaptureManager(cfg.CaptureMode)
		ownsManager = true
	}

	f := &Forwarder{
		agg:            agg,
		captureManager: captureMgr,
		ownsManager:    ownsManager,
	}

	// Create receivers for each interface
	for i, iface := range cfg.Interfaces {
		// Get packet source from capture manager
		source, err := captureMgr.GetSource(iface, cfg.ChannelID, uint8(i))
		if err != nil {
			// Close already created receivers
			for _, r := range f.receivers {
				r.Close()
			}
			if ownsManager {
				captureMgr.Close()
			}
			return nil, err
		}

		rxCfg := ReceiverConfig{
			Interface: iface,
			WlanIdx:   uint8(i),
			ChannelID: cfg.ChannelID,
			Source:    source,
			Handler: func(payload []byte, info *RXInfo) error {
				// Update antenna stats before processing packet
				agg.UpdateAntennaStats(info)
				return agg.ProcessPacket(payload)
			},
		}

		rx, err := NewReceiver(rxCfg)
		if err != nil {
			// Close already created receivers
			for _, r := range f.receivers {
				r.Close()
			}
			if ownsManager {
				captureMgr.Close()
			}
			return nil, err
		}

		f.receivers = append(f.receivers, rx)
	}

	return f, nil
}

// Run starts all receivers. Blocks until Close() is called.
func (f *Forwarder) Run() error {
	// Start capture manager (needed for shared mode)
	f.captureManager.Start()

	var wg sync.WaitGroup
	errCh := make(chan error, len(f.receivers))

	for _, rx := range f.receivers {
		wg.Add(1)
		go func(r *Receiver) {
			defer wg.Done()
			if err := r.Run(); err != nil {
				errCh <- err
			}
		}(rx)
	}

	wg.Wait()

	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

// Close stops all receivers.
func (f *Forwarder) Close() error {
	for _, rx := range f.receivers {
		rx.Close()
	}
	// Close aggregator's background goroutines
	if f.agg != nil {
		f.agg.Close()
	}
	// Only close capture manager if we created it
	if f.ownsManager && f.captureManager != nil {
		f.captureManager.Close()
	}
	return nil
}

// Stats returns aggregator statistics (lock-free, no antenna stats).
func (f *Forwarder) Stats() Stats {
	return f.agg.Stats()
}

// StatsWithAntennas returns aggregator statistics including antenna data.
func (f *Forwarder) StatsWithAntennas() Stats {
	return f.agg.StatsWithAntennas()
}

// MarshalRXForwardPacket builds a forwarded packet with RX metadata header.
func MarshalRXForwardPacket(payload []byte, info *RXInfo) []byte {
	hdr := protocol.RXForwardHeader{
		WlanIdx:   info.WlanIdx,
		Antenna:   info.Antenna,
		RSSI:      info.RSSI,
		Noise:     info.Noise,
		Freq:      info.Freq,
		MCSIndex:  info.MCSIndex,
		Bandwidth: info.Bandwidth,
	}

	pkt := make([]byte, protocol.RXForwardHeaderSize+len(payload))
	copy(pkt[:protocol.RXForwardHeaderSize], hdr.Marshal())
	copy(pkt[protocol.RXForwardHeaderSize:], payload)

	return pkt
}

// UnmarshalRXForwardPacket extracts payload and RX info from a forwarded packet.
func UnmarshalRXForwardPacket(data []byte) ([]byte, *RXInfo, error) {
	if len(data) < protocol.RXForwardHeaderSize {
		return nil, nil, errors.New("packet too short")
	}

	hdr, err := protocol.UnmarshalRXForwardHeader(data)
	if err != nil {
		return nil, nil, err
	}

	info := &RXInfo{
		WlanIdx:   hdr.WlanIdx,
		Antenna:   hdr.Antenna,
		RSSI:      hdr.RSSI,
		Noise:     hdr.Noise,
		Freq:      hdr.Freq,
		MCSIndex:  hdr.MCSIndex,
		Bandwidth: hdr.Bandwidth,
	}

	payload := data[protocol.RXForwardHeaderSize:]

	return payload, info, nil
}

// Helper to build channel ID from raw bytes
func parseChannelIDFromMAC(mac []byte) uint32 {
	if len(mac) < 6 {
		return 0
	}
	return binary.BigEndian.Uint32(mac[2:6])
}
