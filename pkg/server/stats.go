package server

import (
	"sync"
	"time"
)

// StatsAggregator collects and aggregates statistics from all services.
type StatsAggregator struct {
	mu sync.RWMutex

	profile    string
	linkDomain string
	logInterval time.Duration

	// Current stats by service name
	serviceStats map[string]*ServiceStats

	// Last stats for rate calculation
	lastStats    map[string]*ServiceStats
	lastStatsTime time.Time

	// Aggregated antenna stats across all RX services
	antennaStats map[uint32]*AggregatedAntennaStats

	// TX latency stats (aggregated from all services)
	txLatency map[uint32]*LatencyStatsData

	// RF temperature
	rfTemperature map[uint32]int

	// TX antenna selector
	txSelector *TXAntennaSelector

	// Last computed aggregated stats (for GetStats() to return)
	lastAggStats *AggregatedStats

	// Callbacks for stats updates
	callbacks []StatsCallback

	// Control
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// AggregatedAntennaStats holds aggregated stats for a single antenna.
type AggregatedAntennaStats struct {
	WlanIdx   uint8
	Antenna   uint8
	Freq      uint16
	MCSIndex  uint8
	Bandwidth uint8

	// Interval stats (reset each interval)
	PacketsInterval uint64
	RSSIMin         int8
	RSSIAvg         int8
	RSSIMax         int8
	SNRMin          int8
	SNRAvg          int8
	SNRMax          int8

	// Total stats
	PacketsTotal uint64
}

// StatsCallback is called when stats are updated.
type StatsCallback func(stats *AggregatedStats)

// AggregatedStats holds all stats for an interval.
type AggregatedStats struct {
	Timestamp   time.Time
	Profile     string
	LinkDomain  string
	Interval    time.Duration

	// Per-service stats
	Services map[string]*ServiceStatsSnapshot

	// Aggregated antenna stats
	Antennas map[uint32]*AggregatedAntennaStats

	// Selected TX antenna
	TXWlanIdx *uint8

	// TX latency stats per antenna (keyed by wlan_idx << 8 | ant_id)
	TXLatency map[uint32]*LatencyStatsData

	// RF temperature per antenna (keyed by wlan_idx << 8 | rf_path)
	RFTemperature map[uint32]int
}

// ServiceStatsSnapshot holds a snapshot of service stats with rates.
type ServiceStatsSnapshot struct {
	// Counters
	PacketsReceived uint64
	BytesReceived   uint64
	PacketsDecErr   uint64
	PacketsBad      uint64
	PacketsFECRec   uint64
	PacketsLost     uint64
	PacketsOutgoing uint64
	BytesOutgoing   uint64
	PacketsIncoming uint64
	BytesIncoming   uint64
	PacketsInjected uint64
	BytesInjected   uint64
	PacketsDropped  uint64

	// Rates (per second)
	RxRate      float64
	TxRate      float64
	RxBytesRate float64
	TxBytesRate float64
	FECRate     float64
	ErrRate     float64

	// Session info
	SessionEpoch uint64
	SessionFecK  int
	SessionFecN  int
	SessionMCS   int
}

// StatsAggregatorConfig holds configuration for the stats aggregator.
type StatsAggregatorConfig struct {
	Profile     string
	LinkDomain  string
	LogInterval time.Duration
	TXSelector  *TXAntennaSelector
}

// NewStatsAggregator creates a new stats aggregator.
func NewStatsAggregator(cfg StatsAggregatorConfig) *StatsAggregator {
	if cfg.LogInterval <= 0 {
		cfg.LogInterval = time.Second
	}

	return &StatsAggregator{
		profile:       cfg.Profile,
		linkDomain:   cfg.LinkDomain,
		logInterval:  cfg.LogInterval,
		serviceStats:  make(map[string]*ServiceStats),
		lastStats:     make(map[string]*ServiceStats),
		antennaStats:  make(map[uint32]*AggregatedAntennaStats),
		txLatency:     make(map[uint32]*LatencyStatsData),
		rfTemperature: make(map[uint32]int),
		txSelector:    cfg.TXSelector,
		stopCh:        make(chan struct{}),
	}
}

// Start begins the stats aggregation loop.
func (s *StatsAggregator) Start() {
	s.wg.Add(1)
	go s.aggregateLoop()
}

// Stop stops the stats aggregation.
func (s *StatsAggregator) Stop() {
	close(s.stopCh)
	s.wg.Wait()
}

// AddCallback adds a callback for stats updates.
func (s *StatsAggregator) AddCallback(cb StatsCallback) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callbacks = append(s.callbacks, cb)
}

// UpdateStats updates stats for a service.
func (s *StatsAggregator) UpdateStats(name string, stats *ServiceStats) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.serviceStats[name] = stats
}

// UpdateLatencyStats updates TX latency stats (aggregates across services).
func (s *StatsAggregator) UpdateLatencyStats(latency map[uint32]LatencyStatsData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, data := range latency {
		d := data // copy
		s.txLatency[key] = &d
	}
}

// UpdateRFTemperature updates RF temperature readings.
func (s *StatsAggregator) UpdateRFTemperature(temps map[uint32]int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rfTemperature = temps
}

// GetStats returns the last computed aggregated stats.
// Stats are computed periodically by the aggregation loop, so this returns
// the most recent snapshot rather than recomputing (which could cause
// rate calculation issues due to timing between aggregate() and GetStats()).
func (s *StatsAggregator) GetStats() *AggregatedStats {
	s.mu.Lock()
	defer s.mu.Unlock()

	// If no stats computed yet (aggregator not started), compute current snapshot
	if s.lastAggStats == nil {
		aggStats := s.buildAggregatedStatsLocked(s.logInterval)

		// Update TX antenna selection if we have RX stats
		if s.txSelector != nil && len(s.antennaStats) > 0 {
			s.txSelector.Update(s.antennaStats)
			if idx := s.txSelector.GetSelectedWlan(); idx != nil {
				aggStats.TXWlanIdx = idx
			}
		}

		return aggStats
	}
	return s.lastAggStats
}

func (s *StatsAggregator) aggregateLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.logInterval)
	defer ticker.Stop()

	s.lastStatsTime = time.Now()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.aggregate()
		}
	}
}

func (s *StatsAggregator) aggregate() {
	s.mu.Lock()

	now := time.Now()
	interval := now.Sub(s.lastStatsTime)

	// Build aggregated stats
	aggStats := s.buildAggregatedStatsLocked(interval)

	// Update TX antenna selection if we have RX stats
	if s.txSelector != nil {
		s.txSelector.Update(s.antennaStats)
		if idx := s.txSelector.GetSelectedWlan(); idx != nil {
			aggStats.TXWlanIdx = idx
		}
	}

	// Save current stats as last stats
	for name, stats := range s.serviceStats {
		s.lastStats[name] = stats.Clone()
	}
	s.lastStatsTime = now

	// Store for GetStats() to return
	s.lastAggStats = aggStats

	// Release lock before calling callbacks
	s.mu.Unlock()

	for _, cb := range s.callbacks {
		cb(aggStats)
	}
}

func (s *StatsAggregator) buildAggregatedStats() *AggregatedStats {
	return s.buildAggregatedStatsLocked(s.logInterval)
}

func (s *StatsAggregator) buildAggregatedStatsLocked(interval time.Duration) *AggregatedStats {
	intervalSecs := interval.Seconds()
	if intervalSecs <= 0 {
		intervalSecs = 1
	}

	aggStats := &AggregatedStats{
		Timestamp:     time.Now(),
		Profile:       s.profile,
		LinkDomain:    s.linkDomain,
		Interval:      interval,
		Services:      make(map[string]*ServiceStatsSnapshot),
		Antennas:      make(map[uint32]*AggregatedAntennaStats),
		TXLatency:     make(map[uint32]*LatencyStatsData),
		RFTemperature: make(map[uint32]int),
	}

	// Copy TX latency stats
	for k, v := range s.txLatency {
		aggStats.TXLatency[k] = v
	}

	// Copy RF temperature
	for k, v := range s.rfTemperature {
		aggStats.RFTemperature[k] = v
	}

	// Build per-service snapshots with rates
	for name, stats := range s.serviceStats {
		snapshot := &ServiceStatsSnapshot{
			PacketsReceived: stats.PacketsReceived,
			BytesReceived:   stats.BytesReceived,
			PacketsDecErr:   stats.PacketsDecErr,
			PacketsBad:      stats.PacketsBad,
			PacketsFECRec:   stats.PacketsFECRec,
			PacketsLost:     stats.PacketsLost,
			PacketsOutgoing: stats.PacketsOutgoing,
			BytesOutgoing:   stats.BytesOutgoing,
			PacketsIncoming: stats.PacketsIncoming,
			BytesIncoming:   stats.BytesIncoming,
			PacketsInjected: stats.PacketsInjected,
			BytesInjected:   stats.BytesInjected,
			PacketsDropped:  stats.PacketsDropped,
			SessionEpoch:    stats.SessionEpoch,
			SessionFecK:     stats.SessionFecK,
			SessionFecN:     stats.SessionFecN,
			SessionMCS:      stats.SessionMCS,
		}

		// Calculate rates if we have previous stats
		// Use PacketsOutgoing for RX rate (deduplicated, actual throughput)
		if last, ok := s.lastStats[name]; ok {
			snapshot.RxRate = float64(stats.PacketsOutgoing-last.PacketsOutgoing) / intervalSecs
			snapshot.TxRate = float64(stats.PacketsInjected-last.PacketsInjected) / intervalSecs
			snapshot.RxBytesRate = float64(stats.BytesOutgoing-last.BytesOutgoing) / intervalSecs
			snapshot.TxBytesRate = float64(stats.BytesInjected-last.BytesInjected) / intervalSecs
			snapshot.FECRate = float64(stats.PacketsFECRec-last.PacketsFECRec) / intervalSecs
			snapshot.ErrRate = float64(stats.PacketsDecErr-last.PacketsDecErr) / intervalSecs
		}

		aggStats.Services[name] = snapshot

		// Aggregate antenna stats
		for key, antStats := range stats.AntennaStats {
			if _, ok := aggStats.Antennas[key]; !ok {
				aggStats.Antennas[key] = &AggregatedAntennaStats{
					WlanIdx:   antStats.WlanIdx,
					Antenna:   antStats.Antenna,
					Freq:      antStats.Freq,
					MCSIndex:  antStats.MCSIndex,
					Bandwidth: antStats.Bandwidth,
					RSSIMin:   antStats.RSSIMin,
					RSSIMax:   antStats.RSSIMax,
					SNRMin:    antStats.SNRMin,
					SNRMax:    antStats.SNRMax,
				}
			}

			agg := aggStats.Antennas[key]
			agg.PacketsTotal += antStats.PacketsReceived

			// Update min/max
			if antStats.RSSIMin < agg.RSSIMin || agg.PacketsInterval == 0 {
				agg.RSSIMin = antStats.RSSIMin
			}
			if antStats.RSSIMax > agg.RSSIMax {
				agg.RSSIMax = antStats.RSSIMax
			}
			agg.RSSIAvg = antStats.RSSIAvg // Use latest avg

			if antStats.SNRMin < agg.SNRMin || agg.PacketsInterval == 0 {
				agg.SNRMin = antStats.SNRMin
			}
			if antStats.SNRMax > agg.SNRMax {
				agg.SNRMax = antStats.SNRMax
			}
			agg.SNRAvg = antStats.SNRAvg
		}
	}

	// Copy to internal antenna stats for TX selection
	for key, ant := range aggStats.Antennas {
		s.antennaStats[key] = ant
	}

	return aggStats
}
