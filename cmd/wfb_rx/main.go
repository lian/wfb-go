// wfb_rx receives encrypted video/data streams over WiFi broadcast.
//
// Usage:
//
//	wfb_rx [options] interface1 [interface2] ...
//
// Example:
//
//	wfb_rx -K gs.key -c 127.0.0.1 -u 5600 wlan0
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/lian/wfb-go/pkg/protocol"
	"github.com/lian/wfb-go/pkg/rx"
	"github.com/lian/wfb-go/pkg/version"
)

// RX modes
const (
	MODE_LOCAL      = iota // Local RX mode (default)
	MODE_FORWARDER         // Forwarder mode (raw packets via UDP)
	MODE_AGGREGATOR        // Aggregator mode (cluster)
)

// Config holds command-line configuration.
type Config struct {
	// Mode
	Mode           int
	AggregatorPort int // -a port

	// Key file
	KeyPath string

	// Channel
	LinkID    uint32
	RadioPort uint8
	Epoch     uint64

	// Output
	ClientAddr string
	ClientPort int
	UnixSocket string

	// Buffer sizes
	RcvBuf int
	SndBuf int

	// Stats
	LogInterval time.Duration

	// Capture
	CaptureMode rx.CaptureMode

	// Interfaces
	Interfaces []string
}

func main() {
	cfg := parseFlags()

	if len(cfg.Interfaces) == 0 && cfg.Mode != MODE_AGGREGATOR {
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
	var forwarder bool
	var aggregatorPort int
	flag.BoolVar(&forwarder, "f", false, "Forwarder mode (raw packets via UDP)")
	flag.IntVar(&aggregatorPort, "a", 0, "Aggregator mode with server port (cluster)")

	// Key
	flag.StringVar(&cfg.KeyPath, "K", "gs.key", "RX keypair path")

	// Channel
	var linkID int
	flag.IntVar(&linkID, "i", 0, "Link ID (24-bit)")
	var radioPort int
	flag.IntVar(&radioPort, "p", 0, "Radio port (stream number)")
	var epoch int64
	flag.Int64Var(&epoch, "e", 0, "Minimum session epoch")

	// Output
	flag.StringVar(&cfg.ClientAddr, "c", "127.0.0.1", "Client address for output")
	flag.IntVar(&cfg.ClientPort, "u", 5600, "Client port for output")
	flag.StringVar(&cfg.UnixSocket, "U", "", "Unix socket path (alternative to UDP)")

	// Buffer sizes
	flag.IntVar(&cfg.RcvBuf, "R", 0, "UDP receive buffer size (0=system default)")
	flag.IntVar(&cfg.SndBuf, "s", 0, "UDP send buffer size (0=system default)")

	// Stats
	var logInterval int
	flag.IntVar(&logInterval, "l", 1000, "Stats log interval [ms]")

	// Capture mode
	var captureMode string
	flag.StringVar(&captureMode, "capture-mode", "", "Capture mode: 'dedicated', 'shared', or 'libpcap' (default: dedicated)")

	var showVersion bool
	flag.BoolVar(&showVersion, "version", false, "Show version")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "wfb_rx - WFB Receiver\n\n")
		fmt.Fprintf(os.Stderr, "Local RX: %s [-K rx_key] { [-c client_addr] [-u client_port] | [-U unix_socket] } [-p radio_port]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "              [-l log_interval] [-i link_id] [-e epoch] [-R rcv_buf] [-s snd_buf] interface1 [interface2] ...\n\n")
		fmt.Fprintf(os.Stderr, "RX forwarder: %s -f [-c client_addr] [-u client_port] [-p radio_port] [-R rcv_buf] [-s snd_buf]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "                  [-l log_interval] [-i link_id] interface1 [interface2] ...\n\n")
		fmt.Fprintf(os.Stderr, "RX aggregator: %s -a server_port [-K rx_key] { [-c client_addr] [-u client_port] | [-U unix_socket] } [-R rcv_buf]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "                   [-s snd_buf] [-p radio_port] [-l log_interval] [-i link_id] [-e epoch]\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nDefault: K='gs.key', connect=127.0.0.1:5600, link_id=0x000000, radio_port=0, epoch=0, log_interval=1000\n")
		fmt.Fprintf(os.Stderr, "Version: %s\n", version.String())
	}

	flag.Parse()

	if showVersion {
		fmt.Printf("wfb_rx %s\n", version.String())
		os.Exit(0)
	}

	// Determine mode
	if forwarder {
		cfg.Mode = MODE_FORWARDER
	} else if aggregatorPort > 0 {
		cfg.Mode = MODE_AGGREGATOR
		cfg.AggregatorPort = aggregatorPort
	} else {
		cfg.Mode = MODE_LOCAL
	}

	cfg.LinkID = uint32(linkID) & 0xFFFFFF
	cfg.RadioPort = uint8(radioPort)
	cfg.Epoch = uint64(epoch)
	cfg.LogInterval = time.Duration(logInterval) * time.Millisecond

	// Parse capture mode
	switch captureMode {
	case "dedicated", "":
		cfg.CaptureMode = rx.CaptureModeDedicated
	case "shared":
		cfg.CaptureMode = rx.CaptureModeShared
	case "libpcap":
		cfg.CaptureMode = rx.CaptureModeLibpcap
	default:
		fmt.Fprintf(os.Stderr, "Unknown capture mode: %s (use 'dedicated', 'shared', or 'libpcap')\n", captureMode)
		os.Exit(1)
	}

	cfg.Interfaces = flag.Args()

	return cfg
}

func run(cfg *Config) error {
	// Build channel ID
	channelID := protocol.MakeChannelID(cfg.LinkID, cfg.RadioPort)

	log.Printf("wfb_rx starting...")
	log.Printf("  Key: %s", cfg.KeyPath)
	log.Printf("  Channel: link_id=0x%06x, port=%d (channel_id=0x%08x)",
		cfg.LinkID, cfg.RadioPort, channelID)
	if cfg.UnixSocket != "" {
		log.Printf("  Output: unix socket %s", cfg.UnixSocket)
	} else {
		log.Printf("  Output: %s:%d", cfg.ClientAddr, cfg.ClientPort)
	}
	log.Printf("  Interfaces: %v", cfg.Interfaces)
	log.Printf("  Capture mode: %v", cfg.CaptureMode)

	// Create output connection
	var outputConn net.Conn
	var err error

	if cfg.UnixSocket != "" {
		outputConn, err = net.Dial("unixgram", cfg.UnixSocket)
		if err != nil {
			return fmt.Errorf("failed to connect to unix socket %s: %w", cfg.UnixSocket, err)
		}
	} else {
		outputAddr := fmt.Sprintf("%s:%d", cfg.ClientAddr, cfg.ClientPort)
		outputConn, err = net.Dial("udp", outputAddr)
		if err != nil {
			return fmt.Errorf("failed to connect to %s: %w", outputAddr, err)
		}
		// Set buffer sizes if specified
		if udpConn, ok := outputConn.(*net.UDPConn); ok {
			if cfg.RcvBuf > 0 {
				udpConn.SetReadBuffer(cfg.RcvBuf)
			}
			if cfg.SndBuf > 0 {
				udpConn.SetWriteBuffer(cfg.SndBuf)
			}
		}
	}
	defer outputConn.Close()

	// Direct output function (no local counters - use aggregator stats)
	outputFn := func(data []byte) error {
		_, err := outputConn.Write(data)
		return err
	}

	// Load key file
	keyData, err := os.ReadFile(cfg.KeyPath)
	if err != nil {
		return fmt.Errorf("failed to read key file %s: %w", cfg.KeyPath, err)
	}

	// Create forwarder (handles multiple interfaces)
	fwdCfg := rx.ForwarderConfig{
		Interfaces:  cfg.Interfaces,
		ChannelID:   channelID,
		KeyData:     keyData,
		Epoch:       cfg.Epoch,
		OutputFn:    outputFn,
		CaptureMode: cfg.CaptureMode,
	}

	forwarder, err := rx.NewForwarder(fwdCfg)
	if err != nil {
		return fmt.Errorf("failed to create forwarder: %w", err)
	}
	defer forwarder.Close()

	log.Printf("Listening on %d interface(s)", len(cfg.Interfaces))

	// Set up signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start stats reporter
	stopStats := make(chan struct{})
	go func() {
		ticker := time.NewTicker(cfg.LogInterval)
		defer ticker.Stop()

		var lastStats rx.Stats

		for {
			select {
			case <-stopStats:
				return
			case <-ticker.C:
				stats := forwarder.Stats() // Lock-free with antenna stats

				// Calculate rates
				dataRate := stats.PacketsData - lastStats.PacketsData
				outRate := stats.PacketsOutgoing - lastStats.PacketsOutgoing
				bytesRate := stats.BytesOutgoing - lastStats.BytesOutgoing
				fecRate := stats.PacketsFECRec - lastStats.PacketsFECRec

				lastStats = stats

				log.Printf("RX: all=%d uniq=%d session=%d data=%d dec_err=%d bad=%d fec_rec=%d lost=%d override=%d out=%d/%dB (in=%d/s out=%d/s %.1fKB/s fec=%d/s)",
					stats.PacketsAll, stats.PacketsUniq, stats.PacketsSession, stats.PacketsData,
					stats.PacketsDecErr, stats.PacketsBad, stats.PacketsFECRec,
					stats.PacketsLost, stats.PacketsOverride, stats.PacketsOutgoing, stats.BytesOutgoing,
					dataRate, outRate, float64(bytesRate)/1024.0, fecRate)

				// Log per-antenna stats (from lock-free snapshot)
				for _, ant := range stats.AntennaStats {
					var rssiAvg, snrAvg int8
					if ant.PacketsReceived > 0 {
						rssiAvg = int8(ant.RSSISum / int64(ant.PacketsReceived))
						snrAvg = int8(ant.SNRSum / int64(ant.PacketsReceived))
					}
					log.Printf("  ANT[%d:%d] freq=%d mcs=%d bw=%d pkts=%d RSSI=%d/%d/%d SNR=%d/%d/%d",
						ant.WlanIdx, ant.Antenna, ant.Freq, ant.MCSIndex, ant.Bandwidth,
						ant.PacketsReceived,
						ant.RSSIMin, rssiAvg, ant.RSSIMax,
						ant.SNRMin, snrAvg, ant.SNRMax)
				}
			}
		}
	}()

	// Start forwarder in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- forwarder.Run()
	}()

	// Wait for signal or error
	select {
	case sig := <-sigCh:
		log.Printf("Received signal %v, shutting down...", sig)
	case err := <-errCh:
		if err != nil {
			log.Printf("Forwarder error: %v", err)
		}
	}

	// Stop everything
	close(stopStats)
	forwarder.Close() // Safe to call twice (defer will call again)

	// Print final stats (with antenna data)
	stats := forwarder.StatsWithAntennas()
	log.Printf("Final stats:")
	log.Printf("  Packets received: %d (%d bytes)", stats.PacketsAll, stats.BytesAll)
	log.Printf("  Unique packets: %d", stats.PacketsUniq)
	log.Printf("  Session packets: %d", stats.PacketsSession)
	log.Printf("  Data packets: %d", stats.PacketsData)
	log.Printf("  FEC recovered: %d", stats.PacketsFECRec)
	log.Printf("  Packets lost: %d", stats.PacketsLost)
	log.Printf("  Ring overrides: %d", stats.PacketsOverride)
	log.Printf("  Decryption errors: %d", stats.PacketsDecErr)
	log.Printf("  Bad packets: %d", stats.PacketsBad)
	log.Printf("  Output packets: %d (%d bytes)", stats.PacketsOutgoing, stats.BytesOutgoing)

	// Print per-antenna final stats (sorted by wlan:antenna)
	if len(stats.AntennaStats) > 0 {
		keys := make([]uint32, 0, len(stats.AntennaStats))
		for k := range stats.AntennaStats {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

		log.Printf("  Per-antenna stats:")
		for _, k := range keys {
			ant := stats.AntennaStats[k]
			var rssiAvg, snrAvg int8
			if ant.PacketsReceived > 0 {
				rssiAvg = int8(ant.RSSISum / int64(ant.PacketsReceived))
				snrAvg = int8(ant.SNRSum / int64(ant.PacketsReceived))
			}
			log.Printf("    ANT[%d:%d] freq=%dMHz mcs=%d bw=%dMHz pkts=%d RSSI(min/avg/max)=%d/%d/%d SNR=%d/%d/%d",
				ant.WlanIdx, ant.Antenna, ant.Freq, ant.MCSIndex, ant.Bandwidth,
				ant.PacketsReceived,
				ant.RSSIMin, rssiAvg, ant.RSSIMax,
				ant.SNRMin, snrAvg, ant.SNRMax)
		}
	}

	return nil
}
