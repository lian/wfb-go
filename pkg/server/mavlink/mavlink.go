package mavlink

import (
	"encoding/binary"
)

// Mavlink protocol constants
const (
	V1Marker = 0xFE
	V2Marker = 0xFD

	V1HeaderLen  = 6
	V2HeaderLen  = 10
	CRCLen       = 2
	SignatureLen = 13

	// Mavlink v2 incompatibility flags
	IflagSigned = 0x01

	// Message IDs
	MsgIDHeartbeat   = 0
	MsgIDRadioStatus = 109

	// Mode flags
	ModeFlagSafetyArmed = 0x80

	// CRC extra bytes for message types (from mavlink message definitions)
	CRCExtraRadioStatus = 185

	// Default mavlink identities for RSSI injection (matching wfb-ng defaults)
	DefaultSysID  = 3  // Different from autopilot (typically 1)
	DefaultCompID = 68 // MAV_COMP_ID_TELEMETRY_RADIO

	// WFB flags for RADIO_STATUS remnoise field
	WFBFlagLinkLost   = 0x01 // No packets received
	WFBFlagLinkJammed = 0x02 // Bad/corrupted packets detected
)

// Header contains parsed mavlink message header fields.
type Header struct {
	Version    uint8 // 1 or 2
	PayloadLen uint8
	Seq        uint8
	SysID      uint8
	CompID     uint8
	MsgID      uint32
	// V2 only
	IncompatFlags uint8
	CompatFlags   uint8
}

// Message represents a complete mavlink message.
type Message struct {
	Header  Header
	Payload []byte
	Raw     []byte // Complete raw message including header and CRC
}

// Parser is a streaming parser for mavlink messages.
// It buffers incoming data and extracts complete messages.
type Parser struct {
	buffer []byte
	skip   int
}

// NewParser creates a new mavlink parser.
func NewParser() *Parser {
	return &Parser{
		buffer: make([]byte, 0, 4096),
	}
}

// Parse processes incoming data and returns any complete messages.
// This implements the same FSM logic as wfb-ng's mavlink_parser_gen.
func (p *Parser) Parse(data []byte) []Message {
	// Garbage collect buffer periodically
	if p.skip > 4096 {
		p.buffer = append([]byte(nil), p.buffer[p.skip:]...)
		p.skip = 0
	}

	// Append new data
	p.buffer = append(p.buffer, data...)

	var messages []Message

	for len(p.buffer)-p.skip >= 8 {
		version := p.buffer[p.skip]
		var msgLen int

		switch version {
		case V1Marker:
			// Mavlink v1: [STX][LEN][SEQ][SYS][COMP][MSG][PAYLOAD...][CRC][CRC]
			payloadLen := int(p.buffer[p.skip+1])
			msgLen = V1HeaderLen + payloadLen + CRCLen

		case V2Marker:
			// Mavlink v2: [STX][LEN][IFLAGS][CFLAGS][SEQ][SYS][COMP][MSG_L][MSG_M][MSG_H][PAYLOAD...][CRC][CRC][SIG?]
			if len(p.buffer)-p.skip < 3 {
				return messages
			}
			payloadLen := int(p.buffer[p.skip+1])
			flags := p.buffer[p.skip+2]
			msgLen = V2HeaderLen + payloadLen + CRCLen
			if flags&IflagSigned != 0 {
				msgLen += SignatureLen
			}

		default:
			// Not a valid sync byte, skip
			p.skip++
			continue
		}

		// Check if we have complete message
		if len(p.buffer)-p.skip < msgLen {
			return messages
		}

		// Extract complete message
		raw := make([]byte, msgLen)
		copy(raw, p.buffer[p.skip:p.skip+msgLen])

		msg := Message{Raw: raw}

		if version == V1Marker {
			msg.Header = Header{
				Version:    1,
				PayloadLen: raw[1],
				Seq:        raw[2],
				SysID:      raw[3],
				CompID:     raw[4],
				MsgID:      uint32(raw[5]),
			}
			msg.Payload = raw[V1HeaderLen : V1HeaderLen+int(msg.Header.PayloadLen)]
		} else {
			msg.Header = Header{
				Version:       2,
				PayloadLen:    raw[1],
				IncompatFlags: raw[2],
				CompatFlags:   raw[3],
				Seq:           raw[4],
				SysID:         raw[5],
				CompID:        raw[6],
				MsgID:         uint32(raw[7]) | uint32(raw[8])<<8 | uint32(raw[9])<<16,
			}
			msg.Payload = raw[V2HeaderLen : V2HeaderLen+int(msg.Header.PayloadLen)]
		}

		messages = append(messages, msg)
		p.skip += msgLen
	}

	return messages
}

// Reset clears the parser buffer.
func (p *Parser) Reset() {
	p.buffer = p.buffer[:0]
	p.skip = 0
}

// ParseMessage parses a single mavlink message from raw bytes.
// Returns nil if the data is not a valid mavlink message.
func ParseMessage(data []byte) *Message {
	if len(data) < 8 {
		return nil
	}

	version := data[0]
	var msg Message

	switch version {
	case V1Marker:
		if len(data) < V1HeaderLen+CRCLen {
			return nil
		}
		payloadLen := int(data[1])
		msgLen := V1HeaderLen + payloadLen + CRCLen
		if len(data) < msgLen {
			return nil
		}
		msg.Raw = data[:msgLen]
		msg.Header = Header{
			Version:    1,
			PayloadLen: data[1],
			Seq:        data[2],
			SysID:      data[3],
			CompID:     data[4],
			MsgID:      uint32(data[5]),
		}
		msg.Payload = data[V1HeaderLen : V1HeaderLen+payloadLen]

	case V2Marker:
		if len(data) < V2HeaderLen+CRCLen {
			return nil
		}
		payloadLen := int(data[1])
		msgLen := V2HeaderLen + payloadLen + CRCLen
		if data[2]&IflagSigned != 0 {
			msgLen += SignatureLen
		}
		if len(data) < msgLen {
			return nil
		}
		msg.Raw = data[:msgLen]
		msg.Header = Header{
			Version:       2,
			PayloadLen:    data[1],
			IncompatFlags: data[2],
			CompatFlags:   data[3],
			Seq:           data[4],
			SysID:         data[5],
			CompID:        data[6],
			MsgID:         uint32(data[7]) | uint32(data[8])<<8 | uint32(data[9])<<16,
		}
		msg.Payload = data[V2HeaderLen : V2HeaderLen+payloadLen]

	default:
		return nil
	}

	return &msg
}

// ParseHeartbeat extracts heartbeat fields from a mavlink message.
// Returns (baseMode, systemStatus, mavlinkVersion, ok)
func ParseHeartbeat(msg *Message) (baseMode uint8, systemStatus uint8, mavlinkVersion uint8, ok bool) {
	if msg.Header.MsgID != MsgIDHeartbeat {
		return 0, 0, 0, false
	}

	// HEARTBEAT payload structure:
	// - custom_mode: uint32 (4 bytes) - offset 0
	// - type: uint8 - offset 4
	// - autopilot: uint8 - offset 5
	// - base_mode: uint8 - offset 6
	// - system_status: uint8 - offset 7
	// - mavlink_version: uint8 - offset 8
	if len(msg.Payload) < 9 {
		return 0, 0, 0, false
	}

	baseMode = msg.Payload[6]
	systemStatus = msg.Payload[7]
	mavlinkVersion = msg.Payload[8]
	return baseMode, systemStatus, mavlinkVersion, true
}

// IsArmed checks if the base_mode indicates armed state.
func IsArmed(baseMode uint8) bool {
	return baseMode&ModeFlagSafetyArmed != 0
}

// CRC calculates the X.25 CRC used by mavlink.
type CRC struct {
	crc uint16
}

// NewCRC creates a new CRC calculator.
func NewCRC() *CRC {
	return &CRC{crc: 0xFFFF}
}

// Accumulate adds bytes to the CRC calculation.
func (c *CRC) Accumulate(data []byte) {
	for _, b := range data {
		tmp := b ^ uint8(c.crc&0xFF)
		tmp = (tmp ^ (tmp << 4)) & 0xFF
		c.crc = (c.crc >> 8) ^ uint16(tmp)<<8 ^ uint16(tmp)<<3 ^ uint16(tmp)>>4
	}
}

// Value returns the current CRC value.
func (c *CRC) Value() uint16 {
	return c.crc
}

// Bytes returns the CRC as little-endian bytes.
func (c *CRC) Bytes() []byte {
	b := make([]byte, 2)
	binary.LittleEndian.PutUint16(b, c.crc)
	return b
}

// RadioStatusBuilder builds RADIO_STATUS mavlink messages for RSSI injection.
type RadioStatusBuilder struct {
	sysID  uint8
	compID uint8
	seq    uint8
}

// NewRadioStatusBuilder creates a new RADIO_STATUS message builder.
func NewRadioStatusBuilder(sysID, compID uint8) *RadioStatusBuilder {
	return &RadioStatusBuilder{
		sysID:  sysID,
		compID: compID,
	}
}

// Build creates a new RADIO_STATUS mavlink v2 message (matching wfb-ng).
// Parameters match wfb-ng's radio_status_send():
//   - rssi: Local RSSI (0-255, typically dBm as unsigned via two's complement)
//   - remrssi: Remote RSSI (same as rssi for WFB)
//   - txbuf: TX buffer percentage (typically 100)
//   - noise: Noise floor (rssi - snr, as unsigned)
//   - remnoise: Used for WFB flags
//   - rxerrors: RX error count
//   - fixed: FEC fixed count
func (b *RadioStatusBuilder) Build(rssi, remrssi, txbuf, noise, remnoise uint8, rxerrors, fixed uint16) []byte {
	// RADIO_STATUS payload (wire order from mavlink definition):
	// rxerrors: uint16 (2 bytes)
	// fixed: uint16 (2 bytes)
	// rssi: uint8
	// remrssi: uint8
	// txbuf: uint8
	// noise: uint8
	// remnoise: uint8
	// Total: 9 bytes

	const payloadLen = 9
	msg := make([]byte, V2HeaderLen+payloadLen+CRCLen)

	// Header (Mavlink v2)
	msg[0] = V2Marker
	msg[1] = payloadLen
	msg[2] = 0 // incompat_flags
	msg[3] = 0 // compat_flags
	msg[4] = b.seq
	msg[5] = b.sysID
	msg[6] = b.compID
	msg[7] = byte(MsgIDRadioStatus & 0xFF)        // msg_id low
	msg[8] = byte((MsgIDRadioStatus >> 8) & 0xFF) // msg_id mid
	msg[9] = byte((MsgIDRadioStatus >> 16) & 0xFF) // msg_id high

	// Payload (little-endian)
	binary.LittleEndian.PutUint16(msg[10:12], rxerrors)
	binary.LittleEndian.PutUint16(msg[12:14], fixed)
	msg[14] = rssi
	msg[15] = remrssi
	msg[16] = txbuf
	msg[17] = noise
	msg[18] = remnoise

	// Calculate CRC (includes header bytes 1-9 and payload, plus CRC extra)
	crc := NewCRC()
	crc.Accumulate(msg[1 : V2HeaderLen+payloadLen])
	crc.Accumulate([]byte{CRCExtraRadioStatus})

	// Append CRC
	binary.LittleEndian.PutUint16(msg[V2HeaderLen+payloadLen:], crc.Value())

	b.seq++
	return msg
}
