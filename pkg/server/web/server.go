// Package web provides a web-based video player and stats dashboard.
package web

import (
	"bytes"
	"context"
	"embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lian/wfb-go/pkg/config"
)

//go:embed all:static
var staticFiles embed.FS

// Server provides a web interface for video and stats.
type Server struct {
	addr        string
	videoSource string // UDP address to receive video from (e.g., ":5600") - optional, for standalone testing

	mu           sync.RWMutex
	videoClients map[*websocket.Conn]bool
	statsClients map[*websocket.Conn]bool
	stats        *Stats

	upgrader websocket.Upgrader
	parser   *NALParser // Shared parser for direct video sink mode

	// Config API callbacks (set by main server via SetConfigCallbacks)
	getConfig func() *config.Config
	setConfig func(*config.Config) error

	// Stream API callbacks (set by main server via SetStreamCallbacks)
	getStreams  func() []StreamInfo
	startStream func(name string) error
	stopStream  func(name string) error

	// HTTP client for drone proxy requests
	httpClient *http.Client

	// Pit mode state
	pitMode *PitModeState
}

// Stats holds current link statistics.
type Stats struct {
	Timestamp int64 `json:"timestamp"`

	// Summary (best values across all antennas)
	RSSI int `json:"rssi"`
	SNR  int `json:"snr"`

	// Packet statistics
	Packets     uint64 `json:"packets"`
	FECRecovery int    `json:"fec_recovery"`
	FECLost     int    `json:"fec_lost"`
	DecErrors   int    `json:"dec_errors"`

	// Rates
	Bitrate    float64 `json:"bitrate_mbps"`
	PacketRate float64 `json:"packet_rate"` // packets/sec

	// TX stats
	TXInjected uint64 `json:"tx_injected"`
	TXDropped  uint64 `json:"tx_dropped"`

	// Per-antenna stats
	Antennas []AntennaStats `json:"antennas,omitempty"`

	// Per-stream stats
	Streams []StreamStats `json:"streams,omitempty"`

	// Stream running status (name -> running)
	StreamsRunning map[string]bool `json:"streams_running,omitempty"`

	// Selected TX antenna index (nil if single adapter)
	TXWlan *uint8 `json:"tx_wlan,omitempty"`

	// Video info (populated by decoder)
	VideoWidth  int `json:"video_width,omitempty"`
	VideoHeight int `json:"video_height,omitempty"`
	VideoFPS    int `json:"video_fps,omitempty"`

	// Legacy field
	Latency float64 `json:"latency_ms"`

	// Adaptive link stats
	AdaptiveLink *AdaptiveLinkStats `json:"adaptive_link,omitempty"`
}

// AntennaStats holds stats for a single antenna.
type AntennaStats struct {
	WlanIdx   uint8  `json:"wlan_idx"`
	WlanName  string `json:"wlan_name,omitempty"`
	Antenna   uint8  `json:"antenna"`
	Freq      uint16 `json:"freq"`
	MCS       uint8  `json:"mcs"`
	Bandwidth uint8  `json:"bandwidth"`
	Packets   uint64 `json:"packets"`
	RSSIMin   int8   `json:"rssi_min"`
	RSSIAvg   int8   `json:"rssi_avg"`
	RSSIMax   int8   `json:"rssi_max"`
	SNRMin    int8   `json:"snr_min"`
	SNRAvg    int8   `json:"snr_avg"`
	SNRMax    int8   `json:"snr_max"`
}

// StreamStats holds stats for a single stream.
type StreamStats struct {
	Name        string  `json:"name"`
	Type        string  `json:"type"` // "rx" or "tx"
	PacketRate  float64 `json:"packet_rate"`
	ByteRate    float64 `json:"byte_rate"`
	Packets     uint64  `json:"packets"`
	FECRecovery uint64  `json:"fec_recovery,omitempty"`
	FECLost     uint64  `json:"fec_lost,omitempty"`
	DecErrors   uint64  `json:"dec_errors,omitempty"`
	Dropped     uint64  `json:"dropped,omitempty"`
	FecK        int     `json:"fec_k,omitempty"`
	FecN        int     `json:"fec_n,omitempty"`
	MCS         *int    `json:"mcs,omitempty"`
}

// StreamInfo holds information about a stream for the API.
type StreamInfo struct {
	Name        string                `json:"name"`
	Running     bool                  `json:"running"`
	ServiceType string                `json:"service_type"`
	StreamRX    *uint8                `json:"stream_rx,omitempty"`
	StreamTX    *uint8                `json:"stream_tx,omitempty"`
	Peer        string                `json:"peer,omitempty"`
	FEC         [2]int                `json:"fec,omitempty"`
	Config      *config.StreamConfig `json:"config,omitempty"`
}

// AdaptiveLinkStats holds adaptive link status information.
// Presence of this struct in Stats indicates adaptive link is enabled.
type AdaptiveLinkStats struct {
	Mode string `json:"mode"` // "gs" or "drone"

	// Common stats (both modes calculate/receive score)
	Score    float64 `json:"score"`               // Current score (1000-2000 range)
	BestRSSI int     `json:"best_rssi,omitempty"` // Best RSSI used in calculation
	BestSNR  int     `json:"best_snr,omitempty"`  // Best SNR used in calculation
	Noise    float64 `json:"noise,omitempty"`     // Filtered noise estimate

	// Drone mode specific
	InFallback bool `json:"in_fallback,omitempty"` // True if in fallback mode
	Paused     bool `json:"paused,omitempty"`      // True if paused

	// Current profile details (drone mode)
	ProfileMCS      int `json:"profile_mcs,omitempty"`
	ProfileFecK     int `json:"profile_fec_k,omitempty"`
	ProfileFecN     int `json:"profile_fec_n,omitempty"`
	ProfileBitrate  int `json:"profile_bitrate,omitempty"`  // kbps
	ProfileTXPower  int `json:"profile_tx_power,omitempty"` // dBm
	ProfileRangeMin int `json:"profile_range_min,omitempty"`
	ProfileRangeMax int `json:"profile_range_max,omitempty"`
}

// Config for the web server.
type Config struct {
	Addr        string // HTTP listen address (e.g., ":8080")
	VideoSource string // UDP address to receive video (e.g., ":5600") - optional, for standalone testing
}

// NewServer creates a new web server.
func NewServer(cfg Config) *Server {
	return &Server{
		addr:         cfg.Addr,
		videoSource:  cfg.VideoSource,
		videoClients: make(map[*websocket.Conn]bool),
		statsClients: make(map[*websocket.Conn]bool),
		stats:        &Stats{},
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		parser: NewNALParser(),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		pitMode: NewPitModeState(),
	}
}

// Run starts the web server.
func (s *Server) Run(ctx context.Context) error {
	// Start video receiver only if UDP source is configured
	// Otherwise, video comes through WriteVideo() calls (direct sink mode)
	if s.videoSource != "" {
		go s.receiveVideo(ctx)
	} else {
		log.Printf("[web] No video source - using direct sink mode")
	}

	// Stats come directly via UpdateStats() calls from the main server

	// Setup HTTP routes
	mux := http.NewServeMux()

	// Static files
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return err
	}
	mux.Handle("/", http.FileServer(http.FS(staticFS)))

	// WebSocket endpoints
	mux.HandleFunc("/ws/video", s.handleVideoWS)
	mux.HandleFunc("/ws/stats", s.handleStatsWS)

	// API endpoints
	mux.HandleFunc("/api/stats", s.handleStatsAPI)
	mux.HandleFunc("/api/config", s.handleConfigAPI)
	mux.HandleFunc("/api/streams", s.handleStreamsAPI)
	mux.HandleFunc("/api/streams/", s.handleStreamActionAPI)
	mux.HandleFunc("/api/drone/config", s.handleDroneConfigAPI)
	mux.HandleFunc("/api/drone/streams", s.handleDroneStreamsAPI)
	mux.HandleFunc("/api/drone/streams/", s.handleDroneStreamActionAPI)
	mux.HandleFunc("/api/pitmode", s.handlePitModeAPI)

	server := &http.Server{
		Addr:    s.addr,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		server.Shutdown(context.Background())
	}()

	log.Printf("[web] Server starting on %s", s.addr)
	return server.ListenAndServe()
}

// handleVideoWS handles video WebSocket connections.
func (s *Server) handleVideoWS(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[web] Video WebSocket upgrade error: %v", err)
		return
	}

	s.mu.Lock()
	s.videoClients[conn] = true
	s.mu.Unlock()

	log.Printf("[web] Video client connected (%d total)", len(s.videoClients))

	// Keep connection alive, wait for close
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}

	s.mu.Lock()
	delete(s.videoClients, conn)
	s.mu.Unlock()
	conn.Close()

	log.Printf("[web] Video client disconnected (%d remaining)", len(s.videoClients))
}

// handleStatsWS handles stats WebSocket connections.
func (s *Server) handleStatsWS(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[web] Stats WebSocket upgrade error: %v", err)
		return
	}

	s.mu.Lock()
	s.statsClients[conn] = true
	s.mu.Unlock()

	log.Printf("[web] Stats client connected")

	// Keep connection alive
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}

	s.mu.Lock()
	delete(s.statsClients, conn)
	s.mu.Unlock()
	conn.Close()
}

// handleStatsAPI returns current stats as JSON.
func (s *Server) handleStatsAPI(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	stats := *s.stats
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// SetConfigCallbacks sets the callbacks for config API.
func (s *Server) SetConfigCallbacks(getter func() *config.Config, setter func(*config.Config) error) {
	s.getConfig = getter
	s.setConfig = setter
}

// SetStreamCallbacks sets the callbacks for stream API.
func (s *Server) SetStreamCallbacks(
	getter func() []StreamInfo,
	starter func(string) error,
	stopper func(string) error,
) {
	s.getStreams = getter
	s.startStream = starter
	s.stopStream = stopper
}

// SetPitModeCallbacks sets the callbacks for GS TX power control.
func (s *Server) SetPitModeCallbacks(getter func() int, setter func(int) error) {
	s.pitMode.SetGSPowerCallbacks(getter, setter)
}

// IsPitModeEnabled returns whether pit mode is currently enabled.
func (s *Server) IsPitModeEnabled() bool {
	return s.pitMode.IsEnabled()
}

// handleStreamsAPI handles GET for streams list.
func (s *Server) handleStreamsAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	if s.getStreams == nil {
		http.Error(w, `{"error":"streams API not available"}`, http.StatusServiceUnavailable)
		return
	}

	streams := s.getStreams()
	json.NewEncoder(w).Encode(map[string]interface{}{
		"streams": streams,
	})
}

// handleStreamActionAPI handles POST for stream start/stop.
// URL format: /api/streams/{name}/{action}
func (s *Server) handleStreamActionAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Parse URL: /api/streams/{name}/{action}
	path := r.URL.Path[len("/api/streams/"):]
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 {
		http.Error(w, `{"error":"invalid path, expected /api/streams/{name}/{action}"}`, http.StatusBadRequest)
		return
	}

	name := parts[0]
	action := parts[1]

	var err error
	switch action {
	case "start":
		if s.startStream == nil {
			http.Error(w, `{"error":"start API not available"}`, http.StatusServiceUnavailable)
			return
		}
		err = s.startStream(name)
	case "stop":
		if s.stopStream == nil {
			http.Error(w, `{"error":"stop API not available"}`, http.StatusServiceUnavailable)
			return
		}
		err = s.stopStream(name)
	default:
		http.Error(w, `{"error":"unknown action, expected 'start' or 'stop'"}`, http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Stream " + name + " " + action + " successful",
	})
}

// handleConfigAPI handles GET/PUT for config.
func (s *Server) handleConfigAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		s.handleConfigGet(w, r)
	case http.MethodPut:
		s.handleConfigPut(w, r)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// handleConfigGet returns the current config.
// Keys are converted to base64 format for UI editing.
func (s *Server) handleConfigGet(w http.ResponseWriter, r *http.Request) {
	if s.getConfig == nil {
		http.Error(w, `{"error":"config API not available"}`, http.StatusServiceUnavailable)
		return
	}

	cfg := s.getConfig()
	if cfg == nil {
		http.Error(w, `{"error":"no config available"}`, http.StatusInternalServerError)
		return
	}

	// Clone config and convert key files to base64
	safeCfg := cfg.Clone()

	// Convert link key to base64
	if keyData, err := cfg.Link.GetKeyData(); err == nil && len(keyData) > 0 {
		safeCfg.Link.KeyBase64 = base64.StdEncoding.EncodeToString(keyData)
		safeCfg.Link.Key = "" // Clear file path, we're using base64 now
	}

	// Convert stream keys to base64
	for name, stream := range safeCfg.Streams {
		if keyData, err := cfg.GetStreamKeyData(&stream); err == nil && len(keyData) > 0 {
			stream.KeyBase64 = base64.StdEncoding.EncodeToString(keyData)
			stream.Key = "" // Clear file path
			safeCfg.Streams[name] = stream
		}
	}

	json.NewEncoder(w).Encode(safeCfg)
}

// handleConfigPut updates the config.
func (s *Server) handleConfigPut(w http.ResponseWriter, r *http.Request) {
	if s.setConfig == nil {
		http.Error(w, `{"error":"config API not available"}`, http.StatusServiceUnavailable)
		return
	}

	var newCfg config.Config
	if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
		http.Error(w, `{"error":"invalid JSON: `+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	if err := newCfg.Validate(); err != nil {
		http.Error(w, `{"error":"validation failed: `+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	if err := s.setConfig(&newCfg); err != nil {
		http.Error(w, `{"error":"failed to save config: `+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Config saved and applied.",
	})
}

// Default drone address (tunnel IP)
const defaultDroneAddr = "10.5.0.10:8080"

// handleDroneConfigAPI proxies config requests to a remote drone.
// The drone address can be overridden via X-Drone-Addr header or ?addr= query param.
// Mode can be "ssh" (legacy wfb-ng) or "http" (wfb-server) via ?mode= param, defaults to ssh.
func (s *Server) handleDroneConfigAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Get drone address from header, query param, or use default
	droneAddr := r.Header.Get("X-Drone-Addr")
	if droneAddr == "" {
		droneAddr = r.URL.Query().Get("addr")
	}
	if droneAddr == "" {
		droneAddr = defaultDroneAddr
	}

	// Get mode: "ssh" (legacy wfb-ng) or "http" (wfb-server)
	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = "ssh" // Default to SSH for legacy compatibility
	}

	switch mode {
	case "ssh":
		s.handleDroneConfigSSH(w, r, droneAddr)
	case "http":
		droneURL := "http://" + droneAddr + "/api/config"
		switch r.Method {
		case http.MethodGet:
			s.proxyDroneGet(w, droneURL)
		case http.MethodPut:
			s.proxyDronePut(w, r, droneURL)
		default:
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		}
	default:
		http.Error(w, `{"error":"invalid mode, use 'ssh' or 'http'"}`, http.StatusBadRequest)
	}
}

// handleDroneConfigSSH handles drone config via SSH (legacy wfb-ng).
func (s *Server) handleDroneConfigSSH(w http.ResponseWriter, r *http.Request, droneAddr string) {
	client := NewDroneSSHClient(droneAddr)

	switch r.Method {
	case http.MethodGet:
		data, err := client.GetConfigJSON()
		if err != nil {
			http.Error(w, `{"error":"ssh failed: `+err.Error()+`"}`, http.StatusBadGateway)
			return
		}
		w.Write(data)

	case http.MethodPut:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, `{"error":"failed to read request body"}`, http.StatusBadRequest)
			return
		}
		if err := client.SetConfigJSON(body); err != nil {
			http.Error(w, `{"error":"ssh failed: `+err.Error()+`"}`, http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"message": "Config sent to drone (wfb-ng restarting)"})

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// proxyDroneGet fetches config from a remote drone.
func (s *Server) proxyDroneGet(w http.ResponseWriter, droneURL string) {
	resp, err := s.httpClient.Get(droneURL)
	if err != nil {
		http.Error(w, `{"error":"failed to connect to drone: `+err.Error()+`"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// proxyDronePut sends config to a remote drone.
func (s *Server) proxyDronePut(w http.ResponseWriter, r *http.Request, droneURL string) {
	// Read the request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"failed to read request body"}`, http.StatusBadRequest)
		return
	}

	// Create the PUT request to the drone
	req, err := http.NewRequest(http.MethodPut, droneURL, bytes.NewReader(body))
	if err != nil {
		http.Error(w, `{"error":"failed to create request: `+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		http.Error(w, `{"error":"failed to connect to drone: `+err.Error()+`"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// handleDroneStreamsAPI proxies streams requests to a remote drone.
func (s *Server) handleDroneStreamsAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Get drone address
	droneAddr := r.Header.Get("X-Drone-Addr")
	if droneAddr == "" {
		droneAddr = r.URL.Query().Get("addr")
	}
	if droneAddr == "" {
		droneAddr = defaultDroneAddr
	}

	droneURL := "http://" + droneAddr + "/api/streams"
	s.proxyDroneGet(w, droneURL)
}

// handleDroneStreamActionAPI proxies stream start/stop to a remote drone.
// URL format: /api/drone/streams/{name}/{action}
func (s *Server) handleDroneStreamActionAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Get drone address
	droneAddr := r.Header.Get("X-Drone-Addr")
	if droneAddr == "" {
		droneAddr = r.URL.Query().Get("addr")
	}
	if droneAddr == "" {
		droneAddr = defaultDroneAddr
	}

	// Parse URL: /api/drone/streams/{name}/{action}
	path := r.URL.Path[len("/api/drone/streams/"):]
	droneURL := "http://" + droneAddr + "/api/streams/" + path

	// Create POST request to drone
	req, err := http.NewRequest(http.MethodPost, droneURL, nil)
	if err != nil {
		http.Error(w, `{"error":"failed to create request: `+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		http.Error(w, `{"error":"failed to connect to drone: `+err.Error()+`"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// UpdateStats updates the current stats and broadcasts to clients.
func (s *Server) UpdateStats(stats *Stats) {
	s.mu.Lock()
	s.stats = stats
	clients := make([]*websocket.Conn, 0, len(s.statsClients))
	for c := range s.statsClients {
		clients = append(clients, c)
	}
	s.mu.Unlock()

	data, _ := json.Marshal(stats)
	for _, c := range clients {
		c.WriteMessage(websocket.TextMessage, data)
	}
}

// receiveVideo receives H.265 video from UDP and broadcasts to WebSocket clients.
func (s *Server) receiveVideo(ctx context.Context) {
	addr, err := net.ResolveUDPAddr("udp", s.videoSource)
	if err != nil {
		log.Printf("[web] Failed to resolve video source %s: %v", s.videoSource, err)
		return
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Printf("[web] Failed to listen on %s: %v", s.videoSource, err)
		return
	}
	defer conn.Close()

	log.Printf("[web] Receiving video on %s", s.videoSource)

	// NAL unit parser state
	parser := NewNALParser()
	buf := make([]byte, 65536)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Very short timeout for low latency - flush incomplete NAL on timeout
		conn.SetReadDeadline(time.Now().Add(2 * time.Millisecond))
		n, err := conn.Read(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// Flush any buffered NAL unit on timeout
				if nalu := parser.Flush(); nalu != nil {
					s.broadcastNALU(nalu)
				}
				continue
			}
			if err != io.EOF {
				log.Printf("[web] Video read error: %v", err)
			}
			continue
		}

		// Parse NAL units from RTP or raw stream (auto-detects format)
		nalUnits := parser.ParseRTP(buf[:n])

		// Broadcast each NAL unit to video clients
		for _, nalu := range nalUnits {
			s.broadcastNALU(nalu)
		}
	}
}

// broadcastNALU sends a NAL unit to all connected video clients.
func (s *Server) broadcastNALU(nalu []byte) {
	s.mu.RLock()
	clients := make([]*websocket.Conn, 0, len(s.videoClients))
	for c := range s.videoClients {
		clients = append(clients, c)
	}
	s.mu.RUnlock()

	// Send NAL unit with 4-byte length prefix
	msg := make([]byte, 4+len(nalu))
	binary.BigEndian.PutUint32(msg[:4], uint32(len(nalu)))
	copy(msg[4:], nalu)

	for _, c := range clients {
		c.WriteMessage(websocket.BinaryMessage, msg)
	}
}

// NALParser extracts H.265 NAL units from a byte stream.
// Supports both raw Annex B streams and RTP-wrapped HEVC (RFC 7798).
type NALParser struct {
	buffer []byte

	// RTP fragmentation unit reassembly
	fuBuffer  []byte
	fuStarted bool

	// RTP sequence tracking for loss detection
	lastSeq    uint16
	seqStarted bool
}

// NewNALParser creates a new NAL unit parser.
func NewNALParser() *NALParser {
	return &NALParser{
		buffer:   make([]byte, 0, 256*1024), // 256KB buffer
		fuBuffer: make([]byte, 0, 128*1024), // 128KB for FU reassembly
	}
}

// Parse extracts NAL units from incoming data.
// Accumulates data and extracts complete NAL units when start codes are found.
func (p *NALParser) Parse(data []byte) [][]byte {
	// Append new data to buffer
	p.buffer = append(p.buffer, data...)

	var nalUnits [][]byte

	// Find and extract complete NAL units
	for {
		// Find first start code
		startIdx := findStartCode(p.buffer)
		if startIdx < 0 {
			// No start code found - keep buffering
			// But limit buffer size to prevent unbounded growth
			if len(p.buffer) > 128*1024 {
				p.buffer = p.buffer[len(p.buffer)-64*1024:]
			}
			break
		}

		// Skip any data before first start code
		if startIdx > 0 {
			p.buffer = p.buffer[startIdx:]
			startIdx = 0
		}

		// Determine start code length (3 or 4 bytes)
		startCodeLen := 3
		if len(p.buffer) >= 4 && p.buffer[0] == 0 && p.buffer[1] == 0 && p.buffer[2] == 0 && p.buffer[3] == 1 {
			startCodeLen = 4
		}

		// Find next start code (end of current NAL unit)
		endIdx := findStartCode(p.buffer[startCodeLen:])
		if endIdx < 0 {
			// No end found - NAL unit may span more packets
			// Keep the data and wait for more
			break
		}
		endIdx += startCodeLen // Adjust for offset

		// Extract the NAL unit (without start code)
		nalu := make([]byte, endIdx-startCodeLen)
		copy(nalu, p.buffer[startCodeLen:endIdx])
		nalUnits = append(nalUnits, nalu)

		// Remove processed data from buffer
		p.buffer = p.buffer[endIdx:]
	}

	return nalUnits
}

// Flush returns any buffered NAL unit data and clears the buffer.
// Used when no new data arrives for a while (timeout-based flush for low latency).
func (p *NALParser) Flush() []byte {
	if len(p.buffer) == 0 {
		return nil
	}

	// Find first start code
	startIdx := findStartCode(p.buffer)
	if startIdx < 0 {
		// No start code - discard garbage
		p.buffer = p.buffer[:0]
		return nil
	}

	// Skip data before start code and the start code itself
	startCodeLen := 3
	if startIdx+3 < len(p.buffer) && p.buffer[startIdx] == 0 && p.buffer[startIdx+1] == 0 &&
		p.buffer[startIdx+2] == 0 && p.buffer[startIdx+3] == 1 {
		startCodeLen = 4
	}

	naluStart := startIdx + startCodeLen
	if naluStart >= len(p.buffer) {
		p.buffer = p.buffer[:0]
		return nil
	}

	// Return everything after start code as a NAL unit
	nalu := make([]byte, len(p.buffer)-naluStart)
	copy(nalu, p.buffer[naluStart:])
	p.buffer = p.buffer[:0]
	return nalu
}

// findStartCode finds the first Annex B start code (0x000001 or 0x00000001)
func findStartCode(data []byte) int {
	for i := 0; i <= len(data)-3; i++ {
		if data[i] == 0 && data[i+1] == 0 {
			if data[i+2] == 1 {
				return i // 3-byte start code
			}
			if i <= len(data)-4 && data[i+2] == 0 && data[i+3] == 1 {
				return i // 4-byte start code
			}
		}
	}
	return -1
}

// ParseRTP extracts H.265 NAL units from RTP packets (RFC 7798).
// Returns complete NAL units ready for decoding.
func (p *NALParser) ParseRTP(data []byte) [][]byte {
	// Minimum RTP header is 12 bytes
	if len(data) < 12 {
		return nil
	}

	// Check RTP version (must be 2)
	version := (data[0] >> 6) & 0x03
	if version != 2 {
		// Not RTP - try Annex B parsing
		return p.Parse(data)
	}

	// Parse RTP header
	// padding := (data[0] >> 5) & 0x01
	extension := (data[0] >> 4) & 0x01
	csrcCount := data[0] & 0x0F
	// marker := (data[1] >> 7) & 0x01
	// payloadType := data[1] & 0x7F
	seqNum := binary.BigEndian.Uint16(data[2:4])
	// timestamp := binary.BigEndian.Uint32(data[4:8])
	// ssrc := binary.BigEndian.Uint32(data[8:12])

	// Check for packet loss
	if p.seqStarted {
		expectedSeq := p.lastSeq + 1
		if seqNum != expectedSeq {
			// Packet loss detected - discard any incomplete FU
			if p.fuStarted {
				p.fuBuffer = p.fuBuffer[:0]
				p.fuStarted = false
			}
		}
	}
	p.lastSeq = seqNum
	p.seqStarted = true

	// Calculate payload offset
	offset := 12 + int(csrcCount)*4
	if extension == 1 && len(data) > offset+4 {
		// Skip extension header
		extLen := int(binary.BigEndian.Uint16(data[offset+2:offset+4])) * 4
		offset += 4 + extLen
	}

	if offset >= len(data) {
		return nil
	}

	payload := data[offset:]
	if len(payload) < 2 {
		return nil
	}

	// HEVC NAL unit header (2 bytes)
	// First byte: F (1) | Type (6) | LayerId high (1)
	// Second byte: LayerId low (5) | TID (3)
	nalType := (payload[0] >> 1) & 0x3F

	var nalUnits [][]byte

	switch {
	case nalType <= 47:
		// Single NAL unit packet - payload is the NAL unit
		nalu := make([]byte, len(payload))
		copy(nalu, payload)
		nalUnits = append(nalUnits, nalu)

	case nalType == 48:
		// Aggregation Packet (AP) - multiple NAL units
		pos := 2 // Skip PayloadHdr
		for pos+2 < len(payload) {
			naluSize := int(binary.BigEndian.Uint16(payload[pos : pos+2]))
			pos += 2
			if pos+naluSize > len(payload) {
				break
			}
			nalu := make([]byte, naluSize)
			copy(nalu, payload[pos:pos+naluSize])
			nalUnits = append(nalUnits, nalu)
			pos += naluSize
		}

	case nalType == 49:
		// Fragmentation Unit (FU) - NAL unit split across packets
		if len(payload) < 3 {
			return nil
		}

		// FU header
		fuHeader := payload[2]
		fuStart := (fuHeader >> 7) & 0x01
		fuEnd := (fuHeader >> 6) & 0x01
		fuType := fuHeader & 0x3F

		if fuStart == 1 {
			// Start of fragmented NAL unit
			// Reconstruct NAL unit header from PayloadHdr + FU type
			p.fuBuffer = p.fuBuffer[:0]
			// NAL header: preserve LayerId/TID from PayloadHdr, use FU type
			nalHeader := []byte{
				(payload[0] & 0x81) | (fuType << 1), // F | Type | LayerId high
				payload[1],                          // LayerId low | TID
			}
			p.fuBuffer = append(p.fuBuffer, nalHeader...)
			p.fuBuffer = append(p.fuBuffer, payload[3:]...) // FU payload
			p.fuStarted = true
		} else if p.fuStarted {
			// Continuation or end
			p.fuBuffer = append(p.fuBuffer, payload[3:]...)

			if fuEnd == 1 {
				// End of fragmented NAL unit
				nalu := make([]byte, len(p.fuBuffer))
				copy(nalu, p.fuBuffer)
				nalUnits = append(nalUnits, nalu)
				p.fuBuffer = p.fuBuffer[:0]
				p.fuStarted = false
			}
		}
	}

	return nalUnits
}
