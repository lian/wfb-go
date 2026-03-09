// Package adaptive implements adaptive link quality monitoring and profile selection.
// This is compatible with the adaptive-link project's alink_drone component.
package adaptive

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"sync"
	"time"
)

// LinkMonitor monitors link quality and sends updates to the drone.
// It is protocol-compatible with alink_gs.
type LinkMonitor struct {
	mu sync.Mutex

	// UDP connection to drone
	conn      *net.UDPConn
	droneAddr *net.UDPAddr

	// Stats input - function that returns current link statistics
	getStats func() *LinkStats

	// Configuration
	config LinkConfig

	// Kalman filter state for noise estimation
	kalmanEstimate      float64
	kalmanErrorEstimate float64

	// Keyframe request state
	keyframeCode      string
	keyframeRemaining int

	// Previous state for delta calculations
	prevFECRec  uint32
	prevLost    uint32
	prevAllPkts uint32

	// Last calculated values (for status reporting)
	lastScore     float64
	lastBestRSSI  int
	lastBestSNR   int
	lastNoise     float64
	lastFECRec    int
	lastLost      int
	lastTimestamp time.Time

	// Internal state
	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// MonitorStats contains the last calculated stats from the link monitor.
type MonitorStats struct {
	Score     float64
	BestRSSI  int
	BestSNR   int
	Noise     float64
	FECRec    int
	Lost      int
	Timestamp time.Time
}

// LinkStats contains the statistics needed for adaptive link calculation.
// This can be populated from server.AggregatedStats.
type LinkStats struct {
	// Per-antenna RSSI values (dBm, use avg)
	AntennaRSSI []int8
	// Per-antenna SNR values (dB, use avg)
	AntennaSNR []int8
	// FEC recovered packets (cumulative)
	FECRecovered uint32
	// Lost packets (cumulative)
	LostPackets uint32
	// Total packets received (cumulative, for noise calculation)
	AllPackets uint32
	// Number of active antennas
	NumAntennas int
	// Current FEC settings (for adjusted noise calculation)
	FecK int
	FecN int
}

// LinkConfig configures the link monitor.
type LinkConfig struct {
	// DroneAddr is the drone's UDP address (e.g., "10.5.0.10:9999")
	DroneAddr string
	// UpdateInterval is how often to send updates (default 100ms)
	UpdateInterval time.Duration

	// Score range
	ScoreMin int // Minimum score value (default 1000)
	ScoreMax int // Maximum score value (default 2000)

	// RSSI range for scoring
	RSSIMin int // RSSI floor for scoring (default -85 dBm)
	RSSIMax int // RSSI ceiling for scoring (default -40 dBm)

	// SNR range for scoring
	SNRMin int // SNR floor for scoring (default 10 dB)
	SNRMax int // SNR ceiling for scoring (default 36 dB)

	// Weights for combined score (should sum to 1.0)
	SNRWeight  float64 // Weight for SNR in score calculation (default 0.5)
	RSSIWeight float64 // Weight for RSSI in score calculation (default 0.5)

	// Keyframe request settings
	AllowIDR       bool // Allow keyframe requests on packet loss (default true)
	IDRMaxMessages int  // Number of messages to include keyframe code (default 20)

	// Dynamic refinement
	AllowPenalty     bool // Apply penalty based on noise (default false)
	AllowFECIncrease bool // Suggest FEC increase based on noise (default false)

	// Noise parameters for penalty calculation
	MinNoise          float64 // Noise floor below which no penalty (default 0.01)
	MaxNoise          float64 // Noise ceiling for max penalty (default 0.1)
	DeductionExponent float64 // Exponent for penalty curve (default 0.5)

	// Noise parameters for FEC change suggestion
	MinNoiseForFECChange float64 // Noise threshold to start suggesting FEC (default 0.01)
	NoiseForMaxFECChange float64 // Noise level for max FEC suggestion (default 0.1)

	// Kalman filter parameters for noise estimation
	KalmanEstimate      float64 // Initial estimate (default 0.005)
	KalmanErrorEstimate float64 // Initial error estimate (default 0.1)
	ProcessVariance     float64 // Process noise variance (default 1e-5)
	MeasurementVariance float64 // Measurement noise variance (default 0.01)
}

// DefaultLinkConfig returns a default configuration matching alink_gs defaults.
func DefaultLinkConfig() LinkConfig {
	return LinkConfig{
		DroneAddr:      "10.5.0.10:9999",
		UpdateInterval: 100 * time.Millisecond,

		ScoreMin: 1000,
		ScoreMax: 2000,

		RSSIMin: -85,
		RSSIMax: -40,
		SNRMin:  10,
		SNRMax:  36,

		SNRWeight:  0.5,
		RSSIWeight: 0.5,

		AllowIDR:       true,
		IDRMaxMessages: 20,

		AllowPenalty:     false,
		AllowFECIncrease: false,

		MinNoise:          0.01,
		MaxNoise:          0.1,
		DeductionExponent: 0.5,

		MinNoiseForFECChange: 0.01,
		NoiseForMaxFECChange: 0.1,

		KalmanEstimate:      0.005,
		KalmanErrorEstimate: 0.1,
		ProcessVariance:     1e-5,
		MeasurementVariance: 0.01,
	}
}

// NewLinkMonitor creates a new link monitor.
// getStats is a function that returns current link statistics (typically from server.AggregatedStats).
func NewLinkMonitor(cfg LinkConfig, getStats func() *LinkStats) (*LinkMonitor, error) {
	// Apply defaults for zero values
	if cfg.UpdateInterval == 0 {
		cfg.UpdateInterval = 100 * time.Millisecond
	}
	if cfg.ScoreMin == 0 {
		cfg.ScoreMin = 1000
	}
	if cfg.ScoreMax == 0 {
		cfg.ScoreMax = 2000
	}
	if cfg.RSSIMin == 0 {
		cfg.RSSIMin = -85
	}
	if cfg.RSSIMax == 0 {
		cfg.RSSIMax = -40
	}
	if cfg.SNRMin == 0 {
		cfg.SNRMin = 10
	}
	if cfg.SNRMax == 0 {
		cfg.SNRMax = 36
	}
	if cfg.SNRWeight == 0 && cfg.RSSIWeight == 0 {
		cfg.SNRWeight = 0.5
		cfg.RSSIWeight = 0.5
	}
	if cfg.IDRMaxMessages == 0 {
		cfg.IDRMaxMessages = 20
	}
	if cfg.MinNoise == 0 {
		cfg.MinNoise = 0.01
	}
	if cfg.MaxNoise == 0 {
		cfg.MaxNoise = 0.1
	}
	if cfg.DeductionExponent == 0 {
		cfg.DeductionExponent = 0.5
	}
	if cfg.MinNoiseForFECChange == 0 {
		cfg.MinNoiseForFECChange = 0.01
	}
	if cfg.NoiseForMaxFECChange == 0 {
		cfg.NoiseForMaxFECChange = 0.1
	}
	if cfg.KalmanEstimate == 0 {
		cfg.KalmanEstimate = 0.005
	}
	if cfg.KalmanErrorEstimate == 0 {
		cfg.KalmanErrorEstimate = 0.1
	}
	if cfg.ProcessVariance == 0 {
		cfg.ProcessVariance = 1e-5
	}
	if cfg.MeasurementVariance == 0 {
		cfg.MeasurementVariance = 0.01
	}

	addr, err := net.ResolveUDPAddr("udp", cfg.DroneAddr)
	if err != nil {
		return nil, fmt.Errorf("resolve drone addr: %w", err)
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("dial drone: %w", err)
	}

	return &LinkMonitor{
		conn:                conn,
		droneAddr:           addr,
		getStats:            getStats,
		config:              cfg,
		kalmanEstimate:      cfg.KalmanEstimate,
		kalmanErrorEstimate: cfg.KalmanErrorEstimate,
		stopCh:              make(chan struct{}),
	}, nil
}

// Start begins sending link updates to the drone.
func (m *LinkMonitor) Start() {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.mu.Unlock()

	m.wg.Add(1)
	go m.updateLoop()
}

// Stop stops the link monitor.
func (m *LinkMonitor) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	m.running = false
	close(m.stopCh)
	m.mu.Unlock()

	m.wg.Wait()
	m.conn.Close()
}

// updateLoop periodically sends link updates.
func (m *LinkMonitor) updateLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.config.UpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			stats := m.getStats()
			if stats == nil || stats.NumAntennas == 0 {
				continue
			}

			m.processAndSend(stats)
		}
	}
}

// processAndSend calculates scores and sends the update message.
func (m *LinkMonitor) processAndSend(stats *LinkStats) {
	// Calculate delta packets since last update
	deltaFECRec := stats.FECRecovered - m.prevFECRec
	deltaLost := stats.LostPackets - m.prevLost
	deltaAllPkts := stats.AllPackets - m.prevAllPkts

	m.prevFECRec = stats.FECRecovered
	m.prevLost = stats.LostPackets
	m.prevAllPkts = stats.AllPackets

	// Trigger keyframe request on packet loss
	if deltaLost > 0 && m.config.AllowIDR {
		m.keyframeCode = generateKeyframeCode()
		m.keyframeRemaining = m.config.IDRMaxMessages
	}

	// Calculate noise ratio with FEC adjustment
	var filteredNoise float64
	if deltaAllPkts > 0 && stats.NumAntennas > 0 {
		adjustedFECRec := m.adjustFECRecovered(float64(deltaFECRec), stats.FecK, stats.FecN)
		errorRatio := (5*float64(deltaLost) + adjustedFECRec) / (float64(deltaAllPkts) / float64(stats.NumAntennas))
		filteredNoise = m.kalmanFilterUpdate(errorRatio)
	}

	// Find best RSSI and SNR
	bestRSSI := m.findBest(stats.AntennaRSSI, -100)
	bestSNR := m.findBest(stats.AntennaSNR, 0)

	// Calculate normalized scores
	snrNormalized := m.normalize(bestSNR, m.config.SNRMin, m.config.SNRMax)
	rssiNormalized := m.normalize(bestRSSI, m.config.RSSIMin, m.config.RSSIMax)

	// Combined weighted score
	scoreNormalized := m.config.SNRWeight*snrNormalized + m.config.RSSIWeight*rssiNormalized
	rawScore := float64(m.config.ScoreMin) + scoreNormalized*float64(m.config.ScoreMax-m.config.ScoreMin)

	// Apply penalty based on noise
	var finalScore float64
	var penalty float64
	if m.config.AllowPenalty && filteredNoise >= m.config.MinNoise {
		deductionRatio := m.pow((filteredNoise-m.config.MinNoise)/(m.config.MaxNoise-m.config.MinNoise), m.config.DeductionExponent)
		if deductionRatio > 1.0 {
			deductionRatio = 1.0
		}
		finalScore = float64(m.config.ScoreMin) + (rawScore-float64(m.config.ScoreMin))*(1-deductionRatio)
		penalty = finalScore - rawScore
	} else {
		finalScore = rawScore
		penalty = 0
	}

	// Calculate FEC change suggestion
	var fecChange int
	if m.config.AllowFECIncrease && filteredNoise > m.config.MinNoiseForFECChange {
		if filteredNoise >= m.config.NoiseForMaxFECChange {
			fecChange = 5
		} else {
			fecChange = int((filteredNoise - m.config.MinNoiseForFECChange) / (m.config.MaxNoise - m.config.MinNoiseForFECChange) * 5)
		}
	}

	// Get keyframe code if active
	keyframeCode := ""
	if m.keyframeRemaining > 0 {
		keyframeCode = m.keyframeCode
		m.keyframeRemaining--
		if m.keyframeRemaining == 0 {
			m.keyframeCode = ""
		}
	}

	// Store last calculated values for status reporting
	m.mu.Lock()
	m.lastScore = finalScore
	m.lastBestRSSI = bestRSSI
	m.lastBestSNR = bestSNR
	m.lastNoise = filteredNoise
	m.lastFECRec = int(deltaFECRec)
	m.lastLost = int(deltaLost)
	m.lastTimestamp = time.Now()
	m.mu.Unlock()

	// Build and send message
	// Note: we send finalScore for both rssi_score and snr_score since it's the combined score
	msg := m.buildMessage(
		int(finalScore), int(finalScore),
		int(deltaFECRec), int(deltaLost),
		bestRSSI, bestSNR,
		stats.NumAntennas, int(penalty), fecChange,
		keyframeCode,
	)

	m.sendMessage(msg)
}

// adjustFECRecovered adjusts FEC recovered count based on redundancy.
// Higher redundancy means we expect more FEC recovery, so weight it less.
func (m *LinkMonitor) adjustFECRecovered(fecRec float64, fecK, fecN int) float64 {
	if fecK == 0 || fecN == 0 {
		return fecRec
	}
	redundancy := fecN - fecK
	weight := 6.0 / (1.0 + float64(redundancy)) // 6 makes 8/12 FEC neutral
	return fecRec * weight
}

// kalmanFilterUpdate updates the Kalman filter estimate with a new measurement.
func (m *LinkMonitor) kalmanFilterUpdate(measurement float64) float64 {
	// Prediction
	predictedEstimate := m.kalmanEstimate
	predictedError := m.kalmanErrorEstimate + m.config.ProcessVariance

	// Update
	kalmanGain := predictedError / (predictedError + m.config.MeasurementVariance)
	m.kalmanEstimate = predictedEstimate + kalmanGain*(measurement-predictedEstimate)
	m.kalmanErrorEstimate = (1 - kalmanGain) * predictedError

	return m.kalmanEstimate
}

// findBest finds the best (highest) value in a slice.
func (m *LinkMonitor) findBest(values []int8, defaultVal int) int {
	if len(values) == 0 {
		return defaultVal
	}
	best := int(values[0])
	for _, v := range values[1:] {
		if int(v) > best {
			best = int(v)
		}
	}
	return best
}

// normalize maps a value from [min, max] to [0, 1].
func (m *LinkMonitor) normalize(value, min, max int) float64 {
	if value <= min {
		return 0
	}
	if value >= max {
		return 1
	}
	return float64(value-min) / float64(max-min)
}

// pow computes x^y, clamping result to [0, 1].
func (m *LinkMonitor) pow(x, y float64) float64 {
	if x <= 0 {
		return 0
	}
	result := 1.0
	for i := 0; i < int(y); i++ {
		result *= x
	}
	// Handle fractional exponent approximately
	if y != float64(int(y)) {
		frac := y - float64(int(y))
		result *= (1 - frac) + frac*x
	}
	if result > 1 {
		return 1
	}
	return result
}

// generateKeyframeCode generates a random 4-character keyframe code.
func generateKeyframeCode() string {
	const chars = "abcdefghijklmnopqrstuvwxyz"
	code := make([]byte, 4)
	for i := range code {
		code[i] = chars[rand.Intn(len(chars))]
	}
	return string(code)
}

// calculateRSSIScore calculates a score from RSSI values (for backward compatibility).
// Returns (score, bestRSSI).
func (m *LinkMonitor) calculateRSSIScore(rssiValues []int8) (int, int) {
	bestRSSI := m.findBest(rssiValues, -100)
	normalized := m.normalize(bestRSSI, m.config.RSSIMin, m.config.RSSIMax)
	score := m.config.ScoreMin + int(normalized*float64(m.config.ScoreMax-m.config.ScoreMin))
	return score, bestRSSI
}

// calculateSNRScore calculates a score from SNR values (for backward compatibility).
// Returns (score, bestSNR).
func (m *LinkMonitor) calculateSNRScore(snrValues []int8) (int, int) {
	bestSNR := m.findBest(snrValues, 0)
	normalized := m.normalize(bestSNR, m.config.SNRMin, m.config.SNRMax)
	score := m.config.ScoreMin + int(normalized*float64(m.config.ScoreMax-m.config.ScoreMin))
	return score, bestSNR
}

// buildMessage builds a colon-separated message string.
// Format: timestamp:rssi_score:snr_score:fec_rec:lost:best_rssi:best_snr:num_ant:penalty:fec_change[:keyframe_code]
func (m *LinkMonitor) buildMessage(
	rssiScore, snrScore int,
	fecRec, lost int,
	bestRSSI, bestSNR int,
	numAnt, penalty, fecChange int,
	keyframeCode string,
) string {
	timestamp := time.Now().Unix()

	msg := fmt.Sprintf("%d:%d:%d:%d:%d:%d:%d:%d:%d:%d",
		timestamp, rssiScore, snrScore,
		fecRec, lost,
		bestRSSI, bestSNR,
		numAnt, penalty, fecChange,
	)

	if keyframeCode != "" {
		msg = fmt.Sprintf("%s:%s", msg, keyframeCode)
	}

	return msg
}

// sendMessage sends a length-prefixed message to the drone.
// Wire format: 4-byte big-endian length + UTF-8 message.
func (m *LinkMonitor) sendMessage(msg string) error {
	msgBytes := []byte(msg)

	// Build packet: 4-byte length prefix + message
	packet := make([]byte, 4+len(msgBytes))
	binary.BigEndian.PutUint32(packet[0:4], uint32(len(msgBytes)))
	copy(packet[4:], msgBytes)

	_, err := m.conn.Write(packet)
	return err
}

// SendKeyframeRequest sends a keyframe request to the drone.
func (m *LinkMonitor) SendKeyframeRequest(code string) error {
	msg := fmt.Sprintf("special:request_keyframe:%s", code)
	return m.sendMessage(msg)
}

// SendSpecialCommand sends a special command to the drone.
func (m *LinkMonitor) SendSpecialCommand(command string) error {
	msg := fmt.Sprintf("special:%s", command)
	return m.sendMessage(msg)
}

// GetConfig returns the current configuration.
func (m *LinkMonitor) GetConfig() LinkConfig {
	return m.config
}

// SetConfig updates the configuration (except DroneAddr which requires reconnect).
func (m *LinkMonitor) SetConfig(cfg LinkConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Preserve connection-related fields
	cfg.DroneAddr = m.config.DroneAddr
	m.config = cfg
}

// GetStats returns the last calculated monitoring stats.
func (m *LinkMonitor) GetStats() MonitorStats {
	m.mu.Lock()
	defer m.mu.Unlock()

	return MonitorStats{
		Score:     m.lastScore,
		BestRSSI:  m.lastBestRSSI,
		BestSNR:   m.lastBestSNR,
		Noise:     m.lastNoise,
		FECRec:    m.lastFECRec,
		Lost:      m.lastLost,
		Timestamp: m.lastTimestamp,
	}
}
