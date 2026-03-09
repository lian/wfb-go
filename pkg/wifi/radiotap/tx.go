package radiotap

import "encoding/binary"

// TXHeader builds radiotap headers for packet injection.
type TXHeader struct {
	STBC      uint8 // 0, 1, 2, or 3
	LDPC      bool
	ShortGI   bool
	Bandwidth uint8 // 20, 40, 80
	MCSIndex  uint8
	VHTMode   bool  // Use VHT (802.11ac) instead of HT (802.11n)
	VHTNSS    uint8 // VHT spatial streams (1-4)
}

// DefaultTXHeader returns a default TX header for HT mode.
func DefaultTXHeader() *TXHeader {
	return &TXHeader{
		STBC:      0,
		LDPC:      false,
		ShortGI:   false,
		Bandwidth: 20,
		MCSIndex:  1,
		VHTMode:   false,
		VHTNSS:    1,
	}
}

// Build constructs the radiotap header bytes.
func (t *TXHeader) Build() []byte {
	if t.VHTMode {
		return t.buildVHT()
	}
	return t.buildHT()
}

// buildHT builds an HT (802.11n) radiotap header.
// Total size: 13 bytes
func (t *TXHeader) buildHT() []byte {
	// Radiotap header for HT:
	// version (1) + pad (1) + length (2) + present (4) + tx_flags (2) + mcs (3) = 13 bytes
	hdr := make([]byte, 13)

	// Version
	hdr[0] = 0x00

	// Pad
	hdr[1] = 0x00

	// Length (little-endian)
	binary.LittleEndian.PutUint16(hdr[2:4], 13)

	// Present flags: TX_FLAGS (bit 15) + MCS (bit 19)
	// 0x00080000 = MCS, 0x00008000 = TX_FLAGS
	present := uint32(1<<TX_FLAGS) | uint32(1<<MCS)
	binary.LittleEndian.PutUint32(hdr[4:8], present)

	// TX_FLAGS: NOACK (0x0008)
	binary.LittleEndian.PutUint16(hdr[8:10], TX_FLAGS_NOACK)

	// MCS field: known (1) + flags (1) + mcs_index (1)
	known := uint8(MCS_HAVE_MCS | MCS_HAVE_BW | MCS_HAVE_GI | MCS_HAVE_STBC | MCS_HAVE_FEC)
	hdr[10] = known

	flags := uint8(0)
	switch t.Bandwidth {
	case 40:
		flags |= MCS_BW_40
	default:
		flags |= MCS_BW_20
	}
	if t.ShortGI {
		flags |= MCS_SGI
	}
	if t.LDPC {
		flags |= MCS_FEC_LDPC
	}
	flags |= (t.STBC << MCS_STBC_SHIFT) & MCS_STBC_MASK
	hdr[11] = flags

	hdr[12] = t.MCSIndex

	return hdr
}

// buildVHT builds a VHT (802.11ac) radiotap header.
// Total size: 22 bytes
func (t *TXHeader) buildVHT() []byte {
	// Radiotap header for VHT:
	// version (1) + pad (1) + length (2) + present (4) + tx_flags (2) + vht (12) = 22 bytes
	hdr := make([]byte, 22)

	// Version
	hdr[0] = 0x00

	// Pad
	hdr[1] = 0x00

	// Length (little-endian)
	binary.LittleEndian.PutUint16(hdr[2:4], 22)

	// Present flags: TX_FLAGS (bit 15) + VHT (bit 21)
	present := uint32(1<<TX_FLAGS) | uint32(1<<VHT)
	binary.LittleEndian.PutUint32(hdr[4:8], present)

	// TX_FLAGS: NOACK (0x0008)
	binary.LittleEndian.PutUint16(hdr[8:10], TX_FLAGS_NOACK)

	// VHT field (12 bytes):
	// known (2) + flags (1) + bandwidth (1) + mcs_nss[4] (4) + coding (1) + group_id (1) + partial_aid (2)

	// Known: STBC, GI, bandwidth
	known := uint16(VHT_KNOWN_STBC | VHT_KNOWN_GI | VHT_KNOWN_BANDWIDTH)
	binary.LittleEndian.PutUint16(hdr[10:12], known)

	// Flags
	flags := uint8(0)
	if t.STBC > 0 {
		flags |= VHT_FLAG_STBC
	}
	if t.ShortGI {
		flags |= VHT_FLAG_SGI
	}
	hdr[12] = flags

	// Bandwidth
	switch t.Bandwidth {
	case 40:
		hdr[13] = VHT_BW_40
	case 80:
		hdr[13] = VHT_BW_80
	case 160:
		hdr[13] = VHT_BW_160
	default:
		hdr[13] = VHT_BW_20
	}

	// MCS_NSS[0]: MCS in high nibble, NSS in low nibble
	nss := t.VHTNSS
	if nss == 0 {
		nss = 1
	}
	hdr[14] = (t.MCSIndex << 4) | (nss & 0x0F)
	hdr[15] = 0 // MCS_NSS[1]
	hdr[16] = 0 // MCS_NSS[2]
	hdr[17] = 0 // MCS_NSS[3]

	// Coding: LDPC for user 0
	coding := uint8(0)
	if t.LDPC {
		coding |= 0x01
	}
	hdr[18] = coding

	// Group ID (not used)
	hdr[19] = 0

	// Partial AID (not used)
	hdr[20] = 0
	hdr[21] = 0

	return hdr
}

// Size returns the size of the radiotap header.
func (t *TXHeader) Size() int {
	if t.VHTMode {
		return 22
	}
	return 13
}
