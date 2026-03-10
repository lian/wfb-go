// wfb_tx transmits encrypted video/data streams over WiFi broadcast.
//
// Usage:
//
//	wfb_tx [options] interface1 [interface2] ...
//
// Example:
//
//	wfb_tx -K drone.key -u 5600 -k 8 -n 12 wlan0
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/lian/wfb-go/pkg/protocol"
	"github.com/lian/wfb-go/pkg/tx"
	"github.com/lian/wfb-go/pkg/version"
	"github.com/lian/wfb-go/pkg/wifi/radiotap"
)

// TX modes
const (
	MODE_LOCAL       = iota // Local TX mode (default)
	MODE_DISTRIBUTOR        // Cluster distributor mode
	MODE_INJECTOR           // Cluster injector mode
)

// Frame types
const (
	FRAME_DATA = iota
	FRAME_RTS
)

// Config holds command-line configuration.
type Config struct {
	// Mode
	Mode        int
	InjectorPort int // -I port

	// Key file
	KeyPath string

	// FEC parameters
	FecK       int
	FecN       int
	FecDelay   int // microseconds between FEC packets
	FecTimeout int // milliseconds

	// Network
	UDPPort    int
	UnixSocket string
	RcvBuf     int
	SndBuf     int

	// Channel
	LinkID    uint32
	RadioPort uint8
	Epoch     uint64

	// Radio settings
	Bandwidth int
	ShortGI   bool
	STBC      int
	LDPC      int
	MCSIndex  int
	VHTMode   bool
	VHTNSS    int

	// Frame type
	FrameType int

	// Operation modes
	Mirror bool

	// Qdisc
	UseQdisc bool
	Fwmark   uint32

	// Control
	CmdPort   int
	DebugPort int

	// Injection
	Retries    int
	RetryDelay time.Duration

	// Stats
	LogInterval time.Duration

	// Interfaces
	Interfaces []string
}

// Stats holds runtime statistics.
type Stats struct {
	PacketsReceived  uint64
	BytesReceived    uint64
	PacketsInjected  uint64
	BytesInjected    uint64
	PacketsDropped   uint64
	PacketsTruncated uint64
	FECTimeouts      uint64
}

func main() {
	cfg := parseFlags()

	if len(cfg.Interfaces) == 0 {
		fmt.Fprintf(os.Stderr, "Error: at least one interface required\n")
		flag.Usage()
		os.Exit(1)
	}

	if err := run(cfg); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func parseFlags() *Config {
	cfg := &Config{}

	// Mode flags
	var injectorPort int
	var distributor bool
	flag.IntVar(&injectorPort, "I", 0, "Injector mode with server port (cluster)")
	flag.BoolVar(&distributor, "d", false, "Distributor mode (cluster)")

	// Key
	flag.StringVar(&cfg.KeyPath, "K", "drone.key", "TX keypair path")

	// FEC
	flag.IntVar(&cfg.FecK, "k", 8, "FEC data shards (RS_K)")
	flag.IntVar(&cfg.FecN, "n", 12, "FEC total shards (RS_N)")
	flag.IntVar(&cfg.FecDelay, "F", 0, "FEC delay between packets [us]")
	flag.IntVar(&cfg.FecTimeout, "T", 0, "FEC timeout [ms] (0=disabled)")

	// Network
	flag.IntVar(&cfg.UDPPort, "u", 5600, "UDP listen port")
	flag.StringVar(&cfg.UnixSocket, "U", "", "Unix socket path (alternative to UDP)")
	flag.IntVar(&cfg.RcvBuf, "R", 0, "UDP receive buffer size (0=system default)")
	flag.IntVar(&cfg.SndBuf, "s", 0, "UDP send buffer size (0=system default)")

	// Channel
	var linkID int
	flag.IntVar(&linkID, "i", 0, "Link ID (24-bit)")
	var radioPort int
	flag.IntVar(&radioPort, "p", 0, "Radio port (stream number)")
	var epoch int64
	flag.Int64Var(&epoch, "e", 0, "Session epoch")

	// Radio
	flag.IntVar(&cfg.Bandwidth, "B", 20, "Bandwidth (20/40/80 MHz)")
	var gi string
	flag.StringVar(&gi, "G", "long", "Guard interval (short/long)")
	flag.IntVar(&cfg.STBC, "S", 0, "STBC streams (0-2)")
	flag.IntVar(&cfg.LDPC, "L", 0, "LDPC (0=off, 1=on)")
	flag.IntVar(&cfg.MCSIndex, "M", 1, "MCS index")
	flag.IntVar(&cfg.VHTNSS, "N", 1, "VHT spatial streams")
	flag.BoolVar(&cfg.VHTMode, "V", false, "Force VHT mode (802.11ac)")

	// Frame type
	var frameType string
	flag.StringVar(&frameType, "f", "data", "Frame type (data/rts)")

	// Operation
	flag.BoolVar(&cfg.Mirror, "m", false, "Mirror mode (send on all interfaces)")

	// Qdisc
	flag.BoolVar(&cfg.UseQdisc, "Q", false, "Use qdisc (don't bypass)")
	var fwmark int
	flag.IntVar(&fwmark, "P", 0, "Fwmark for qdisc")

	// Control
	flag.IntVar(&cfg.CmdPort, "C", 0, "Control port for wfb_tx_cmd (0=disabled)")
	flag.IntVar(&cfg.DebugPort, "D", 0, "Debug port (0=disabled)")

	// Injection
	flag.IntVar(&cfg.Retries, "J", 3, "Injection retries on ENOBUFS")
	var retryDelay int
	flag.IntVar(&retryDelay, "E", 100, "Injection retry delay [us]")

	// Stats
	var logInterval int
	flag.IntVar(&logInterval, "l", 1000, "Stats log interval [ms]")

	var showVersion bool
	flag.BoolVar(&showVersion, "version", false, "Show version")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "wfb_tx - WFB Transmitter\n\n")
		fmt.Fprintf(os.Stderr, "Local TX: %s [-K tx_key] [-k RS_K] [-n RS_N] { [-u udp_port] | [-U unix_socket] } [-R rcv_buf] [-p radio_port]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "             [-F fec_delay] [-B bandwidth] [-G guard_interval] [-S stbc] [-L ldpc] [-M mcs_index] [-N VHT_NSS]\n")
		fmt.Fprintf(os.Stderr, "             [-T fec_timeout] [-l log_interval] [-e epoch] [-i link_id] [-f { data | rts }] [-m] [-V] [-Q]\n")
		fmt.Fprintf(os.Stderr, "             [-P fwmark] [-J inject_retries] [-E inject_retry_delay] [-C control_port] interface1 [interface2] ...\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nDefault: K='drone.key', k=8, n=12, fec_delay=0 [us], udp_port=5600, link_id=0x000000, radio_port=0,\n")
		fmt.Fprintf(os.Stderr, "         epoch=0, bandwidth=20, guard_interval=long, stbc=0, ldpc=0, mcs_index=1, vht_nss=1,\n")
		fmt.Fprintf(os.Stderr, "         vht_mode=false, fec_timeout=0, log_interval=1000, frame_type=data, mirror=false\n")
		fmt.Fprintf(os.Stderr, "\nRadio MTU: %d\n", protocol.MAX_PAYLOAD_SIZE)
		fmt.Fprintf(os.Stderr, "Version: %s\n", version.String())
	}

	flag.Parse()

	if showVersion {
		fmt.Printf("wfb_tx %s\n", version.String())
		os.Exit(0)
	}

	// Determine mode
	if injectorPort > 0 {
		cfg.Mode = MODE_INJECTOR
		cfg.InjectorPort = injectorPort
	} else if distributor {
		cfg.Mode = MODE_DISTRIBUTOR
	} else {
		cfg.Mode = MODE_LOCAL
	}

	// Parse frame type
	switch strings.ToLower(frameType) {
	case "data":
		cfg.FrameType = FRAME_DATA
	case "rts":
		cfg.FrameType = FRAME_RTS
	default:
		cfg.FrameType = FRAME_DATA
	}

	cfg.LinkID = uint32(linkID) & 0xFFFFFF
	cfg.RadioPort = uint8(radioPort)
	cfg.Epoch = uint64(epoch)
	cfg.ShortGI = strings.ToLower(gi) == "short" || gi == "s" || gi == "S"
	cfg.Fwmark = uint32(fwmark)
	cfg.LogInterval = time.Duration(logInterval) * time.Millisecond
	cfg.RetryDelay = time.Duration(retryDelay) * time.Microsecond

	cfg.Interfaces = flag.Args()

	return cfg
}

func run(cfg *Config) error {
	// Build channel ID
	channelID := protocol.MakeChannelID(cfg.LinkID, cfg.RadioPort)

	log.Printf("wfb_tx starting...")
	log.Printf("  Key: %s", cfg.KeyPath)
	log.Printf("  FEC: k=%d, n=%d, delay=%dus, timeout=%dms", cfg.FecK, cfg.FecN, cfg.FecDelay, cfg.FecTimeout)
	log.Printf("  Channel: link_id=0x%06x, port=%d (channel_id=0x%08x)",
		cfg.LinkID, cfg.RadioPort, channelID)
	if cfg.UnixSocket != "" {
		log.Printf("  Unix socket: %s", cfg.UnixSocket)
	} else {
		log.Printf("  UDP port: %d", cfg.UDPPort)
	}
	log.Printf("  Radio: BW=%dMHz, MCS=%d, STBC=%d, LDPC=%d, SGI=%v, VHT=%v, NSS=%d",
		cfg.Bandwidth, cfg.MCSIndex, cfg.STBC, cfg.LDPC, cfg.ShortGI, cfg.VHTMode, cfg.VHTNSS)
	log.Printf("  Interfaces: %v", cfg.Interfaces)

	// Create radiotap header
	rtHeader := buildRadiotapHeader(cfg)

	// Create raw socket injector
	rawCfg := tx.RawSocketConfig{
		Interfaces: cfg.Interfaces,
		ChannelID:  channelID,
		Radiotap:   rtHeader,
		UseQdisc:   cfg.UseQdisc,
		Retries:    cfg.Retries,
		RetryDelay: cfg.RetryDelay,
	}

	rawInj, err := tx.NewRawSocketInjector(rawCfg)
	if err != nil {
		return fmt.Errorf("failed to create raw socket injector: %w", err)
	}
	defer rawInj.Close()

	log.Printf("Opened %d interface(s)", rawInj.InterfaceCount())

	// Set mirror mode or start with first interface
	if cfg.Mirror {
		rawInj.SelectInterface(-1) // Mirror mode: inject on all interfaces
		log.Printf("Mirror mode enabled: sending on all interfaces")
	} else {
		rawInj.SelectInterface(0) // Start with first interface
	}

	// Load key file
	keyData, err := os.ReadFile(cfg.KeyPath)
	if err != nil {
		return fmt.Errorf("failed to read key file %s: %w", cfg.KeyPath, err)
	}

	// Create transmitter
	txCfg := tx.Config{
		FecK:       cfg.FecK,
		FecN:       cfg.FecN,
		FecDelay:   time.Duration(cfg.FecDelay) * time.Microsecond,
		FecTimeout: time.Duration(cfg.FecTimeout) * time.Millisecond,
		Epoch:      cfg.Epoch,
		ChannelID:  channelID,
		KeyData:    keyData,
	}

	transmitter, err := tx.New(txCfg, rawInj)
	if err != nil {
		return fmt.Errorf("failed to create transmitter: %w", err)
	}
	defer transmitter.Close()

	// Send initial session key
	if err := transmitter.SendSessionKey(); err != nil {
		return fmt.Errorf("failed to send session key: %w", err)
	}
	log.Printf("Session key sent")

	// Create input listener (UDP or Unix socket)
	var conn net.PacketConn
	if cfg.UnixSocket != "" {
		// Remove existing socket file
		os.Remove(cfg.UnixSocket)
		conn, err = net.ListenPacket("unixgram", cfg.UnixSocket)
		if err != nil {
			return fmt.Errorf("failed to listen on unix socket %s: %w", cfg.UnixSocket, err)
		}
		log.Printf("Listening on unix socket %s", cfg.UnixSocket)
	} else {
		addr := fmt.Sprintf(":%d", cfg.UDPPort)
		conn, err = net.ListenPacket("udp", addr)
		if err != nil {
			return fmt.Errorf("failed to listen on UDP %s: %w", addr, err)
		}
		// Set buffer sizes if specified
		if udpConn, ok := conn.(*net.UDPConn); ok {
			if cfg.RcvBuf > 0 {
				udpConn.SetReadBuffer(cfg.RcvBuf)
			}
			if cfg.SndBuf > 0 {
				udpConn.SetWriteBuffer(cfg.SndBuf)
			}
		}
		log.Printf("Listening on UDP %s", addr)
	}
	defer conn.Close()

	// Set up signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Stats
	var stats Stats
	var statsMu sync.Mutex

	// Start stats reporter
	stopStats := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		statsReporter(cfg.LogInterval, &stats, &statsMu, stopStats)
	}()

	// Start session key announcer
	stopAnnounce := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		sessionAnnouncer(transmitter, stopAnnounce)
	}()

	// FEC timeout is now handled internally by the transmitter

	// Start command listener if enabled
	var stopCmd chan struct{}
	if cfg.CmdPort > 0 {
		stopCmd = make(chan struct{})
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmdListener(cfg.CmdPort, transmitter, rawInj, stopCmd)
		}()
		log.Printf("Command port: %d", cfg.CmdPort)
	}

	// Main receive loop
	buf := make([]byte, protocol.MAX_PAYLOAD_SIZE+100)
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-done:
				return
			default:
			}

			// Set read deadline for responsiveness
			conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))

			n, _, err := conn.ReadFrom(buf)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				select {
				case <-done:
					return
				default:
					log.Printf("Read error: %v", err)
					continue
				}
			}

			if n == 0 {
				continue
			}

			// Update stats
			atomic.AddUint64(&stats.PacketsReceived, 1)
			atomic.AddUint64(&stats.BytesReceived, uint64(n))

			// Truncate if too large
			if n > protocol.MAX_PAYLOAD_SIZE {
				n = protocol.MAX_PAYLOAD_SIZE
			}

			// Send packet
			_, err = transmitter.SendPacket(buf[:n])
			if err != nil {
				atomic.AddUint64(&stats.PacketsDropped, 1)
				log.Printf("Send error: %v", err)
				continue
			}

			// Get TX stats (including FEC timeouts from transmitter)
			txStats := transmitter.Stats()
			atomic.StoreUint64(&stats.PacketsInjected, txStats.PacketsInjected)
			atomic.StoreUint64(&stats.BytesInjected, txStats.BytesInjected)
			atomic.StoreUint64(&stats.PacketsTruncated, txStats.PacketsTruncated)
			atomic.StoreUint64(&stats.FECTimeouts, txStats.FECTimeouts)
		}
	}()

	// Wait for signal
	sig := <-sigCh
	log.Printf("Received signal %v, shutting down...", sig)

	// Stop everything
	close(done)
	close(stopStats)
	close(stopAnnounce)
	if stopCmd != nil {
		close(stopCmd)
	}

	wg.Wait()

	// Print final stats
	statsMu.Lock()
	log.Printf("Final stats:")
	log.Printf("  Packets received: %d (%d bytes)", stats.PacketsReceived, stats.BytesReceived)
	log.Printf("  Packets injected: %d (%d bytes)", stats.PacketsInjected, stats.BytesInjected)
	log.Printf("  Packets dropped: %d", stats.PacketsDropped)
	log.Printf("  Packets truncated: %d", stats.PacketsTruncated)
	log.Printf("  FEC timeouts: %d", stats.FECTimeouts)
	statsMu.Unlock()

	return nil
}

func buildRadiotapHeader(cfg *Config) *radiotap.TXHeader {
	if cfg.VHTMode || cfg.Bandwidth >= 80 {
		// VHT mode (802.11ac)
		return &radiotap.TXHeader{
			VHTMode:   true,
			MCSIndex:  uint8(cfg.MCSIndex),
			Bandwidth: uint8(cfg.Bandwidth),
			ShortGI:   cfg.ShortGI,
			STBC:      uint8(cfg.STBC),
			LDPC:      cfg.LDPC != 0,
			VHTNSS:    uint8(cfg.VHTNSS),
		}
	}

	// HT mode (802.11n)
	return &radiotap.TXHeader{
		VHTMode:   false,
		MCSIndex:  uint8(cfg.MCSIndex),
		Bandwidth: uint8(cfg.Bandwidth),
		ShortGI:   cfg.ShortGI,
		STBC:      uint8(cfg.STBC),
		LDPC:      cfg.LDPC != 0,
	}
}

func statsReporter(interval time.Duration, stats *Stats, mu *sync.Mutex, stop chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var lastRx, lastTx, lastBytes uint64

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			rx := atomic.LoadUint64(&stats.PacketsReceived)
			txPkt := atomic.LoadUint64(&stats.PacketsInjected)
			txBytes := atomic.LoadUint64(&stats.BytesInjected)
			dropped := atomic.LoadUint64(&stats.PacketsDropped)
			truncated := atomic.LoadUint64(&stats.PacketsTruncated)
			fecTO := atomic.LoadUint64(&stats.FECTimeouts)

			rxRate := rx - lastRx
			txRate := txPkt - lastTx
			bytesRate := txBytes - lastBytes

			lastRx = rx
			lastTx = txPkt
			lastBytes = txBytes

			log.Printf("TX: rx=%d/s tx=%d/s (%.1f KB/s) dropped=%d truncated=%d fec_to=%d",
				rxRate, txRate, float64(bytesRate)/1024.0, dropped, truncated, fecTO)
		}
	}
}

func sessionAnnouncer(t *tx.Transmitter, stop chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if err := t.SendSessionKey(); err != nil {
				log.Printf("Failed to send session key: %v", err)
			}
		}
	}
}


func cmdListener(port int, t *tx.Transmitter, rawInj *tx.RawSocketInjector, stop chan struct{}) {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		log.Printf("Failed to listen on command port %d: %v", port, err)
		return
	}
	defer conn.Close()

	buf := make([]byte, 64)

	for {
		select {
		case <-stop:
			return
		default:
		}

		conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, remoteAddr, err := conn.ReadFrom(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			select {
			case <-stop:
				return
			default:
				continue
			}
		}

		// Parse and handle command
		req, err := protocol.UnmarshalCmdRequest(buf[:n])
		if err != nil {
			log.Printf("Invalid command: %v", err)
			continue
		}

		resp := handleCommand(req, t, rawInj)
		respData := protocol.MarshalCmdResponse(resp, req.CmdID)
		if respData != nil {
			conn.WriteTo(respData, remoteAddr)
		}
	}
}

func handleCommand(req *protocol.CmdRequest, t *tx.Transmitter, rawInj *tx.RawSocketInjector) *protocol.CmdResponse {
	resp := &protocol.CmdResponse{
		ReqID: req.ReqID,
		RC:    0,
	}

	switch req.CmdID {
	case protocol.CMD_SET_FEC:
		k := int(req.SetFEC.K)
		n := int(req.SetFEC.N)
		if err := t.SetFEC(k, n); err != nil {
			resp.RC = 22 // EINVAL
			log.Printf("CMD_SET_FEC failed: %v", err)
		} else {
			log.Printf("CMD_SET_FEC: k=%d, n=%d", k, n)
		}

	case protocol.CMD_SET_RADIO:
		hdr := &radiotap.TXHeader{
			STBC:      req.SetRadio.STBC,
			LDPC:      req.SetRadio.LDPC,
			ShortGI:   req.SetRadio.ShortGI,
			Bandwidth: req.SetRadio.Bandwidth,
			MCSIndex:  req.SetRadio.MCSIndex,
			VHTMode:   req.SetRadio.VHTMode,
			VHTNSS:    req.SetRadio.VHTNSS,
		}
		rawInj.SetRadiotap(hdr)
		log.Printf("CMD_SET_RADIO: BW=%d, MCS=%d, STBC=%d, LDPC=%v, SGI=%v, VHT=%v, NSS=%d",
			hdr.Bandwidth, hdr.MCSIndex, hdr.STBC, hdr.LDPC, hdr.ShortGI, hdr.VHTMode, hdr.VHTNSS)

	case protocol.CMD_GET_FEC:
		k, n := t.FEC()
		resp.GetFEC.K = uint8(k)
		resp.GetFEC.N = uint8(n)

	case protocol.CMD_GET_RADIO:
		hdr := rawInj.GetRadiotap()
		resp.GetRadio.STBC = hdr.STBC
		resp.GetRadio.LDPC = hdr.LDPC
		resp.GetRadio.ShortGI = hdr.ShortGI
		resp.GetRadio.Bandwidth = hdr.Bandwidth
		resp.GetRadio.MCSIndex = hdr.MCSIndex
		resp.GetRadio.VHTMode = hdr.VHTMode
		resp.GetRadio.VHTNSS = hdr.VHTNSS

	default:
		resp.RC = 22 // EINVAL
	}

	return resp
}
