package tx

import (
	"bytes"
	"testing"
	"time"

	"github.com/lian/wfb-go/pkg/crypto"
	"github.com/lian/wfb-go/pkg/protocol"
)

func TestTransmitter(t *testing.T) {
	// Create buffer injector for testing
	injector := NewBufferInjector()

	// Create transmitter
	cfg := Config{
		FecK:      8,
		FecN:      12,
		Epoch:     12345,
		ChannelID: protocol.MakeChannelID(0x010203, 0),
		KeyData:   generateTXKey(t),
	}

	tx, err := New(cfg, injector)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	// Send session key
	if err := tx.SendSessionKey(); err != nil {
		t.Fatalf("SendSessionKey failed: %v", err)
	}

	// Should have 1 packet (session key)
	if injector.Count() != 1 {
		t.Errorf("Expected 1 packet after SendSessionKey, got %d", injector.Count())
	}

	// Verify session packet structure
	pkt := injector.Packets[0]
	if pkt[0] != protocol.WFB_PACKET_SESSION {
		t.Errorf("First packet type = %d, want %d", pkt[0], protocol.WFB_PACKET_SESSION)
	}

	injector.Clear()

	// Send k data packets to complete a block
	for i := 0; i < 8; i++ {
		data := bytes.Repeat([]byte{byte(i)}, 100)
		more, err := tx.SendPacket(data)
		if err != nil {
			t.Fatalf("SendPacket failed: %v", err)
		}
		if i < 7 && !more {
			t.Errorf("Packet %d: expected more=true", i)
		}
	}

	// Should have k + (n-k) = 12 packets (data + FEC)
	if injector.Count() != 12 {
		t.Errorf("Expected 12 packets after full block, got %d", injector.Count())
	}

	// Verify all packets are data packets
	for i, pkt := range injector.Packets {
		if pkt[0] != protocol.WFB_PACKET_DATA {
			t.Errorf("Packet %d: type = %d, want %d", i, pkt[0], protocol.WFB_PACKET_DATA)
		}
	}
}

func TestTransmitterFECOnly(t *testing.T) {
	keyData := generateTXKey(t)

	injector := NewBufferInjector()

	cfg := Config{
		FecK:      4,
		FecN:      6,
		Epoch:     12345,
		ChannelID: 0x01020300,
		KeyData:   keyData,
	}

	tx, err := New(cfg, injector)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	// Send 2 data packets (incomplete block)
	for i := 0; i < 2; i++ {
		_, err := tx.SendPacket([]byte{byte(i)})
		if err != nil {
			t.Fatalf("SendPacket failed: %v", err)
		}
	}

	// Should have 2 packets
	if injector.Count() != 2 {
		t.Errorf("Expected 2 packets, got %d", injector.Count())
	}

	// Send FEC-only packets to close the block
	for {
		closed, err := tx.SendFECOnly()
		if err != nil {
			t.Fatalf("SendFECOnly failed: %v", err)
		}
		if !closed {
			break
		}
	}

	// Should have k + (n-k) = 6 packets total
	if injector.Count() != 6 {
		t.Errorf("Expected 6 packets after FEC close, got %d", injector.Count())
	}
}

func TestTransmitterSetFEC(t *testing.T) {
	keyData := generateTXKey(t)

	injector := NewBufferInjector()

	cfg := Config{
		FecK:    8,
		FecN:    12,
		KeyData: keyData,
	}

	tx, err := New(cfg, injector)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	k, n := tx.FEC()
	if k != 8 || n != 12 {
		t.Errorf("FEC = (%d, %d), want (8, 12)", k, n)
	}

	// Change FEC parameters
	if err := tx.SetFEC(4, 6); err != nil {
		t.Fatalf("SetFEC failed: %v", err)
	}

	k, n = tx.FEC()
	if k != 4 || n != 6 {
		t.Errorf("FEC = (%d, %d), want (4, 6)", k, n)
	}

	// Session key announcements (n-k+1 = 3)
	// The injector should have received session key packets
	if injector.Count() < 3 {
		t.Errorf("Expected at least 3 session packets, got %d", injector.Count())
	}
}

func TestTransmitterStats(t *testing.T) {
	keyData := generateTXKey(t)

	injector := NewBufferInjector()

	cfg := Config{
		FecK:    2,
		FecN:    3,
		KeyData: keyData,
	}

	tx, err := New(cfg, injector)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	stats := tx.Stats()
	if stats.SessionsStarted != 1 {
		t.Errorf("SessionsStarted = %d, want 1", stats.SessionsStarted)
	}

	// Send packets to complete a block
	for i := 0; i < 2; i++ {
		tx.SendPacket([]byte{byte(i)})
	}

	stats = tx.Stats()
	if stats.PacketsInjected != 3 {
		t.Errorf("PacketsInjected = %d, want 3", stats.PacketsInjected)
	}
}

func TestTransmitterFECTimeout(t *testing.T) {
	keyData := generateTXKey(t)

	injector := NewBufferInjector()

	cfg := Config{
		FecK:       4,
		FecN:       6,
		FecTimeout: 50 * time.Millisecond,
		KeyData:    keyData,
	}

	tx, err := New(cfg, injector)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer tx.Close()

	// Send 2 data packets (incomplete block, k=4)
	for i := 0; i < 2; i++ {
		_, err := tx.SendPacket([]byte{byte(i)})
		if err != nil {
			t.Fatalf("SendPacket failed: %v", err)
		}
	}

	// Should have 2 packets so far
	if injector.Count() != 2 {
		t.Errorf("Expected 2 packets initially, got %d", injector.Count())
	}

	// Wait for FEC timeout to trigger
	time.Sleep(100 * time.Millisecond)

	// Should have 6 packets now (2 data + 2 FEC-only padding + 2 parity)
	if injector.Count() != 6 {
		t.Errorf("Expected 6 packets after timeout, got %d", injector.Count())
	}

	// Check stats
	stats := tx.Stats()
	if stats.FECTimeouts != 2 {
		t.Errorf("FECTimeouts = %d, want 2", stats.FECTimeouts)
	}
}

func TestBufferInjector(t *testing.T) {
	inj := NewBufferInjector()

	if inj.Count() != 0 {
		t.Errorf("Initial count = %d, want 0", inj.Count())
	}

	inj.Inject([]byte{1, 2, 3})
	inj.Inject([]byte{4, 5, 6})

	if inj.Count() != 2 {
		t.Errorf("Count = %d, want 2", inj.Count())
	}

	inj.Clear()

	if inj.Count() != 0 {
		t.Errorf("Count after Clear = %d, want 0", inj.Count())
	}
}

// generateTXKey generates TX key data for testing.
func generateTXKey(t *testing.T) []byte {
	t.Helper()
	droneKey, _, err := crypto.GenerateWFBKeys()
	if err != nil {
		t.Fatalf("GenerateWFBKeys failed: %v", err)
	}
	return droneKey
}
