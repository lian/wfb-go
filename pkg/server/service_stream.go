package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/lian/wfb-go/pkg/rx"
	"github.com/lian/wfb-go/pkg/server/mavlink"
	"github.com/lian/wfb-go/pkg/server/util"
	"github.com/lian/wfb-go/pkg/tx"
)

// StreamService is a unified service that handles all stream types:
// - TX only (video uplink)
// - RX only (video downlink)
// - Bidirectional (mavlink, tunnel, proxy)
//
// The behavior is determined by configuration, not by service type.
type StreamService struct {
	name string
	cfg  *ServiceConfig

	// TX components (one per wlan for antenna selection)
	transmitters []*tx.Transmitter
	injectors    []*tx.RawSocketInjector
	currentTX    int32 // atomic

	// RX component
	forwarder *rx.Forwarder

	// Packet aggregation (for mavlink/tunnel efficiency)
	aggregator *util.PacketAggregator

	// Mavlink parser (for serial port message framing)
	mavlinkParser *mavlink.Parser

	// Peer I/O - only one is active based on peer type
	udpConn     net.Conn       // UDP connect mode
	udpListen   net.PacketConn // UDP listen mode
	tunFd       *os.File       // TUN device
	tunReader   *tunReader     // Poll-based TUN reader for efficient shutdown
	serialFd    int            // Serial port fd
	tcpListener net.Listener   // TCP server
	tcpClients  []net.Conn     // Connected TCP clients

	// OSD mirroring (mavlink only)
	osdConn net.Conn // UDP connection to OSD

	// Mavlink TCP port (additional TCP server for QGC etc)
	mavlinkTCPListener net.Listener
	mavlinkTCPClients  []net.Conn

	// State
	lastSender net.Addr // Track sender for UDP listen reply
	lastRSSI   int8
	lastSNR    int8

	// RSSI injection (for mavlink)
	rssiBuilder  *mavlink.RadioStatusBuilder
	lastRXErrors uint16
	lastFECFixed uint16
	rssiFlags    uint8 // WFB flags for RADIO_STATUS

	// Mavlink message logging
	binLogger     *util.BinaryLogger
	mavlinkLogger *mavlink.Logger

	// Keepalive state (for tunnel mode)
	pktInSem  int32 // RX activity semaphore (reset to 2 on RX, decrement each interval)
	pktOutSem int32 // TX activity semaphore (reset to 1 on TX, decrement each interval)

	// Mavlink arm state tracking (for arm/disarm callbacks)
	mavlinkArmed int32 // 0=unknown, 1=disarmed, 2=armed

	stats   *ServiceStats
	stopCh  chan struct{}
	stopped atomic.Bool
	wg      sync.WaitGroup
	mu      sync.Mutex
}

// NewStreamService creates a unified stream service.
func NewStreamService(cfg *ServiceConfig) (*StreamService, error) {
	return &StreamService{
		name:   cfg.Name,
		cfg:    cfg,
		stats:  NewServiceStats(),
		stopCh: make(chan struct{}),
	}, nil
}

func (s *StreamService) Name() string {
	return s.name
}

func (s *StreamService) Type() ServiceType {
	return s.cfg.ServiceType
}

func (s *StreamService) Start(ctx context.Context) error {
	// Setup peer connection
	if err := s.setupPeer(); err != nil {
		return fmt.Errorf("setup peer: %w", err)
	}

	// Initialize RSSI builder for mavlink services
	if s.cfg.InjectRSSI && s.cfg.ServiceType == ServiceMavlink {
		s.rssiBuilder = mavlink.NewRadioStatusBuilder(s.cfg.MavlinkSysID, s.cfg.MavlinkCompID)
	}

	// Initialize mavlink message logging if configured
	if s.cfg.LogMessages && s.cfg.BinaryLogFile != "" && s.cfg.ServiceType == ServiceMavlink {
		if err := s.setupMavlinkLogger(); err != nil {
			s.cleanup()
			return fmt.Errorf("setup mavlink logger: %w", err)
		}
	}

	// Setup OSD mirroring if configured (mavlink only)
	if s.cfg.OSD != "" && s.cfg.ServiceType == ServiceMavlink {
		if err := s.setupOSD(); err != nil {
			s.cleanup()
			return fmt.Errorf("setup osd: %w", err)
		}
	}

	// Setup mavlink TCP port if configured (additional TCP server)
	if s.cfg.MavlinkTCPPort > 0 && s.cfg.ServiceType == ServiceMavlink {
		if err := s.setupMavlinkTCP(); err != nil {
			s.cleanup()
			return fmt.Errorf("setup mavlink tcp: %w", err)
		}
	}

	// Create channel IDs
	var rxChannelID, txChannelID uint32
	if s.cfg.StreamRX != nil {
		rxChannelID = MakeChannelID(s.cfg.LinkID, *s.cfg.StreamRX)
	}
	if s.cfg.StreamTX != nil {
		txChannelID = MakeChannelID(s.cfg.LinkID, *s.cfg.StreamTX)
	}

	// Start TX if configured
	if s.cfg.StreamTX != nil {
		if err := s.startTX(ctx, txChannelID); err != nil {
			s.cleanup()
			return fmt.Errorf("start tx: %w", err)
		}
	}

	// Start RX if configured
	if s.cfg.StreamRX != nil {
		if err := s.startRX(ctx, rxChannelID); err != nil {
			s.cleanup()
			return fmt.Errorf("start rx: %w", err)
		}
	}

	// Start TCP accept loop if TCP peer
	if s.tcpListener != nil {
		s.wg.Add(1)
		go s.tcpAcceptLoop(ctx)
	}

	// Start mavlink TCP accept loop if configured
	if s.mavlinkTCPListener != nil {
		s.wg.Add(1)
		go s.mavlinkTCPAcceptLoop(ctx)
	}

	return nil
}

func (s *StreamService) setupPeer() error {
	peer := s.cfg.Peer
	if peer == "" {
		return nil // No peer configured (stats-only service?)
	}

	switch {
	case strings.HasPrefix(peer, "connect://"):
		// UDP connect mode
		addr := strings.TrimPrefix(peer, "connect://")
		conn, err := net.Dial("udp", addr)
		if err != nil {
			return fmt.Errorf("dial udp %s: %w", addr, err)
		}
		if udp, ok := conn.(*net.UDPConn); ok {
			if s.cfg.SndBufSize > 0 {
				udp.SetWriteBuffer(s.cfg.SndBufSize)
			}
			if s.cfg.RcvBufSize > 0 {
				udp.SetReadBuffer(s.cfg.RcvBufSize)
			}
		}
		s.udpConn = conn
		log.Printf("[%s] UDP connected to %s", s.name, addr)

	case strings.HasPrefix(peer, "listen://"):
		// UDP listen mode
		addr := strings.TrimPrefix(peer, "listen://")
		conn, err := net.ListenPacket("udp", addr)
		if err != nil {
			return fmt.Errorf("listen udp %s: %w", addr, err)
		}
		if udp, ok := conn.(*net.UDPConn); ok {
			if s.cfg.RcvBufSize > 0 {
				udp.SetReadBuffer(s.cfg.RcvBufSize)
			}
			if s.cfg.SndBufSize > 0 {
				udp.SetWriteBuffer(s.cfg.SndBufSize)
			}
		}
		s.udpListen = conn
		log.Printf("[%s] UDP listening on %s", s.name, addr)

	case strings.HasPrefix(peer, "tcp://"):
		// TCP server for QGC etc
		addr := strings.TrimPrefix(peer, "tcp://")
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("listen tcp %s: %w", addr, err)
		}
		s.tcpListener = listener
		log.Printf("[%s] TCP listening on %s", s.name, addr)

	case strings.HasPrefix(peer, "serial:"):
		// Serial port: serial:/dev/ttyUSB0:115200
		parts := strings.Split(strings.TrimPrefix(peer, "serial:"), ":")
		if len(parts) < 2 {
			return fmt.Errorf("invalid serial format: %s", peer)
		}
		port := parts[0]
		baud, err := strconv.Atoi(parts[1])
		if err != nil {
			return fmt.Errorf("invalid baud rate: %w", err)
		}
		fd, err := openSerialPort(port, baud)
		if err != nil {
			return fmt.Errorf("open serial: %w", err)
		}
		s.serialFd = fd
		log.Printf("[%s] Serial %s @ %d", s.name, port, baud)

	case strings.HasPrefix(peer, "tun:"):
		// TUN device: tun:wfb-tun
		tunName := strings.TrimPrefix(peer, "tun:")
		fd, err := openTunDevice(tunName)
		if err != nil {
			return fmt.Errorf("open tun: %w", err)
		}
		s.tunFd = fd
		s.tunReader = newTunReader(fd)
		log.Printf("[%s] TUN device %s created", s.name, tunName)

		// Configure IP if specified
		if s.cfg.IfAddr != "" {
			mtu := 1445 - 2
			if err := configureTunDevice(tunName, s.cfg.IfAddr, mtu, s.cfg.DefaultRoute); err != nil {
				s.tunFd.Close()
				return fmt.Errorf("configure tun: %w", err)
			}
			log.Printf("[%s] TUN configured: %s", s.name, s.cfg.IfAddr)
		}

	default:
		return fmt.Errorf("unknown peer format: %s", peer)
	}

	return nil
}

// SetAntenna sets the current TX antenna.
// In mirror mode, this is a no-op since all antennas are always used.
func (s *StreamService) SetAntenna(wlanIdx uint8) {
	if s.cfg.Mirror {
		return // Mirror mode ignores antenna selection
	}
	if int(wlanIdx) < len(s.transmitters) {
		atomic.StoreInt32(&s.currentTX, int32(wlanIdx))
	}
}

// SetTXParams updates TX parameters dynamically (for adaptive link).
func (s *StreamService) SetTXParams(params TXParams) {
	// Update radiotap headers on all injectors
	for _, inj := range s.injectors {
		hdr := inj.GetRadiotap()
		hdr.MCSIndex = uint8(params.MCS)
		hdr.ShortGI = params.ShortGI
		hdr.STBC = uint8(params.STBC)
		hdr.LDPC = params.LDPC
		if params.Bandwidth > 0 {
			hdr.Bandwidth = uint8(params.Bandwidth)
		}
		// VHT mode is typically needed for bandwidth > 20 or higher MCS
		hdr.VHTMode = params.Bandwidth > 20 || params.MCS > 7
		inj.SetRadiotap(hdr)
	}

	// Update FEC on all transmitters
	if params.FecK > 0 && params.FecN > 0 {
		for _, t := range s.transmitters {
			if err := t.SetFEC(params.FecK, params.FecN); err != nil {
				log.Printf("[%s] SetFEC failed: %v", s.name, err)
			}
		}
	}

	// TX power requires external command (iw) - not handled here
	// The server's applyTXProfile handles it via wlan manager

	log.Printf("[%s] TX params updated: MCS=%d, GI=%v, BW=%d, FEC=%d/%d",
		s.name, params.MCS, params.ShortGI, params.Bandwidth, params.FecK, params.FecN)
}

// GetTXParams returns current TX parameters.
func (s *StreamService) GetTXParams() TXParams {
	params := TXParams{}
	if len(s.injectors) > 0 {
		hdr := s.injectors[0].GetRadiotap()
		params.MCS = int(hdr.MCSIndex)
		params.ShortGI = hdr.ShortGI
		params.Bandwidth = int(hdr.Bandwidth)
	}
	if len(s.transmitters) > 0 {
		params.FecK, params.FecN = s.transmitters[0].FEC()
	}
	return params
}

// UpdateRSSI updates RSSI values for injection.
// This is a simple wrapper for backward compatibility.
func (s *StreamService) UpdateRSSI(rssi, snr int8) {
	s.UpdateRSSIStats(rssi, snr, 0, 0, 0)
}

// UpdateRSSIStats updates RSSI values and sends a RADIO_STATUS message if enabled.
// Parameters match wfb-ng's rssi_cb signature:
//   - rssi: RSSI in dBm (-128 to 127)
//   - snr: SNR value
//   - rxErrors: Count of decode errors + bad packets + lost packets
//   - fecFixed: Count of FEC recovered packets
//   - flags: WFB status flags (0x01 = link lost, 0x02 = link jammed)
func (s *StreamService) UpdateRSSIStats(rssi, snr int8, rxErrors, fecFixed uint16, flags uint8) {
	s.mu.Lock()
	s.lastRSSI = rssi
	s.lastSNR = snr
	s.lastRXErrors = rxErrors
	s.lastFECFixed = fecFixed
	s.rssiFlags = flags

	// Build and send RADIO_STATUS if enabled
	if s.rssiBuilder != nil {
		// Convert signed dBm to unsigned 0-255 range
		// Go's int8 to uint8 cast is equivalent to Python's % 256 (two's complement)
		// e.g., int8(-50) -> uint8(206)
		rssiU8 := uint8(rssi)
		// Noise is the noise floor: rssi - snr (matching wfb-ng)
		noiseU8 := uint8(rssi - snr)

		msg := s.rssiBuilder.Build(
			rssiU8,   // rssi
			rssiU8,   // remrssi (same as local for WFB)
			100,      // txbuf (fixed 100%)
			noiseU8,  // noise (noise floor = rssi - snr)
			flags,    // remnoise (reused for WFB flags)
			rxErrors, // rxerrors
			fecFixed, // fixed (FEC recovered)
		)
		s.mu.Unlock()

		// Send to peer (outside lock to avoid deadlock)
		if err := s.writeToPeer(msg); err != nil {
			log.Printf("[%s] Failed to send RSSI message: %v", s.name, err)
		}
		return
	}
	s.mu.Unlock()
}

func (s *StreamService) cleanup() {
	// Stop aggregator first to flush any buffered data
	if s.aggregator != nil {
		s.aggregator.Stop()
	}

	if s.udpConn != nil {
		s.udpConn.Close()
	}
	if s.udpListen != nil {
		s.udpListen.Close()
	}
	if s.osdConn != nil {
		s.osdConn.Close()
	}
	if s.binLogger != nil {
		s.binLogger.Close()
	}
	if s.mavlinkTCPListener != nil {
		s.mavlinkTCPListener.Close()
	}
	if s.tcpListener != nil {
		s.tcpListener.Close()
	}
	s.mu.Lock()
	for _, c := range s.tcpClients {
		c.Close()
	}
	s.tcpClients = nil
	for _, c := range s.mavlinkTCPClients {
		c.Close()
	}
	s.mavlinkTCPClients = nil
	s.mu.Unlock()
	if s.tunFd != nil {
		s.tunFd.Close()
	}
	if s.serialFd > 0 {
		syscall.Close(s.serialFd)
	}
	for _, t := range s.transmitters {
		t.Close()
	}
	for _, inj := range s.injectors {
		inj.Close()
	}
	if s.forwarder != nil {
		s.forwarder.Close()
	}
}

func (s *StreamService) Stop() error {
	if s.stopped.Swap(true) {
		return nil
	}
	close(s.stopCh)
	s.cleanup()
	s.wg.Wait()
	return nil
}

func (s *StreamService) Stats() *ServiceStats {
	if s.forwarder != nil {
		rxStats := s.forwarder.Stats()
		s.stats.mu.Lock()
		s.stats.PacketsReceived = rxStats.PacketsAll
		s.stats.BytesReceived = rxStats.BytesAll
		s.stats.PacketsDecErr = rxStats.PacketsDecErr
		s.stats.PacketsBad = rxStats.PacketsBad
		s.stats.PacketsFECRec = rxStats.PacketsFECRec
		s.stats.PacketsLost = rxStats.PacketsLost
		s.stats.PacketsOutgoing = rxStats.PacketsOutgoing
		s.stats.BytesOutgoing = rxStats.BytesOutgoing
		// Session info from TX
		s.stats.SessionEpoch = rxStats.Epoch
		s.stats.SessionFecK = rxStats.FecK
		s.stats.SessionFecN = rxStats.FecN

		// Copy antenna stats from forwarder and extract MCS
		if rxStats.AntennaStats != nil {
			if s.stats.AntennaStats == nil {
				s.stats.AntennaStats = make(map[uint32]*AntennaStats)
			}
			for key, rxAnt := range rxStats.AntennaStats {
				// Calculate averages from sums
				var rssiAvg, snrAvg int8
				if rxAnt.PacketsReceived > 0 {
					rssiAvg = int8(rxAnt.RSSISum / int64(rxAnt.PacketsReceived))
					snrAvg = int8(rxAnt.SNRSum / int64(rxAnt.PacketsReceived))
				}

				s.stats.AntennaStats[key] = &AntennaStats{
					WlanIdx:         rxAnt.WlanIdx,
					Antenna:         rxAnt.Antenna,
					Freq:            rxAnt.Freq,
					MCSIndex:        rxAnt.MCSIndex,
					Bandwidth:       rxAnt.Bandwidth,
					PacketsReceived: rxAnt.PacketsReceived,
					RSSIMin:         rxAnt.RSSIMin,
					RSSIAvg:         rssiAvg,
					RSSIMax:         rxAnt.RSSIMax,
					SNRMin:          rxAnt.SNRMin,
					SNRAvg:          snrAvg,
					SNRMax:          rxAnt.SNRMax,
				}

				// Use MCS from any active antenna (all should be same from TX)
				if rxAnt.PacketsReceived > 0 {
					s.stats.SessionMCS = int(rxAnt.MCSIndex)
				}
			}
		}
		s.stats.mu.Unlock()
	}

	var injected, bytes, fecTimeouts uint64
	for _, t := range s.transmitters {
		st := t.Stats()
		injected += st.PacketsInjected
		bytes += st.BytesInjected
		fecTimeouts += st.FECTimeouts
	}
	s.stats.mu.Lock()
	s.stats.PacketsInjected = injected
	s.stats.BytesInjected = bytes
	s.stats.FECTimeouts = fecTimeouts
	// Set TX params from config for TX services
	if len(s.transmitters) > 0 {
		s.stats.SessionFecK = s.cfg.FecK
		s.stats.SessionFecN = s.cfg.FecN
		s.stats.SessionMCS = s.cfg.MCSIndex
	}
	s.stats.mu.Unlock()

	return s.stats.Clone()
}

// GetLatencyStats returns injection latency stats per wlan.
func (s *StreamService) GetLatencyStats() map[uint32]LatencyStatsData {
	result := make(map[uint32]LatencyStatsData)
	for i, inj := range s.injectors {
		stats := inj.GetLatencyStatsNoReset()
		if len(stats) > 0 {
			st := stats[0]
			// Key is (wlan_idx << 8) | 0xff (antenna 0xff means aggregate)
			key := uint32(i)<<8 | 0xff
			var avg uint64
			if st.PacketsInjected+st.PacketsDropped > 0 {
				avg = st.LatencySum / (st.PacketsInjected + st.PacketsDropped)
			}
			result[key] = LatencyStatsData{
				PacketsInjected: st.PacketsInjected,
				PacketsDropped:  st.PacketsDropped,
				LatencyMin:      st.LatencyMin,
				LatencyMax:      st.LatencyMax,
				LatencyAvg:      avg,
			}
		}
	}
	return result
}

// Ensure interfaces
var _ Service = (*StreamService)(nil)
var _ AntennaRoutable = (*StreamService)(nil)
var _ LatencyProvider = (*StreamService)(nil)
