package rx

import (
	"context"
	"testing"
)

func TestNewCaptureManager(t *testing.T) {
	// Dedicated mode
	cm := NewCaptureManager(CaptureModeDedicated)
	if cm.Mode() != CaptureModeDedicated {
		t.Errorf("Mode = %v, want CaptureModeDedicated", cm.Mode())
	}

	// Shared mode
	cm2 := NewCaptureManager(CaptureModeShared)
	if cm2.Mode() != CaptureModeShared {
		t.Errorf("Mode = %v, want CaptureModeShared", cm2.Mode())
	}
}

func TestSharedCaptureExtractChannelID(t *testing.T) {
	// Create a minimal SharedCapture to test extractChannelID
	sc := &SharedCapture{}

	// Build a test packet:
	// - Radiotap header (minimal): 8 bytes
	//   - version: 0
	//   - pad: 0
	//   - length: 8 (little endian)
	//   - present flags: 0
	// - 802.11 header: 24 bytes
	//   - frame control: 2 bytes
	//   - duration: 2 bytes
	//   - addr1: 6 bytes
	//   - addr2: 6 bytes (channel ID is in bytes 2-6 of addr2)
	//   - addr3: 6 bytes
	//   - seq: 2 bytes

	packet := make([]byte, 8+24)

	// Radiotap header
	packet[0] = 0    // version
	packet[1] = 0    // pad
	packet[2] = 8    // length low byte
	packet[3] = 0    // length high byte
	packet[4] = 0    // present flags
	packet[5] = 0
	packet[6] = 0
	packet[7] = 0

	// 802.11 header - channel ID is at offset 12-16 from start of 802.11
	// (after radiotap, which ends at byte 8)
	// So channel ID bytes are at packet[8+12] through packet[8+15]
	channelIDOffset := 8 + 12
	packet[channelIDOffset] = 0x12
	packet[channelIDOffset+1] = 0x34
	packet[channelIDOffset+2] = 0x56
	packet[channelIDOffset+3] = 0x78

	channelID := sc.extractChannelID(packet)
	expected := uint32(0x12345678)
	if channelID != expected {
		t.Errorf("extractChannelID = 0x%08x, want 0x%08x", channelID, expected)
	}
}

func TestSharedCaptureExtractChannelIDTooShort(t *testing.T) {
	sc := &SharedCapture{}

	// Too short packet
	short := make([]byte, 3)
	channelID := sc.extractChannelID(short)
	if channelID != 0 {
		t.Errorf("extractChannelID(short) = 0x%08x, want 0", channelID)
	}

	// Packet with radiotap but no room for 802.11
	noRoom := make([]byte, 10)
	noRoom[2] = 8 // radiotap length
	noRoom[3] = 0
	channelID = sc.extractChannelID(noRoom)
	if channelID != 0 {
		t.Errorf("extractChannelID(noRoom) = 0x%08x, want 0", channelID)
	}
}

func TestCaptureModeConstants(t *testing.T) {
	// Verify the constants have distinct values
	if CaptureModeDedicated == CaptureModeShared {
		t.Error("CaptureModeDedicated and CaptureModeShared should have different values")
	}
}

func TestCaptureModeString(t *testing.T) {
	tests := []struct {
		mode CaptureMode
		want string
	}{
		{CaptureModeDedicated, "dedicated"},
		{CaptureModeShared, "shared"},
		{CaptureModeLibpcap, "libpcap"},
		{CaptureMode(99), "unknown(99)"},
	}

	for _, tt := range tests {
		got := tt.mode.String()
		if got != tt.want {
			t.Errorf("CaptureMode(%d).String() = %q, want %q", int(tt.mode), got, tt.want)
		}
	}
}

func TestSharedConsumer(t *testing.T) {
	// Test sharedConsumer channel operations
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	consumer := &sharedConsumer{
		channelID: 0x12345678,
		wlanIdx:   0,
		packets:   make(chan capturedPacket, 2),
		ctx:       ctx,
	}

	// Send a packet
	testData := []byte{1, 2, 3, 4}
	consumer.packets <- capturedPacket{data: testData}

	// Receive it
	data, _, err := consumer.ReadPacket()
	if err != nil {
		t.Fatalf("ReadPacket failed: %v", err)
	}
	if len(data) != len(testData) {
		t.Errorf("Data length = %d, want %d", len(data), len(testData))
	}

	// Close the consumer
	if err := consumer.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

func TestSharedConsumerContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	consumer := &sharedConsumer{
		channelID: 0x12345678,
		wlanIdx:   0,
		packets:   make(chan capturedPacket, 2),
		ctx:       ctx,
	}

	// Cancel context
	cancel()

	// ReadPacket should return error
	_, _, err := consumer.ReadPacket()
	if err == nil {
		t.Error("Expected error when context canceled")
	}
}

func TestSharedCaptureAddConsumer(t *testing.T) {
	// Create SharedCapture without actual pcap handle (just test consumer management)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sc := &SharedCapture{
		iface:     "test0",
		consumers: make(map[uint32]*sharedConsumer),
		ctx:       ctx,
		cancel:    cancel,
	}

	// Add first consumer
	c1 := sc.AddConsumer(0x11111111, 0)
	if c1 == nil {
		t.Fatal("AddConsumer returned nil")
	}
	if c1.channelID != 0x11111111 {
		t.Errorf("Consumer channelID = 0x%08x, want 0x11111111", c1.channelID)
	}

	// Add second consumer for different channel
	c2 := sc.AddConsumer(0x22222222, 1)
	if c2 == nil {
		t.Fatal("AddConsumer returned nil for second consumer")
	}

	// Adding same channel should return same consumer
	c1again := sc.AddConsumer(0x11111111, 0)
	if c1again != c1 {
		t.Error("AddConsumer should return existing consumer for same channel")
	}

	// Verify both consumers registered
	if len(sc.consumers) != 2 {
		t.Errorf("Consumer count = %d, want 2", len(sc.consumers))
	}
}

func TestSharedCaptureDispatchToCorrectConsumer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sc := &SharedCapture{
		iface:     "test0",
		consumers: make(map[uint32]*sharedConsumer),
		ctx:       ctx,
		cancel:    cancel,
	}

	// Add consumers for different channels
	c1 := sc.AddConsumer(0x11111111, 0)
	c2 := sc.AddConsumer(0x22222222, 1)

	// Build test packets with channel IDs embedded
	// Packet format: radiotap (8 min) + 802.11 header (24)
	// Channel ID is at offset radiotap_len + 12 (in transmitter MAC bytes 2-5)

	makePacket := func(channelID uint32) []byte {
		packet := make([]byte, 8+24+10) // radiotap + 802.11 + payload
		// Radiotap header
		packet[0] = 0                                  // version
		packet[2] = 8                                  // length low
		packet[3] = 0                                  // length high
		// 802.11 header starts at offset 8
		// Channel ID at offset 8+12 = 20
		packet[20] = byte(channelID >> 24)
		packet[21] = byte(channelID >> 16)
		packet[22] = byte(channelID >> 8)
		packet[23] = byte(channelID)
		return packet
	}

	packet1 := makePacket(0x11111111)
	packet2 := makePacket(0x22222222)
	packet3 := makePacket(0x33333333) // No consumer for this

	// Simulate dispatch (what Run() would do)
	dispatch := func(data []byte) {
		channelID := sc.extractChannelID(data)
		sc.mu.RLock()
		consumer, ok := sc.consumers[channelID]
		sc.mu.RUnlock()
		if ok {
			select {
			case consumer.packets <- capturedPacket{data: data}:
			default:
			}
		}
	}

	dispatch(packet1)
	dispatch(packet2)
	dispatch(packet3) // Should be dropped

	// Verify c1 got packet1
	select {
	case pkt := <-c1.packets:
		if sc.extractChannelID(pkt.data) != 0x11111111 {
			t.Error("c1 received wrong packet")
		}
	default:
		t.Error("c1 should have received a packet")
	}

	// Verify c2 got packet2
	select {
	case pkt := <-c2.packets:
		if sc.extractChannelID(pkt.data) != 0x22222222 {
			t.Error("c2 received wrong packet")
		}
	default:
		t.Error("c2 should have received a packet")
	}

	// Verify no extra packets
	select {
	case <-c1.packets:
		t.Error("c1 should not have extra packets")
	default:
	}

	select {
	case <-c2.packets:
		t.Error("c2 should not have extra packets")
	default:
	}
}

func TestSharedCaptureBufferOverflow(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sc := &SharedCapture{
		iface:     "test0",
		consumers: make(map[uint32]*sharedConsumer),
		ctx:       ctx,
		cancel:    cancel,
	}

	// Add consumer with small buffer
	consumer := &sharedConsumer{
		channelID: 0x11111111,
		wlanIdx:   0,
		packets:   make(chan capturedPacket, 2), // Only 2 slots
		ctx:       ctx,
	}
	sc.consumers[0x11111111] = consumer

	makePacket := func() []byte {
		packet := make([]byte, 8+24)
		packet[2] = 8 // radiotap length
		packet[20] = 0x11
		packet[21] = 0x11
		packet[22] = 0x11
		packet[23] = 0x11
		return packet
	}

	// Dispatch helper (non-blocking)
	dispatch := func(data []byte) bool {
		channelID := sc.extractChannelID(data)
		sc.mu.RLock()
		c, ok := sc.consumers[channelID]
		sc.mu.RUnlock()
		if ok {
			select {
			case c.packets <- capturedPacket{data: data}:
				return true
			default:
				return false // Dropped
			}
		}
		return false
	}

	// Fill the buffer
	if !dispatch(makePacket()) {
		t.Error("First packet should be accepted")
	}
	if !dispatch(makePacket()) {
		t.Error("Second packet should be accepted")
	}

	// Buffer full - should drop
	if dispatch(makePacket()) {
		t.Error("Third packet should be dropped (buffer full)")
	}

	// Drain one
	<-consumer.packets

	// Now should accept again
	if !dispatch(makePacket()) {
		t.Error("Should accept after draining")
	}
}

func TestCaptureManagerGetSourceDedicated(t *testing.T) {
	// Can't test actual pcap opening, but can test the manager logic
	cm := NewCaptureManager(CaptureModeDedicated)

	if cm.Mode() != CaptureModeDedicated {
		t.Errorf("Mode = %v, want CaptureModeDedicated", cm.Mode())
	}

	// In dedicated mode, shared map should remain empty
	if len(cm.shared) != 0 {
		t.Error("Shared map should be empty in dedicated mode")
	}
}

func TestCaptureManagerGetSourceShared(t *testing.T) {
	cm := NewCaptureManager(CaptureModeShared)

	if cm.Mode() != CaptureModeShared {
		t.Errorf("Mode = %v, want CaptureModeShared", cm.Mode())
	}
}

func TestBuildWFBMagicFilter(t *testing.T) {
	filter := BuildWFBMagicFilter()

	// Should have 10 instructions (handles little-endian radiotap length)
	if len(filter) != 10 {
		t.Fatalf("Filter length = %d, want 10", len(filter))
	}

	// Verify structure (little-endian radiotap length conversion):
	// 0: ldb [3] - load high byte of radiotap length
	// 1: lsh #8 - shift left 8
	// 2: tax - X = high << 8
	// 3: ldb [2] - load low byte
	// 4: add x - A = low + (high << 8)
	// 5: tax - X = radiotap length (LE16)
	// 6: ldh [x+10] - load WFB magic
	// 7: jeq #0x5742, accept, drop
	// 8: ret #-1 (accept)
	// 9: ret #0 (drop)

	// Check ldb [3] (high byte)
	if filter[0].Op != 0x30 || filter[0].K != 3 {
		t.Errorf("Instruction 0: got op=0x%x k=%d, want op=0x30 k=3", filter[0].Op, filter[0].K)
	}

	// Check lsh #8
	if filter[1].Op != 0x64 || filter[1].K != 8 {
		t.Errorf("Instruction 1: got op=0x%x k=%d, want op=0x64 k=8", filter[1].Op, filter[1].K)
	}

	// Check ldh [x+10] (indexed load for WFB magic)
	if filter[6].Op != 0x48 || filter[6].K != 10 {
		t.Errorf("Instruction 6: got op=0x%x k=%d, want op=0x48 k=10", filter[6].Op, filter[6].K)
	}

	// Check jeq #0x5742 with correct jump offset
	if filter[7].Op != 0x15 || filter[7].K != 0x5742 || filter[7].Jf != 1 {
		t.Errorf("Instruction 7: got op=0x%x k=0x%x jf=%d, want op=0x15 k=0x5742 jf=1",
			filter[7].Op, filter[7].K, filter[7].Jf)
	}

	// Check ret #-1 (accept)
	if filter[8].Op != 0x06 || filter[8].K != 0xFFFFFFFF {
		t.Errorf("Instruction 8: got op=0x%x k=0x%x, want op=0x06 k=0xFFFFFFFF", filter[8].Op, filter[8].K)
	}

	// Check ret #0 (reject)
	if filter[9].Op != 0x06 || filter[9].K != 0 {
		t.Errorf("Instruction 9: got op=0x%x k=0x%x, want op=0x06 k=0", filter[9].Op, filter[9].K)
	}
}

func TestBuildWFBFilter(t *testing.T) {
	channelID := uint32(0x12345678)
	filter := BuildWFBFilter(channelID)

	// Should have 12 instructions (handles little-endian radiotap length)
	if len(filter) != 12 {
		t.Fatalf("Filter length = %d, want 12", len(filter))
	}

	// Verify structure (little-endian radiotap length conversion):
	// 0: ldb [3] - load high byte of radiotap length
	// 1: lsh #8 - shift left 8
	// 2: tax - X = high << 8
	// 3: ldb [2] - load low byte
	// 4: add x - A = low + (high << 8)
	// 5: tax - X = radiotap length (LE16)
	// 6: ldh [x+10] - load WFB magic
	// 7: jeq #0x5742, next, drop
	// 8: ld [x+12] - load channel ID
	// 9: jeq #channelID, accept, drop
	// 10: ret #-1 (accept)
	// 11: ret #0 (drop)

	// Check ldb [3] (high byte)
	if filter[0].Op != 0x30 || filter[0].K != 3 {
		t.Errorf("Instruction 0: got op=0x%x k=%d, want op=0x30 k=3", filter[0].Op, filter[0].K)
	}

	// Check lsh #8
	if filter[1].Op != 0x64 || filter[1].K != 8 {
		t.Errorf("Instruction 1: got op=0x%x k=%d, want op=0x64 k=8", filter[1].Op, filter[1].K)
	}

	// Check ldh [x+10] (indexed load for magic)
	if filter[6].Op != 0x48 || filter[6].K != 10 {
		t.Errorf("Instruction 6: got op=0x%x k=%d, want op=0x48 k=10", filter[6].Op, filter[6].K)
	}

	// Check jeq #0x5742 with correct jump offset
	if filter[7].Op != 0x15 || filter[7].K != 0x5742 || filter[7].Jf != 3 {
		t.Errorf("Instruction 7: got op=0x%x k=0x%x jf=%d, want op=0x15 k=0x5742 jf=3",
			filter[7].Op, filter[7].K, filter[7].Jf)
	}

	// Check ld [x+12] (indexed load for channel ID)
	if filter[8].Op != 0x40 || filter[8].K != 12 {
		t.Errorf("Instruction 8: got op=0x%x k=%d, want op=0x40 k=12", filter[8].Op, filter[8].K)
	}

	// Check jeq #channelID
	if filter[9].Op != 0x15 || filter[9].K != channelID || filter[9].Jf != 1 {
		t.Errorf("Instruction 9: got op=0x%x k=0x%x jf=%d, want op=0x15 k=0x%x jf=1",
			filter[9].Op, filter[9].K, filter[9].Jf, channelID)
	}

	// Check ret #-1 (accept)
	if filter[10].Op != 0x06 || filter[10].K != 0xFFFFFFFF {
		t.Errorf("Instruction 10: got op=0x%x k=0x%x, want op=0x06 k=0xFFFFFFFF", filter[10].Op, filter[10].K)
	}

	// Check ret #0 (reject)
	if filter[11].Op != 0x06 || filter[11].K != 0 {
		t.Errorf("Instruction 11: got op=0x%x k=0x%x, want op=0x06 k=0", filter[11].Op, filter[11].K)
	}
}
