// wfb_server orchestrates WFB services based on a YAML configuration.
//
// Usage:
//
//	wfb_server --config <config.yaml> [--wlans wlan0,wlan1]
//
// Example:
//
//	wfb_server --config /etc/wfb/drone.yaml
//	wfb_server --config /etc/wfb/gs.yaml --wlans wlan0,wlan1
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lian/wfb-go/pkg/config"
	"github.com/lian/wfb-go/pkg/rx"
	"github.com/lian/wfb-go/pkg/server"
	"github.com/lian/wfb-go/pkg/version"
)

func main() {
	var (
		configFile   string
		wlansStr     string
		skipWlanInit bool
		logInterval  int
		jsonPort     int
		msgpackPort  int
		showVersion  bool
		captureMode  string
	)

	flag.StringVar(&configFile, "config", "", "Config file path (YAML)")
	flag.StringVar(&wlansStr, "wlans", "", "WiFi interfaces (comma-separated, overrides config)")
	flag.BoolVar(&skipWlanInit, "skip-wlan-init", false, "Skip WiFi interface initialization")
	flag.IntVar(&logInterval, "log-interval", 1000, "Stats log interval in ms (0 to disable)")
	flag.IntVar(&jsonPort, "json-port", 0, "JSON API port (0 to use config or disable)")
	flag.IntVar(&msgpackPort, "msgpack-port", 0, "MsgPack API port (0 to use config or disable)")
	flag.BoolVar(&showVersion, "version", false, "Show version")
	flag.StringVar(&captureMode, "capture-mode", "", "Capture mode: 'dedicated', 'shared', or 'libpcap' (overrides config)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "wfb_server - WFB Service Orchestrator\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  %s --config <config.yaml>\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s --config /etc/wfb/drone.yaml\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --config /etc/wfb/gs.yaml --wlans wlan0,wlan1\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nVersion: %s\n", version.String())
	}

	flag.Parse()

	if showVersion {
		fmt.Printf("wfb_server %s\n", version.String())
		os.Exit(0)
	}

	if configFile == "" {
		fmt.Fprintf(os.Stderr, "Error: --config is required\n")
		flag.Usage()
		os.Exit(1)
	}

	// Load config
	cfg, err := config.Load(configFile)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Get WLANs from CLI or config
	var wlans []string
	if wlansStr != "" {
		wlans = strings.Split(wlansStr, ",")
		for i := range wlans {
			wlans[i] = strings.TrimSpace(wlans[i])
		}
	} else {
		wlans = cfg.Hardware.WLANs
	}

	if len(wlans) == 0 && !skipWlanInit {
		log.Fatalf("No WLANs specified in config or --wlans")
	}

	// Get capture mode from CLI or config
	var capMode rx.CaptureMode
	if captureMode != "" {
		switch captureMode {
		case "dedicated":
			capMode = rx.CaptureModeDedicated
		case "shared":
			capMode = rx.CaptureModeShared
		case "libpcap":
			capMode = rx.CaptureModeLibpcap
		default:
			log.Fatalf("Unknown capture mode: %s (use 'dedicated', 'shared', or 'libpcap')", captureMode)
		}
	} else {
		switch strings.ToLower(cfg.Hardware.CaptureMode) {
		case "shared":
			capMode = rx.CaptureModeShared
		case "libpcap":
			capMode = rx.CaptureModeLibpcap
		default:
			capMode = rx.CaptureModeDedicated
		}
	}

	// Ensure tun module is loaded (needed for tunnel service)
	ensureTunModule()

	log.Printf("wfb_server starting...")
	log.Printf("  Config: %s", configFile)
	log.Printf("  WLANs: %v", wlans)
	log.Printf("  Capture mode: %v", capMode)

	// Override API ports if specified
	if jsonPort > 0 {
		if cfg.API == nil {
			cfg.API = &config.APIConfig{}
		}
		cfg.API.JSONPort = jsonPort
	}
	if msgpackPort > 0 {
		if cfg.API == nil {
			cfg.API = &config.APIConfig{}
		}
		cfg.API.StatsPort = msgpackPort
	}

	// Create server
	srv, err := server.NewServer(server.ServerConfig{
		Config:       cfg,
		ConfigPath:   configFile,
		Wlans:        wlans,
		SkipWlanInit: skipWlanInit,
		JSONPort:     jsonPort,
		MsgPackPort:  msgpackPort,
		CaptureMode:  capMode,
	})
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	// Start server
	if err := srv.Start(); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}

	// Set up signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start stats reporter
	stopStats := make(chan struct{})
	if logInterval > 0 {
		go statsReporter(srv, logInterval, stopStats)
	}

	// Wait for signal
	sig := <-sigCh
	log.Printf("Received signal %v, shutting down...", sig)

	close(stopStats)

	// Stop server
	if err := srv.Stop(); err != nil {
		log.Printf("Error stopping server: %v", err)
	}

	log.Printf("Server shutdown complete")
}

func statsReporter(srv *server.Server, logInterval int, stopCh chan struct{}) {
	ticker := time.NewTicker(time.Duration(logInterval) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			aggStats := srv.GetAggregatedStats()
			if aggStats == nil {
				continue
			}

			for name, s := range aggStats.Services {
				if s.PacketsReceived > 0 {
					log.Printf("[%s] RX: all=%d dec_err=%d bad=%d fec_rec=%d lost=%d out=%d/%dB (%.0f/s %.1fKB/s fec=%.0f/s)",
						name,
						s.PacketsReceived, s.PacketsDecErr, s.PacketsBad,
						s.PacketsFECRec, s.PacketsLost,
						s.PacketsOutgoing, s.BytesOutgoing,
						s.RxRate, s.RxBytesRate/1024, s.FECRate)
				}
				if s.PacketsInjected > 0 {
					txPower := srv.GetTXPower()
					log.Printf("[%s] TX: in=%d/%dB inj=%d/%dB dropped=%d (%.0f/s %.1fKB/s) fec=%d/%d pwr=%ddBm",
						name,
						s.PacketsIncoming, s.BytesIncoming,
						s.PacketsInjected, s.BytesInjected, s.PacketsDropped,
						s.TxRate, s.TxBytesRate/1024,
						s.SessionFecK, s.SessionFecN,
						txPower)
				}
			}

			// Log antenna stats if available
			for _, ant := range aggStats.Antennas {
				log.Printf("  ANT[%d:%d] freq=%d mcs=%d bw=%d pkts=%d RSSI=%d/%d/%d SNR=%d/%d/%d",
					ant.WlanIdx, ant.Antenna, ant.Freq, ant.MCSIndex, ant.Bandwidth,
					ant.PacketsTotal,
					ant.RSSIMin, ant.RSSIAvg, ant.RSSIMax,
					ant.SNRMin, ant.SNRAvg, ant.SNRMax)
			}

			// Log TX antenna selection
			if selector := srv.GetTXAntennaSelector(); selector != nil {
				if wlanIdx := selector.GetSelectedWlan(); wlanIdx != nil {
					log.Printf("[antenna] tx_wlan=%d", *wlanIdx)
				}
			}
		}
	}
}

// ensureTunModule loads the tun kernel module if not already loaded.
func ensureTunModule() {
	// Check if /dev/net/tun exists
	if _, err := os.Stat("/dev/net/tun"); err == nil {
		return // Already loaded
	}

	// Try to load the module
	cmd := exec.Command("modprobe", "tun")
	if err := cmd.Run(); err != nil {
		log.Printf("Warning: failed to load tun module: %v (tunnel service may not work)", err)
	}
}
