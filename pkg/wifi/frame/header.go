// Package frame handles IEEE 802.11 frame headers for WFB.
package frame

import (
	"encoding/binary"
)

const (
	HeaderSize = 24

	// Frame control field values
	FrameTypeData = 0x08
	FrameTypeRTS  = 0xb4

	// WFB MAC address prefix
	// First byte 0x57 ('W') has multicast + locally administered bits set
	MACPrefix0 = 0x57 // 'W'
	MACPrefix1 = 0x42 // 'B'
)

// Header represents an IEEE 802.11 data frame header.
// WFB uses data frames with broadcast receiver.
type Header struct {
	FrameControl [2]byte  // Frame type and flags
	Duration     [2]byte  // Duration/ID
	Receiver     [6]byte  // Destination address (broadcast)
	Transmitter  [6]byte  // Source address (WB:channel_id)
	BSSID        [6]byte  // BSSID (WB:channel_id)
	SeqControl   [2]byte  // Sequence number + fragment number
}

// DefaultHeader returns a WFB compatible 802.11 header template.
// The channel_id must be set before use.
func DefaultHeader() *Header {
	return &Header{
		FrameControl: [2]byte{0x08, 0x01}, // Data frame, not protected, from STA to DS
		Duration:     [2]byte{0x00, 0x00},
		Receiver:     [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, // Broadcast
		Transmitter:  [6]byte{MACPrefix0, MACPrefix1, 0x00, 0x00, 0x00, 0x00},
		BSSID:        [6]byte{MACPrefix0, MACPrefix1, 0x00, 0x00, 0x00, 0x00},
		SeqControl:   [2]byte{0x00, 0x00},
	}
}

// SetChannelID sets the channel_id in the transmitter and BSSID addresses.
// channel_id format: link_id (24 bits) | port (8 bits) = 32 bits total
// Placed in bytes 2-5 of the MAC address (after WB prefix).
func (h *Header) SetChannelID(channelID uint32) {
	// Bytes 2-5 of transmitter and BSSID get the channel_id (big-endian)
	binary.BigEndian.PutUint32(h.Transmitter[2:6], channelID)
	binary.BigEndian.PutUint32(h.BSSID[2:6], channelID)
}

// GetChannelID extracts the channel_id from the transmitter address.
func (h *Header) GetChannelID() uint32 {
	return binary.BigEndian.Uint32(h.Transmitter[2:6])
}

// SetSequence sets the sequence number (12 bits) and fragment number (4 bits).
func (h *Header) SetSequence(seqNum uint16, fragNum uint8) {
	// seq_control = (seq_num << 4) | frag_num
	val := (seqNum << 4) | uint16(fragNum&0x0F)
	binary.LittleEndian.PutUint16(h.SeqControl[:], val)
}

// GetSequence extracts sequence number and fragment number.
func (h *Header) GetSequence() (seqNum uint16, fragNum uint8) {
	val := binary.LittleEndian.Uint16(h.SeqControl[:])
	return val >> 4, uint8(val & 0x0F)
}

// Marshal serializes the header to bytes.
func (h *Header) Marshal() []byte {
	buf := make([]byte, HeaderSize)
	copy(buf[0:2], h.FrameControl[:])
	copy(buf[2:4], h.Duration[:])
	copy(buf[4:10], h.Receiver[:])
	copy(buf[10:16], h.Transmitter[:])
	copy(buf[16:22], h.BSSID[:])
	copy(buf[22:24], h.SeqControl[:])
	return buf
}

// Unmarshal deserializes the header from bytes.
func Unmarshal(data []byte) (*Header, error) {
	if len(data) < HeaderSize {
		return nil, ErrHeaderTooShort
	}
	h := &Header{}
	copy(h.FrameControl[:], data[0:2])
	copy(h.Duration[:], data[2:4])
	copy(h.Receiver[:], data[4:10])
	copy(h.Transmitter[:], data[10:16])
	copy(h.BSSID[:], data[16:22])
	copy(h.SeqControl[:], data[22:24])
	return h, nil
}

// IsWFBPacket checks if this frame has the WFB MAC prefix.
func (h *Header) IsWFBPacket() bool {
	return h.Transmitter[0] == MACPrefix0 && h.Transmitter[1] == MACPrefix1
}

// Errors
type headerError string

func (e headerError) Error() string { return string(e) }

const ErrHeaderTooShort = headerError("ieee80211: header too short")
