package protocol

import (
	"encoding/binary"
	"errors"
)

var (
	ErrPacketTooShort   = errors.New("packet too short")
	ErrInvalidPacketType = errors.New("invalid packet type")
	ErrInvalidFECType   = errors.New("invalid FEC type")
)

// SessionHeader is the unencrypted header for session key packets.
// Total size: 25 bytes
type SessionHeader struct {
	PacketType   uint8     // WFB_PACKET_SESSION (0x02)
	SessionNonce [24]byte  // Random nonce for crypto_box
}

// SessionData is the encrypted payload of session packets.
// Total size: 47 bytes (plus optional TLV tags)
type SessionData struct {
	Epoch      uint64   // Reject packets from old epochs
	ChannelID  uint32   // (link_id << 8) | port
	FECType    uint8    // WFB_FEC_VDM_RS
	K          uint8    // FEC data shards
	N          uint8    // FEC total shards
	SessionKey [32]byte // ChaCha20-Poly1305 key
	// Tags follow (optional TLV list)
}

const SessionDataSize = 8 + 4 + 1 + 1 + 1 + 32 // 47 bytes

// BlockHeader is the header for data packets (before encryption).
// Total size: 9 bytes
type BlockHeader struct {
	PacketType uint8  // WFB_PACKET_DATA (0x01)
	DataNonce  uint64 // Big-endian: (block_idx << 8) | fragment_idx
}

// PacketHeader is inside the encrypted payload of data packets.
// Total size: 3 bytes
type PacketHeader struct {
	Flags      uint8  // WFB_PACKET_FEC_ONLY if empty
	PacketSize uint16 // Big-endian: actual payload size
}

// TLVHeader is the header for optional TLV attributes in session data.
type TLVHeader struct {
	ID    uint8
	Len   uint16
	Value []byte
}

// MarshalSessionHeader serializes a SessionHeader to bytes.
func (h *SessionHeader) Marshal() []byte {
	buf := make([]byte, SESSION_HDR_SIZE)
	buf[0] = h.PacketType
	copy(buf[1:25], h.SessionNonce[:])
	return buf
}

// UnmarshalSessionHeader deserializes a SessionHeader from bytes.
func UnmarshalSessionHeader(data []byte) (*SessionHeader, error) {
	if len(data) < SESSION_HDR_SIZE {
		return nil, ErrPacketTooShort
	}
	h := &SessionHeader{
		PacketType: data[0],
	}
	copy(h.SessionNonce[:], data[1:25])
	return h, nil
}

// MarshalSessionData serializes SessionData to bytes (for encryption).
func (d *SessionData) Marshal() []byte {
	buf := make([]byte, SessionDataSize)
	binary.BigEndian.PutUint64(buf[0:8], d.Epoch)
	binary.BigEndian.PutUint32(buf[8:12], d.ChannelID)
	buf[12] = d.FECType
	buf[13] = d.K
	buf[14] = d.N
	copy(buf[15:47], d.SessionKey[:])
	return buf
}

// UnmarshalSessionData deserializes SessionData from decrypted bytes.
func UnmarshalSessionData(data []byte) (*SessionData, error) {
	if len(data) < SessionDataSize {
		return nil, ErrPacketTooShort
	}
	d := &SessionData{
		Epoch:     binary.BigEndian.Uint64(data[0:8]),
		ChannelID: binary.BigEndian.Uint32(data[8:12]),
		FECType:   data[12],
		K:         data[13],
		N:         data[14],
	}
	copy(d.SessionKey[:], data[15:47])
	return d, nil
}

// MarshalBlockHeader serializes a BlockHeader to bytes.
func (h *BlockHeader) Marshal() []byte {
	buf := make([]byte, BLOCK_HDR_SIZE)
	buf[0] = h.PacketType
	binary.BigEndian.PutUint64(buf[1:9], h.DataNonce)
	return buf
}

// UnmarshalBlockHeader deserializes a BlockHeader from bytes.
func UnmarshalBlockHeader(data []byte) (*BlockHeader, error) {
	if len(data) < BLOCK_HDR_SIZE {
		return nil, ErrPacketTooShort
	}
	return &BlockHeader{
		PacketType: data[0],
		DataNonce:  binary.BigEndian.Uint64(data[1:9]),
	}, nil
}

// MakeDataNonce creates the 8-byte nonce for data packet encryption.
// nonce = (block_idx << 8) | fragment_idx
func MakeDataNonce(blockIdx uint64, fragmentIdx uint8) uint64 {
	return ((blockIdx & BLOCK_IDX_MASK) << 8) | uint64(fragmentIdx)
}

// ParseDataNonce extracts block_idx and fragment_idx from nonce.
func ParseDataNonce(nonce uint64) (blockIdx uint64, fragmentIdx uint8) {
	return nonce >> 8, uint8(nonce & 0xFF)
}

// MarshalPacketHeader serializes a PacketHeader to bytes.
func (h *PacketHeader) Marshal() []byte {
	buf := make([]byte, PACKET_HDR_SIZE)
	buf[0] = h.Flags
	binary.BigEndian.PutUint16(buf[1:3], h.PacketSize)
	return buf
}

// UnmarshalPacketHeader deserializes a PacketHeader from bytes.
func UnmarshalPacketHeader(data []byte) (*PacketHeader, error) {
	if len(data) < PACKET_HDR_SIZE {
		return nil, ErrPacketTooShort
	}
	return &PacketHeader{
		Flags:      data[0],
		PacketSize: binary.BigEndian.Uint16(data[1:3]),
	}, nil
}

// RXForwardHeader is prepended when forwarding raw packets via UDP.
// Used in distributed/cluster mode.
// Total size: 20 bytes
type RXForwardHeader struct {
	WlanIdx   uint8
	Antenna   [RX_ANT_MAX]uint8 // Antenna indices, 0xFF for unused
	RSSI      [RX_ANT_MAX]int8  // Signal strength per antenna
	Noise     [RX_ANT_MAX]int8  // Noise floor per antenna
	Freq      uint16            // Channel frequency in MHz (big-endian)
	MCSIndex  uint8
	Bandwidth uint8
}

const RXForwardHeaderSize = 1 + RX_ANT_MAX + RX_ANT_MAX + RX_ANT_MAX + 2 + 1 + 1 // 20 bytes

// MarshalRXForwardHeader serializes the forward header.
func (h *RXForwardHeader) Marshal() []byte {
	buf := make([]byte, RXForwardHeaderSize)
	buf[0] = h.WlanIdx
	copy(buf[1:5], h.Antenna[:])
	for i := 0; i < RX_ANT_MAX; i++ {
		buf[5+i] = byte(h.RSSI[i])
	}
	for i := 0; i < RX_ANT_MAX; i++ {
		buf[9+i] = byte(h.Noise[i])
	}
	binary.BigEndian.PutUint16(buf[13:15], h.Freq)
	buf[15] = h.MCSIndex
	buf[16] = h.Bandwidth
	return buf
}

// UnmarshalRXForwardHeader deserializes the forward header.
func UnmarshalRXForwardHeader(data []byte) (*RXForwardHeader, error) {
	if len(data) < RXForwardHeaderSize {
		return nil, ErrPacketTooShort
	}
	h := &RXForwardHeader{
		WlanIdx:   data[0],
		Freq:      binary.BigEndian.Uint16(data[13:15]),
		MCSIndex:  data[15],
		Bandwidth: data[16],
	}
	copy(h.Antenna[:], data[1:5])
	for i := 0; i < RX_ANT_MAX; i++ {
		h.RSSI[i] = int8(data[5+i])
		h.Noise[i] = int8(data[9+i])
	}
	return h, nil
}
