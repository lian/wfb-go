package server

import (
	"log"
	"sync"
)

// TXAntennaSelector implements TX antenna selection based on RX RSSI.
// It chooses the wlan with the best signal quality for transmission.
type TXAntennaSelector struct {
	mu sync.RWMutex

	// Current selected wlan index
	selectedWlan *uint8

	// Selection parameters
	rssiDelta       int     // RSSI hysteresis (dB)
	counterAbsDelta int     // Packet counter hysteresis (absolute)
	counterRelDelta float64 // Packet counter hysteresis (relative)

	// Last selection stats for hysteresis
	lastBestWlan    *uint8
	lastBestRSSI    int8
	lastBestPackets uint64

	// Callbacks for selection changes
	callbacks []func(wlanIdx uint8)
}

// TXAntennaSelectorConfig holds configuration for the selector.
type TXAntennaSelectorConfig struct {
	RssiDelta       int     // Default: 3 dB
	CounterAbsDelta int     // Default: 3 packets
	CounterRelDelta float64 // Default: 0.1 (10%)
}

// NewTXAntennaSelector creates a new TX antenna selector.
func NewTXAntennaSelector(cfg TXAntennaSelectorConfig) *TXAntennaSelector {
	if cfg.RssiDelta <= 0 {
		cfg.RssiDelta = 3
	}
	if cfg.CounterAbsDelta <= 0 {
		cfg.CounterAbsDelta = 3
	}
	if cfg.CounterRelDelta <= 0 {
		cfg.CounterRelDelta = 0.1
	}

	return &TXAntennaSelector{
		rssiDelta:       cfg.RssiDelta,
		counterAbsDelta: cfg.CounterAbsDelta,
		counterRelDelta: cfg.CounterRelDelta,
	}
}

// AddCallback adds a callback for selection changes.
func (s *TXAntennaSelector) AddCallback(cb func(wlanIdx uint8)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callbacks = append(s.callbacks, cb)
}

// Update updates the selection based on current antenna stats.
// Algorithm matches wfb-ng:
// 1. Aggregate stats per wlan (use max RSSI from all antennas, max packet count)
// 2. Filter to wlans with near-maximum packet counts
// 3. Select wlan with best RSSI among filtered set
// 4. Apply RSSI hysteresis before switching
func (s *TXAntennaSelector) Update(stats map[uint32]*AggregatedAntennaStats) {
	s.mu.Lock()

	// Aggregate stats per wlan - use max RSSI and max packets from all antennas
	wlanStats := make(map[uint8]*wlanAggStats)
	var maxPackets uint64

	for _, ant := range stats {
		ws, ok := wlanStats[ant.WlanIdx]
		if !ok {
			ws = &wlanAggStats{
				rssiMax: -128,
				packets: 0,
			}
			wlanStats[ant.WlanIdx] = ws
		}

		// Use max RSSI from all antennas on this wlan (matching wfb-ng)
		if ant.RSSIAvg > ws.rssiMax {
			ws.rssiMax = ant.RSSIAvg
		}
		// Use max packet count from all antennas
		if ant.PacketsTotal > ws.packets {
			ws.packets = ant.PacketsTotal
		}
	}

	// Find max packets across all wlans
	for _, ws := range wlanStats {
		if ws.packets > maxPackets {
			maxPackets = ws.packets
		}
	}

	if maxPackets == 0 {
		s.mu.Unlock()
		return // No packets received
	}

	// Calculate packet count threshold (wlans must have near-maximum packets to be considered)
	// tx_sel_counter_thr = max_pkts - max(abs_delta, max_pkts * rel_delta)
	absDelta := uint64(s.counterAbsDelta)
	relDelta := uint64(float64(maxPackets) * s.counterRelDelta)
	delta := absDelta
	if relDelta > delta {
		delta = relDelta
	}
	packetThreshold := uint64(0)
	if maxPackets > delta {
		packetThreshold = maxPackets - delta
	}

	// Filter wlans with sufficient packet count
	eligibleWlans := make(map[uint8]*wlanAggStats)
	for wlanIdx, ws := range wlanStats {
		if ws.packets >= packetThreshold {
			eligibleWlans[wlanIdx] = ws
		}
	}

	if len(eligibleWlans) == 0 {
		s.mu.Unlock()
		return // No eligible wlans
	}

	// Find best RSSI among eligible wlans
	// When RSSI is tied, prefer higher wlan index (matches wfb-ng behavior)
	var bestWlan *uint8
	var bestRSSI int8 = -128

	for wlanIdx, ws := range eligibleWlans {
		if bestWlan == nil || ws.rssiMax > bestRSSI ||
			(ws.rssiMax == bestRSSI && wlanIdx > *bestWlan) {
			idx := wlanIdx
			bestWlan = &idx
			bestRSSI = ws.rssiMax
		}
	}

	if bestWlan == nil {
		s.mu.Unlock()
		return
	}

	// Get current wlan's RSSI (default to -128 if not in stats, matching wfb-ng's -1000 default)
	var currentRSSI int8 = -128
	if s.selectedWlan != nil {
		if ws, ok := wlanStats[*s.selectedWlan]; ok {
			currentRSSI = ws.rssiMax
		}
	}

	// Determine if we should switch
	shouldSwitch := false

	if s.selectedWlan == nil {
		// First selection
		shouldSwitch = true
	} else if *s.selectedWlan != *bestWlan {
		// Check if currently selected wlan is still eligible
		_, currentEligible := eligibleWlans[*s.selectedWlan]

		if !currentEligible {
			// Current wlan dropped below packet threshold (or disappeared), must switch
			shouldSwitch = true
		} else {
			// Both current and new are eligible - apply RSSI hysteresis
			rssiDiff := int(bestRSSI) - int(currentRSSI)

			if rssiDiff >= s.rssiDelta {
				shouldSwitch = true
			}
		}
	}

	// Track if we need to call callbacks (after releasing lock)
	var notifyWlan *uint8

	if shouldSwitch {
		oldWlan := s.selectedWlan
		s.selectedWlan = bestWlan
		s.lastBestWlan = bestWlan
		s.lastBestRSSI = bestRSSI
		s.lastBestPackets = wlanStats[*bestWlan].packets

		// Log the change
		if oldWlan == nil || *oldWlan != *bestWlan {
			if oldWlan == nil {
				log.Printf("TX antenna selected: wlan%d (RSSI=%ddB, pkts=%d)",
					*bestWlan, bestRSSI, s.lastBestPackets)
			} else {
				log.Printf("TX antenna switch: wlan%d (RSSI=%ddB) -> wlan%d (RSSI=%ddB, delta=%+ddB)",
					*oldWlan, currentRSSI, *bestWlan, bestRSSI, int(bestRSSI)-int(currentRSSI))
			}
			wlan := *bestWlan
			notifyWlan = &wlan
		}
	}

	// Release lock before calling callbacks (prevents deadlock with server lock)
	s.mu.Unlock()

	if notifyWlan != nil {
		for _, cb := range s.callbacks {
			cb(*notifyWlan)
		}
	}
}

// GetSelectedWlan returns the currently selected wlan index.
func (s *TXAntennaSelector) GetSelectedWlan() *uint8 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.selectedWlan == nil {
		return nil
	}
	idx := *s.selectedWlan
	return &idx
}

// GetSelectionInfo returns detailed selection information.
func (s *TXAntennaSelector) GetSelectionInfo() (wlanIdx *uint8, rssi int8, packets uint64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.selectedWlan, s.lastBestRSSI, s.lastBestPackets
}

type wlanAggStats struct {
	rssiSum   int64
	rssiCount uint64
	packets   uint64
	rssiMax   int8
}
