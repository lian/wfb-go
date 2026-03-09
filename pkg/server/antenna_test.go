package server

import (
	"sync"
	"testing"
)

func TestNewTXAntennaSelector(t *testing.T) {
	selector := NewTXAntennaSelector(TXAntennaSelectorConfig{})

	// Check defaults
	if selector.rssiDelta != 3 {
		t.Errorf("rssiDelta = %d, want 3", selector.rssiDelta)
	}
	if selector.counterAbsDelta != 3 {
		t.Errorf("counterAbsDelta = %d, want 3", selector.counterAbsDelta)
	}
	if selector.counterRelDelta != 0.1 {
		t.Errorf("counterRelDelta = %f, want 0.1", selector.counterRelDelta)
	}

	// No selection yet
	if selector.GetSelectedWlan() != nil {
		t.Error("Expected nil selection initially")
	}
}

func TestTXAntennaSelectorWithConfig(t *testing.T) {
	selector := NewTXAntennaSelector(TXAntennaSelectorConfig{
		RssiDelta:       5,
		CounterAbsDelta: 10,
		CounterRelDelta: 0.2,
	})

	if selector.rssiDelta != 5 {
		t.Errorf("rssiDelta = %d, want 5", selector.rssiDelta)
	}
	if selector.counterAbsDelta != 10 {
		t.Errorf("counterAbsDelta = %d, want 10", selector.counterAbsDelta)
	}
	if selector.counterRelDelta != 0.2 {
		t.Errorf("counterRelDelta = %f, want 0.2", selector.counterRelDelta)
	}
}

func TestTXAntennaSelectorBasicSelection(t *testing.T) {
	selector := NewTXAntennaSelector(TXAntennaSelectorConfig{})

	// Create stats with wlan 0 having best RSSI
	stats := map[uint32]*AggregatedAntennaStats{
		makeAntennaKey(0, 0): {
			WlanIdx:      0,
			Antenna:      0,
			RSSIAvg:      -50,
			PacketsTotal: 100,
		},
		makeAntennaKey(1, 0): {
			WlanIdx:      1,
			Antenna:      0,
			RSSIAvg:      -60,
			PacketsTotal: 100,
		},
	}

	selector.Update(stats)

	selected := selector.GetSelectedWlan()
	if selected == nil {
		t.Fatal("Expected a selection")
	}
	if *selected != 0 {
		t.Errorf("Selected wlan = %d, want 0 (best RSSI)", *selected)
	}
}

func TestTXAntennaSelectorHysteresis(t *testing.T) {
	selector := NewTXAntennaSelector(TXAntennaSelectorConfig{
		RssiDelta: 5, // Require 5dB improvement to switch
	})

	// Initial selection: wlan 0
	stats := map[uint32]*AggregatedAntennaStats{
		makeAntennaKey(0, 0): {
			WlanIdx:      0,
			Antenna:      0,
			RSSIAvg:      -50,
			PacketsTotal: 100,
		},
		makeAntennaKey(1, 0): {
			WlanIdx:      1,
			Antenna:      0,
			RSSIAvg:      -55,
			PacketsTotal: 100,
		},
	}

	selector.Update(stats)
	if *selector.GetSelectedWlan() != 0 {
		t.Error("Initial selection should be wlan 0")
	}

	// Now wlan 1 is slightly better (-48 vs -50), but not by 5dB
	stats[makeAntennaKey(0, 0)].RSSIAvg = -50
	stats[makeAntennaKey(1, 0)].RSSIAvg = -48

	selector.Update(stats)

	// Should NOT switch due to hysteresis
	if *selector.GetSelectedWlan() != 0 {
		t.Error("Should not switch with only 2dB improvement (hysteresis = 5dB)")
	}

	// Now wlan 1 is much better (-40 vs -50), difference > 5dB
	stats[makeAntennaKey(1, 0)].RSSIAvg = -40

	selector.Update(stats)

	// Should switch now
	if *selector.GetSelectedWlan() != 1 {
		t.Error("Should switch with 10dB improvement")
	}
}

func TestTXAntennaSelectorCallback(t *testing.T) {
	selector := NewTXAntennaSelector(TXAntennaSelectorConfig{})

	var callbackWlan uint8
	var callbackCount int
	var mu sync.Mutex

	selector.AddCallback(func(wlanIdx uint8) {
		mu.Lock()
		defer mu.Unlock()
		callbackWlan = wlanIdx
		callbackCount++
	})

	stats := map[uint32]*AggregatedAntennaStats{
		makeAntennaKey(0, 0): {
			WlanIdx:      0,
			Antenna:      0,
			RSSIAvg:      -50,
			PacketsTotal: 100,
		},
	}

	selector.Update(stats)

	mu.Lock()
	if callbackCount != 1 {
		t.Errorf("Callback count = %d, want 1", callbackCount)
	}
	if callbackWlan != 0 {
		t.Errorf("Callback wlan = %d, want 0", callbackWlan)
	}
	mu.Unlock()
}

func TestTXAntennaSelectorGetSelectionInfo(t *testing.T) {
	selector := NewTXAntennaSelector(TXAntennaSelectorConfig{})

	stats := map[uint32]*AggregatedAntennaStats{
		makeAntennaKey(0, 0): {
			WlanIdx:      0,
			Antenna:      0,
			RSSIAvg:      -45,
			PacketsTotal: 150,
		},
	}

	selector.Update(stats)

	wlan, rssi, packets := selector.GetSelectionInfo()
	if wlan == nil || *wlan != 0 {
		t.Error("Expected wlan 0")
	}
	if rssi != -45 {
		t.Errorf("RSSI = %d, want -45", rssi)
	}
	if packets != 150 {
		t.Errorf("Packets = %d, want 150", packets)
	}
}

func TestTXAntennaSelectorMultipleAntennasSameWlan(t *testing.T) {
	selector := NewTXAntennaSelector(TXAntennaSelectorConfig{})

	// Multiple antennas on same wlan - uses MAX RSSI (wfb-ng algorithm)
	// Both wlans have same packet count to ensure both are eligible
	stats := map[uint32]*AggregatedAntennaStats{
		makeAntennaKey(0, 0): {
			WlanIdx:      0,
			Antenna:      0,
			RSSIAvg:      -40,
			PacketsTotal: 100,
		},
		makeAntennaKey(0, 1): {
			WlanIdx:      0,
			Antenna:      1,
			RSSIAvg:      -60,
			PacketsTotal: 100,
		},
		makeAntennaKey(1, 0): {
			WlanIdx:      1,
			Antenna:      0,
			RSSIAvg:      -45,
			PacketsTotal: 100, // Same packet count so both are eligible
		},
	}

	selector.Update(stats)

	// Wlan 0: max RSSI = -40 (from antenna 0), max packets = 100
	// Wlan 1: max RSSI = -45, packets = 100
	// Both eligible (same packet count), wlan 0 should be selected (best max RSSI)
	selected := selector.GetSelectedWlan()
	if selected == nil || *selected != 0 {
		t.Errorf("Expected wlan 0 to be selected (best max RSSI = -40), got wlan %d", *selected)
	}
}

// TestTXAntennaSelectorUsesMaxRSSI verifies the wfb-ng algorithm uses max RSSI per wlan
func TestTXAntennaSelectorUsesMaxRSSI(t *testing.T) {
	selector := NewTXAntennaSelector(TXAntennaSelectorConfig{})

	// Wlan 0: has one weak antenna (-70) and one strong antenna (-30)
	// Wlan 1: has one medium antenna (-45)
	// wfb-ng uses max RSSI, so wlan 0 should win with -30
	// Both have same packet count to ensure both are eligible
	stats := map[uint32]*AggregatedAntennaStats{
		makeAntennaKey(0, 0): {
			WlanIdx:      0,
			Antenna:      0,
			RSSIAvg:      -70,
			PacketsTotal: 100,
		},
		makeAntennaKey(0, 1): {
			WlanIdx:      0,
			Antenna:      1,
			RSSIAvg:      -30, // Strong antenna
			PacketsTotal: 100,
		},
		makeAntennaKey(1, 0): {
			WlanIdx:      1,
			Antenna:      0,
			RSSIAvg:      -45,
			PacketsTotal: 100, // Same packet count so both are eligible
		},
	}

	selector.Update(stats)

	selected := selector.GetSelectedWlan()
	if selected == nil || *selected != 0 {
		t.Errorf("Expected wlan 0 (max RSSI -30), got wlan %d", *selected)
	}
}

// TestTXAntennaSelectorPacketCountThreshold tests that wlans with low packet counts are excluded
func TestTXAntennaSelectorPacketCountThreshold(t *testing.T) {
	selector := NewTXAntennaSelector(TXAntennaSelectorConfig{
		CounterAbsDelta: 10,   // Allow 10 packets below max
		CounterRelDelta: 0.05, // Or 5% below max
	})

	// Wlan 0: 100 packets, RSSI -30 (excellent)
	// Wlan 1: 1000 packets, RSSI -50 (good)
	// Threshold = 1000 - max(10, 1000*0.05) = 1000 - 50 = 950
	// Wlan 0 (100 packets) < 950, so excluded
	// Wlan 1 should be selected despite worse RSSI
	stats := map[uint32]*AggregatedAntennaStats{
		makeAntennaKey(0, 0): {
			WlanIdx:      0,
			Antenna:      0,
			RSSIAvg:      -30, // Excellent RSSI but low packet count
			PacketsTotal: 100,
		},
		makeAntennaKey(1, 0): {
			WlanIdx:      1,
			Antenna:      0,
			RSSIAvg:      -50,
			PacketsTotal: 1000,
		},
	}

	selector.Update(stats)

	selected := selector.GetSelectedWlan()
	if selected == nil || *selected != 1 {
		t.Errorf("Expected wlan 1 (wlan 0 excluded by packet threshold), got wlan %v", selected)
	}
}

// TestTXAntennaSelectorPacketThresholdAbsVsRel tests abs vs rel delta calculation
func TestTXAntennaSelectorPacketThresholdAbsVsRel(t *testing.T) {
	// Test case 1: abs delta is larger
	t.Run("abs_delta_larger", func(t *testing.T) {
		selector := NewTXAntennaSelector(TXAntennaSelectorConfig{
			CounterAbsDelta: 50,   // 50 packets
			CounterRelDelta: 0.01, // 1% = 1 packet at 100
		})

		// Max packets = 100, threshold = 100 - max(50, 1) = 50
		stats := map[uint32]*AggregatedAntennaStats{
			makeAntennaKey(0, 0): {
				WlanIdx:      0,
				RSSIAvg:      -30,
				PacketsTotal: 100,
			},
			makeAntennaKey(1, 0): {
				WlanIdx:      1,
				RSSIAvg:      -40,
				PacketsTotal: 51, // Above threshold (50)
			},
		}

		selector.Update(stats)

		// Both should be eligible, wlan 0 should win on RSSI
		selected := selector.GetSelectedWlan()
		if selected == nil || *selected != 0 {
			t.Errorf("Expected wlan 0, got %v", selected)
		}
	})

	// Test case 2: rel delta is larger
	t.Run("rel_delta_larger", func(t *testing.T) {
		selector := NewTXAntennaSelector(TXAntennaSelectorConfig{
			CounterAbsDelta: 5,   // 5 packets
			CounterRelDelta: 0.2, // 20% = 200 packets at 1000
		})

		// Max packets = 1000, threshold = 1000 - max(5, 200) = 800
		stats := map[uint32]*AggregatedAntennaStats{
			makeAntennaKey(0, 0): {
				WlanIdx:      0,
				RSSIAvg:      -30,
				PacketsTotal: 750, // Below threshold (800), excluded
			},
			makeAntennaKey(1, 0): {
				WlanIdx:      1,
				RSSIAvg:      -50,
				PacketsTotal: 1000,
			},
		}

		selector.Update(stats)

		// Wlan 0 excluded, wlan 1 should be selected
		selected := selector.GetSelectedWlan()
		if selected == nil || *selected != 1 {
			t.Errorf("Expected wlan 1 (wlan 0 excluded by rel threshold), got %v", selected)
		}
	})
}

// TestTXAntennaSelectorForcedSwitchBelowThreshold tests immediate switch when current drops below threshold
func TestTXAntennaSelectorForcedSwitchBelowThreshold(t *testing.T) {
	selector := NewTXAntennaSelector(TXAntennaSelectorConfig{
		RssiDelta:       10,  // High hysteresis
		CounterAbsDelta: 10,
		CounterRelDelta: 0.1,
	})

	// Initial: wlan 0 selected
	stats := map[uint32]*AggregatedAntennaStats{
		makeAntennaKey(0, 0): {
			WlanIdx:      0,
			RSSIAvg:      -40,
			PacketsTotal: 100,
		},
		makeAntennaKey(1, 0): {
			WlanIdx:      1,
			RSSIAvg:      -45, // Only 5dB worse
			PacketsTotal: 100,
		},
	}

	selector.Update(stats)
	if *selector.GetSelectedWlan() != 0 {
		t.Fatal("Initial selection should be wlan 0")
	}

	// Now wlan 0 drops significantly in packet count
	// Max = 100, threshold = 100 - max(10, 10) = 90
	// Wlan 0 now has only 50 packets (below 90), should switch immediately
	stats[makeAntennaKey(0, 0)].PacketsTotal = 50
	stats[makeAntennaKey(0, 0)].RSSIAvg = -35 // Even better RSSI, but doesn't matter

	selector.Update(stats)

	// Should switch to wlan 1 despite only 5dB difference (no hysteresis when current is ineligible)
	selected := selector.GetSelectedWlan()
	if selected == nil || *selected != 1 {
		t.Errorf("Expected immediate switch to wlan 1 when wlan 0 dropped below threshold, got %v", selected)
	}
}

// TestTXAntennaSelectorHysteresisOnlyWhenBothEligible tests hysteresis applies only when both wlans eligible
func TestTXAntennaSelectorHysteresisOnlyWhenBothEligible(t *testing.T) {
	selector := NewTXAntennaSelector(TXAntennaSelectorConfig{
		RssiDelta:       10, // 10dB hysteresis
		CounterAbsDelta: 5,
		CounterRelDelta: 0.1,
	})

	// Initial: wlan 0 selected, both have similar packet counts
	stats := map[uint32]*AggregatedAntennaStats{
		makeAntennaKey(0, 0): {
			WlanIdx:      0,
			RSSIAvg:      -50,
			PacketsTotal: 100,
		},
		makeAntennaKey(1, 0): {
			WlanIdx:      1,
			RSSIAvg:      -55,
			PacketsTotal: 100,
		},
	}

	selector.Update(stats)
	if *selector.GetSelectedWlan() != 0 {
		t.Fatal("Initial selection should be wlan 0")
	}

	// Wlan 1 improves by 8dB (still less than 10dB hysteresis)
	stats[makeAntennaKey(1, 0)].RSSIAvg = -42 // 8dB better than wlan 0's -50

	selector.Update(stats)

	// Should NOT switch - hysteresis prevents it
	if *selector.GetSelectedWlan() != 0 {
		t.Error("Should not switch with 8dB improvement (hysteresis = 10dB)")
	}

	// Wlan 1 improves to 12dB better
	stats[makeAntennaKey(1, 0)].RSSIAvg = -38 // 12dB better than wlan 0's -50

	selector.Update(stats)

	// Should switch now - exceeds hysteresis
	if *selector.GetSelectedWlan() != 1 {
		t.Error("Should switch with 12dB improvement")
	}
}

// TestTXAntennaSelectorNoPackets tests handling of zero packets
func TestTXAntennaSelectorNoPackets(t *testing.T) {
	selector := NewTXAntennaSelector(TXAntennaSelectorConfig{})

	stats := map[uint32]*AggregatedAntennaStats{
		makeAntennaKey(0, 0): {
			WlanIdx:      0,
			RSSIAvg:      -40,
			PacketsTotal: 0,
		},
	}

	selector.Update(stats)

	// Should not select anything with 0 packets
	if selector.GetSelectedWlan() != nil {
		t.Error("Should not select wlan with 0 packets")
	}
}

// TestTXAntennaSelectorEmptyStats tests handling of empty stats
func TestTXAntennaSelectorEmptyStats(t *testing.T) {
	selector := NewTXAntennaSelector(TXAntennaSelectorConfig{})

	selector.Update(map[uint32]*AggregatedAntennaStats{})

	if selector.GetSelectedWlan() != nil {
		t.Error("Should not select anything with empty stats")
	}
}

// TestTXAntennaSelectorMaxPacketsPerWlan tests that max packet count per wlan is used
func TestTXAntennaSelectorMaxPacketsPerWlan(t *testing.T) {
	selector := NewTXAntennaSelector(TXAntennaSelectorConfig{
		CounterAbsDelta: 5,
		CounterRelDelta: 0.05,
	})

	// Wlan 0: antenna 0 has 50 packets, antenna 1 has 100 packets -> max = 100
	// Wlan 1: 95 packets
	// Max across all = 100, threshold = 100 - max(5, 5) = 95
	// Both should be eligible (wlan 0 max=100, wlan 1=95)
	stats := map[uint32]*AggregatedAntennaStats{
		makeAntennaKey(0, 0): {
			WlanIdx:      0,
			Antenna:      0,
			RSSIAvg:      -40,
			PacketsTotal: 50,
		},
		makeAntennaKey(0, 1): {
			WlanIdx:      0,
			Antenna:      1,
			RSSIAvg:      -45,
			PacketsTotal: 100, // Max for wlan 0
		},
		makeAntennaKey(1, 0): {
			WlanIdx:      1,
			Antenna:      0,
			RSSIAvg:      -35, // Best RSSI
			PacketsTotal: 95,
		},
	}

	selector.Update(stats)

	// Wlan 1 has best RSSI and is eligible
	selected := selector.GetSelectedWlan()
	if selected == nil || *selected != 1 {
		t.Errorf("Expected wlan 1 (best RSSI among eligible), got %v", selected)
	}
}

// TestTXAntennaSelectorStaysOnCurrentWhenEqual tests no unnecessary switching
func TestTXAntennaSelectorStaysOnCurrentWhenEqual(t *testing.T) {
	selector := NewTXAntennaSelector(TXAntennaSelectorConfig{
		RssiDelta: 3,
	})

	// Both wlans have identical stats
	stats := map[uint32]*AggregatedAntennaStats{
		makeAntennaKey(0, 0): {
			WlanIdx:      0,
			RSSIAvg:      -50,
			PacketsTotal: 100,
		},
		makeAntennaKey(1, 0): {
			WlanIdx:      1,
			RSSIAvg:      -50, // Same RSSI
			PacketsTotal: 100,
		},
	}

	selector.Update(stats)
	initial := *selector.GetSelectedWlan()

	// Update multiple times with same stats
	for i := 0; i < 10; i++ {
		selector.Update(stats)
	}

	// Should stay on same wlan (no oscillation)
	if *selector.GetSelectedWlan() != initial {
		t.Error("Should stay on initial selection when stats are equal")
	}
}

// TestTXAntennaSelectorWlanDisappears tests handling when selected wlan disappears from stats
func TestTXAntennaSelectorWlanDisappears(t *testing.T) {
	selector := NewTXAntennaSelector(TXAntennaSelectorConfig{
		RssiDelta: 10, // High hysteresis
	})

	// Initial: wlan 0 selected
	stats := map[uint32]*AggregatedAntennaStats{
		makeAntennaKey(0, 0): {
			WlanIdx:      0,
			RSSIAvg:      -40,
			PacketsTotal: 100,
		},
		makeAntennaKey(1, 0): {
			WlanIdx:      1,
			RSSIAvg:      -50, // 10dB worse
			PacketsTotal: 100,
		},
	}

	selector.Update(stats)
	if *selector.GetSelectedWlan() != 0 {
		t.Fatal("Initial selection should be wlan 0")
	}

	// Wlan 0 completely disappears from stats (e.g., adapter removed)
	stats = map[uint32]*AggregatedAntennaStats{
		makeAntennaKey(1, 0): {
			WlanIdx:      1,
			RSSIAvg:      -50,
			PacketsTotal: 100,
		},
	}

	selector.Update(stats)

	// Should switch to wlan 1 (the only one available)
	selected := selector.GetSelectedWlan()
	if selected == nil || *selected != 1 {
		t.Errorf("Expected switch to wlan 1 when wlan 0 disappeared, got %v", selected)
	}
}

// TestTXAntennaSelectorTiebreaker tests that higher wlan index wins on RSSI tie (matches wfb-ng)
func TestTXAntennaSelectorTiebreaker(t *testing.T) {
	selector := NewTXAntennaSelector(TXAntennaSelectorConfig{})

	// Both wlans have identical RSSI
	stats := map[uint32]*AggregatedAntennaStats{
		makeAntennaKey(0, 0): {
			WlanIdx:      0,
			RSSIAvg:      -50,
			PacketsTotal: 100,
		},
		makeAntennaKey(1, 0): {
			WlanIdx:      1,
			RSSIAvg:      -50, // Same RSSI
			PacketsTotal: 100,
		},
	}

	selector.Update(stats)

	// Higher wlan index (1) should win on tie
	selected := selector.GetSelectedWlan()
	if selected == nil || *selected != 1 {
		t.Errorf("Expected wlan 1 to win on RSSI tie (higher index), got wlan %v", selected)
	}
}

// Helper function
func makeAntennaKey(wlanIdx, antenna uint8) uint32 {
	return uint32(wlanIdx)<<8 | uint32(antenna)
}
