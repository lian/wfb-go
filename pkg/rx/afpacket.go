//go:build linux

package rx

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/gopacket/gopacket"
	"golang.org/x/net/bpf"
	"golang.org/x/sys/unix"
)

// AFPacket configuration constants
const (
	// Default ring buffer settings (32 blocks * 128KB = 4MB total)
	defaultBlockSize    = 1 << 17 // 128KB per block
	defaultBlockNumbers = 32      // 32 blocks
	defaultFrameSize    = 1 << 16 // 64KB max frame

	// Block status offset in TpacketBlockDesc
	blockStatusOffset = 8

	// Poll timeout in milliseconds
	pollTimeoutMs = 100
)

// tpacketAlign aligns to TPACKET_ALIGNMENT (16 bytes)
func tpacketAlign(x int) int {
	return (x + 15) &^ 15
}

// AFPacketSource captures packets using AF_PACKET with TPACKET_V3 mmap ring buffer.
// This provides low-latency capture with Retire_blk_tov=1 for quick block delivery.
type AFPacketSource struct {
	mu     sync.Mutex
	fd     int
	ring   []byte
	closed uint32

	// Ring buffer parameters
	blockSize    int
	blockNumbers int
	blockIdx     int // Current block index

	// Poll file descriptor
	pollFds  []unix.PollFd
	ifaceIdx int // Interface index for error reporting

	// Packet cache (multiple packets per block)
	cache []capturedPkt

	// Pre-allocated buffer for packet data (reused per block, zero allocations)
	packetBuf    []byte
	packetBufOff int // Current offset into packetBuf
}

type capturedPkt struct {
	data []byte
	ci   gopacket.CaptureInfo
}

// AFPacketConfig holds configuration for AF_PACKET capture.
type AFPacketConfig struct {
	Interface    string
	Promiscuous  bool
	BlockSize    int // Ring buffer block size (default: 128KB)
	BlockNumbers int // Number of blocks (default: 32)
	BPFFilter    []bpf.RawInstruction // Optional BPF filter (runs in kernel)
}

// NewAFPacketSource creates a new AF_PACKET capture source.
func NewAFPacketSource(cfg AFPacketConfig) (*AFPacketSource, error) {
	// Get interface
	iface, err := net.InterfaceByName(cfg.Interface)
	if err != nil {
		return nil, fmt.Errorf("interface %s: %w", cfg.Interface, err)
	}

	// Check interface is up
	if iface.Flags&net.FlagUp == 0 {
		return nil, fmt.Errorf("interface %s is not up", cfg.Interface)
	}

	// Create AF_PACKET socket
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW, int(htons(unix.ETH_P_ALL)))
	if err != nil {
		return nil, fmt.Errorf("socket: %w", err)
	}

	success := false
	defer func() {
		if !success {
			unix.Close(fd)
		}
	}()

	// Set TPACKET_V3
	if err := unix.SetsockoptInt(fd, unix.SOL_PACKET, unix.PACKET_VERSION, unix.TPACKET_V3); err != nil {
		return nil, fmt.Errorf("set TPACKET_V3: %w", err)
	}

	// Set up ring buffer parameters
	blockSize := cfg.BlockSize
	if blockSize <= 0 {
		blockSize = defaultBlockSize
	}
	blockNumbers := cfg.BlockNumbers
	if blockNumbers <= 0 {
		blockNumbers = defaultBlockNumbers
	}

	// Calculate frame size
	frameSize := tpacketAlign(int(unix.SizeofTpacket3Hdr)+16) + tpacketAlign(65535)
	if frameSize > blockSize {
		frameSize = blockSize
	}
	framesPerBlock := blockSize / frameSize
	frameNumbers := blockNumbers * framesPerBlock

	// Configure ring buffer with low-latency settings
	req := unix.TpacketReq3{
		Block_size:     uint32(blockSize),
		Block_nr:       uint32(blockNumbers),
		Frame_size:     uint32(frameSize),
		Frame_nr:       uint32(frameNumbers),
		Retire_blk_tov: 1, // 1ms timeout - retire blocks quickly for low latency
	}

	if err := unix.SetsockoptTpacketReq3(fd, unix.SOL_PACKET, unix.PACKET_RX_RING, &req); err != nil {
		return nil, fmt.Errorf("set ring buffer: %w", err)
	}

	// Mmap the ring buffer
	totalSize := blockSize * blockNumbers
	ring, err := unix.Mmap(fd, 0, totalSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return nil, fmt.Errorf("mmap: %w", err)
	}

	// Bind to interface
	sa := unix.SockaddrLinklayer{
		Protocol: htons(unix.ETH_P_ALL),
		Ifindex:  iface.Index,
	}
	if err := unix.Bind(fd, &sa); err != nil {
		unix.Munmap(ring)
		return nil, fmt.Errorf("bind: %w", err)
	}

	// Set promiscuous mode if requested
	if cfg.Promiscuous {
		mreq := unix.PacketMreq{
			Ifindex: int32(iface.Index),
			Type:    unix.PACKET_MR_PROMISC,
		}
		if err := unix.SetsockoptPacketMreq(fd, unix.SOL_PACKET, unix.PACKET_ADD_MEMBERSHIP, &mreq); err != nil {
			unix.Munmap(ring)
			return nil, fmt.Errorf("set promiscuous: %w", err)
		}
	}

	// Attach BPF filter if provided (runs in kernel for efficiency)
	if len(cfg.BPFFilter) > 0 {
		prog := unix.SockFprog{
			Len:    uint16(len(cfg.BPFFilter)),
			Filter: (*unix.SockFilter)(unsafe.Pointer(&cfg.BPFFilter[0])),
		}
		if err := unix.SetsockoptSockFprog(fd, unix.SOL_SOCKET, unix.SO_ATTACH_FILTER, &prog); err != nil {
			unix.Munmap(ring)
			return nil, fmt.Errorf("attach BPF filter: %w", err)
		}
	}

	success = true
	return &AFPacketSource{
		fd:           fd,
		ring:         ring,
		blockSize:    blockSize,
		blockNumbers: blockNumbers,
		pollFds:      []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN | unix.POLLERR | unix.POLLNVAL}},
		ifaceIdx:     iface.Index,
		packetBuf:    make([]byte, blockSize), // Pre-allocate once, reuse per block
	}, nil
}

// ReadPacket reads the next packet from the capture.
func (s *AFPacketSource) ReadPacket() ([]byte, gopacket.CaptureInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if atomic.LoadUint32(&s.closed) != 0 {
		return nil, gopacket.CaptureInfo{}, errors.New("source closed")
	}

	// Return cached packet if available
	if len(s.cache) > 0 {
		pkt := s.cache[0]
		s.cache = s.cache[1:]
		return pkt.data, pkt.ci, nil
	}

	// Read new block
	for {
		if atomic.LoadUint32(&s.closed) != 0 {
			return nil, gopacket.CaptureInfo{}, errors.New("source closed")
		}

		// Check if current block is ready
		blockBase := s.blockIdx * s.blockSize
		blockStatus := s.getBlockStatus(blockBase)

		if blockStatus&unix.TP_STATUS_USER != 0 {
			// Block is ready - process packets
			packets, err := s.processBlock(blockBase)
			if err != nil {
				return nil, gopacket.CaptureInfo{}, err
			}

			// Return block to kernel
			s.setBlockStatus(blockBase, unix.TP_STATUS_KERNEL)

			// Advance to next block
			s.blockIdx = (s.blockIdx + 1) % s.blockNumbers

			if len(packets) > 0 {
				s.cache = packets[1:]
				return packets[0].data, packets[0].ci, nil
			}
			continue
		}

		// Block not ready - poll
		n, err := unix.Poll(s.pollFds, pollTimeoutMs)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return nil, gopacket.CaptureInfo{}, fmt.Errorf("poll: %w", err)
		}
		if n == 0 {
			// Timeout - check again
			continue
		}

		// Check for poll errors
		revents := s.pollFds[0].Revents
		if revents&unix.POLLNVAL != 0 {
			return nil, gopacket.CaptureInfo{}, errors.New("socket closed (POLLNVAL)")
		}
		if revents&unix.POLLERR != 0 {
			// Check socket error
			sockErr, err := unix.GetsockoptInt(s.fd, unix.SOL_SOCKET, unix.SO_ERROR)
			if err != nil {
				return nil, gopacket.CaptureInfo{}, fmt.Errorf("poll error, getsockopt failed: %w", err)
			}
			if sockErr == int(unix.ENETDOWN) {
				return nil, gopacket.CaptureInfo{}, errors.New("interface is down")
			}
			return nil, gopacket.CaptureInfo{}, fmt.Errorf("socket error: %d", sockErr)
		}
	}
}

// processBlock extracts all packets from a ready block.
func (s *AFPacketSource) processBlock(blockBase int) ([]capturedPkt, error) {
	block := s.ring[blockBase : blockBase+s.blockSize]

	// Parse block header (TpacketBlockDesc)
	// Offset 0: version (4 bytes)
	// Offset 4: offset_to_priv (4 bytes)
	// Offset 8: TpacketHdrV1 starts
	//   - block_status (4 bytes)
	//   - num_pkts (4 bytes)
	//   - offset_to_first_pkt (4 bytes)
	//   - ... more fields

	numPkts := binary.LittleEndian.Uint32(block[8+4:])
	offsetToFirst := binary.LittleEndian.Uint32(block[8+8:])

	if numPkts == 0 {
		return nil, nil
	}

	// Reset packet buffer offset - safe because cache is always empty when we process a new block
	s.packetBufOff = 0

	packets := make([]capturedPkt, 0, numPkts)
	offset := int(offsetToFirst)

	for i := uint32(0); i < numPkts; i++ {
		if offset >= s.blockSize {
			break
		}

		pkt := block[offset:]
		if len(pkt) < int(unix.SizeofTpacket3Hdr) {
			break
		}

		// Parse Tpacket3Hdr
		hdr := (*unix.Tpacket3Hdr)(unsafe.Pointer(&pkt[0]))

		// Extract packet data
		macOffset := uint32(hdr.Mac)
		snaplen := hdr.Snaplen

		if int(macOffset)+int(snaplen) > len(pkt) {
			break
		}

		// Copy packet data into pre-allocated buffer (required since ring buffer can be reused)
		// This avoids per-packet allocations which reduce GC pressure
		if s.packetBufOff+int(snaplen) > len(s.packetBuf) {
			// Buffer full (shouldn't happen with proper sizing, but be safe)
			// Fall back to allocation
			data := make([]byte, snaplen)
			copy(data, pkt[macOffset:macOffset+snaplen])
			packets = append(packets, capturedPkt{
				data: data,
				ci: gopacket.CaptureInfo{
					Timestamp:     time.Unix(int64(hdr.Sec), int64(hdr.Nsec)),
					CaptureLength: int(snaplen),
					Length:        int(hdr.Len),
				},
			})
		} else {
			// Use pre-allocated buffer
			data := s.packetBuf[s.packetBufOff : s.packetBufOff+int(snaplen)]
			copy(data, pkt[macOffset:macOffset+snaplen])
			s.packetBufOff += int(snaplen)

			packets = append(packets, capturedPkt{
				data: data,
				ci: gopacket.CaptureInfo{
					Timestamp:     time.Unix(int64(hdr.Sec), int64(hdr.Nsec)),
					CaptureLength: int(snaplen),
					Length:        int(hdr.Len),
				},
			})
		}

		// Move to next packet
		if hdr.Next_offset == 0 {
			break
		}
		offset += int(hdr.Next_offset)
	}

	return packets, nil
}

// getBlockStatus reads the block status from the ring buffer.
func (s *AFPacketSource) getBlockStatus(blockBase int) uint32 {
	return atomic.LoadUint32((*uint32)(unsafe.Pointer(&s.ring[blockBase+blockStatusOffset])))
}

// setBlockStatus writes the block status to the ring buffer.
func (s *AFPacketSource) setBlockStatus(blockBase int, status uint32) {
	atomic.StoreUint32((*uint32)(unsafe.Pointer(&s.ring[blockBase+blockStatusOffset])), status)
}

// Close releases all resources.
func (s *AFPacketSource) Close() error {
	if !atomic.CompareAndSwapUint32(&s.closed, 0, 1) {
		return nil // Already closed
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ring != nil {
		unix.Munmap(s.ring)
		s.ring = nil
	}
	if s.fd != 0 {
		unix.Close(s.fd)
		s.fd = 0
	}

	return nil
}

// LinkType returns the link type (DLT_EN10MB for Ethernet).
func (s *AFPacketSource) LinkType() uint32 {
	return 1 // DLT_EN10MB
}

// htons converts a uint16 from host to network byte order.
func htons(v uint16) uint16 {
	return (v << 8) | (v >> 8)
}

// BPF opcodes for building filters
const (
	bpfLdB    = 0x30 // ldb [k] - load byte from packet
	bpfLdH    = 0x28 // ldh [k] - load half-word (16-bit)
	bpfLd     = 0x20 // ld [k] - load word (32-bit)
	bpfLdHInd = 0x48 // ldh [x+k] - load half-word with index
	bpfLdInd  = 0x40 // ld [x+k] - load word with index
	bpfTax    = 0x07 // tax - transfer A to X
	bpfAddX   = 0x0c // add x - A += X
	bpfLsh    = 0x64 // lsh #k - A <<= k
	bpfJeq    = 0x15 // jeq #k, jt, jf - jump if equal
	bpfRet    = 0x06 // ret #k - return
)

// BuildWFBFilter creates a BPF filter for WFB packets with a specific channel ID.
// Handles variable-length radiotap header in monitor mode.
// Matches: packet[rtap_len+10:2]==0x5742 && packet[rtap_len+12:4]==channelID
// Use this for dedicated mode (one channel per socket).
func BuildWFBFilter(channelID uint32) []bpf.RawInstruction {
	// BPF program for monitor mode (radiotap + 802.11):
	// Radiotap header length is at offset 2-3 (LITTLE-ENDIAN)
	// BPF ldh loads as big-endian, so we manually convert:
	//   ldb [3]; lsh #8; tax; ldb [2]; add x; tax  -> X = LE16 radiotap length
	// WFB magic is at rtap_len + 10 (transmitter MAC bytes 0-1)
	// Channel ID is at rtap_len + 12 (transmitter MAC bytes 2-5)
	return []bpf.RawInstruction{
		// Load radiotap length (little-endian at offset 2-3)
		{Op: bpfLdB, K: 3},   // 0: ldb [3] - A = high byte
		{Op: bpfLsh, K: 8},   // 1: lsh #8 - A <<= 8
		{Op: bpfTax},         // 2: tax - X = high << 8
		{Op: bpfLdB, K: 2},   // 3: ldb [2] - A = low byte
		{Op: bpfAddX},        // 4: add x - A = low + (high << 8) = LE16 value
		{Op: bpfTax},         // 5: tax - X = radiotap length

		// Check WFB magic at rtap_len + 10
		{Op: bpfLdHInd, K: 10},          // 6: ldh [x+10] - load WFB magic
		{Op: bpfJeq, Jf: 3, K: 0x5742},  // 7: jeq #0x5742, next, drop (skip 3 -> inst 11)

		// Check channel ID at rtap_len + 12
		{Op: bpfLdInd, K: 12},                       // 8: ld [x+12] - load channel ID
		{Op: bpfJeq, Jf: 1, K: uint32(channelID)},   // 9: jeq #channelID, accept, drop (skip 1 -> inst 11)

		{Op: bpfRet, K: 0xFFFFFFFF}, // 10: ret #-1 (accept)
		{Op: bpfRet, K: 0},          // 11: ret #0 (drop)
	}
}

// BuildWFBMagicFilter creates a BPF filter for all WFB packets (any channel).
// Handles variable-length radiotap header in monitor mode.
// Matches: packet[rtap_len+10:2]==0x5742
// Use this for shared mode (multiple channels per socket, demux in userspace).
func BuildWFBMagicFilter() []bpf.RawInstruction {
	// BPF program for monitor mode (radiotap + 802.11):
	// Same LE16 conversion as above for radiotap length
	return []bpf.RawInstruction{
		// Load radiotap length (little-endian at offset 2-3)
		{Op: bpfLdB, K: 3},   // 0: ldb [3] - A = high byte
		{Op: bpfLsh, K: 8},   // 1: lsh #8 - A <<= 8
		{Op: bpfTax},         // 2: tax - X = high << 8
		{Op: bpfLdB, K: 2},   // 3: ldb [2] - A = low byte
		{Op: bpfAddX},        // 4: add x - A = low + (high << 8) = LE16 value
		{Op: bpfTax},         // 5: tax - X = radiotap length

		// Check WFB magic at rtap_len + 10
		{Op: bpfLdHInd, K: 10},          // 6: ldh [x+10] - load WFB magic
		{Op: bpfJeq, Jf: 1, K: 0x5742},  // 7: jeq #0x5742, accept, drop (skip 1 -> inst 9)

		{Op: bpfRet, K: 0xFFFFFFFF}, // 8: ret #-1 (accept)
		{Op: bpfRet, K: 0},          // 9: ret #0 (drop)
	}
}
