package adaptive

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// LinkReceiver receives link quality updates from the ground station
// and adjusts TX parameters accordingly. Compatible with alink_drone.
type LinkReceiver struct {
	mu sync.Mutex

	// UDP listener
	conn *net.UDPConn
	port int

	// Profiles for parameter selection
	profiles []TXProfile

	// Callbacks for parameter adjustment
	onProfileChange func(profile *TXProfile, msg *LinkMessage)
	onFECChange     func(fecK, fecN, bitrate int) // Called when dynamic FEC changes

	// External commands (optional)
	encoderCommand  string // Template for encoder settings (bitrate, gop, etc.)
	keyframeCommand string // Command to request IDR frame
	fecCommand      string // Template for FEC changes: {fecK}, {fecN}
	bitrateCommand  string // Template for bitrate changes: {bitrate}

	// Hold-down timers to prevent rapid switching
	holdUpDuration   time.Duration // Time to wait before switching to higher quality
	holdDownDuration time.Duration // Time to wait before switching to lower quality
	lastChangeTime   time.Time
	lastProfileIdx   int

	// Fallback on GS heartbeat loss
	fallbackTimeout   time.Duration // Time without GS message before fallback
	fallbackHoldTime  time.Duration // Minimum time to stay in fallback mode
	lastMessageTime   time.Time
	inFallbackMode    bool
	fallbackEntryTime time.Time

	// Exponential smoothing for score stability
	smoothingFactor     float64 // 0-1, lower = more smoothing (default 0.1)
	smoothingFactorDown float64 // Smoothing for score decreases (default 1.0 = no smoothing)
	smoothedScore       float64
	scoreInitialized    bool

	// Hysteresis to prevent oscillation
	hysteresisPercent     float64 // Percent change required to switch up
	hysteresisPercentDown float64 // Percent change required to switch down
	lastAppliedScore      float64 // Score at last profile change (for hysteresis)

	// Minimum time between any profile changes
	minBetweenChanges time.Duration

	// Keyframe request handling
	keyframeInterval   time.Duration
	lastKeyframeTime   time.Time
	idrOnProfileChange bool // Request IDR on every profile change
	keyframeCodes      []keyframeCode
	keyframeCodeExpiry time.Duration

	// Dynamic FEC from GS fec_change field
	allowDynamicFEC   bool
	fecKAdjust        bool    // true = divide K, false = multiply N
	spikeFix          bool    // Only apply if bitrate >= 4000
	lastFECChange     int     // Track previous fec_change to detect changes
	lastFECChangeTime time.Time

	// TX dropped monitoring
	wlanInterface          string
	txDroppedCheckInterval time.Duration
	allowTxDropKeyframe    bool          // Request keyframe on tx drops
	allowTxDropBitrate     bool          // Reduce bitrate on tx drops
	txDropBitrateFactor    float64       // Factor to reduce bitrate (e.g., 0.5)
	txDropRestoreDelay     time.Duration // Time without drops before restoring bitrate
	lastTxDropped          int64
	lastTxDropTime         time.Time
	bitrateReduced         bool
	totalTxDropped         int64
	txDropKeyframeCount    int

	// Current applied FEC/bitrate (for dynamic adjustment)
	currentFecK    int
	currentFecN    int
	currentBitrate int
	currentGOP     float64

	// Pause/resume
	paused bool

	// State
	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// keyframeCode tracks a keyframe request code with expiry
type keyframeCode struct {
	code      string
	timestamp time.Time
}

// TXProfile defines transmission parameters for a link quality range.
// Format matches txprofiles.conf:
// rangeMin - rangeMax setGI setMCS setFecK setFecN setBitrate setGop wfbPower ROIqp bandwidth setQpDelta
type TXProfile struct {
	RangeMin  int     // Minimum score for this profile (1000-2000)
	RangeMax  int     // Maximum score for this profile (1000-2000)
	ShortGI   bool    // Short guard interval
	MCS       int     // MCS index
	FecK      int     // FEC K (data packets)
	FecN      int     // FEC N (total packets)
	Bitrate   int     // Video bitrate in kbps
	GOP       float64 // GOP length in seconds
	TXPower   int     // TX power in dBm
	ROIqp     string  // ROI QP settings (comma-separated)
	Bandwidth int     // Channel bandwidth (20/40)
	QpDelta   int     // QP delta
}

// LinkMessage is a parsed message from the ground station.
type LinkMessage struct {
	Timestamp    int64
	RSSIScore    int
	SNRScore     int
	FECRecovered int
	LostPackets  int
	BestRSSI     int
	BestSNR      int
	NumAntennas  int
	Penalty      int
	FECChange    int
	KeyframeCode string
}

// LinkReceiverConfig configures the link receiver.
type LinkReceiverConfig struct {
	// Port to listen on (default 9999)
	Port int
	// Profiles for parameter selection
	Profiles []TXProfile
	// Callback when profile changes (for adjusting TX parameters)
	OnProfileChange func(profile *TXProfile, msg *LinkMessage)
	// Callback when dynamic FEC changes (optional, for custom handling)
	OnFECChange func(fecK, fecN, bitrate int)

	// External command template for video encoder (optional)
	// Supports placeholders: {bitrate}, {gop}, {qp_delta}, {roi_qp}, {mcs}, {fec_k}, {fec_n}
	EncoderCommand string
	// Command to request IDR/keyframe (e.g., "curl -s http://localhost/api/v1/request_idr")
	KeyframeCommand string
	// Command template for FEC changes: {fecK}, {fecN}
	FECCommand string
	// Command template for bitrate changes: {bitrate}
	BitrateCommand string

	// Hold-up duration before switching to higher quality (default 3s, alink.conf: hold_modes_down_s)
	HoldUpDuration time.Duration
	// Hold-down duration before switching to lower quality (default 0 = immediate)
	HoldDownDuration time.Duration

	// Fallback timeout - switch to safe profile if no GS message (default 1s, alink.conf: fallback_ms)
	FallbackTimeout time.Duration
	// Minimum time to stay in fallback mode (default 1s, alink.conf: hold_fallback_mode_s)
	FallbackHoldTime time.Duration

	// Exponential smoothing factor for score (0-1, default 0.1, alink.conf: exp_smoothing_factor)
	// Lower values = more smoothing, slower response
	SmoothingFactor float64
	// Smoothing factor for decreasing scores (default 1.0 = immediate, alink.conf: exp_smoothing_factor_down)
	SmoothingFactorDown float64

	// Hysteresis percent to prevent oscillation (default 5%, alink.conf: hysteresis_percent)
	HysteresisPercent float64
	// Hysteresis percent for downward changes (default 5%, alink.conf: hysteresis_percent_down)
	HysteresisPercentDown float64

	// Minimum time between any profile changes (default 200ms, alink.conf: min_between_changes_ms)
	MinBetweenChanges time.Duration

	// Minimum interval between keyframe requests (default 1112ms, alink.conf: request_keyframe_interval_ms)
	KeyframeInterval time.Duration
	// Request IDR on every profile change (default false, alink.conf: idr_every_change)
	IDROnProfileChange bool
	// Keyframe code expiry time (default 1s)
	KeyframeCodeExpiry time.Duration

	// Dynamic FEC: adjust FEC based on GS fec_change field (default false, alink.conf: allow_dynamic_fec)
	AllowDynamicFEC bool
	// FEC K adjust mode: true = divide K, false = multiply N (default true, alink.conf: fec_k_adjust)
	FECKAdjust bool
	// Spike fix: only apply dynamic FEC if bitrate >= 4000 (default false, alink.conf: spike_fix_dynamic_fec)
	SpikeFix bool

	// WLAN interface for TX dropped monitoring (default "wlan0")
	WLANInterface string
	// TX dropped check interval (default 2250ms, alink.conf: check_xtx_period_ms)
	TxDroppedCheckInterval time.Duration
	// Request keyframe on TX drops (default true, alink.conf: allow_rq_kf_by_tx_d)
	AllowTxDropKeyframe bool
	// Reduce bitrate on TX drops (default true, alink.conf: allow_xtx_reduce_bitrate)
	AllowTxDropBitrate bool
	// Bitrate reduction factor on TX drops (default 0.8, alink.conf: xtx_reduce_bitrate_factor)
	TxDropBitrateFactor float64
	// Time without TX drops before restoring bitrate (default 1s)
	TxDropRestoreDelay time.Duration
}

// DefaultProfiles returns a set of default TX profiles.
func DefaultProfiles() []TXProfile {
	return []TXProfile{
		// Long range / low quality
		{RangeMin: 1000, RangeMax: 1200, ShortGI: false, MCS: 1, FecK: 1, FecN: 2, Bitrate: 4000, GOP: 1.0, TXPower: 58, Bandwidth: 20},
		// Medium range / medium quality
		{RangeMin: 1201, RangeMax: 1400, ShortGI: false, MCS: 2, FecK: 4, FecN: 6, Bitrate: 8000, GOP: 1.0, TXPower: 40, Bandwidth: 20},
		// Medium-short range
		{RangeMin: 1401, RangeMax: 1600, ShortGI: false, MCS: 3, FecK: 8, FecN: 12, Bitrate: 12000, GOP: 0.5, TXPower: 30, Bandwidth: 20},
		// Short range / high quality
		{RangeMin: 1601, RangeMax: 1800, ShortGI: true, MCS: 4, FecK: 8, FecN: 10, Bitrate: 18000, GOP: 0.5, TXPower: 20, Bandwidth: 20},
		// Very short range / best quality
		{RangeMin: 1801, RangeMax: 2000, ShortGI: true, MCS: 5, FecK: 12, FecN: 14, Bitrate: 25000, GOP: 0.3, TXPower: 10, Bandwidth: 20},
	}
}

// NewLinkReceiver creates a new link receiver.
// Defaults match alink.conf values for compatibility.
func NewLinkReceiver(cfg LinkReceiverConfig) (*LinkReceiver, error) {
	// Apply defaults (matching alink.conf)
	if cfg.Port == 0 {
		cfg.Port = 9999
	}
	if len(cfg.Profiles) == 0 {
		cfg.Profiles = DefaultProfiles()
	}
	if cfg.HoldUpDuration == 0 {
		cfg.HoldUpDuration = 3 * time.Second // alink.conf: hold_modes_down_s=3
	}
	// HoldDownDuration defaults to 0 (immediate) - vendor has no delay for decreasing quality
	if cfg.FallbackTimeout == 0 {
		cfg.FallbackTimeout = 1 * time.Second // alink.conf: fallback_ms=1000
	}
	if cfg.FallbackHoldTime == 0 {
		cfg.FallbackHoldTime = 1 * time.Second // alink.conf: hold_fallback_mode_s=1
	}
	if cfg.SmoothingFactor == 0 {
		cfg.SmoothingFactor = 0.1 // alink.conf: exp_smoothing_factor=0.1
	}
	if cfg.SmoothingFactorDown == 0 {
		cfg.SmoothingFactorDown = 1.0 // alink.conf: exp_smoothing_factor_down=1.0
	}
	if cfg.HysteresisPercent == 0 {
		cfg.HysteresisPercent = 5 // alink.conf: hysteresis_percent=5
	}
	if cfg.HysteresisPercentDown == 0 {
		cfg.HysteresisPercentDown = 5 // alink.conf: hysteresis_percent_down=5
	}
	if cfg.MinBetweenChanges == 0 {
		cfg.MinBetweenChanges = 200 * time.Millisecond // alink.conf: min_between_changes_ms=200
	}
	if cfg.KeyframeInterval == 0 {
		cfg.KeyframeInterval = 1112 * time.Millisecond // alink.conf: request_keyframe_interval_ms=1112
	}
	if cfg.KeyframeCodeExpiry == 0 {
		cfg.KeyframeCodeExpiry = 1 * time.Second
	}
	// alink.conf: allow_dynamic_fec=0, so default false is correct
	if !cfg.FECKAdjust {
		cfg.FECKAdjust = true // alink.conf: fec_k_adjust=1
	}
	// alink.conf: spike_fix_dynamic_fec=0, so default false is correct
	if cfg.WLANInterface == "" {
		cfg.WLANInterface = "wlan0"
	}
	if cfg.TxDroppedCheckInterval == 0 {
		cfg.TxDroppedCheckInterval = 2250 * time.Millisecond // alink.conf: check_xtx_period_ms=2250
	}
	if !cfg.AllowTxDropKeyframe {
		cfg.AllowTxDropKeyframe = true // alink.conf: allow_rq_kf_by_tx_d=1
	}
	if !cfg.AllowTxDropBitrate {
		cfg.AllowTxDropBitrate = true // alink.conf: allow_xtx_reduce_bitrate=1
	}
	if cfg.TxDropBitrateFactor == 0 {
		cfg.TxDropBitrateFactor = 0.8 // alink.conf: xtx_reduce_bitrate_factor=0.8
	}
	if cfg.TxDropRestoreDelay == 0 {
		cfg.TxDropRestoreDelay = 1 * time.Second
	}

	addr := &net.UDPAddr{Port: cfg.Port}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen UDP: %w", err)
	}

	return &LinkReceiver{
		conn:                   conn,
		port:                   cfg.Port,
		profiles:               cfg.Profiles,
		onProfileChange:        cfg.OnProfileChange,
		onFECChange:            cfg.OnFECChange,
		encoderCommand:         cfg.EncoderCommand,
		keyframeCommand:        cfg.KeyframeCommand,
		fecCommand:             cfg.FECCommand,
		bitrateCommand:         cfg.BitrateCommand,
		holdUpDuration:         cfg.HoldUpDuration,
		holdDownDuration:       cfg.HoldDownDuration,
		fallbackTimeout:        cfg.FallbackTimeout,
		fallbackHoldTime:       cfg.FallbackHoldTime,
		smoothingFactor:        cfg.SmoothingFactor,
		smoothingFactorDown:    cfg.SmoothingFactorDown,
		hysteresisPercent:      cfg.HysteresisPercent,
		hysteresisPercentDown:  cfg.HysteresisPercentDown,
		minBetweenChanges:      cfg.MinBetweenChanges,
		keyframeInterval:       cfg.KeyframeInterval,
		idrOnProfileChange:     cfg.IDROnProfileChange,
		keyframeCodeExpiry:     cfg.KeyframeCodeExpiry,
		allowDynamicFEC:        cfg.AllowDynamicFEC,
		fecKAdjust:             cfg.FECKAdjust,
		spikeFix:               cfg.SpikeFix,
		wlanInterface:          cfg.WLANInterface,
		txDroppedCheckInterval: cfg.TxDroppedCheckInterval,
		allowTxDropKeyframe:    cfg.AllowTxDropKeyframe,
		allowTxDropBitrate:     cfg.AllowTxDropBitrate,
		txDropBitrateFactor:    cfg.TxDropBitrateFactor,
		txDropRestoreDelay:     cfg.TxDropRestoreDelay,
		lastProfileIdx:         -1,
		lastFECChange:          -1,
		stopCh:                 make(chan struct{}),
	}, nil
}

// Start begins listening for link quality messages.
func (r *LinkReceiver) Start() {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	r.running = true
	r.lastMessageTime = time.Now()
	r.mu.Unlock()

	r.wg.Add(3)
	go r.receiveLoop()
	go r.fallbackLoop()
	go r.txDroppedLoop()

	log.Printf("[adaptive] Link receiver started on port %d with %d profiles", r.port, len(r.profiles))
}

// Stop stops the link receiver.
func (r *LinkReceiver) Stop() {
	r.mu.Lock()
	wasRunning := r.running
	if r.running {
		r.running = false
		close(r.stopCh)
	}
	// Always close the connection (it's opened in NewLinkReceiver)
	if r.conn != nil {
		r.conn.Close()
	}
	r.mu.Unlock()

	if wasRunning {
		r.wg.Wait()
	}
}

// receiveLoop reads and processes incoming messages.
func (r *LinkReceiver) receiveLoop() {
	defer r.wg.Done()

	buf := make([]byte, 1024)

	for {
		select {
		case <-r.stopCh:
			return
		default:
		}

		r.conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, _, err := r.conn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			if r.running {
				log.Printf("[adaptive] Read error: %v", err)
			}
			continue
		}

		if n < 5 {
			continue
		}

		r.processPacket(buf[:n])
	}
}

// fallbackLoop monitors for GS heartbeat loss and triggers fallback.
func (r *LinkReceiver) fallbackLoop() {
	defer r.wg.Done()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.checkFallback()
		}
	}
}

// txDroppedLoop monitors TX dropped packets and adjusts bitrate/requests keyframes.
func (r *LinkReceiver) txDroppedLoop() {
	defer r.wg.Done()

	ticker := time.NewTicker(r.txDroppedCheckInterval)
	defer ticker.Stop()

	// Wait for initialization
	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.mu.Lock()
			initialized := r.lastProfileIdx >= 0
			r.mu.Unlock()
			if initialized {
				goto monitoring
			}
		}
	}

monitoring:
	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.checkTxDropped()
		}
	}
}

// checkTxDropped monitors TX dropped packets and takes action.
func (r *LinkReceiver) checkTxDropped() {
	// Read current tx_dropped from sysfs
	path := fmt.Sprintf("/sys/class/net/%s/statistics/tx_dropped", r.wlanInterface)
	file, err := os.Open(path)
	if err != nil {
		return // Silently ignore if interface doesn't exist
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		return
	}

	txDropped, err := strconv.ParseInt(strings.TrimSpace(scanner.Text()), 10, 64)
	if err != nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Calculate delta since last check
	delta := txDropped - r.lastTxDropped
	r.lastTxDropped = txDropped

	if delta <= 0 {
		// No new drops - check if we should restore bitrate
		if r.bitrateReduced && time.Since(r.lastTxDropTime) >= r.txDropRestoreDelay {
			r.restoreBitrate()
		}
		return
	}

	// New TX drops detected
	r.totalTxDropped += delta
	r.lastTxDropTime = time.Now()

	log.Printf("[adaptive] TX dropped: %d (total: %d)", delta, r.totalTxDropped)

	// Reduce bitrate if enabled and not already reduced
	if r.allowTxDropBitrate && !r.bitrateReduced && r.currentBitrate > 0 {
		reducedBitrate := int(float64(r.currentBitrate) * r.txDropBitrateFactor)
		r.executeFECAndBitrate(r.currentFecK, r.currentFecN, reducedBitrate)
		r.bitrateReduced = true
		log.Printf("[adaptive] Reduced bitrate due to TX drops: %d -> %d", r.currentBitrate, reducedBitrate)
	}

	// Request keyframe if enabled and GOP > 0.5
	if r.allowTxDropKeyframe && r.currentGOP > 0.5 {
		if time.Since(r.lastKeyframeTime) >= r.keyframeInterval {
			r.txDropKeyframeCount++
			go r.executeKeyframeCommand("tx_dropped")
		}
	}
}

// restoreBitrate restores the original bitrate after TX drops subside.
func (r *LinkReceiver) restoreBitrate() {
	if r.currentBitrate > 0 {
		r.executeFECAndBitrate(r.currentFecK, r.currentFecN, r.currentBitrate)
		log.Printf("[adaptive] Restored bitrate: %d", r.currentBitrate)
	}
	r.bitrateReduced = false
}

// checkFallback checks if we should enter or exit fallback mode.
func (r *LinkReceiver) checkFallback() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.paused {
		return
	}

	now := time.Now()
	timeSinceMessage := now.Sub(r.lastMessageTime)

	if !r.inFallbackMode {
		// Check if we should enter fallback mode
		if timeSinceMessage > r.fallbackTimeout && r.lastProfileIdx != 0 {
			r.inFallbackMode = true
			r.fallbackEntryTime = now
			r.lastProfileIdx = 0
			r.lastChangeTime = now
			r.smoothedScore = 999
			r.lastAppliedScore = 999
			r.scoreInitialized = true

			log.Printf("[adaptive] Fallback mode: no GS heartbeat for %v, switching to safe profile", timeSinceMessage)

			// Apply fallback profile (profile 0 = safest)
			if len(r.profiles) > 0 {
				profile := &r.profiles[0]
				r.applyProfile(profile, nil)
			}
		}
	}
	// Exit from fallback mode happens when we receive a message (in processPacket)
}

// processPacket parses and handles an incoming packet.
func (r *LinkReceiver) processPacket(data []byte) {
	// Skip 4-byte length prefix
	if len(data) < 5 {
		return
	}
	msgStr := string(data[4:])

	// Check for special commands FIRST (must work even when paused)
	if strings.HasPrefix(msgStr, "special:") {
		r.handleSpecialCommand(msgStr[8:])
		return
	}

	// Update last message time (heartbeat)
	r.mu.Lock()
	r.lastMessageTime = time.Now()

	// Check if paused
	if r.paused {
		r.mu.Unlock()
		return
	}

	// Exit fallback mode if hold time has passed
	if r.inFallbackMode {
		if time.Since(r.fallbackEntryTime) >= r.fallbackHoldTime {
			r.inFallbackMode = false
			log.Printf("[adaptive] Exiting fallback mode, GS heartbeat restored")
		} else {
			// Still in fallback hold period, ignore messages
			r.mu.Unlock()
			return
		}
	}
	r.mu.Unlock()

	// Parse link message
	msg, err := parseMessage(msgStr)
	if err != nil {
		log.Printf("[adaptive] Parse error: %v", err)
		return
	}

	// Handle keyframe request from GS (in message field)
	if msg.KeyframeCode != "" {
		r.handleKeyframeCode(msg.KeyframeCode)
	}

	// Handle dynamic FEC change from GS
	r.handleDynamicFEC(msg)

	// Select profile based on score (use RSSI score as primary)
	r.selectAndApplyProfile(msg)
}

// parseMessage parses a colon-separated link message.
func parseMessage(s string) (*LinkMessage, error) {
	parts := strings.Split(s, ":")
	if len(parts) < 10 {
		return nil, fmt.Errorf("not enough fields: %d", len(parts))
	}

	msg := &LinkMessage{}

	var err error
	if msg.Timestamp, err = strconv.ParseInt(parts[0], 10, 64); err != nil {
		return nil, fmt.Errorf("parse timestamp: %w", err)
	}
	if msg.RSSIScore, err = strconv.Atoi(parts[1]); err != nil {
		return nil, fmt.Errorf("parse rssi_score: %w", err)
	}
	if msg.SNRScore, err = strconv.Atoi(parts[2]); err != nil {
		return nil, fmt.Errorf("parse snr_score: %w", err)
	}
	if msg.FECRecovered, err = strconv.Atoi(parts[3]); err != nil {
		return nil, fmt.Errorf("parse fec_rec: %w", err)
	}
	if msg.LostPackets, err = strconv.Atoi(parts[4]); err != nil {
		return nil, fmt.Errorf("parse lost: %w", err)
	}
	if msg.BestRSSI, err = strconv.Atoi(parts[5]); err != nil {
		return nil, fmt.Errorf("parse best_rssi: %w", err)
	}
	if msg.BestSNR, err = strconv.Atoi(parts[6]); err != nil {
		return nil, fmt.Errorf("parse best_snr: %w", err)
	}
	if msg.NumAntennas, err = strconv.Atoi(parts[7]); err != nil {
		return nil, fmt.Errorf("parse num_ant: %w", err)
	}
	if msg.Penalty, err = strconv.Atoi(parts[8]); err != nil {
		return nil, fmt.Errorf("parse penalty: %w", err)
	}
	if msg.FECChange, err = strconv.Atoi(parts[9]); err != nil {
		return nil, fmt.Errorf("parse fec_change: %w", err)
	}

	// Optional keyframe code
	if len(parts) > 10 {
		msg.KeyframeCode = parts[10]
	}

	return msg, nil
}

// handleKeyframeCode processes a keyframe code from GS with deduplication and expiry.
func (r *LinkReceiver) handleKeyframeCode(code string) {
	if code == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()

	// Clean up expired codes
	validCodes := r.keyframeCodes[:0]
	for _, kc := range r.keyframeCodes {
		if now.Sub(kc.timestamp) < r.keyframeCodeExpiry {
			validCodes = append(validCodes, kc)
		}
	}
	r.keyframeCodes = validCodes

	// Check if code already exists (dedupe)
	for _, kc := range r.keyframeCodes {
		if kc.code == code {
			return // Already processed this code
		}
	}

	// Check rate limiting
	if now.Sub(r.lastKeyframeTime) < r.keyframeInterval {
		return
	}

	// Add code and request keyframe
	r.keyframeCodes = append(r.keyframeCodes, keyframeCode{code: code, timestamp: now})

	// Only request if GOP > 0.5
	if r.currentGOP > 0.5 || r.currentGOP == 0 {
		go r.executeKeyframeCommand(code)
	}
}

// handleDynamicFEC adjusts FEC based on GS fec_change field.
func (r *LinkReceiver) handleDynamicFEC(msg *LinkMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.allowDynamicFEC {
		return
	}

	// Only process if fec_change has changed
	if msg.FECChange == r.lastFECChange {
		return
	}

	// Only apply rate limiting for actual FEC changes (fec_change > 0)
	if msg.FECChange > 0 && time.Since(r.lastFECChangeTime) < 1*time.Second {
		return
	}

	r.lastFECChange = msg.FECChange

	// Apply dynamic FEC adjustment
	if msg.FECChange > 0 && msg.FECChange <= 5 && r.currentFecK > 0 && r.currentFecN > 0 {
		// Only update the timestamp when we actually apply a change
		r.lastFECChangeTime = time.Now()
		// Spike fix: only apply if bitrate >= 4000
		if r.spikeFix && r.currentBitrate < 4000 {
			return
		}

		// Calculate adjustment based on fec_change level
		denominators := []float64{1, 1.11111, 1.25, 1.42, 1.66667, 2.0}
		denominator := denominators[msg.FECChange]

		newBitrate := int(float64(r.currentBitrate) / denominator)
		var newFecK, newFecN int
		if r.fecKAdjust {
			newFecK = int(float64(r.currentFecK) / denominator)
			newFecN = r.currentFecN
		} else {
			newFecK = r.currentFecK
			newFecN = int(float64(r.currentFecN) * denominator)
		}

		log.Printf("[adaptive] Dynamic FEC: change=%d, FEC %d/%d -> %d/%d, bitrate %d -> %d",
			msg.FECChange, r.currentFecK, r.currentFecN, newFecK, newFecN, r.currentBitrate, newBitrate)

		r.executeFECAndBitrate(newFecK, newFecN, newBitrate)
	}
}

// executeFECAndBitrate executes FEC and bitrate commands in the correct order.
func (r *LinkReceiver) executeFECAndBitrate(fecK, fecN, bitrate int) {
	// Order depends on whether we're increasing or decreasing bitrate
	if bitrate > r.currentBitrate {
		// Increasing: FEC first, then bitrate
		r.executeFECCommand(fecK, fecN)
		r.executeBitrateCommand(bitrate)
	} else {
		// Decreasing: bitrate first, then FEC
		r.executeBitrateCommand(bitrate)
		r.executeFECCommand(fecK, fecN)
	}

	// Notify callback
	if r.onFECChange != nil {
		r.onFECChange(fecK, fecN, bitrate)
	}
}

// executeFECCommand runs the FEC command template.
func (r *LinkReceiver) executeFECCommand(fecK, fecN int) {
	if r.fecCommand == "" {
		return
	}

	cmd := r.fecCommand
	cmd = strings.ReplaceAll(cmd, "{fecK}", strconv.Itoa(fecK))
	cmd = strings.ReplaceAll(cmd, "{fecN}", strconv.Itoa(fecN))

	out, err := exec.Command("sh", "-c", cmd).CombinedOutput()
	if err != nil {
		log.Printf("[adaptive] FEC command failed: %v: %s", err, out)
	}
}

// executeBitrateCommand runs the bitrate command template.
func (r *LinkReceiver) executeBitrateCommand(bitrate int) {
	if r.bitrateCommand == "" {
		return
	}

	cmd := r.bitrateCommand
	cmd = strings.ReplaceAll(cmd, "{bitrate}", strconv.Itoa(bitrate))

	out, err := exec.Command("sh", "-c", cmd).CombinedOutput()
	if err != nil {
		log.Printf("[adaptive] Bitrate command failed: %v: %s", err, out)
	}
}

// selectAndApplyProfile selects and applies the appropriate profile.
func (r *LinkReceiver) selectAndApplyProfile(msg *LinkMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Apply exponential smoothing to score
	rawScore := float64(msg.RSSIScore)
	var effectiveScore float64

	if !r.scoreInitialized {
		r.smoothedScore = rawScore
		r.scoreInitialized = true
		effectiveScore = rawScore
	} else {
		// Determine smoothing direction based on whether we're above/below last applied profile score
		// This matches vendor behavior: if still above last stable state, smooth slowly;
		// if dropped below last stable state, smooth quickly to react to degradation.
		var alpha float64
		if r.lastAppliedScore == 0 {
			// No profile applied yet - use fast smoothing to be responsive
			alpha = r.smoothingFactorDown
		} else if rawScore < r.lastAppliedScore {
			alpha = r.smoothingFactorDown // Fast response to degrading signal (below last stable)
		} else {
			alpha = r.smoothingFactor // Slow response to improving/stable signal
		}
		r.smoothedScore = alpha*rawScore + (1-alpha)*r.smoothedScore
		effectiveScore = r.smoothedScore
	}

	// Clamp score to 1000-2000
	if effectiveScore < 1000 {
		effectiveScore = 1000
	} else if effectiveScore > 2000 {
		effectiveScore = 2000
	}

	// Find matching profile based on smoothed score
	profileIdx := -1
	var profile *TXProfile
	for i := range r.profiles {
		p := &r.profiles[i]
		if int(effectiveScore) >= p.RangeMin && int(effectiveScore) <= p.RangeMax {
			profileIdx = i
			profile = p
			break
		}
	}

	if profile == nil {
		// No matching profile, use closest
		if effectiveScore < 1000 {
			profileIdx = 0
			profile = &r.profiles[0]
		} else {
			profileIdx = len(r.profiles) - 1
			profile = &r.profiles[profileIdx]
		}
	}

	// Check minimum time between changes
	now := time.Now()
	if r.lastProfileIdx >= 0 && now.Sub(r.lastChangeTime) < r.minBetweenChanges {
		return
	}

	// Apply hysteresis - require larger change to switch
	// Vendor logic: threshold based on score direction (up/down), not profile direction
	if r.lastProfileIdx >= 0 && profileIdx != r.lastProfileIdx && r.lastAppliedScore > 0 {
		// Calculate percent change from last applied score
		percentChange := (effectiveScore - r.lastAppliedScore) / r.lastAppliedScore * 100
		absPercentChange := percentChange
		if absPercentChange < 0 {
			absPercentChange = -absPercentChange
		}

		// Determine which hysteresis threshold to use based on score direction
		// (not profile direction - matches vendor behavior)
		var threshold float64
		if effectiveScore >= r.lastAppliedScore {
			threshold = r.hysteresisPercent // Going up
		} else {
			threshold = r.hysteresisPercentDown // Going down
		}

		if absPercentChange < threshold {
			return // Change not significant enough
		}
	}

	// Check hold-down timers
	if r.lastProfileIdx >= 0 && profileIdx != r.lastProfileIdx {
		elapsed := now.Sub(r.lastChangeTime)

		// Special handling for actual fallback mode (triggered by heartbeat loss)
		// Only apply fallback hold time if we entered via fallback, not just because
		// we happen to be on profile 0 due to low score
		if r.inFallbackMode {
			if elapsed < r.fallbackHoldTime {
				return
			}
		} else if profileIdx > r.lastProfileIdx {
			// Switching to higher quality
			if elapsed < r.holdUpDuration {
				return
			}
		} else {
			// Switching to lower quality
			if elapsed < r.holdDownDuration {
				return
			}
		}
	}

	// Profile changed
	if profileIdx != r.lastProfileIdx {
		r.lastProfileIdx = profileIdx
		r.lastChangeTime = now
		r.lastAppliedScore = effectiveScore // Track score for hysteresis

		log.Printf("[adaptive] Profile changed: score=%d (smoothed=%.0f) -> profile=%d (MCS=%d, FEC=%d/%d, bitrate=%d)",
			msg.RSSIScore, effectiveScore, profileIdx, profile.MCS, profile.FecK, profile.FecN, profile.Bitrate)

		r.applyProfile(profile, msg)
	}
}

// applyProfile applies the selected profile.
func (r *LinkReceiver) applyProfile(profile *TXProfile, msg *LinkMessage) {
	// Update current values for dynamic FEC
	r.currentFecK = profile.FecK
	r.currentFecN = profile.FecN
	r.currentBitrate = profile.Bitrate
	r.currentGOP = profile.GOP
	r.bitrateReduced = false // Reset on profile change

	// Notify callback
	if r.onProfileChange != nil {
		r.onProfileChange(profile, msg)
	}

	// Execute encoder command if configured
	if r.encoderCommand != "" {
		go r.executeEncoderCommand(profile)
	}

	// Request IDR on profile change if enabled
	if r.idrOnProfileChange && r.currentGOP > 0.5 {
		go r.executeKeyframeCommand("profile_change")
	}
}

// handleSpecialCommand handles special commands from the ground station.
func (r *LinkReceiver) handleSpecialCommand(cmd string) {
	parts := strings.SplitN(cmd, ":", 2)
	if len(parts) < 1 {
		return
	}

	switch parts[0] {
	case "request_keyframe":
		if len(parts) > 1 {
			r.handleKeyframeCode(parts[1])
		}
	case "pause_adaptive":
		r.mu.Lock()
		r.paused = true
		r.mu.Unlock()
		log.Printf("[adaptive] Paused")
	case "resume_adaptive":
		r.mu.Lock()
		r.paused = false
		r.mu.Unlock()
		log.Printf("[adaptive] Resumed")
	default:
		log.Printf("[adaptive] Unknown special command: %s", parts[0])
	}
}

// executeKeyframeCommand requests an IDR frame from the video encoder.
func (r *LinkReceiver) executeKeyframeCommand(code string) {
	r.mu.Lock()
	// Rate limit keyframe requests
	if time.Since(r.lastKeyframeTime) < r.keyframeInterval {
		r.mu.Unlock()
		return
	}
	r.lastKeyframeTime = time.Now()
	cmd := r.keyframeCommand
	r.mu.Unlock()

	if cmd == "" {
		log.Printf("[adaptive] Keyframe requested: %s (no command configured)", code)
		return
	}

	log.Printf("[adaptive] Keyframe requested: %s", code)

	out, err := exec.Command("sh", "-c", cmd).CombinedOutput()
	if err != nil {
		log.Printf("[adaptive] Keyframe command failed: %v: %s", err, out)
	}
}

// executeEncoderCommand runs the configured encoder command with profile values.
func (r *LinkReceiver) executeEncoderCommand(profile *TXProfile) {
	cmd := r.encoderCommand
	cmd = strings.ReplaceAll(cmd, "{bitrate}", strconv.Itoa(profile.Bitrate))
	cmd = strings.ReplaceAll(cmd, "{gop}", fmt.Sprintf("%.1f", profile.GOP))
	cmd = strings.ReplaceAll(cmd, "{qp_delta}", strconv.Itoa(profile.QpDelta))
	cmd = strings.ReplaceAll(cmd, "{roi_qp}", profile.ROIqp)
	cmd = strings.ReplaceAll(cmd, "{mcs}", strconv.Itoa(profile.MCS))
	cmd = strings.ReplaceAll(cmd, "{fec_k}", strconv.Itoa(profile.FecK))
	cmd = strings.ReplaceAll(cmd, "{fec_n}", strconv.Itoa(profile.FecN))

	out, err := exec.Command("sh", "-c", cmd).CombinedOutput()
	if err != nil {
		log.Printf("[adaptive] Encoder command failed: %v: %s", err, out)
	}
}

// GetCurrentProfile returns the currently selected profile.
func (r *LinkReceiver) GetCurrentProfile() *TXProfile {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.lastProfileIdx >= 0 && r.lastProfileIdx < len(r.profiles) {
		return &r.profiles[r.lastProfileIdx]
	}
	return nil
}

// SetProfiles updates the profile list.
func (r *LinkReceiver) SetProfiles(profiles []TXProfile) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.profiles = profiles
	r.lastProfileIdx = -1 // Reset selection
	r.scoreInitialized = false
}

// IsInFallbackMode returns true if currently in fallback mode.
func (r *LinkReceiver) IsInFallbackMode() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.inFallbackMode
}

// IsPaused returns true if adaptive mode is paused.
func (r *LinkReceiver) IsPaused() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.paused
}

// SetPaused sets the paused state.
func (r *LinkReceiver) SetPaused(paused bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.paused = paused
}

// GetSmoothedScore returns the current smoothed score.
func (r *LinkReceiver) GetSmoothedScore() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.smoothedScore
}

// GetTxDroppedStats returns TX dropped statistics.
func (r *LinkReceiver) GetTxDroppedStats() (total int64, keyframeCount int, bitrateReduced bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.totalTxDropped, r.txDropKeyframeCount, r.bitrateReduced
}

// LoadProfilesFromString parses profiles from txprofiles.conf format.
// Format: rangeMin - rangeMax setGI setMCS setFecK setFecN setBitrate setGop wfbPower ROIqp bandwidth setQpDelta
func LoadProfilesFromString(data string) ([]TXProfile, error) {
	var profiles []TXProfile

	for _, line := range strings.Split(data, "\n") {
		// Remove comments
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = line[:idx]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Normalize whitespace
		fields := strings.Fields(line)
		if len(fields) < 12 {
			continue
		}

		// Parse: rangeMin - rangeMax setGI setMCS setFecK setFecN setBitrate setGop wfbPower ROIqp bandwidth setQpDelta
		p := TXProfile{}
		var err error

		if p.RangeMin, err = strconv.Atoi(fields[0]); err != nil {
			continue
		}
		// fields[1] is "-"
		if p.RangeMax, err = strconv.Atoi(fields[2]); err != nil {
			continue
		}
		p.ShortGI = strings.ToLower(fields[3]) == "short"
		if p.MCS, err = strconv.Atoi(fields[4]); err != nil {
			continue
		}
		if p.FecK, err = strconv.Atoi(fields[5]); err != nil {
			continue
		}
		if p.FecN, err = strconv.Atoi(fields[6]); err != nil {
			continue
		}
		if p.Bitrate, err = strconv.Atoi(fields[7]); err != nil {
			continue
		}
		if p.GOP, err = strconv.ParseFloat(fields[8], 64); err != nil {
			continue
		}
		if p.TXPower, err = strconv.Atoi(fields[9]); err != nil {
			continue
		}
		p.ROIqp = fields[10]
		if p.Bandwidth, err = strconv.Atoi(fields[11]); err != nil {
			continue
		}
		if len(fields) > 12 {
			p.QpDelta, _ = strconv.Atoi(fields[12])
		}

		profiles = append(profiles, p)
	}

	if len(profiles) == 0 {
		return nil, fmt.Errorf("no valid profiles found")
	}

	return profiles, nil
}
