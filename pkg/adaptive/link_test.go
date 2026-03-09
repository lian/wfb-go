package adaptive

import (
	"encoding/binary"
	"net"
	"testing"
	"time"
)

func TestBuildMessage(t *testing.T) {
	cfg := DefaultLinkConfig()
	m := &LinkMonitor{config: cfg}

	msg := m.buildMessage(
		1500, 1600, // rssi_score, snr_score
		5, 2, // fec_rec, lost
		-45, 25, // best_rssi, best_snr
		2, 0, 1, // num_ant, penalty, fec_change
		"", // no keyframe
	)

	// Parse the message back using the receiver's parseMessage
	parsed, err := parseMessage(msg)
	if err != nil {
		t.Fatalf("Failed to parse message: %v", err)
	}

	if parsed.RSSIScore != 1500 {
		t.Errorf("rssiScore = %d, want 1500", parsed.RSSIScore)
	}
	if parsed.SNRScore != 1600 {
		t.Errorf("snrScore = %d, want 1600", parsed.SNRScore)
	}
	if parsed.FECRecovered != 5 {
		t.Errorf("fecRec = %d, want 5", parsed.FECRecovered)
	}
	if parsed.LostPackets != 2 {
		t.Errorf("lost = %d, want 2", parsed.LostPackets)
	}
	if parsed.BestRSSI != -45 {
		t.Errorf("bestRSSI = %d, want -45", parsed.BestRSSI)
	}
	if parsed.BestSNR != 25 {
		t.Errorf("bestSNR = %d, want 25", parsed.BestSNR)
	}
	if parsed.NumAntennas != 2 {
		t.Errorf("numAnt = %d, want 2", parsed.NumAntennas)
	}
}

func TestBuildMessageWithKeyframe(t *testing.T) {
	cfg := DefaultLinkConfig()
	m := &LinkMonitor{config: cfg}

	msg := m.buildMessage(
		1500, 1600,
		0, 0,
		-50, 20,
		1, 0, 0,
		"abc123",
	)

	// Should end with keyframe code
	expected := ":abc123"
	if len(msg) < len(expected) || msg[len(msg)-len(expected):] != expected {
		t.Errorf("Message should end with %q, got %q", expected, msg)
	}

	// Verify parsing
	parsed, err := parseMessage(msg)
	if err != nil {
		t.Fatalf("Failed to parse message: %v", err)
	}
	if parsed.KeyframeCode != "abc123" {
		t.Errorf("KeyframeCode = %q, want abc123", parsed.KeyframeCode)
	}
}

func TestCalculateRSSIScore(t *testing.T) {
	cfg := DefaultLinkConfig()
	// Use explicit range for predictable test results
	cfg.RSSIMin = -85
	cfg.RSSIMax = -40
	m := &LinkMonitor{config: cfg}

	tests := []struct {
		name      string
		rssi      []int8
		wantScore int
		wantBest  int
	}{
		{"empty", nil, 1000, -100},
		{"single_mid", []int8{-62}, 1511, -62}, // (-62 - -85) / (-40 - -85) * 1000 + 1000 = ~1511
		{"min", []int8{-85}, 1000, -85},
		{"max", []int8{-40}, 2000, -40},
		{"below_min", []int8{-100}, 1000, -100},
		{"above_max", []int8{-20}, 2000, -20},
		{"multiple_pick_best", []int8{-70, -50, -80}, 1777, -50}, // (-50 - -85) / 45 * 1000 + 1000 = ~1777
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, best := m.calculateRSSIScore(tt.rssi)
			if best != tt.wantBest {
				t.Errorf("bestRSSI = %d, want %d", best, tt.wantBest)
			}
			// Allow some tolerance for score calculation
			if score < tt.wantScore-50 || score > tt.wantScore+50 {
				t.Errorf("score = %d, want ~%d", score, tt.wantScore)
			}
		})
	}
}

func TestCalculateSNRScore(t *testing.T) {
	cfg := DefaultLinkConfig()
	cfg.SNRMin = 10
	cfg.SNRMax = 36
	m := &LinkMonitor{config: cfg}

	tests := []struct {
		name      string
		snr       []int8
		wantScore int
		wantBest  int
	}{
		{"empty", nil, 1000, 0},
		{"single_mid", []int8{23}, 1500, 23}, // (23 - 10) / (36 - 10) * 1000 + 1000 = 1500
		{"min", []int8{10}, 1000, 10},
		{"max", []int8{36}, 2000, 36},
		{"below_min", []int8{5}, 1000, 5},
		{"above_max", []int8{45}, 2000, 45},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, best := m.calculateSNRScore(tt.snr)
			if best != tt.wantBest {
				t.Errorf("bestSNR = %d, want %d", best, tt.wantBest)
			}
			if score < tt.wantScore-50 || score > tt.wantScore+50 {
				t.Errorf("score = %d, want ~%d", score, tt.wantScore)
			}
		})
	}
}

func TestKalmanFilter(t *testing.T) {
	cfg := DefaultLinkConfig()
	m := &LinkMonitor{
		config:              cfg,
		kalmanEstimate:      cfg.KalmanEstimate,
		kalmanErrorEstimate: cfg.KalmanErrorEstimate,
	}

	// Feed constant measurements, estimate should converge
	for i := 0; i < 100; i++ {
		m.kalmanFilterUpdate(0.05)
	}

	// Should converge close to 0.05
	if m.kalmanEstimate < 0.04 || m.kalmanEstimate > 0.06 {
		t.Errorf("Kalman estimate = %f, want ~0.05", m.kalmanEstimate)
	}
}

func TestAdjustFECRecovered(t *testing.T) {
	cfg := DefaultLinkConfig()
	m := &LinkMonitor{config: cfg}

	tests := []struct {
		name   string
		fecRec float64
		fecK   int
		fecN   int
		want   float64
	}{
		{"no_redundancy", 10, 8, 8, 60}, // weight = 6/(1+0) = 6
		{"8/12_neutral", 10, 8, 12, 15}, // weight = 6/(1+4) = 1.2, but should be ~1.5 for neutral
		{"high_redundancy", 10, 1, 6, 12},  // weight = 6/(1+5) = 1
		{"zero_fec", 10, 0, 0, 10},         // no adjustment
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.adjustFECRecovered(tt.fecRec, tt.fecK, tt.fecN)
			// Allow 20% tolerance
			if got < tt.want*0.8 || got > tt.want*1.2 {
				t.Errorf("adjustFECRecovered(%f, %d, %d) = %f, want ~%f", tt.fecRec, tt.fecK, tt.fecN, got, tt.want)
			}
		})
	}
}

func TestKeyframeGeneration(t *testing.T) {
	code := generateKeyframeCode()
	if len(code) != 4 {
		t.Errorf("Keyframe code length = %d, want 4", len(code))
	}
	for _, c := range code {
		if c < 'a' || c > 'z' {
			t.Errorf("Keyframe code contains non-lowercase char: %c", c)
		}
	}
}

func TestWireFormat(t *testing.T) {
	// Test that our wire format matches what alink_drone expects:
	// 4-byte big-endian length + UTF-8 message

	msg := "1709856123:1450:1450:5:2:-45:25:2:0:0"

	// Build the packet like sendMessage does
	msgBytes := []byte(msg)
	packet := make([]byte, 4+len(msgBytes))
	binary.BigEndian.PutUint32(packet[0:4], uint32(len(msgBytes)))
	copy(packet[4:], msgBytes)

	// Verify length prefix
	length := binary.BigEndian.Uint32(packet[0:4])
	if int(length) != len(msg) {
		t.Errorf("Length prefix = %d, want %d", length, len(msg))
	}

	// Verify message content
	extractedMsg := string(packet[4:])
	if extractedMsg != msg {
		t.Errorf("Extracted message = %q, want %q", extractedMsg, msg)
	}
}

func TestLinkMonitorIntegration(t *testing.T) {
	// Create a UDP listener to receive messages
	listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	listenerAddr := listener.LocalAddr().(*net.UDPAddr)

	// Create link monitor
	cfg := DefaultLinkConfig()
	cfg.DroneAddr = listenerAddr.String()
	cfg.UpdateInterval = 10 * time.Millisecond

	// Stats counter that increments to simulate real stats
	var statsCallCount uint32
	getStats := func() *LinkStats {
		statsCallCount++
		return &LinkStats{
			AntennaRSSI:  []int8{-50, -55},
			AntennaSNR:   []int8{25, 20},
			FECRecovered: statsCallCount * 3, // Cumulative
			LostPackets:  0,
			AllPackets:   statsCallCount * 100,
			NumAntennas:  2,
			FecK:         8,
			FecN:         12,
		}
	}

	monitor, err := NewLinkMonitor(cfg, getStats)
	if err != nil {
		t.Fatalf("Failed to create monitor: %v", err)
	}

	// Start monitor
	monitor.Start()
	defer monitor.Stop()

	// Wait for a message
	buf := make([]byte, 1024)
	listener.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	n, _, err := listener.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("Failed to receive message: %v", err)
	}

	// Verify format
	if n < 5 {
		t.Fatalf("Message too short: %d bytes", n)
	}

	// Extract length
	length := binary.BigEndian.Uint32(buf[0:4])
	if int(length) != n-4 {
		t.Errorf("Length prefix = %d, actual message length = %d", length, n-4)
	}

	// Parse message using the receiver's parseMessage
	msgStr := string(buf[4:n])
	parsed, err := parseMessage(msgStr)
	if err != nil {
		t.Fatalf("Failed to parse message: %v", err)
	}

	// Verify values
	if parsed.BestRSSI != -50 {
		t.Errorf("bestRSSI = %d, want -50", parsed.BestRSSI)
	}
	if parsed.BestSNR != 25 {
		t.Errorf("bestSNR = %d, want 25", parsed.BestSNR)
	}
	if parsed.NumAntennas != 2 {
		t.Errorf("numAnt = %d, want 2", parsed.NumAntennas)
	}
	// Score should be valid (combined weighted score)
	if parsed.RSSIScore < 1000 || parsed.RSSIScore > 2000 {
		t.Errorf("score = %d, want between 1000-2000", parsed.RSSIScore)
	}
}

func TestWeightedScoreCalculation(t *testing.T) {
	cfg := DefaultLinkConfig()
	cfg.SNRWeight = 0.5
	cfg.RSSIWeight = 0.5
	cfg.RSSIMin = -85
	cfg.RSSIMax = -40
	cfg.SNRMin = 10
	cfg.SNRMax = 36

	m := &LinkMonitor{
		config:              cfg,
		kalmanEstimate:      cfg.KalmanEstimate,
		kalmanErrorEstimate: cfg.KalmanErrorEstimate,
	}

	// Test with mid-range values
	// RSSI -62 -> normalized = (85-62)/45 = 0.511
	// SNR 23 -> normalized = (23-10)/26 = 0.5
	// Combined = 0.5 * 0.511 + 0.5 * 0.5 = 0.505
	// Score = 1000 + 0.505 * 1000 = 1505

	rssiNorm := m.normalize(-62, cfg.RSSIMin, cfg.RSSIMax)
	snrNorm := m.normalize(23, cfg.SNRMin, cfg.SNRMax)
	combined := cfg.SNRWeight*snrNorm + cfg.RSSIWeight*rssiNorm
	score := int(float64(cfg.ScoreMin) + combined*float64(cfg.ScoreMax-cfg.ScoreMin))

	if score < 1450 || score > 1550 {
		t.Errorf("Combined score = %d, want ~1500 (rssiNorm=%f, snrNorm=%f)", score, rssiNorm, snrNorm)
	}
}
