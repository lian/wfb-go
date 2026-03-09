package server

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/vmihailenco/msgpack/v5"
)

// APIServer serves statistics over TCP.
type APIServer struct {
	mu sync.Mutex

	profile     string
	isCluster   bool
	wlans       []string
	logInterval time.Duration

	jsonListener    net.Listener
	msgpackListener net.Listener

	jsonSessions    []*JSONSession
	msgpackSessions []*MsgPackSession

	aggregator *StatsAggregator

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// APIServerConfig holds configuration for the API server.
type APIServerConfig struct {
	Profile       string
	IsCluster     bool
	Wlans         []string
	LogInterval   time.Duration
	JSONPort      int  // 0 to disable
	MsgPackPort   int  // 0 to disable
	Aggregator    *StatsAggregator
}

// NewAPIServer creates a new API server.
func NewAPIServer(cfg APIServerConfig) *APIServer {
	return &APIServer{
		profile:     cfg.Profile,
		isCluster:   cfg.IsCluster,
		wlans:       cfg.Wlans,
		logInterval: cfg.LogInterval,
		aggregator:  cfg.Aggregator,
		stopCh:      make(chan struct{}),
	}
}

// Start starts the API server.
func (s *APIServer) Start(jsonPort, msgpackPort int) error {
	// Start JSON API
	if jsonPort > 0 {
		listener, err := net.Listen("tcp", fmt.Sprintf(":%d", jsonPort))
		if err != nil {
			return fmt.Errorf("json api listen: %w", err)
		}
		s.jsonListener = listener
		log.Printf("JSON API listening on port %d", jsonPort)

		s.wg.Add(1)
		go s.acceptJSONLoop()
	}

	// Start MsgPack API
	if msgpackPort > 0 {
		listener, err := net.Listen("tcp", fmt.Sprintf(":%d", msgpackPort))
		if err != nil {
			if s.jsonListener != nil {
				s.jsonListener.Close()
			}
			return fmt.Errorf("msgpack api listen: %w", err)
		}
		s.msgpackListener = listener
		log.Printf("MsgPack API listening on port %d", msgpackPort)

		s.wg.Add(1)
		go s.acceptMsgPackLoop()
	}

	// Register with aggregator for stats updates
	if s.aggregator != nil {
		s.aggregator.AddCallback(s.onStats)
	}

	return nil
}

// Stop stops the API server.
func (s *APIServer) Stop() {
	close(s.stopCh)

	if s.jsonListener != nil {
		s.jsonListener.Close()
	}
	if s.msgpackListener != nil {
		s.msgpackListener.Close()
	}

	s.mu.Lock()
	for _, sess := range s.jsonSessions {
		sess.Close()
	}
	for _, sess := range s.msgpackSessions {
		sess.Close()
	}
	s.jsonSessions = nil
	s.msgpackSessions = nil
	s.mu.Unlock()

	s.wg.Wait()
}

func (s *APIServer) onStats(stats *AggregatedStats) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Send to JSON sessions
	for _, sess := range s.jsonSessions {
		sess.SendStats(stats)
	}

	// Send to MsgPack sessions
	for _, sess := range s.msgpackSessions {
		sess.SendStats(stats)
	}
}

func (s *APIServer) acceptJSONLoop() {
	defer s.wg.Done()

	for {
		conn, err := s.jsonListener.Accept()
		if err != nil {
			select {
			case <-s.stopCh:
				return
			default:
				log.Printf("JSON API accept error: %v", err)
				continue
			}
		}

		sess := NewJSONSession(conn, s.profile, s.isCluster, s.wlans, s.logInterval)
		s.mu.Lock()
		s.jsonSessions = append(s.jsonSessions, sess)
		s.mu.Unlock()

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			sess.Run()
			s.removeJSONSession(sess)
		}()
	}
}

func (s *APIServer) removeJSONSession(sess *JSONSession) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, existing := range s.jsonSessions {
		if existing == sess {
			s.jsonSessions = append(s.jsonSessions[:i], s.jsonSessions[i+1:]...)
			break
		}
	}
}

func (s *APIServer) acceptMsgPackLoop() {
	defer s.wg.Done()

	for {
		conn, err := s.msgpackListener.Accept()
		if err != nil {
			select {
			case <-s.stopCh:
				return
			default:
				log.Printf("MsgPack API accept error: %v", err)
				continue
			}
		}

		sess := NewMsgPackSession(conn, s.profile, s.isCluster, int(s.logInterval.Milliseconds()))
		s.mu.Lock()
		s.msgpackSessions = append(s.msgpackSessions, sess)
		s.mu.Unlock()

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			sess.Run()
			s.removeMsgPackSession(sess)
		}()
	}
}

func (s *APIServer) removeMsgPackSession(sess *MsgPackSession) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, existing := range s.msgpackSessions {
		if existing == sess {
			s.msgpackSessions = append(s.msgpackSessions[:i], s.msgpackSessions[i+1:]...)
			break
		}
	}
}

// JSONSession handles a single JSON API connection.
type JSONSession struct {
	conn        net.Conn
	profile     string
	isCluster   bool
	wlans       []string
	logInterval time.Duration

	mu       sync.Mutex
	writer   *bufio.Writer
	closed   bool
}

// NewJSONSession creates a new JSON session.
func NewJSONSession(conn net.Conn, profile string, isCluster bool, wlans []string, logInterval time.Duration) *JSONSession {
	return &JSONSession{
		conn:        conn,
		profile:     profile,
		isCluster:   isCluster,
		wlans:       wlans,
		logInterval: logInterval,
		writer:      bufio.NewWriter(conn),
	}
}

// Run runs the session (sends initial settings).
func (s *JSONSession) Run() {
	// Send initial settings message
	settings := map[string]interface{}{
		"type":       "settings",
		"profile":    s.profile,
		"is_cluster": s.isCluster,
		"wlans":      s.wlans,
	}

	s.sendJSON(settings)

	// Keep connection open until closed
	buf := make([]byte, 1)
	for {
		s.conn.SetReadDeadline(time.Now().Add(time.Second))
		_, err := s.conn.Read(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			break
		}
	}
}

// SendStats sends stats to the session.
func (s *JSONSession) SendStats(stats *AggregatedStats) {
	// Send RX stats for each service
	for name, svcStats := range stats.Services {
		if svcStats.PacketsReceived > 0 {
			rxMsg := s.buildRXMessage(name, svcStats, stats)
			s.sendJSON(rxMsg)
		}
		if svcStats.PacketsInjected > 0 {
			txMsg := s.buildTXMessage(name, svcStats)
			s.sendJSON(txMsg)
		}
	}
}

func (s *JSONSession) buildRXMessage(name string, svcStats *ServiceStatsSnapshot, aggStats *AggregatedStats) map[string]interface{} {
	msg := map[string]interface{}{
		"type":      "rx",
		"timestamp": float64(aggStats.Timestamp.UnixNano()) / 1e9,
		"id":        name,
		"packets": map[string]interface{}{
			"all":      []uint64{uint64(svcStats.RxRate), svcStats.PacketsReceived},
			"dec_err":  []uint64{uint64(svcStats.ErrRate), svcStats.PacketsDecErr},
			"fec_rec":  []uint64{uint64(svcStats.FECRate), svcStats.PacketsFECRec},
			"lost":     []uint64{0, svcStats.PacketsLost},
			"bad":      []uint64{0, svcStats.PacketsBad},
			"outgoing": []uint64{uint64(svcStats.RxRate), svcStats.PacketsOutgoing},
		},
	}

	if aggStats.TXWlanIdx != nil {
		msg["tx_wlan"] = *aggStats.TXWlanIdx
	}

	// Add antenna stats
	var antStats []map[string]interface{}
	for key, ant := range aggStats.Antennas {
		antStats = append(antStats, map[string]interface{}{
			"ant":      key,
			"freq":     ant.Freq,
			"mcs":      ant.MCSIndex,
			"bw":       ant.Bandwidth,
			"pkt_recv": ant.PacketsTotal,
			"rssi_min": ant.RSSIMin,
			"rssi_avg": ant.RSSIAvg,
			"rssi_max": ant.RSSIMax,
			"snr_min":  ant.SNRMin,
			"snr_avg":  ant.SNRAvg,
			"snr_max":  ant.SNRMax,
		})
	}
	if len(antStats) > 0 {
		msg["rx_ant_stats"] = antStats
	}

	// Add session info
	if svcStats.SessionEpoch > 0 {
		msg["session"] = map[string]interface{}{
			"epoch": svcStats.SessionEpoch,
			"fec_k": svcStats.SessionFecK,
			"fec_n": svcStats.SessionFecN,
		}
	}

	return msg
}

func (s *JSONSession) buildTXMessage(name string, svcStats *ServiceStatsSnapshot) map[string]interface{} {
	return map[string]interface{}{
		"type":      "tx",
		"timestamp": float64(time.Now().UnixNano()) / 1e9,
		"id":        name,
		"packets": map[string]interface{}{
			"incoming": []uint64{uint64(svcStats.TxRate), svcStats.PacketsIncoming},
			"injected": []uint64{uint64(svcStats.TxRate), svcStats.PacketsInjected},
			"dropped":  []uint64{0, svcStats.PacketsDropped},
		},
	}
}

func (s *JSONSession) sendJSON(data interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return io.EOF
	}

	bytes, err := json.Marshal(data)
	if err != nil {
		return err
	}

	s.writer.Write(bytes)
	s.writer.WriteByte('\n')
	return s.writer.Flush()
}

// Close closes the session.
func (s *JSONSession) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.closed {
		s.closed = true
		s.conn.Close()
	}
}

// MsgPackSession handles a single MsgPack API connection.
type MsgPackSession struct {
	conn        net.Conn
	profile     string
	isCluster   bool
	logInterval int

	mu     sync.Mutex
	closed bool
}

// NewMsgPackSession creates a new MsgPack session.
func NewMsgPackSession(conn net.Conn, profile string, isCluster bool, logInterval int) *MsgPackSession {
	return &MsgPackSession{
		conn:        conn,
		profile:     profile,
		isCluster:   isCluster,
		logInterval: logInterval,
	}
}

// Run runs the session (sends initial message).
func (s *MsgPackSession) Run() {
	// Send initial cli_title message
	initMsg := map[string]interface{}{
		"type":                  "cli_title",
		"cli_title":             fmt.Sprintf("wfb_server [%s]", s.profile),
		"is_cluster":            s.isCluster,
		"log_interval":          s.logInterval,
		"temp_overheat_warning": 60,
	}

	s.sendMsgPack(initMsg)

	// Keep connection open until closed
	buf := make([]byte, 1)
	for {
		s.conn.SetReadDeadline(time.Now().Add(time.Second))
		_, err := s.conn.Read(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			break
		}
	}
}

// SendStats sends stats to the session.
func (s *MsgPackSession) SendStats(stats *AggregatedStats) {
	// Send RX stats
	for name, svcStats := range stats.Services {
		if svcStats.PacketsReceived > 0 {
			// Build antenna stats array
			var antStats []map[string]interface{}
			for key, ant := range stats.Antennas {
				antStats = append(antStats, map[string]interface{}{
					"ant":      key,
					"freq":     ant.Freq,
					"mcs":      ant.MCSIndex,
					"bw":       ant.Bandwidth,
					"pkt_s":    ant.PacketsInterval,
					"pkt_recv": ant.PacketsTotal,
					"rssi_min": ant.RSSIMin,
					"rssi_avg": ant.RSSIAvg,
					"rssi_max": ant.RSSIMax,
					"snr_min":  ant.SNRMin,
					"snr_avg":  ant.SNRAvg,
					"snr_max":  ant.SNRMax,
				})
			}

			rxMsg := map[string]interface{}{
				"type":      "rx",
				"timestamp": float64(stats.Timestamp.UnixNano()) / 1e9,
				"id":        name,
				"packets": map[string]interface{}{
					"all":      [2]uint64{uint64(svcStats.RxRate), svcStats.PacketsReceived},
					"dec_err":  [2]uint64{uint64(svcStats.ErrRate), svcStats.PacketsDecErr},
					"fec_rec":  [2]uint64{uint64(svcStats.FECRate), svcStats.PacketsFECRec},
					"lost":     [2]uint64{0, svcStats.PacketsLost},
					"bad":      [2]uint64{0, svcStats.PacketsBad},
					"outgoing": [2]uint64{uint64(svcStats.RxRate), svcStats.PacketsOutgoing},
				},
				"session": map[string]interface{}{
					"epoch": svcStats.SessionEpoch,
					"fec_k": svcStats.SessionFecK,
					"fec_n": svcStats.SessionFecN,
				},
				"rx_ant_stats": antStats,
			}

			if stats.TXWlanIdx != nil {
				rxMsg["tx_wlan"] = *stats.TXWlanIdx
			}

			s.sendMsgPack(rxMsg)
		}

		if svcStats.PacketsInjected > 0 {
			// Build latency dict keyed by antenna ID
			latencyDict := make(map[uint32][5]uint64)
			for antID, lat := range stats.TXLatency {
				latencyDict[antID] = [5]uint64{
					lat.PacketsInjected,
					lat.PacketsDropped,
					lat.LatencyMin,
					lat.LatencyAvg,
					lat.LatencyMax,
				}
			}

			txMsg := map[string]interface{}{
				"type":      "tx",
				"timestamp": float64(time.Now().UnixNano()) / 1e9,
				"id":        name,
				"packets": map[string]interface{}{
					"incoming": [2]uint64{uint64(svcStats.TxRate), svcStats.PacketsIncoming},
					"injected": [2]uint64{uint64(svcStats.TxRate), svcStats.PacketsInjected},
					"dropped":  [2]uint64{0, svcStats.PacketsDropped},
				},
				"latency":        latencyDict,
				"rf_temperature": stats.RFTemperature,
			}
			s.sendMsgPack(txMsg)
		}
	}
}

// sendMsgPack sends a msgpack-encoded message with length prefix.
// Format: 4-byte big-endian length + msgpack data (matches wfb-ng protocol).
func (s *MsgPackSession) sendMsgPack(data interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return io.EOF
	}

	// Encode as msgpack
	bytes, err := msgpack.Marshal(data)
	if err != nil {
		return err
	}

	// Write length-prefixed message (4-byte big-endian)
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(bytes)))

	if _, err := s.conn.Write(lenBuf); err != nil {
		return err
	}
	if _, err := s.conn.Write(bytes); err != nil {
		return err
	}

	return nil
}

// Close closes the session.
func (s *MsgPackSession) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.closed {
		s.closed = true
		s.conn.Close()
	}
}
