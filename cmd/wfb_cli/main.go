// wfb_cli displays real-time statistics from a running wfb_server.
//
// Usage:
//
//	wfb_cli [options] [profile]
//
// Example:
//
//	wfb_cli -host 127.0.0.1 -port 8003 gs
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/lian/wfb-go/pkg/version"
	"github.com/vmihailenco/msgpack/v5"
)

// ANSI escape codes
const (
	clearScreen  = "\033[2J"
	moveCursor   = "\033[%d;%dH"
	hideCursor   = "\033[?25l"
	showCursor   = "\033[?25h"
	bold         = "\033[1m"
	dim          = "\033[2m"
	reverse      = "\033[7m"
	reset        = "\033[0m"
	colorRed     = "\033[31m"
	colorGreen   = "\033[32m"
	colorYellow  = "\033[33m"
	colorBlue    = "\033[34m"
	colorMagenta = "\033[35m"
	colorCyan    = "\033[36m"
)

// Config holds CLI configuration.
type Config struct {
	Host    string
	Port    int
	Profile string
}

// Stats holds the current stats state.
type Stats struct {
	mu          sync.RWMutex
	cliTitle    string
	isCluster   bool
	logInterval int
	rxStats     map[string]*RXStats
	txStats     map[string]*TXStats
	lastUpdate  time.Time
}

// RXStats holds RX statistics for a service.
type RXStats struct {
	ID        string
	Timestamp float64
	Packets   map[string][]uint64
	Session   map[string]interface{}
	TXWlan    *int
	Antennas  []AntennaStats
}

// TXStats holds TX statistics for a service.
type TXStats struct {
	ID        string
	Timestamp float64
	Packets   map[string][]uint64
	Latency   map[string][]int64
	RFTemp    map[string]int
}

// AntennaStats holds per-antenna statistics.
type AntennaStats struct {
	AntID    uint64
	Freq     int
	MCS      int
	BW       int
	PktRecv  int
	RSSIMin  int
	RSSIAvg  int
	RSSIMax  int
	SNRMin   int
	SNRAvg   int
	SNRMax   int
}

func main() {
	cfg := parseFlags()

	if err := run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags() *Config {
	cfg := &Config{}

	flag.StringVar(&cfg.Host, "host", "127.0.0.1", "WFB server host")
	flag.IntVar(&cfg.Port, "port", 0, "Stats port (0=auto-detect from profile)")

	var showVersion bool
	flag.BoolVar(&showVersion, "version", false, "Show version")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "wfb_cli - WFB Statistics Viewer\n\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [options] [profile]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s -port 8003 gs\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -host 192.168.1.1 -port 8003\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nVersion: %s\n", version.String())
	}

	flag.Parse()

	if showVersion {
		fmt.Printf("wfb_cli %s\n", version.String())
		os.Exit(0)
	}

	// Profile is optional positional argument
	if flag.NArg() > 0 {
		cfg.Profile = flag.Arg(0)
	}

	// Default port if not specified
	if cfg.Port == 0 {
		cfg.Port = 8003 // Default stats port
	}

	return cfg
}

func run(cfg *Config) error {
	// Set up signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Initialize stats
	stats := &Stats{
		rxStats:     make(map[string]*RXStats),
		txStats:     make(map[string]*TXStats),
		logInterval: 1000,
	}

	// Connect to server
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	fmt.Printf("Connecting to %s...\n", addr)

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", addr, err)
	}
	defer conn.Close()

	// Hide cursor and clear screen
	fmt.Print(hideCursor)
	fmt.Print(clearScreen)
	defer fmt.Print(showCursor)

	// Start receiver goroutine
	done := make(chan struct{})
	errCh := make(chan error, 1)

	go func() {
		errCh <- receiveStats(conn, stats, done)
	}()

	// Start display goroutine
	displayDone := make(chan struct{})
	go func() {
		displayLoop(stats, displayDone)
	}()

	// Wait for signal or error
	select {
	case sig := <-sigCh:
		fmt.Print(clearScreen)
		fmt.Printf(moveCursor, 1, 1)
		fmt.Printf("Received signal %v, exiting...\n", sig)
	case err := <-errCh:
		if err != nil && err != io.EOF {
			fmt.Print(clearScreen)
			fmt.Printf(moveCursor, 1, 1)
			fmt.Printf("Connection error: %v\n", err)
		}
	}

	close(done)
	close(displayDone)

	return nil
}

func receiveStats(conn net.Conn, stats *Stats, done chan struct{}) error {
	// Read length-prefixed msgpack messages (Int32StringReceiver format)
	lenBuf := make([]byte, 4)

	for {
		select {
		case <-done:
			return nil
		default:
		}

		// Set read deadline for responsiveness
		conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))

		// Read length prefix (big-endian uint32)
		_, err := io.ReadFull(conn, lenBuf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			return err
		}

		msgLen := binary.BigEndian.Uint32(lenBuf)
		if msgLen > 1024*1024 {
			return fmt.Errorf("message too large: %d bytes", msgLen)
		}

		// Read message body
		msgBuf := make([]byte, msgLen)
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, err = io.ReadFull(conn, msgBuf)
		if err != nil {
			return err
		}

		// Parse msgpack
		var msg map[string]interface{}
		if err := msgpack.Unmarshal(msgBuf, &msg); err != nil {
			continue // Skip malformed messages
		}

		// Process message
		processMessage(stats, msg)
	}
}

func processMessage(stats *Stats, msg map[string]interface{}) {
	msgType, _ := msg["type"].(string)

	stats.mu.Lock()
	defer stats.mu.Unlock()

	switch msgType {
	case "cli_title":
		stats.cliTitle, _ = msg["cli_title"].(string)
		stats.isCluster, _ = msg["is_cluster"].(bool)
		if interval, ok := msg["log_interval"].(int64); ok {
			stats.logInterval = int(interval)
		} else if interval, ok := msg["log_interval"].(uint64); ok {
			stats.logInterval = int(interval)
		}

	case "rx":
		rx := parseRXStats(msg)
		if rx != nil {
			stats.rxStats[rx.ID] = rx
		}

	case "tx":
		tx := parseTXStats(msg)
		if tx != nil {
			stats.txStats[tx.ID] = tx
		}
	}

	stats.lastUpdate = time.Now()
}

func parseRXStats(msg map[string]interface{}) *RXStats {
	id, _ := msg["id"].(string)
	if id == "" {
		return nil
	}

	rx := &RXStats{
		ID:       id,
		Packets:  make(map[string][]uint64),
		Antennas: []AntennaStats{},
	}

	if ts, ok := msg["timestamp"].(float64); ok {
		rx.Timestamp = ts
	}

	// Parse packets
	if packets, ok := msg["packets"].(map[string]interface{}); ok {
		for k, v := range packets {
			if arr, ok := v.([]interface{}); ok && len(arr) >= 2 {
				rx.Packets[k] = []uint64{toUint64(arr[0]), toUint64(arr[1])}
			}
		}
	}

	// Parse session
	if session, ok := msg["session"].(map[string]interface{}); ok {
		rx.Session = session
	}

	// Parse tx_wlan
	if txWlan, ok := msg["tx_wlan"].(int64); ok {
		v := int(txWlan)
		rx.TXWlan = &v
	} else if txWlan, ok := msg["tx_wlan"].(uint64); ok {
		v := int(txWlan)
		rx.TXWlan = &v
	}

	// Parse antenna stats
	if antStats, ok := msg["rx_ant_stats"].([]interface{}); ok {
		for _, ant := range antStats {
			if antMap, ok := ant.(map[string]interface{}); ok {
				as := AntennaStats{
					AntID:   toUint64(antMap["ant"]),
					Freq:    toInt(antMap["freq"]),
					MCS:     toInt(antMap["mcs"]),
					BW:      toInt(antMap["bw"]),
					PktRecv: toInt(antMap["pkt_recv"]),
					RSSIMin: toInt(antMap["rssi_min"]),
					RSSIAvg: toInt(antMap["rssi_avg"]),
					RSSIMax: toInt(antMap["rssi_max"]),
					SNRMin:  toInt(antMap["snr_min"]),
					SNRAvg:  toInt(antMap["snr_avg"]),
					SNRMax:  toInt(antMap["snr_max"]),
				}
				rx.Antennas = append(rx.Antennas, as)
			}
		}
	}

	return rx
}

func parseTXStats(msg map[string]interface{}) *TXStats {
	id, _ := msg["id"].(string)
	if id == "" {
		return nil
	}

	tx := &TXStats{
		ID:      id,
		Packets: make(map[string][]uint64),
		Latency: make(map[string][]int64),
		RFTemp:  make(map[string]int),
	}

	if ts, ok := msg["timestamp"].(float64); ok {
		tx.Timestamp = ts
	}

	// Parse packets
	if packets, ok := msg["packets"].(map[string]interface{}); ok {
		for k, v := range packets {
			if arr, ok := v.([]interface{}); ok && len(arr) >= 2 {
				tx.Packets[k] = []uint64{toUint64(arr[0]), toUint64(arr[1])}
			}
		}
	}

	// Parse latency
	if latency, ok := msg["latency"].(map[string]interface{}); ok {
		for k, v := range latency {
			if arr, ok := v.([]interface{}); ok && len(arr) >= 5 {
				tx.Latency[k] = []int64{
					toInt64(arr[0]), toInt64(arr[1]),
					toInt64(arr[2]), toInt64(arr[3]), toInt64(arr[4]),
				}
			}
		}
	}

	// Parse RF temperature
	if rfTemp, ok := msg["rf_temperature"].(map[string]interface{}); ok {
		for k, v := range rfTemp {
			tx.RFTemp[k] = toInt(v)
		}
	}

	return tx
}

func displayLoop(stats *Stats, done chan struct{}) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			renderStats(stats)
		}
	}
}

func renderStats(stats *Stats) {
	stats.mu.RLock()
	defer stats.mu.RUnlock()

	// Move to top-left
	fmt.Printf(moveCursor, 1, 1)

	// Title bar
	title := stats.cliTitle
	if title == "" {
		title = "wfb_cli"
	}
	fmt.Printf("%s%s %s %s\n", bold, colorCyan, title, reset)
	fmt.Printf("%s─────────────────────────────────────────────────────────────────────────────────%s\n", dim, reset)

	row := 3

	// Sort service names for consistent display
	var rxNames []string
	for name := range stats.rxStats {
		rxNames = append(rxNames, name)
	}
	sort.Strings(rxNames)

	var txNames []string
	for name := range stats.txStats {
		txNames = append(txNames, name)
	}
	sort.Strings(txNames)

	// Render RX stats
	for _, name := range rxNames {
		rx := stats.rxStats[name]
		row = renderRXStats(rx, stats.logInterval, row)
	}

	// Render TX stats
	for _, name := range txNames {
		tx := stats.txStats[name]
		row = renderTXStats(tx, stats.logInterval, row)
	}

	// Status line
	fmt.Printf(moveCursor, row, 1)
	if time.Since(stats.lastUpdate) > 3*time.Second {
		fmt.Printf("%s%s[No data - waiting...]%s", reverse, colorYellow, reset)
	} else {
		fmt.Printf("%sLast update: %s%s", dim, stats.lastUpdate.Format("15:04:05"), reset)
	}

	// Clear rest of screen
	fmt.Print("\033[J")
}

func renderRXStats(rx *RXStats, logInterval int, row int) int {
	fmt.Printf(moveCursor, row, 1)
	fmt.Printf("%s%s[RX: %s]%s\n", bold, colorGreen, rx.ID, reset)
	row++

	// Header
	fmt.Printf(moveCursor, row, 1)
	fmt.Printf("%s     pkt/s    pkt%s\n", bold, reset)
	row++

	// Normalize rate by log interval
	norm := func(vals []uint64) (int, uint64) {
		if len(vals) < 2 {
			return 0, 0
		}
		rate := int(1000 * vals[0] / uint64(logInterval))
		return rate, vals[1]
	}

	// Stats rows
	statsRows := []struct {
		name      string
		key       string
		highlight bool
	}{
		{"recv", "all", false},
		{"udp", "outgoing", false},
		{"fec_r", "fec_rec", true},
		{"lost", "lost", true},
		{"d_err", "dec_err", true},
		{"bad", "bad", true},
	}

	for _, sr := range statsRows {
		fmt.Printf(moveCursor, row, 1)
		vals := rx.Packets[sr.key]
		rate, total := norm(vals)

		attr := ""
		if sr.highlight && rate > 0 {
			attr = reverse + colorRed
		}
		fmt.Printf("%s%-5s %5d  (%d)%s\n", attr, sr.name, rate, total, reset)
		row++
	}

	// Flow and FEC info
	fmt.Printf(moveCursor, row, 1)
	allRate, _ := norm(rx.Packets["all"])
	outRate, _ := norm(rx.Packets["outgoing"])
	fmt.Printf("%sFlow:%s %s → %s", bold, reset, humanRate(allRate), humanRate(outRate))

	if rx.Session != nil {
		fecK := toInt(rx.Session["fec_k"])
		fecN := toInt(rx.Session["fec_n"])
		if fecK > 0 && fecN > 0 {
			fmt.Printf("  %sFEC:%s %d/%d", bold, reset, fecK, fecN)
		}
	}
	fmt.Println()
	row++

	// Antenna stats
	if len(rx.Antennas) > 0 {
		fmt.Printf(moveCursor, row, 1)
		fmt.Printf("%sFreq MCS BW [ANT]   pkt/s   RSSI [dBm]       SNR [dB]%s\n", bold, reset)
		row++

		// Sort antennas by ID
		sort.Slice(rx.Antennas, func(i, j int) bool {
			return rx.Antennas[i].AntID < rx.Antennas[j].AntID
		})

		for _, ant := range rx.Antennas {
			fmt.Printf(moveCursor, row, 1)
			antStr := formatAntenna(ant.AntID)
			pktRate := 1000 * ant.PktRecv / logInterval

			// Check if this is the active TX antenna
			isActiveTX := rx.TXWlan != nil && int(ant.AntID>>8) == *rx.TXWlan

			attr := dim
			if isActiveTX {
				attr = bold
			}

			fmt.Printf("%s%4d %3d %2d %s  %5d  %3d < %3d < %3d  %3d < %3d < %3d%s\n",
				attr,
				ant.Freq, ant.MCS, ant.BW, antStr, pktRate,
				ant.RSSIMin, ant.RSSIAvg, ant.RSSIMax,
				ant.SNRMin, ant.SNRAvg, ant.SNRMax,
				reset)
			row++
		}
	}

	row++ // Blank line
	return row
}

func renderTXStats(tx *TXStats, logInterval int, row int) int {
	fmt.Printf(moveCursor, row, 1)
	fmt.Printf("%s%s[TX: %s]%s\n", bold, colorBlue, tx.ID, reset)
	row++

	// Header
	fmt.Printf(moveCursor, row, 1)
	fmt.Printf("%s     pkt/s    pkt%s\n", bold, reset)
	row++

	norm := func(vals []uint64) (int, uint64) {
		if len(vals) < 2 {
			return 0, 0
		}
		rate := int(1000 * vals[0] / uint64(logInterval))
		return rate, vals[1]
	}

	// Stats rows
	statsRows := []struct {
		name      string
		key       string
		highlight bool
	}{
		{"sent", "injected", false},
		{"udp", "incoming", false},
		{"drop", "dropped", true},
	}

	for _, sr := range statsRows {
		fmt.Printf(moveCursor, row, 1)
		vals := tx.Packets[sr.key]
		rate, total := norm(vals)

		attr := ""
		if sr.highlight && rate > 0 {
			attr = reverse + colorRed
		}
		fmt.Printf("%s%-5s %5d  (%d)%s\n", attr, sr.name, rate, total, reset)
		row++
	}

	// Flow info
	fmt.Printf(moveCursor, row, 1)
	inRate, _ := norm(tx.Packets["incoming"])
	outRate, _ := norm(tx.Packets["injected"])
	fmt.Printf("%sFlow:%s %s → %s\n", bold, reset, humanRate(inRate), humanRate(outRate))
	row++

	// Latency stats
	if len(tx.Latency) > 0 {
		fmt.Printf(moveCursor, row, 1)
		fmt.Printf("%s[ANT]   pkt/s  °C     Injection [us]%s\n", bold, reset)
		row++

		// Sort by key
		var keys []string
		for k := range tx.Latency {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			lat := tx.Latency[k]
			if len(lat) < 5 {
				continue
			}

			fmt.Printf(moveCursor, row, 1)
			injected := 1000 * int(lat[0]) / logInterval

			// Get temperature
			tempStr := " --"
			if temp, ok := tx.RFTemp[k]; ok {
				tempStr = fmt.Sprintf("%3d", temp)
			}

			fmt.Printf("%s  %5d  %s  %4d < %4d < %4d\n",
				k, injected, tempStr,
				lat[2], lat[3], lat[4])
			row++
		}
	}

	row++ // Blank line
	return row
}

func formatAntenna(antID uint64) string {
	if antID < (1 << 32) {
		if antID&0xff == 0xff {
			return fmt.Sprintf("%2X:X ", antID>>8)
		}
		return fmt.Sprintf("%2X:%X ", antID>>8, antID&0xff)
	}

	if antID&0xff == 0xff {
		return fmt.Sprintf("%08X:%X:X", antID>>32, (antID>>8)&0xff)
	}
	return fmt.Sprintf("%08X:%X:%X", antID>>32, (antID>>8)&0xff, antID&0xff)
}

func humanRate(bytesPerSec int) string {
	rate := bytesPerSec * 8

	if rate >= 1000*1000 {
		r := float64(rate) / 1024 / 1024
		if r < 10 {
			return fmt.Sprintf("%.1f mbit/s", r)
		}
		return fmt.Sprintf("%3d mbit/s", int(r))
	}

	r := float64(rate) / 1024
	if r < 10 {
		return fmt.Sprintf("%.1f kbit/s", r)
	}
	return fmt.Sprintf("%3d kbit/s", int(r))
}

func toUint64(v interface{}) uint64 {
	switch n := v.(type) {
	case uint64:
		return n
	case int64:
		return uint64(n)
	case int:
		return uint64(n)
	case float64:
		return uint64(n)
	}
	return 0
}

func toInt64(v interface{}) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case uint64:
		return int64(n)
	case int:
		return int64(n)
	case float64:
		return int64(n)
	}
	return 0
}

func toInt(v interface{}) int {
	return int(toInt64(v))
}
