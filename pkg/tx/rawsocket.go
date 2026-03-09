package tx

import (
	"errors"
	"net"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/lian/wfb-go/pkg/wifi/frame"
	"github.com/lian/wfb-go/pkg/wifi/radiotap"
)

const (
	// SOL_PACKET is the socket option level for packet sockets
	SOL_PACKET = 263

	// PACKET_QDISC_BYPASS bypasses the kernel qdisc
	PACKET_QDISC_BYPASS = 20
)

var (
	ErrInterfaceNotFound = errors.New("tx: interface not found")
	ErrSocketCreate      = errors.New("tx: failed to create socket")
	ErrSocketBind        = errors.New("tx: failed to bind socket")
)

// LatencyStats holds injection latency statistics per antenna.
type LatencyStats struct {
	PacketsInjected uint64
	PacketsDropped  uint64
	LatencyMin      uint64 // microseconds
	LatencyMax      uint64 // microseconds
	LatencySum      uint64 // for computing average
}

// RawSocketInjector injects packets via raw PF_PACKET sockets.
type RawSocketInjector struct {
	mu           sync.Mutex
	sockfds      []int
	ifIndices    []int
	radiotapHdr  *radiotap.TXHeader
	ieee80211Hdr *frame.Header
	channelID    uint32
	seq          uint16
	currentWlan  int
	useQdisc     bool
	retries      int
	retryDelay   time.Duration

	// Per-antenna latency stats (indexed by wlan idx)
	latencyStats []LatencyStats
}

// RawSocketConfig configures the raw socket injector.
type RawSocketConfig struct {
	Interfaces []string           // WiFi interfaces (e.g., "wlan0", "wlan1")
	ChannelID  uint32             // Channel ID for 802.11 header
	Radiotap   *radiotap.TXHeader // Radiotap header settings
	UseQdisc   bool               // If false, bypass qdisc for lower latency
	Fwmark     uint32             // Packet mark for tc qdisc rules (only when UseQdisc=true)
	Retries    int                // Number of injection retries on ENOBUFS
	RetryDelay time.Duration      // Delay between retries
}

// NewRawSocketInjector creates a new raw socket injector.
func NewRawSocketInjector(cfg RawSocketConfig) (*RawSocketInjector, error) {
	if len(cfg.Interfaces) == 0 {
		return nil, errors.New("tx: no interfaces specified")
	}

	if cfg.Radiotap == nil {
		cfg.Radiotap = radiotap.DefaultTXHeader()
	}

	if cfg.Retries <= 0 {
		cfg.Retries = 3
	}

	if cfg.RetryDelay <= 0 {
		cfg.RetryDelay = 100 * time.Microsecond
	}

	inj := &RawSocketInjector{
		radiotapHdr: cfg.Radiotap,
		channelID:   cfg.ChannelID,
		useQdisc:    cfg.UseQdisc,
		retries:     cfg.Retries,
		retryDelay:  cfg.RetryDelay,
	}

	// Create 802.11 header
	inj.ieee80211Hdr = frame.DefaultHeader()
	inj.ieee80211Hdr.SetChannelID(cfg.ChannelID)

	// Open sockets for each interface
	for _, ifname := range cfg.Interfaces {
		fd, ifIndex, err := openRawSocket(ifname, !cfg.UseQdisc, cfg.Fwmark)
		if err != nil {
			// Close any already opened sockets
			inj.Close()
			return nil, err
		}
		inj.sockfds = append(inj.sockfds, fd)
		inj.ifIndices = append(inj.ifIndices, ifIndex)
		inj.latencyStats = append(inj.latencyStats, LatencyStats{})
	}

	return inj, nil
}

// openRawSocket opens a PF_PACKET raw socket bound to the interface.
func openRawSocket(ifname string, bypassQdisc bool, fwmark uint32) (int, int, error) {
	// Create PF_PACKET socket
	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW, 0)
	if err != nil {
		return -1, 0, err
	}

	// Get interface index
	iface, err := net.InterfaceByName(ifname)
	if err != nil {
		syscall.Close(fd)
		return -1, 0, err
	}

	// Bypass qdisc for lower latency
	if bypassQdisc {
		optval := 1
		_, _, errno := syscall.Syscall6(
			syscall.SYS_SETSOCKOPT,
			uintptr(fd),
			uintptr(SOL_PACKET),
			uintptr(PACKET_QDISC_BYPASS),
			uintptr(unsafe.Pointer(&optval)),
			unsafe.Sizeof(optval),
			0,
		)
		if errno != 0 {
			// Non-fatal: some kernels don't support this
			// log.Printf("Warning: PACKET_QDISC_BYPASS not supported: %v", errno)
		}
	} else if fwmark != 0 {
		// Set SO_MARK for tc qdisc rules when using qdisc
		if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_MARK, int(fwmark)); err != nil {
			syscall.Close(fd)
			return -1, 0, err
		}
	}

	// Bind to interface
	sll := syscall.SockaddrLinklayer{
		Protocol: 0,
		Ifindex:  iface.Index,
	}
	if err := syscall.Bind(fd, &sll); err != nil {
		syscall.Close(fd)
		return -1, 0, err
	}

	return fd, iface.Index, nil
}

// Inject sends a WFB packet (without radiotap/802.11 headers).
func (r *RawSocketInjector) Inject(data []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.sockfds) == 0 {
		return ErrNoInjector
	}

	// Build radiotap header
	rtHdr := r.radiotapHdr.Build()

	// Update 802.11 sequence number
	r.seq++
	r.ieee80211Hdr.SetSequence(r.seq, 0) // seq, frag=0
	ieee80211Bytes := r.ieee80211Hdr.Marshal()

	// Build complete frame: radiotap + 802.11 + payload
	frame := make([]byte, len(rtHdr)+len(ieee80211Bytes)+len(data))
	copy(frame[0:], rtHdr)
	copy(frame[len(rtHdr):], ieee80211Bytes)
	copy(frame[len(rtHdr)+len(ieee80211Bytes):], data)

	// Inject on current interface with latency tracking
	wlanIdx := r.currentWlan
	fd := r.sockfds[wlanIdx]

	startTime := time.Now()
	var err error
	for i := 0; i < r.retries; i++ {
		_, err = syscall.Write(fd, frame)
		if err == nil {
			break
		}

		// Retry on ENOBUFS (driver buffer full)
		if errors.Is(err, syscall.ENOBUFS) {
			time.Sleep(r.retryDelay)
			continue
		}

		break
	}

	// Record latency stats
	latencyUs := uint64(time.Since(startTime).Microseconds())
	stats := &r.latencyStats[wlanIdx]

	if err == nil {
		stats.PacketsInjected++
		// Update min/max
		if stats.PacketsInjected == 1 {
			stats.LatencyMin = latencyUs
			stats.LatencyMax = latencyUs
		} else {
			if latencyUs < stats.LatencyMin {
				stats.LatencyMin = latencyUs
			}
			if latencyUs > stats.LatencyMax {
				stats.LatencyMax = latencyUs
			}
		}
		stats.LatencySum += latencyUs
	} else {
		stats.PacketsDropped++
	}

	return err
}

// SelectInterface selects which interface to use for injection.
func (r *RawSocketInjector) SelectInterface(idx int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if idx >= 0 && idx < len(r.sockfds) {
		r.currentWlan = idx
	}
}

// SetRadiotap updates the radiotap header settings.
func (r *RawSocketInjector) SetRadiotap(hdr *radiotap.TXHeader) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.radiotapHdr = hdr
}

// GetRadiotap returns the current radiotap header settings.
func (r *RawSocketInjector) GetRadiotap() *radiotap.TXHeader {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Return a copy
	cpy := *r.radiotapHdr
	return &cpy
}

// Close closes all sockets.
func (r *RawSocketInjector) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var lastErr error
	for _, fd := range r.sockfds {
		if err := syscall.Close(fd); err != nil {
			lastErr = err
		}
	}
	r.sockfds = nil
	r.ifIndices = nil

	return lastErr
}

// InterfaceCount returns the number of configured interfaces.
func (r *RawSocketInjector) InterfaceCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sockfds)
}

// GetLatencyStats returns a copy of the latency stats and resets them.
func (r *RawSocketInjector) GetLatencyStats() []LatencyStats {
	r.mu.Lock()
	defer r.mu.Unlock()

	result := make([]LatencyStats, len(r.latencyStats))
	copy(result, r.latencyStats)

	// Reset stats for next interval
	for i := range r.latencyStats {
		r.latencyStats[i] = LatencyStats{}
	}

	return result
}

// GetLatencyStatsNoReset returns a copy of the latency stats without resetting.
func (r *RawSocketInjector) GetLatencyStatsNoReset() []LatencyStats {
	r.mu.Lock()
	defer r.mu.Unlock()

	result := make([]LatencyStats, len(r.latencyStats))
	copy(result, r.latencyStats)
	return result
}

// MirrorInjector wraps an injector to send packets on all interfaces.
type MirrorInjector struct {
	raw *RawSocketInjector
}

// NewMirrorInjector creates an injector that sends on all interfaces.
func NewMirrorInjector(raw *RawSocketInjector) *MirrorInjector {
	return &MirrorInjector{raw: raw}
}

// Inject sends the packet on all interfaces.
func (m *MirrorInjector) Inject(data []byte) error {
	m.raw.mu.Lock()
	defer m.raw.mu.Unlock()

	if len(m.raw.sockfds) == 0 {
		return ErrNoInjector
	}

	// Build radiotap header
	rtHdr := m.raw.radiotapHdr.Build()

	// Update 802.11 sequence number
	m.raw.seq++
	m.raw.ieee80211Hdr.SetSequence(m.raw.seq, 0) // seq, frag=0
	ieee80211Bytes := m.raw.ieee80211Hdr.Marshal()

	// Build complete frame
	frame := make([]byte, len(rtHdr)+len(ieee80211Bytes)+len(data))
	copy(frame[0:], rtHdr)
	copy(frame[len(rtHdr):], ieee80211Bytes)
	copy(frame[len(rtHdr)+len(ieee80211Bytes):], data)

	// Send on all interfaces
	var lastErr error
	for _, fd := range m.raw.sockfds {
		for i := 0; i < m.raw.retries; i++ {
			_, err := syscall.Write(fd, frame)
			if err == nil {
				break
			}
			if errors.Is(err, syscall.ENOBUFS) {
				time.Sleep(m.raw.retryDelay)
				continue
			}
			lastErr = err
			break
		}
	}

	return lastErr
}
