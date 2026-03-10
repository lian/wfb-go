package rx

import (
	"sync"
	"testing"

	"github.com/lian/wfb-go/pkg/crypto"
	"github.com/lian/wfb-go/pkg/protocol"
	"github.com/lian/wfb-go/pkg/tx"
)

// TestAggregatorWithTransmitter tests the full TX->RX cycle.
func TestAggregatorWithTransmitter(t *testing.T) {
	// Generate keypairs
	droneKey, gsKey, err := crypto.GenerateWFBKeys()
	if err != nil {
		t.Fatalf("GenerateWFBKeys failed: %v", err)
	}

	// Create transmitter
	injector := tx.NewBufferInjector()
	channelID := protocol.MakeChannelID(0x010203, 0)

	txCfg := tx.Config{
		FecK:      4,
		FecN:      6,
		Epoch:     1000,
		ChannelID: channelID,
		KeyData:   droneKey,
	}

	transmitter, err := tx.New(txCfg, injector)
	if err != nil {
		t.Fatalf("tx.New failed: %v", err)
	}

	// Collect received packets
	var received [][]byte
	var mu sync.Mutex

	outputFn := func(data []byte) error {
		mu.Lock()
		defer mu.Unlock()
		cp := make([]byte, len(data))
		copy(cp, data)
		received = append(received, cp)
		return nil
	}

	// Create aggregator
	aggCfg := AggregatorConfig{
		KeyData:   gsKey,
		Epoch:     0,
		ChannelID: channelID,
		OutputFn:  outputFn,
	}

	agg, err := NewAggregator(aggCfg)
	if err != nil {
		t.Fatalf("NewAggregator failed: %v", err)
	}

	// Send session key
	if err := transmitter.SendSessionKey(); err != nil {
		t.Fatalf("SendSessionKey failed: %v", err)
	}

	// Process session packet
	sessionPkt := injector.Packets[0]
	if err := agg.ProcessPacket(sessionPkt); err != nil {
		t.Fatalf("ProcessPacket (session) failed: %v", err)
	}

	if !agg.HasSession() {
		t.Error("Aggregator should have session after processing session packet")
	}

	injector.Clear()

	// Send some data packets
	testData := [][]byte{
		[]byte("Hello, World!"),
		[]byte("This is packet 2"),
		[]byte("And packet 3"),
		[]byte("Final packet 4"),
	}

	for _, data := range testData {
		if _, err := transmitter.SendPacket(data); err != nil {
			t.Fatalf("SendPacket failed: %v", err)
		}
	}

	// Process all TX packets through aggregator
	for _, pkt := range injector.Packets {
		if err := agg.ProcessPacket(pkt); err != nil {
			t.Errorf("ProcessPacket failed: %v", err)
		}
	}

	// Flush pending async operations
	agg.Flush()

	// Verify received data
	mu.Lock()
	defer mu.Unlock()

	if len(received) != len(testData) {
		t.Errorf("Received %d packets, want %d", len(received), len(testData))
	}

	for i, want := range testData {
		if i >= len(received) {
			break
		}
		if string(received[i]) != string(want) {
			t.Errorf("Packet %d: got %q, want %q", i, received[i], want)
		}
	}
}

func TestAggregatorNoSession(t *testing.T) {
	_, gsKey, _ := crypto.GenerateWFBKeys()

	agg, err := NewAggregator(AggregatorConfig{
		KeyData:   gsKey,
		ChannelID: 0x01020300,
		OutputFn:  func(data []byte) error { return nil },
	})
	if err != nil {
		t.Fatalf("NewAggregator failed: %v", err)
	}

	// Create a fake data packet
	dataPacket := make([]byte, 100)
	dataPacket[0] = protocol.WFB_PACKET_DATA

	// Should fail with no session
	err = agg.ProcessPacket(dataPacket)
	if err != ErrNoSession {
		t.Errorf("Expected ErrNoSession, got %v", err)
	}
}

func TestAggregatorDuplicateSession(t *testing.T) {
	droneKey, gsKey, _ := crypto.GenerateWFBKeys()

	injector := tx.NewBufferInjector()
	channelID := protocol.MakeChannelID(0x010203, 0)

	transmitter, _ := tx.New(tx.Config{
		FecK:      4,
		FecN:      6,
		Epoch:     1000,
		ChannelID: channelID,
		KeyData:   droneKey,
	}, injector)

	agg, _ := NewAggregator(AggregatorConfig{
		KeyData:   gsKey,
		ChannelID: channelID,
		OutputFn:  func(data []byte) error { return nil },
	})

	// Send session key
	transmitter.SendSessionKey()
	sessionPkt := injector.Packets[0]

	// First session packet
	agg.ProcessPacket(sessionPkt)
	stats1 := agg.Stats()

	// Same session packet again (duplicate)
	agg.ProcessPacket(sessionPkt)
	stats2 := agg.Stats()

	if stats2.PacketsSession != stats1.PacketsSession+1 {
		t.Error("Duplicate session should increment PacketsSession counter")
	}

	if stats2.PacketsDecErr != stats1.PacketsDecErr {
		t.Error("Duplicate session should not increment PacketsDecErr")
	}
}

func TestAggregatorInvalidPacket(t *testing.T) {
	_, gsKey, _ := crypto.GenerateWFBKeys()

	agg, _ := NewAggregator(AggregatorConfig{
		KeyData:   gsKey,
		ChannelID: 0x01020300,
		OutputFn:  func(data []byte) error { return nil },
	})

	// Empty packet (returns error but doesn't increment PacketsBad)
	err := agg.ProcessPacket([]byte{})
	if err != ErrInvalidPacket {
		t.Errorf("Empty packet: got %v, want ErrInvalidPacket", err)
	}

	// Unknown packet type (increments PacketsBad)
	err = agg.ProcessPacket([]byte{0xFF, 0x00, 0x00})
	if err != ErrInvalidPacket {
		t.Errorf("Unknown type: got %v, want ErrInvalidPacket", err)
	}

	stats := agg.Stats()
	// Only unknown type increments PacketsBad, not empty packets
	if stats.PacketsBad != 1 {
		t.Errorf("PacketsBad = %d, want 1", stats.PacketsBad)
	}
	if stats.PacketsAll != 2 {
		t.Errorf("PacketsAll = %d, want 2", stats.PacketsAll)
	}
}

func TestAggregatorWrongChannel(t *testing.T) {
	droneKey, gsKey, _ := crypto.GenerateWFBKeys()

	injector := tx.NewBufferInjector()

	// Transmitter uses channel 0x010203
	transmitter, _ := tx.New(tx.Config{
		FecK:      4,
		FecN:      6,
		Epoch:     1000,
		ChannelID: protocol.MakeChannelID(0x010203, 0),
		KeyData:   droneKey,
	}, injector)

	// Aggregator expects different channel
	agg, _ := NewAggregator(AggregatorConfig{
		KeyData:   gsKey,
		ChannelID: protocol.MakeChannelID(0xAABBCC, 0),
		OutputFn:  func(data []byte) error { return nil },
	})

	transmitter.SendSessionKey()
	err := agg.ProcessPacket(injector.Packets[0])

	if err != ErrWrongChannel {
		t.Errorf("Wrong channel: got %v, want ErrWrongChannel", err)
	}
}

func TestAggregatorOldEpoch(t *testing.T) {
	droneKey, gsKey, _ := crypto.GenerateWFBKeys()

	injector := tx.NewBufferInjector()
	channelID := protocol.MakeChannelID(0x010203, 0)

	// Transmitter uses epoch 100
	transmitter, _ := tx.New(tx.Config{
		FecK:      4,
		FecN:      6,
		Epoch:     100,
		ChannelID: channelID,
		KeyData:   droneKey,
	}, injector)

	// Aggregator expects epoch >= 1000
	agg, _ := NewAggregator(AggregatorConfig{
		KeyData:   gsKey,
		Epoch:     1000,
		ChannelID: channelID,
		OutputFn:  func(data []byte) error { return nil },
	})

	transmitter.SendSessionKey()
	err := agg.ProcessPacket(injector.Packets[0])

	if err != ErrOldEpoch {
		t.Errorf("Old epoch: got %v, want ErrOldEpoch", err)
	}
}

func TestAggregatorStats(t *testing.T) {
	droneKey, gsKey, _ := crypto.GenerateWFBKeys()

	injector := tx.NewBufferInjector()
	channelID := protocol.MakeChannelID(0x010203, 0)

	// Use FEC k=4, n=6 - so a full block is 4 data + 2 parity = 6 packets
	transmitter, _ := tx.New(tx.Config{
		FecK:      4,
		FecN:      6,
		Epoch:     1000,
		ChannelID: channelID,
		KeyData:   droneKey,
	}, injector)

	var receivedCount int
	agg, _ := NewAggregator(AggregatorConfig{
		KeyData:   gsKey,
		ChannelID: channelID,
		OutputFn: func(data []byte) error {
			receivedCount++
			return nil
		},
	})

	// Session
	transmitter.SendSessionKey()
	agg.ProcessPacket(injector.Packets[0])
	injector.Clear()

	// Send 4 packets (completes one FEC block, generates 4 data + 2 parity = 6 total)
	for i := 0; i < 4; i++ {
		transmitter.SendPacket([]byte{byte(i)})
	}

	for _, pkt := range injector.Packets {
		agg.ProcessPacket(pkt)
	}

	// Flush pending async operations
	agg.Flush()

	stats := agg.Stats()

	if stats.PacketsSession != 1 {
		t.Errorf("PacketsSession = %d, want 1", stats.PacketsSession)
	}

	// With immediate send optimization, when data fragments 0-3 arrive in order
	// (no gaps), the block completes and is advanced before parity packets arrive.
	// The parity packets then reference an already-processed block and are rejected.
	// So only 4 data packets are counted (not 6).
	if stats.PacketsData != 4 {
		t.Errorf("PacketsData = %d, want 4", stats.PacketsData)
	}

	// All 4 data payloads should be output
	if stats.PacketsOutgoing != 4 {
		t.Errorf("PacketsOutgoing = %d, want 4", stats.PacketsOutgoing)
	}

	if receivedCount != 4 {
		t.Errorf("receivedCount = %d, want 4", receivedCount)
	}
}

func TestAggregatorFEC(t *testing.T) {
	agg, _ := NewAggregator(AggregatorConfig{
		KeyData:   generateGSKey(t),
		ChannelID: 0x01020300,
		OutputFn:  func(data []byte) error { return nil },
	})

	// Initial FEC defaults
	k, n := agg.FEC()
	if k != 8 || n != 12 {
		t.Errorf("Initial FEC = (%d, %d), want (8, 12)", k, n)
	}
}

func TestAggregatorResetStats(t *testing.T) {
	agg, _ := NewAggregator(AggregatorConfig{
		KeyData:   generateGSKey(t),
		ChannelID: 0x01020300,
		OutputFn:  func(data []byte) error { return nil },
	})

	// Process some bad packets to increment stats
	agg.ProcessPacket([]byte{})
	agg.ProcessPacket([]byte{0xFF})

	stats := agg.Stats()
	if stats.PacketsBad == 0 {
		t.Error("Expected non-zero PacketsBad")
	}

	// Reset
	agg.ResetStats()

	stats = agg.Stats()
	if stats.PacketsBad != 0 {
		t.Errorf("After reset: PacketsBad = %d, want 0", stats.PacketsBad)
	}
	if stats.PacketsAll != 0 {
		t.Errorf("After reset: PacketsAll = %d, want 0", stats.PacketsAll)
	}
}

// Helper functions

func generateGSKey(t *testing.T) []byte {
	t.Helper()
	_, gsKey, err := crypto.GenerateWFBKeys()
	if err != nil {
		t.Fatalf("GenerateWFBKeys failed: %v", err)
	}
	return gsKey
}

func TestAggregatorUpdateAntennaStats(t *testing.T) {
	agg, _ := NewAggregator(AggregatorConfig{
		KeyData:   generateGSKey(t),
		ChannelID: 0x01020300,
		OutputFn:  func(data []byte) error { return nil },
	})

	// Update with antenna info from wlan0
	info := &RXInfo{
		WlanIdx:   0,
		Antenna:   [protocol.RX_ANT_MAX]uint8{0, 1, 0, 0},
		RSSI:      [protocol.RX_ANT_MAX]int8{-50, -55, 0, 0}, // Only first two active
		Noise:     [protocol.RX_ANT_MAX]int8{-90, -90, 0, 0},
		Freq:      5180,
		MCSIndex:  3,
		Bandwidth: 20,
	}

	agg.UpdateAntennaStats(info)

	// Flush pending async operations
	agg.Flush()

	stats := agg.Stats()
	if stats.AntennaStats == nil {
		t.Fatal("AntennaStats is nil")
	}

	// Check wlan0/ant0
	key0 := uint32(0)<<8 | uint32(0)
	ant0, ok := stats.AntennaStats[key0]
	if !ok {
		t.Fatalf("Missing stats for wlan0/ant0 (key=%d)", key0)
	}
	if ant0.WlanIdx != 0 {
		t.Errorf("ant0.WlanIdx = %d, want 0", ant0.WlanIdx)
	}
	if ant0.Antenna != 0 {
		t.Errorf("ant0.Antenna = %d, want 0", ant0.Antenna)
	}
	if ant0.PacketsReceived != 1 {
		t.Errorf("ant0.PacketsReceived = %d, want 1", ant0.PacketsReceived)
	}
	if ant0.RSSIMin != -50 || ant0.RSSIMax != -50 {
		t.Errorf("ant0 RSSI = %d/%d, want -50/-50", ant0.RSSIMin, ant0.RSSIMax)
	}

	// Check wlan0/ant1
	key1 := uint32(0)<<8 | uint32(1)
	ant1, ok := stats.AntennaStats[key1]
	if !ok {
		t.Fatalf("Missing stats for wlan0/ant1 (key=%d)", key1)
	}
	if ant1.RSSIMin != -55 || ant1.RSSIMax != -55 {
		t.Errorf("ant1 RSSI = %d/%d, want -55/-55", ant1.RSSIMin, ant1.RSSIMax)
	}

	// SNR should be RSSI - Noise
	expectedSNR := int8(-50 - (-90)) // = 40
	if ant0.SNRMin != expectedSNR || ant0.SNRMax != expectedSNR {
		t.Errorf("ant0 SNR = %d/%d, want %d/%d", ant0.SNRMin, ant0.SNRMax, expectedSNR, expectedSNR)
	}
}

func TestAggregatorAntennaStatsMultiplePackets(t *testing.T) {
	agg, _ := NewAggregator(AggregatorConfig{
		KeyData:   generateGSKey(t),
		ChannelID: 0x01020300,
		OutputFn:  func(data []byte) error { return nil },
	})

	// First packet
	info1 := &RXInfo{
		WlanIdx: 0,
		Antenna: [protocol.RX_ANT_MAX]uint8{0, 0, 0, 0},
		RSSI:    [protocol.RX_ANT_MAX]int8{-60, 0, 0, 0},
		Noise:   [protocol.RX_ANT_MAX]int8{-90, 0, 0, 0},
	}
	agg.UpdateAntennaStats(info1)

	// Second packet - better signal
	info2 := &RXInfo{
		WlanIdx: 0,
		Antenna: [protocol.RX_ANT_MAX]uint8{0, 0, 0, 0},
		RSSI:    [protocol.RX_ANT_MAX]int8{-40, 0, 0, 0},
		Noise:   [protocol.RX_ANT_MAX]int8{-90, 0, 0, 0},
	}
	agg.UpdateAntennaStats(info2)

	// Third packet - worse signal
	info3 := &RXInfo{
		WlanIdx: 0,
		Antenna: [protocol.RX_ANT_MAX]uint8{0, 0, 0, 0},
		RSSI:    [protocol.RX_ANT_MAX]int8{-70, 0, 0, 0},
		Noise:   [protocol.RX_ANT_MAX]int8{-90, 0, 0, 0},
	}
	agg.UpdateAntennaStats(info3)

	// Flush pending async operations
	agg.Flush()

	stats := agg.Stats()
	key := uint32(0)<<8 | uint32(0)
	ant := stats.AntennaStats[key]

	if ant.PacketsReceived != 3 {
		t.Errorf("PacketsReceived = %d, want 3", ant.PacketsReceived)
	}
	if ant.RSSIMin != -70 {
		t.Errorf("RSSIMin = %d, want -70", ant.RSSIMin)
	}
	if ant.RSSIMax != -40 {
		t.Errorf("RSSIMax = %d, want -40", ant.RSSIMax)
	}

	// Sum should be -60 + -40 + -70 = -170
	expectedSum := int64(-60 + -40 + -70)
	if ant.RSSISum != expectedSum {
		t.Errorf("RSSISum = %d, want %d", ant.RSSISum, expectedSum)
	}
}

func TestAggregatorAntennaStatsMultipleWlans(t *testing.T) {
	agg, _ := NewAggregator(AggregatorConfig{
		KeyData:   generateGSKey(t),
		ChannelID: 0x01020300,
		OutputFn:  func(data []byte) error { return nil },
	})

	// Packet from wlan0
	info0 := &RXInfo{
		WlanIdx: 0,
		Antenna: [protocol.RX_ANT_MAX]uint8{0, 0, 0, 0},
		RSSI:    [protocol.RX_ANT_MAX]int8{-50, 0, 0, 0},
		Noise:   [protocol.RX_ANT_MAX]int8{-90, 0, 0, 0},
	}
	agg.UpdateAntennaStats(info0)

	// Packet from wlan1
	info1 := &RXInfo{
		WlanIdx: 1,
		Antenna: [protocol.RX_ANT_MAX]uint8{0, 0, 0, 0},
		RSSI:    [protocol.RX_ANT_MAX]int8{-60, 0, 0, 0},
		Noise:   [protocol.RX_ANT_MAX]int8{-90, 0, 0, 0},
	}
	agg.UpdateAntennaStats(info1)

	// Flush pending async operations
	agg.Flush()

	stats := agg.Stats()

	// Should have 2 entries
	if len(stats.AntennaStats) != 2 {
		t.Errorf("len(AntennaStats) = %d, want 2", len(stats.AntennaStats))
	}

	// Check wlan0
	key0 := uint32(0)<<8 | uint32(0)
	ant0 := stats.AntennaStats[key0]
	if ant0.WlanIdx != 0 {
		t.Errorf("ant0.WlanIdx = %d, want 0", ant0.WlanIdx)
	}
	if ant0.RSSIMax != -50 {
		t.Errorf("ant0.RSSIMax = %d, want -50", ant0.RSSIMax)
	}

	// Check wlan1
	key1 := uint32(1)<<8 | uint32(0)
	ant1 := stats.AntennaStats[key1]
	if ant1.WlanIdx != 1 {
		t.Errorf("ant1.WlanIdx = %d, want 1", ant1.WlanIdx)
	}
	if ant1.RSSIMax != -60 {
		t.Errorf("ant1.RSSIMax = %d, want -60", ant1.RSSIMax)
	}
}

func TestAggregatorAntennaStatsNilInfo(t *testing.T) {
	agg, _ := NewAggregator(AggregatorConfig{
		KeyData:   generateGSKey(t),
		ChannelID: 0x01020300,
		OutputFn:  func(data []byte) error { return nil },
	})

	// Should not panic with nil info
	agg.UpdateAntennaStats(nil)

	// Flush pending async operations
	agg.Flush()

	stats := agg.Stats()
	if len(stats.AntennaStats) != 0 {
		t.Errorf("len(AntennaStats) = %d, want 0", len(stats.AntennaStats))
	}
}

// TestAggregatorCorruptPacketDoesNotPoisonRing verifies that a corrupt packet
// with garbage blockIdx doesn't poison the ring's lastKnown value, which would
// cause all subsequent valid packets to be rejected as "too old".
func TestAggregatorCorruptPacketDoesNotPoisonRing(t *testing.T) {
	droneKey, gsKey, _ := crypto.GenerateWFBKeys()
	injector := tx.NewBufferInjector()
	channelID := protocol.MakeChannelID(0x010203, 0)

	transmitter, _ := tx.New(tx.Config{
		FecK:      4,
		FecN:      6,
		Epoch:     1000,
		ChannelID: channelID,
		KeyData:   droneKey,
	}, injector)

	var received []string
	var mu sync.Mutex

	agg, _ := NewAggregator(AggregatorConfig{
		KeyData:   gsKey,
		Epoch:     0,
		ChannelID: channelID,
		OutputFn: func(data []byte) error {
			mu.Lock()
			received = append(received, string(data))
			mu.Unlock()
			return nil
		},
	})

	// Establish session
	transmitter.SendSessionKey()
	agg.ProcessPacket(injector.Packets[0])
	injector.Clear()

	// Send first valid packet
	transmitter.SendPacket([]byte("packet1"))
	for _, pkt := range injector.Packets {
		agg.ProcessPacket(pkt)
	}
	injector.Clear()

	statsBefore := agg.Stats()

	// Create a corrupt packet with garbage blockIdx in the header.
	// Before the fix, this would poison lastKnown and break all subsequent packets.
	corruptPkt := make([]byte, 100)
	corruptPkt[0] = protocol.WFB_PACKET_DATA
	corruptPkt[1] = 0xFF // Large garbage blockIdx
	corruptPkt[2] = 0xFF

	err := agg.ProcessPacket(corruptPkt)
	if err != ErrDecryptFailed {
		t.Errorf("Expected ErrDecryptFailed, got %v", err)
	}

	statsAfter := agg.Stats()
	if statsAfter.PacketsDecErr != statsBefore.PacketsDecErr+1 {
		t.Errorf("PacketsDecErr should increment")
	}

	// Send more valid packets - should still work after corrupt packet
	transmitter.SendPacket([]byte("packet2"))
	transmitter.SendPacket([]byte("packet3"))
	for _, pkt := range injector.Packets {
		agg.ProcessPacket(pkt)
	}

	agg.Flush()

	mu.Lock()
	defer mu.Unlock()

	if len(received) != 3 {
		t.Errorf("Expected 3 packets, got %d (corrupt packet poisoned ring)", len(received))
	}
}
