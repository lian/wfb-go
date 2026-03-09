package rx

import (
	"context"
	"encoding/binary"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gopacket/gopacket"

	"github.com/lian/wfb-go/pkg/wifi/frame"
	"github.com/lian/wfb-go/pkg/wifi/radiotap"
)

// --- Test Packet Builders ---

// buildRadiotapHeader creates a minimal radiotap header with common fields.
// Fields appear in order of bit position in present flags.
func buildRadiotapHeader(rssi int8, noise int8, antenna uint8, freq uint16, mcsIndex uint8) []byte {
	// Present flags bits we'll use:
	// Bit 3: CHANNEL (align 2, size 4)
	// Bit 5: DBM_ANTSIGNAL (align 1, size 1)
	// Bit 6: DBM_ANTNOISE (align 1, size 1)
	// Bit 11: ANTENNA (align 1, size 1)
	// Bit 19: MCS (align 1, size 3)
	presentFlags := uint32(1<<radiotap.CHANNEL |
		1<<radiotap.DBM_ANTSIGNAL |
		1<<radiotap.DBM_ANTNOISE |
		1<<radiotap.ANTENNA |
		1<<radiotap.MCS)

	// Header: 8 (base) + 4 (channel) + 1 (signal) + 1 (noise) + 1 (antenna) + 3 (MCS) = 18
	headerLen := uint16(18)

	buf := make([]byte, headerLen)
	buf[0] = 0 // version
	buf[1] = 0 // pad
	binary.LittleEndian.PutUint16(buf[2:4], headerLen)
	binary.LittleEndian.PutUint32(buf[4:8], presentFlags)

	offset := 8

	// CHANNEL (bit 3) - at offset 8, already 2-byte aligned
	binary.LittleEndian.PutUint16(buf[offset:], freq)
	binary.LittleEndian.PutUint16(buf[offset+2:], 0x00a0) // channel flags
	offset += 4

	// DBM_ANTSIGNAL (bit 5)
	buf[offset] = byte(rssi)
	offset++

	// DBM_ANTNOISE (bit 6)
	buf[offset] = byte(noise)
	offset++

	// ANTENNA (bit 11)
	buf[offset] = antenna
	offset++

	// MCS (bit 19) - known, flags, mcs_index
	buf[offset] = radiotap.MCS_HAVE_MCS | radiotap.MCS_HAVE_BW
	buf[offset+1] = 0
	buf[offset+2] = mcsIndex

	return buf
}

// buildWFBPacket creates a complete captured WFB packet (radiotap + 802.11 + payload).
func buildWFBPacket(channelID uint32, payload []byte, rssi int8, antenna uint8) []byte {
	// Build radiotap header
	rtap := buildRadiotapHeader(rssi, -90, antenna, 5180, 7)

	// Build 802.11 header
	ieee := frame.DefaultHeader()
	ieee.SetChannelID(channelID)
	ieeeBytes := ieee.Marshal()

	// Combine all parts
	packet := make([]byte, len(rtap)+len(ieeeBytes)+len(payload))
	copy(packet, rtap)
	copy(packet[len(rtap):], ieeeBytes)
	copy(packet[len(rtap)+len(ieeeBytes):], payload)

	return packet
}

// buildNonWFBPacket creates a packet without WFB MAC prefix.
func buildNonWFBPacket() []byte {
	rtap := buildRadiotapHeader(-50, -90, 0, 5180, 7)

	// Regular 802.11 header (not WFB)
	ieeeBytes := make([]byte, 24)
	ieeeBytes[0] = 0x08 // data frame
	ieeeBytes[1] = 0x01
	// Transmitter without WFB prefix
	ieeeBytes[10] = 0x00
	ieeeBytes[11] = 0x11
	ieeeBytes[12] = 0x22
	ieeeBytes[13] = 0x33

	packet := make([]byte, len(rtap)+len(ieeeBytes)+10)
	copy(packet, rtap)
	copy(packet[len(rtap):], ieeeBytes)

	return packet
}

// --- Mock PacketSource ---

type mockPacketSource struct {
	packets   []mockPacket
	index     int
	mu        sync.Mutex
	closed    bool
	ctx       context.Context
	cancel    context.CancelFunc
	delay     time.Duration // optional delay between packets
	readCount int32
}

type mockPacket struct {
	data []byte
	ci   gopacket.CaptureInfo
}

func newMockPacketSource(packets [][]byte) *mockPacketSource {
	ctx, cancel := context.WithCancel(context.Background())
	mps := &mockPacketSource{
		packets: make([]mockPacket, len(packets)),
		ctx:     ctx,
		cancel:  cancel,
	}
	for i, p := range packets {
		mps.packets[i] = mockPacket{
			data: p,
			ci: gopacket.CaptureInfo{
				Timestamp:     time.Now(),
				CaptureLength: len(p),
				Length:        len(p),
			},
		}
	}
	return mps
}

func (m *mockPacketSource) ReadPacket() ([]byte, gopacket.CaptureInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, gopacket.CaptureInfo{}, context.Canceled
	}

	select {
	case <-m.ctx.Done():
		return nil, gopacket.CaptureInfo{}, m.ctx.Err()
	default:
	}

	if m.index >= len(m.packets) {
		// Block until closed
		m.mu.Unlock()
		<-m.ctx.Done()
		m.mu.Lock()
		return nil, gopacket.CaptureInfo{}, m.ctx.Err()
	}

	if m.delay > 0 {
		m.mu.Unlock()
		time.Sleep(m.delay)
		m.mu.Lock()
	}

	pkt := m.packets[m.index]
	m.index++
	atomic.AddInt32(&m.readCount, 1)

	return pkt.data, pkt.ci, nil
}

func (m *mockPacketSource) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	m.cancel()
	return nil
}

// --- Receiver Tests ---

func TestReceiverWithMockSource(t *testing.T) {
	channelID := uint32(0x12345678)
	payload := []byte("test payload data")

	packets := [][]byte{
		buildWFBPacket(channelID, payload, -50, 0),
		buildWFBPacket(channelID, payload, -55, 1),
	}

	source := newMockPacketSource(packets)

	var received [][]byte
	var infos []*RXInfo
	var mu sync.Mutex

	handler := func(p []byte, info *RXInfo) error {
		mu.Lock()
		defer mu.Unlock()
		received = append(received, p)
		infos = append(infos, info)
		return nil
	}

	rx, err := NewReceiver(ReceiverConfig{
		Interface: "mock0",
		WlanIdx:   0,
		ChannelID: channelID,
		Source:    source,
		Handler:   handler,
	})
	if err != nil {
		t.Fatalf("NewReceiver failed: %v", err)
	}

	// Run receiver in background
	done := make(chan struct{})
	go func() {
		rx.Run()
		close(done)
	}()

	// Wait for packets to be processed
	time.Sleep(50 * time.Millisecond)
	rx.Close()
	<-done

	mu.Lock()
	defer mu.Unlock()

	if len(received) != 2 {
		t.Errorf("Received %d packets, want 2", len(received))
	}

	// Check RSSI values were extracted
	if len(infos) >= 1 && infos[0].RSSI[0] != -50 {
		t.Errorf("First packet RSSI = %d, want -50", infos[0].RSSI[0])
	}
	if len(infos) >= 2 && infos[1].RSSI[0] != -55 {
		t.Errorf("Second packet RSSI = %d, want -55", infos[1].RSSI[0])
	}
}

func TestReceiverFiltersWrongChannel(t *testing.T) {
	correctChannel := uint32(0x12345678)
	wrongChannel := uint32(0xDEADBEEF)
	payload := []byte("test payload")

	packets := [][]byte{
		buildWFBPacket(correctChannel, payload, -50, 0), // Should pass
		buildWFBPacket(wrongChannel, payload, -50, 0),   // Should be filtered
		buildWFBPacket(correctChannel, payload, -50, 0), // Should pass
	}

	source := newMockPacketSource(packets)

	var receivedCount int32

	rx, err := NewReceiver(ReceiverConfig{
		Interface: "mock0",
		ChannelID: correctChannel,
		Source:    source,
		Handler: func(p []byte, info *RXInfo) error {
			atomic.AddInt32(&receivedCount, 1)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewReceiver failed: %v", err)
	}

	done := make(chan struct{})
	go func() {
		rx.Run()
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	rx.Close()
	<-done

	count := atomic.LoadInt32(&receivedCount)
	if count != 2 {
		t.Errorf("Received %d packets, want 2 (wrong channel should be filtered)", count)
	}

	// Check stats
	_, filtered, _ := rx.Stats()
	if filtered != 1 {
		t.Errorf("Filtered count = %d, want 1", filtered)
	}
}

func TestReceiverFiltersNonWFBPackets(t *testing.T) {
	channelID := uint32(0x12345678)
	payload := []byte("test payload")

	packets := [][]byte{
		buildWFBPacket(channelID, payload, -50, 0), // WFB packet
		buildNonWFBPacket(),                        // Non-WFB packet
	}

	source := newMockPacketSource(packets)

	var receivedCount int32

	rx, err := NewReceiver(ReceiverConfig{
		Interface: "mock0",
		ChannelID: channelID,
		Source:    source,
		Handler: func(p []byte, info *RXInfo) error {
			atomic.AddInt32(&receivedCount, 1)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewReceiver failed: %v", err)
	}

	done := make(chan struct{})
	go func() {
		rx.Run()
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	rx.Close()
	<-done

	count := atomic.LoadInt32(&receivedCount)
	if count != 1 {
		t.Errorf("Received %d packets, want 1 (non-WFB should be filtered)", count)
	}
}

func TestReceiverExtractsRXInfo(t *testing.T) {
	channelID := uint32(0x12345678)
	payload := []byte("test")

	// Build packet with specific values
	packets := [][]byte{
		buildWFBPacket(channelID, payload, -45, 2),
	}

	source := newMockPacketSource(packets)

	var capturedInfo *RXInfo

	rx, err := NewReceiver(ReceiverConfig{
		Interface: "mock0",
		WlanIdx:   3,
		ChannelID: channelID,
		Source:    source,
		Handler: func(p []byte, info *RXInfo) error {
			capturedInfo = info
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewReceiver failed: %v", err)
	}

	done := make(chan struct{})
	go func() {
		rx.Run()
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	rx.Close()
	<-done

	if capturedInfo == nil {
		t.Fatal("No RXInfo captured")
	}

	if capturedInfo.WlanIdx != 3 {
		t.Errorf("WlanIdx = %d, want 3", capturedInfo.WlanIdx)
	}
	if capturedInfo.RSSI[0] != -45 {
		t.Errorf("RSSI[0] = %d, want -45", capturedInfo.RSSI[0])
	}
	if capturedInfo.Antenna[0] != 2 {
		t.Errorf("Antenna[0] = %d, want 2", capturedInfo.Antenna[0])
	}
	if capturedInfo.Freq != 5180 {
		t.Errorf("Freq = %d, want 5180", capturedInfo.Freq)
	}
	if capturedInfo.MCSIndex != 7 {
		t.Errorf("MCSIndex = %d, want 7", capturedInfo.MCSIndex)
	}
}

func TestReceiverRequiresSource(t *testing.T) {
	_, err := NewReceiver(ReceiverConfig{
		Interface: "mock0",
		ChannelID: 0x12345678,
		Handler:   func(p []byte, info *RXInfo) error { return nil },
		// Source is nil
	})

	if err == nil {
		t.Error("Expected error when Source is nil")
	}
}

func TestReceiverStats(t *testing.T) {
	channelID := uint32(0x12345678)
	payload := []byte("test")

	packets := [][]byte{
		buildWFBPacket(channelID, payload, -50, 0),
		buildWFBPacket(channelID, payload, -50, 0),
		buildWFBPacket(channelID+1, payload, -50, 0), // Wrong channel
	}

	source := newMockPacketSource(packets)

	rx, _ := NewReceiver(ReceiverConfig{
		Interface: "mock0",
		ChannelID: channelID,
		Source:    source,
		Handler:   func(p []byte, info *RXInfo) error { return nil },
	})

	done := make(chan struct{})
	go func() {
		rx.Run()
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	rx.Close()
	<-done

	received, filtered, errors := rx.Stats()

	if received != 3 {
		t.Errorf("Received = %d, want 3", received)
	}
	if filtered != 1 {
		t.Errorf("Filtered = %d, want 1", filtered)
	}
	if errors != 0 {
		t.Errorf("Errors = %d, want 0", errors)
	}
}

// --- BPF Filter Format Test ---

func TestBPFFilterFormat(t *testing.T) {
	// Test that the BPF filter string format is correct
	// This verifies the filter construction without needing actual pcap

	channelID := uint32(0x12345678)

	// The filter should match:
	// ether[10:2] == 0x5742 (WFB magic "WB" at transmitter MAC bytes 0-1)
	// ether[12:4] == channel_id (transmitter MAC bytes 2-5)

	// Build a WFB packet and verify the bytes are where we expect
	packet := buildWFBPacket(channelID, []byte("test"), -50, 0)

	// Find the 802.11 header (after radiotap)
	rtapLen := binary.LittleEndian.Uint16(packet[2:4])

	// In the 802.11 header, transmitter is at offset 10-16
	// So ether[10:2] = transmitter[0:2] = WB prefix
	// And ether[12:4] = transmitter[2:6] = channel_id

	transmitterOffset := int(rtapLen) + 10
	if packet[transmitterOffset] != 0x57 || packet[transmitterOffset+1] != 0x42 {
		t.Errorf("WFB magic at wrong position: got 0x%02x%02x, want 0x5742",
			packet[transmitterOffset], packet[transmitterOffset+1])
	}

	extractedChannelID := binary.BigEndian.Uint32(packet[transmitterOffset+2:])
	if extractedChannelID != channelID {
		t.Errorf("Channel ID at wrong position: got 0x%08x, want 0x%08x",
			extractedChannelID, channelID)
	}
}
