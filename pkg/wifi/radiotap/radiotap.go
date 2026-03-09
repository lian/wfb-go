// Package radiotap handles IEEE 802.11 radiotap headers for WFB.
// Radiotap is a de facto standard for 802.11 frame injection and reception.
package radiotap

import (
	"encoding/binary"
	"errors"
)

// Radiotap field types
const (
	TSFT            = 0
	FLAGS           = 1
	RATE            = 2
	CHANNEL         = 3
	FHSS            = 4
	DBM_ANTSIGNAL   = 5
	DBM_ANTNOISE    = 6
	LOCK_QUALITY    = 7
	TX_ATTENUATION  = 8
	DB_TX_ATTEN     = 9
	DBM_TX_POWER    = 10
	ANTENNA         = 11
	DB_ANTSIGNAL    = 12
	DB_ANTNOISE     = 13
	RX_FLAGS        = 14
	TX_FLAGS        = 15
	RTS_RETRIES     = 16
	DATA_RETRIES    = 17
	MCS             = 19
	AMPDU_STATUS    = 20
	VHT             = 21
	TIMESTAMP       = 22
	RADIOTAP_NS     = 29
	VENDOR_NS       = 30
	EXT             = 31
)

// TX_FLAGS bits
const (
	TX_FLAGS_FAIL  = 0x0001
	TX_FLAGS_CTS   = 0x0002
	TX_FLAGS_RTS   = 0x0004
	TX_FLAGS_NOACK = 0x0008
)

// FLAGS field bits (radiotap field type 1)
const (
	FLAGS_CFP      = 0x01 // sent/received during CFP
	FLAGS_PREAMBLE = 0x02 // sent/received with short preamble
	FLAGS_WEP      = 0x04 // sent/received with WEP encryption
	FLAGS_FRAG     = 0x08 // sent/received with fragmentation
	FLAGS_FCS      = 0x10 // frame includes FCS
	FLAGS_DATAPAD  = 0x20 // frame has padding between 802.11 header and payload
	FLAGS_BADFCS   = 0x40 // frame failed FCS check
)

// MCS known bits
const (
	MCS_HAVE_BW   = 0x01
	MCS_HAVE_MCS  = 0x02
	MCS_HAVE_GI   = 0x04
	MCS_HAVE_FMT  = 0x08
	MCS_HAVE_FEC  = 0x10
	MCS_HAVE_STBC = 0x20
)

// MCS flags bits
const (
	MCS_BW_MASK  = 0x03
	MCS_BW_20    = 0
	MCS_BW_40    = 1
	MCS_BW_20L   = 2
	MCS_BW_20U   = 3
	MCS_SGI      = 0x04 // Short Guard Interval
	MCS_FMT_GF   = 0x08 // Greenfield format
	MCS_FEC_LDPC = 0x10
	MCS_STBC_MASK  = 0x60
	MCS_STBC_SHIFT = 5
)

// VHT known bits
const (
	VHT_KNOWN_STBC      = 0x0001
	VHT_KNOWN_TXOP_PS   = 0x0002
	VHT_KNOWN_GI        = 0x0004
	VHT_KNOWN_SGI_NSYM  = 0x0008
	VHT_KNOWN_LDPC_EXTRA = 0x0010
	VHT_KNOWN_BEAMFORMED = 0x0020
	VHT_KNOWN_BANDWIDTH = 0x0040
	VHT_KNOWN_GROUP_ID  = 0x0080
	VHT_KNOWN_PARTIAL_AID = 0x0100
)

// VHT flags bits
const (
	VHT_FLAG_STBC = 0x01
	VHT_FLAG_SGI  = 0x04
	VHT_FLAG_LDPC = 0x10
)

// VHT bandwidth values
const (
	VHT_BW_20  = 0
	VHT_BW_40  = 1
	VHT_BW_80  = 4
	VHT_BW_160 = 11
)

const (
	// MinHeaderSize is the minimum radiotap header size (8 bytes).
	MinHeaderSize = 8
)

var (
	ErrTooShort       = errors.New("radiotap: header too short")
	ErrInvalidVersion = errors.New("radiotap: invalid version")
	ErrInvalidLength  = errors.New("radiotap: invalid length")
)

// MaxAntennas is the maximum number of antennas we track.
const MaxAntennas = 4

// AntennaInfo holds per-antenna signal information from extended radiotap bitmaps.
type AntennaInfo struct {
	Antenna   uint8
	DBMSignal int8
	DBMNoise  int8
	Valid     bool
}

// Header represents a parsed radiotap header.
type Header struct {
	Length uint16 // Total header length

	// Parsed fields (valid if corresponding Has* is true)
	HasTSFT         bool
	TSFT            uint64

	HasFlags        bool
	Flags           uint8

	HasRate         bool
	Rate            uint8 // in 500kbps units

	HasChannel      bool
	ChannelFreq     uint16 // MHz
	ChannelFlags    uint16

	HasDBMSignal    bool
	DBMSignal       int8

	HasDBMNoise     bool
	DBMNoise        int8

	HasAntenna      bool
	Antenna         uint8

	HasTXFlags      bool
	TXFlags         uint16

	HasMCS          bool
	MCSKnown        uint8
	MCSFlags        uint8
	MCSIndex        uint8

	HasVHT          bool
	VHTKnown        uint16
	VHTFlags        uint8
	VHTBandwidth    uint8
	VHTMCSNSS       [4]uint8
	VHTCoding       uint8
	VHTGroupID      uint8
	VHTPartialAID   uint16

	// Per-antenna info from extended present bitmaps
	Antennas     [MaxAntennas]AntennaInfo
	AntennaCount int
}

// fieldAlignment returns the alignment and size for each radiotap field type.
// Returns (alignment, size).
func fieldAlignment(fieldType int) (int, int) {
	switch fieldType {
	case TSFT:
		return 8, 8
	case FLAGS:
		return 1, 1
	case RATE:
		return 1, 1
	case CHANNEL:
		return 2, 4
	case FHSS:
		return 2, 2
	case DBM_ANTSIGNAL:
		return 1, 1
	case DBM_ANTNOISE:
		return 1, 1
	case LOCK_QUALITY:
		return 2, 2
	case TX_ATTENUATION:
		return 2, 2
	case DB_TX_ATTEN:
		return 2, 2
	case DBM_TX_POWER:
		return 1, 1
	case ANTENNA:
		return 1, 1
	case DB_ANTSIGNAL:
		return 1, 1
	case DB_ANTNOISE:
		return 1, 1
	case RX_FLAGS:
		return 2, 2
	case TX_FLAGS:
		return 2, 2
	case RTS_RETRIES:
		return 1, 1
	case DATA_RETRIES:
		return 1, 1
	case MCS:
		return 1, 3
	case AMPDU_STATUS:
		return 4, 8
	case VHT:
		return 2, 12
	case TIMESTAMP:
		return 8, 12
	default:
		return 0, 0
	}
}

// Parse parses a radiotap header from raw bytes.
// Returns the parsed header and the offset to the 802.11 frame.
func Parse(data []byte) (*Header, int, error) {
	if len(data) < 8 {
		return nil, 0, ErrTooShort
	}

	// Version must be 0
	if data[0] != 0 {
		return nil, 0, ErrInvalidVersion
	}

	// Header length (little-endian)
	length := binary.LittleEndian.Uint16(data[2:4])
	if int(length) > len(data) {
		return nil, 0, ErrInvalidLength
	}

	h := &Header{
		Length: length,
	}

	// Collect all present bitmaps (primary + extensions)
	var presentBitmaps []uint32
	offset := 4

	for {
		if offset+4 > int(length) {
			return nil, 0, ErrTooShort
		}
		present := binary.LittleEndian.Uint32(data[offset : offset+4])
		presentBitmaps = append(presentBitmaps, present)
		offset += 4

		// Check if there are more extended bitmaps
		if present&(1<<EXT) == 0 {
			break
		}
	}

	// Track current antenna index for extended bitmaps
	antennaIdx := 0

	// Parse fields from each present bitmap
	for bitmapIdx, present := range presentBitmaps {
		// For extended bitmaps (after the first), we're parsing per-antenna data
		isExtended := bitmapIdx > 0

		for i := 0; i < 32; i++ {
			if present&(1<<i) == 0 {
				continue
			}
			if i == EXT || i == RADIOTAP_NS || i == VENDOR_NS {
				continue
			}

			align, size := fieldAlignment(i)
			if align == 0 {
				continue // Unknown field
			}

			// Align offset
			if align > 1 && offset%align != 0 {
				offset += align - (offset % align)
			}

			if offset+size > int(length) {
				break
			}

			switch i {
			case TSFT:
				h.HasTSFT = true
				h.TSFT = binary.LittleEndian.Uint64(data[offset:])
			case FLAGS:
				h.HasFlags = true
				h.Flags = data[offset]
			case RATE:
				h.HasRate = true
				h.Rate = data[offset]
			case CHANNEL:
				h.HasChannel = true
				h.ChannelFreq = binary.LittleEndian.Uint16(data[offset:])
				h.ChannelFlags = binary.LittleEndian.Uint16(data[offset+2:])
			case DBM_ANTSIGNAL:
				if isExtended && antennaIdx < MaxAntennas {
					h.Antennas[antennaIdx].DBMSignal = int8(data[offset])
					h.Antennas[antennaIdx].Valid = true
				} else {
					h.HasDBMSignal = true
					h.DBMSignal = int8(data[offset])
				}
			case DBM_ANTNOISE:
				if isExtended && antennaIdx < MaxAntennas {
					h.Antennas[antennaIdx].DBMNoise = int8(data[offset])
				} else {
					h.HasDBMNoise = true
					h.DBMNoise = int8(data[offset])
				}
			case ANTENNA:
				if isExtended && antennaIdx < MaxAntennas {
					h.Antennas[antennaIdx].Antenna = data[offset]
				} else {
					h.HasAntenna = true
					h.Antenna = data[offset]
				}
			case TX_FLAGS:
				h.HasTXFlags = true
				h.TXFlags = binary.LittleEndian.Uint16(data[offset:])
			case MCS:
				h.HasMCS = true
				h.MCSKnown = data[offset]
				h.MCSFlags = data[offset+1]
				h.MCSIndex = data[offset+2]
			case VHT:
				h.HasVHT = true
				h.VHTKnown = binary.LittleEndian.Uint16(data[offset:])
				h.VHTFlags = data[offset+2]
				h.VHTBandwidth = data[offset+3]
				copy(h.VHTMCSNSS[:], data[offset+4:offset+8])
				h.VHTCoding = data[offset+8]
				h.VHTGroupID = data[offset+9]
				h.VHTPartialAID = binary.LittleEndian.Uint16(data[offset+10:])
			}

			offset += size
		}

		// Move to next antenna after processing each extended bitmap
		if isExtended {
			antennaIdx++
		}
	}

	// Count valid antennas
	for i := 0; i < MaxAntennas; i++ {
		if h.Antennas[i].Valid {
			h.AntennaCount = i + 1
		}
	}

	return h, int(length), nil
}

// Bandwidth returns the bandwidth in MHz.
func (h *Header) Bandwidth() int {
	if h.HasVHT {
		switch h.VHTBandwidth {
		case VHT_BW_20:
			return 20
		case VHT_BW_40:
			return 40
		case VHT_BW_80:
			return 80
		case VHT_BW_160:
			return 160
		}
	}
	if h.HasMCS {
		switch h.MCSFlags & MCS_BW_MASK {
		case MCS_BW_20, MCS_BW_20L, MCS_BW_20U:
			return 20
		case MCS_BW_40:
			return 40
		}
	}
	return 20 // Default
}

// ShortGI returns true if short guard interval is used.
func (h *Header) ShortGI() bool {
	if h.HasVHT {
		return h.VHTFlags&VHT_FLAG_SGI != 0
	}
	if h.HasMCS {
		return h.MCSFlags&MCS_SGI != 0
	}
	return false
}

// LDPC returns true if LDPC coding is used.
func (h *Header) LDPC() bool {
	if h.HasVHT {
		return h.VHTCoding&0x01 != 0 // LDPC user 0
	}
	if h.HasMCS {
		return h.MCSFlags&MCS_FEC_LDPC != 0
	}
	return false
}

// STBC returns the number of STBC streams.
func (h *Header) STBC() int {
	if h.HasVHT {
		if h.VHTFlags&VHT_FLAG_STBC != 0 {
			return 1
		}
		return 0
	}
	if h.HasMCS {
		return int((h.MCSFlags & MCS_STBC_MASK) >> MCS_STBC_SHIFT)
	}
	return 0
}

// HasFCS returns true if the frame includes FCS at the end (4 bytes).
func (h *Header) HasFCS() bool {
	return h.HasFlags && (h.Flags&FLAGS_FCS != 0)
}

// HasBadFCS returns true if the frame failed FCS check.
func (h *Header) HasBadFCS() bool {
	return h.HasFlags && (h.Flags&FLAGS_BADFCS != 0)
}

// GetMCSIndex returns the MCS index.
func (h *Header) GetMCSIndex() int {
	if h.HasVHT {
		// VHT MCS is in MCSNSS[0], high nibble
		return int((h.VHTMCSNSS[0] >> 4) & 0x0F)
	}
	if h.HasMCS {
		return int(h.MCSIndex)
	}
	return 0
}

// GetNSS returns the number of spatial streams (VHT only).
func (h *Header) GetNSS() int {
	if h.HasVHT {
		return int(h.VHTMCSNSS[0] & 0x0F)
	}
	return 1
}

// IsSelfInjected returns true if this packet was injected by us (has TX_FLAGS).
// Self-injected packets should be ignored to avoid processing our own transmissions.
func (h *Header) IsSelfInjected() bool {
	return h.HasTXFlags
}
