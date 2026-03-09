// Package protocol defines WFB packet structures and constants.
// Reference: _vendor/wfb-ng/doc/wfb-ng-std-draft.md
package protocol

const (
	// Packet types
	WFB_PACKET_DATA    = 0x01
	WFB_PACKET_SESSION = 0x02

	// FEC types
	WFB_FEC_VDM_RS = 0x01 // Reed-Solomon on Vandermonde matrix

	// Packet flags
	WFB_PACKET_FEC_ONLY = 0x01 // Empty packet to close FEC block

	// MTU and size limits
	WIFI_MTU = 4045 // Max injected packet size including all headers

	// Header sizes
	IEEE80211_HDR_SIZE = 24
	BLOCK_HDR_SIZE     = 9  // packet_type (1) + data_nonce (8)
	SESSION_HDR_SIZE   = 25 // packet_type (1) + session_nonce (24)
	PACKET_HDR_SIZE    = 3  // flags (1) + packet_size (2)

	// Crypto sizes
	CHACHA_KEY_SIZE   = 32
	CHACHA_NONCE_SIZE = 8  // Original ChaCha20-Poly1305 (NOT IETF 12-byte)
	CHACHA_TAG_SIZE   = 16 // Poly1305 tag
	BOX_NONCE_SIZE    = 24 // crypto_box_NONCEBYTES
	BOX_MAC_SIZE      = 16 // crypto_box_MACBYTES

	// Derived payload limits
	// MAX_PAYLOAD_SIZE = WIFI_MTU - IEEE80211_HDR - BLOCK_HDR - TAG - PACKET_HDR
	MAX_PAYLOAD_SIZE = WIFI_MTU - IEEE80211_HDR_SIZE - BLOCK_HDR_SIZE - CHACHA_TAG_SIZE - PACKET_HDR_SIZE // 3993

	// MAX_FEC_PAYLOAD = WIFI_MTU - IEEE80211_HDR - BLOCK_HDR - TAG
	MAX_FEC_PAYLOAD = WIFI_MTU - IEEE80211_HDR_SIZE - BLOCK_HDR_SIZE - CHACHA_TAG_SIZE // 3996

	// Session announcement interval
	SESSION_KEY_ANNOUNCE_MS = 1000

	// RX ring buffer size for FEC block reassembly
	RX_RING_SIZE = 40

	// Max RX interfaces
	MAX_RX_INTERFACES = 8

	// Max antennas per interface
	RX_ANT_MAX = 4

	// Block index limits
	BLOCK_IDX_MASK = (1 << 56) - 1
	MAX_BLOCK_IDX  = (1 << 55) - 1
)

// Stream allocation ranges
const (
	// Down streams (vehicle to GS): 0-127
	STREAM_VIDEO_MIN   = 0
	STREAM_VIDEO_MAX   = 15
	STREAM_VIDEO_DEF   = 0
	STREAM_MAVLINK_MIN = 16
	STREAM_MAVLINK_MAX = 31
	STREAM_MAVLINK_DEF = 16
	STREAM_TUNNEL_MIN  = 32
	STREAM_TUNNEL_MAX  = 47
	STREAM_TUNNEL_DEF  = 32

	// Up streams (GS to vehicle): 128-255
	STREAM_UP_BASE = 128
)

// MakeChannelID creates a channel ID from link_id and port.
// channel_id format: link_id (24 bits) << 8 | port (8 bits)
func MakeChannelID(linkID uint32, port uint8) uint32 {
	return (linkID << 8) | uint32(port)
}

// ParseChannelID extracts link_id and port from channel_id.
func ParseChannelID(channelID uint32) (linkID uint32, port uint8) {
	return channelID >> 8, uint8(channelID & 0xFF)
}
