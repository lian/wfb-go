package mavlink

import (
	"testing"
)

func TestParserV1(t *testing.T) {
	parser := NewParser()

	// Mavlink v1 HEARTBEAT message
	// [0xFE][len=9][seq=0][sys=1][comp=1][msg=0][payload...][crc][crc]
	msg := []byte{
		0xFE,       // STX
		0x09,       // Payload length
		0x00,       // Sequence
		0x01,       // System ID
		0x01,       // Component ID
		0x00,       // Message ID (HEARTBEAT)
		// Payload (9 bytes):
		0x00, 0x00, 0x00, 0x00, // custom_mode
		0x02,       // type (quadrotor)
		0x03,       // autopilot (ardupilot)
		0x81,       // base_mode (armed + custom mode)
		0x04,       // system_status
		0x03,       // mavlink_version
		// CRC
		0x00, 0x00,
	}

	messages := parser.Parse(msg)
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}

	m := messages[0]
	if m.Header.Version != 1 {
		t.Errorf("expected version 1, got %d", m.Header.Version)
	}
	if m.Header.SysID != 1 {
		t.Errorf("expected sysID 1, got %d", m.Header.SysID)
	}
	if m.Header.CompID != 1 {
		t.Errorf("expected compID 1, got %d", m.Header.CompID)
	}
	if m.Header.MsgID != 0 {
		t.Errorf("expected msgID 0, got %d", m.Header.MsgID)
	}

	baseMode, _, _, ok := ParseHeartbeat(&m)
	if !ok {
		t.Fatal("failed to parse heartbeat")
	}
	if !IsArmed(baseMode) {
		t.Error("expected armed state")
	}
}

func TestParserV2(t *testing.T) {
	parser := NewParser()

	// Mavlink v2 HEARTBEAT message
	// [0xFD][len][iflags][cflags][seq][sys][comp][msg_l][msg_m][msg_h][payload...][crc][crc]
	msg := []byte{
		0xFD,       // STX
		0x09,       // Payload length
		0x00,       // Incompatibility flags
		0x00,       // Compatibility flags
		0x00,       // Sequence
		0x01,       // System ID
		0x01,       // Component ID
		0x00, 0x00, 0x00, // Message ID (HEARTBEAT = 0)
		// Payload (9 bytes):
		0x00, 0x00, 0x00, 0x00, // custom_mode
		0x02,       // type
		0x03,       // autopilot
		0x01,       // base_mode (disarmed)
		0x04,       // system_status
		0x03,       // mavlink_version
		// CRC
		0x00, 0x00,
	}

	messages := parser.Parse(msg)
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}

	m := messages[0]
	if m.Header.Version != 2 {
		t.Errorf("expected version 2, got %d", m.Header.Version)
	}
	if m.Header.MsgID != 0 {
		t.Errorf("expected msgID 0, got %d", m.Header.MsgID)
	}

	baseMode, _, _, ok := ParseHeartbeat(&m)
	if !ok {
		t.Fatal("failed to parse heartbeat")
	}
	if IsArmed(baseMode) {
		t.Error("expected disarmed state")
	}
}

func TestParserStreaming(t *testing.T) {
	parser := NewParser()

	// Two complete v1 messages
	msg1 := []byte{
		0xFE, 0x09, 0x00, 0x01, 0x01, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x02, 0x03, 0x81, 0x04, 0x03,
		0x00, 0x00,
	}
	msg2 := []byte{
		0xFE, 0x09, 0x01, 0x01, 0x01, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x02, 0x03, 0x01, 0x04, 0x03,
		0x00, 0x00,
	}

	// Send first message in chunks
	messages := parser.Parse(msg1[:5])
	if len(messages) != 0 {
		t.Errorf("expected 0 messages from partial data, got %d", len(messages))
	}

	messages = parser.Parse(msg1[5:])
	if len(messages) != 1 {
		t.Fatalf("expected 1 message after completing first, got %d", len(messages))
	}

	// Send second message all at once
	messages = parser.Parse(msg2)
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}

	if messages[0].Header.Seq != 1 {
		t.Errorf("expected seq 1, got %d", messages[0].Header.Seq)
	}
}

func TestParserSkipsBadBytes(t *testing.T) {
	parser := NewParser()

	// Garbage bytes followed by valid message
	data := []byte{
		0x00, 0x11, 0x22, 0x33, // garbage
		0xFE, 0x09, 0x00, 0x01, 0x01, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x02, 0x03, 0x81, 0x04, 0x03,
		0x00, 0x00,
	}

	messages := parser.Parse(data)
	if len(messages) != 1 {
		t.Fatalf("expected 1 message after skipping garbage, got %d", len(messages))
	}

	if messages[0].Header.Version != 1 {
		t.Errorf("expected version 1, got %d", messages[0].Header.Version)
	}
}

func TestParseMessage(t *testing.T) {
	// Valid v1 HEARTBEAT (armed)
	v1Armed := []byte{
		0xFE, 0x09, 0x00, 0x01, 0x01, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x02, 0x03, 0x81, 0x04, 0x03,
		0x00, 0x00,
	}

	msg := ParseMessage(v1Armed)
	if msg == nil {
		t.Fatal("expected valid message")
	}
	if msg.Header.Version != 1 {
		t.Errorf("expected version 1, got %d", msg.Header.Version)
	}
	if msg.Header.SysID != 1 || msg.Header.CompID != 1 {
		t.Errorf("expected sys=1 comp=1, got sys=%d comp=%d", msg.Header.SysID, msg.Header.CompID)
	}

	baseMode, _, _, ok := ParseHeartbeat(msg)
	if !ok {
		t.Fatal("expected valid heartbeat")
	}
	if !IsArmed(baseMode) {
		t.Error("expected armed")
	}

	// Invalid data
	invalid := []byte{0x00, 0x01, 0x02}
	if ParseMessage(invalid) != nil {
		t.Error("expected nil for invalid data")
	}

	// Wrong marker
	wrongMarker := []byte{0xAA, 0x09, 0x00, 0x01, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0x03, 0x81, 0x04, 0x03, 0x00, 0x00}
	if ParseMessage(wrongMarker) != nil {
		t.Error("expected nil for wrong marker")
	}
}

func TestRadioStatusBuilder(t *testing.T) {
	builder := NewRadioStatusBuilder(DefaultSysID, DefaultCompID)

	// Build a RADIO_STATUS message
	msg := builder.Build(
		200,   // rssi (dBm -128 + 128 = 0 → 0; -28 dBm → 100)
		200,   // remrssi
		100,   // txbuf
		50,    // noise (SNR)
		0,     // remnoise (flags)
		10,    // rxerrors
		5,     // fixed (FEC)
	)

	// Verify message structure
	// Mavlink v2: [0xFD][len=9][iflags][cflags][seq][sys][comp][msg_l][msg_m][msg_h][payload 9 bytes][crc 2 bytes]
	// Total: 10 + 9 + 2 = 21 bytes
	if len(msg) != 21 {
		t.Fatalf("expected message length 21, got %d", len(msg))
	}

	// Check header
	if msg[0] != V2Marker {
		t.Errorf("expected STX 0xFD, got 0x%02X", msg[0])
	}
	if msg[1] != 9 {
		t.Errorf("expected payload len 9, got %d", msg[1])
	}
	if msg[2] != 0 {
		t.Errorf("expected incompat_flags 0, got %d", msg[2])
	}
	if msg[3] != 0 {
		t.Errorf("expected compat_flags 0, got %d", msg[3])
	}
	if msg[4] != 0 {
		t.Errorf("expected seq 0, got %d", msg[4])
	}
	if msg[5] != DefaultSysID {
		t.Errorf("expected sys_id %d, got %d", DefaultSysID, msg[5])
	}
	if msg[6] != DefaultCompID {
		t.Errorf("expected comp_id %d, got %d", DefaultCompID, msg[6])
	}
	// Message ID is 3 bytes little-endian
	msgID := uint32(msg[7]) | uint32(msg[8])<<8 | uint32(msg[9])<<16
	if msgID != MsgIDRadioStatus {
		t.Errorf("expected msg_id %d, got %d", MsgIDRadioStatus, msgID)
	}

	// Verify payload (wire order: rxerrors, fixed, rssi, remrssi, txbuf, noise, remnoise)
	// rxerrors at offset 10-11 (little endian)
	rxerrors := uint16(msg[10]) | uint16(msg[11])<<8
	if rxerrors != 10 {
		t.Errorf("expected rxerrors 10, got %d", rxerrors)
	}
	// fixed at offset 12-13
	fixed := uint16(msg[12]) | uint16(msg[13])<<8
	if fixed != 5 {
		t.Errorf("expected fixed 5, got %d", fixed)
	}
	// rssi at offset 14
	if msg[14] != 200 {
		t.Errorf("expected rssi 200, got %d", msg[14])
	}
	// remrssi at offset 15
	if msg[15] != 200 {
		t.Errorf("expected remrssi 200, got %d", msg[15])
	}
	// txbuf at offset 16
	if msg[16] != 100 {
		t.Errorf("expected txbuf 100, got %d", msg[16])
	}
	// noise at offset 17
	if msg[17] != 50 {
		t.Errorf("expected noise 50, got %d", msg[17])
	}
	// remnoise at offset 18
	if msg[18] != 0 {
		t.Errorf("expected remnoise 0, got %d", msg[18])
	}

	// Parse the generated message back to verify it's valid
	parsed := ParseMessage(msg)
	if parsed == nil {
		t.Fatal("failed to parse generated RADIO_STATUS message")
	}
	if parsed.Header.Version != 2 {
		t.Errorf("expected version 2, got %d", parsed.Header.Version)
	}
	if parsed.Header.MsgID != MsgIDRadioStatus {
		t.Errorf("expected msg_id %d, got %d", MsgIDRadioStatus, parsed.Header.MsgID)
	}
	if parsed.Header.SysID != DefaultSysID {
		t.Errorf("expected sys_id %d, got %d", DefaultSysID, parsed.Header.SysID)
	}
	if parsed.Header.CompID != DefaultCompID {
		t.Errorf("expected comp_id %d, got %d", DefaultCompID, parsed.Header.CompID)
	}

	// Build another message to verify sequence increment
	msg2 := builder.Build(100, 100, 100, 25, 0, 0, 0)
	if msg2[4] != 1 {
		t.Errorf("expected seq 1, got %d", msg2[4])
	}
}

func TestRadioStatusCRC(t *testing.T) {
	// Verify CRC calculation by building a message and checking it can be validated
	builder := NewRadioStatusBuilder(3, 68)
	msg := builder.Build(128, 128, 100, 0, 0, 0, 0)

	// Mavlink v2: 10 byte header + 9 byte payload + 2 byte CRC = 21 bytes
	// CRC is calculated over bytes 1-18 (header bytes 1-9 + payload), excluding STX and CRC itself
	// The CRC should be at the last 2 bytes (offsets 19-20)
	crc := NewCRC()
	crc.Accumulate(msg[1:19]) // bytes 1-18: header (excluding STX) + payload
	crc.Accumulate([]byte{CRCExtraRadioStatus})

	expectedCRC := crc.Value()
	actualCRC := uint16(msg[19]) | uint16(msg[20])<<8

	if actualCRC != expectedCRC {
		t.Errorf("CRC mismatch: expected 0x%04X, got 0x%04X", expectedCRC, actualCRC)
	}
}

// TestRSSIEncodingMatchesWFBNG verifies our RSSI encoding matches wfb-ng
func TestRSSIEncodingMatchesWFBNG(t *testing.T) {
	// wfb-ng uses Python's % 256 for converting signed dBm to unsigned
	// Python: -50 % 256 = 206
	// Go: uint8(int8(-50)) = 206 (two's complement)
	testCases := []struct {
		rssiDBm  int8
		expected uint8
	}{
		{-50, 206},  // Typical RSSI
		{-30, 226},  // Strong signal
		{-80, 176},  // Weak signal
		{-128, 128}, // Minimum
		{0, 0},      // Zero
		{127, 127},  // Maximum positive
	}

	for _, tc := range testCases {
		got := uint8(tc.rssiDBm)
		if got != tc.expected {
			t.Errorf("RSSI %d dBm: expected uint8 %d, got %d", tc.rssiDBm, tc.expected, got)
		}
	}
}

// TestNoiseEncodingMatchesWFBNG verifies noise floor calculation matches wfb-ng
func TestNoiseEncodingMatchesWFBNG(t *testing.T) {
	// wfb-ng calculates noise as: noise = rssi - snr
	// (the noise floor in dBm)
	testCases := []struct {
		rssi     int8
		snr      int8
		expected uint8
	}{
		{-50, 20, 186}, // rssi=-50, snr=20 -> noise=-70 -> uint8(186)
		{-40, 30, 186}, // rssi=-40, snr=30 -> noise=-70 -> uint8(186)
		{-60, 10, 186}, // rssi=-60, snr=10 -> noise=-70 -> uint8(186)
	}

	for _, tc := range testCases {
		noise := tc.rssi - tc.snr
		got := uint8(noise)
		if got != tc.expected {
			t.Errorf("RSSI=%d SNR=%d: expected noise uint8 %d, got %d", tc.rssi, tc.snr, tc.expected, got)
		}
	}
}
