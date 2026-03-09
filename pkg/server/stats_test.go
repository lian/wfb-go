package server

import (
	"sync"
	"testing"
	"time"
)

func TestNewStatsAggregator(t *testing.T) {
	agg := NewStatsAggregator(StatsAggregatorConfig{
		Profile:     "test",
		LinkDomain:  "domain",
		LogInterval: 500 * time.Millisecond,
	})

	if agg.profile != "test" {
		t.Errorf("profile = %s, want test", agg.profile)
	}
	if agg.linkDomain != "domain" {
		t.Errorf("linkDomain = %s, want domain", agg.linkDomain)
	}
	if agg.logInterval != 500*time.Millisecond {
		t.Errorf("logInterval = %v, want 500ms", agg.logInterval)
	}
}

func TestStatsAggregatorDefaults(t *testing.T) {
	// Test default log interval
	agg := NewStatsAggregator(StatsAggregatorConfig{})

	if agg.logInterval != time.Second {
		t.Errorf("Default logInterval = %v, want 1s", agg.logInterval)
	}
}

func TestStatsAggregatorUpdateStats(t *testing.T) {
	agg := NewStatsAggregator(StatsAggregatorConfig{})

	stats := &ServiceStats{
		PacketsReceived: 100,
		BytesReceived:   5000,
		PacketsInjected: 50,
	}

	agg.UpdateStats("service1", stats)

	// Check stats are stored
	agg.mu.RLock()
	stored := agg.serviceStats["service1"]
	agg.mu.RUnlock()

	if stored == nil {
		t.Fatal("Stats not stored")
	}
	if stored.PacketsReceived != 100 {
		t.Errorf("PacketsReceived = %d, want 100", stored.PacketsReceived)
	}
}

func TestStatsAggregatorGetStats(t *testing.T) {
	agg := NewStatsAggregator(StatsAggregatorConfig{
		Profile:    "test_profile",
		LinkDomain: "test_domain",
	})

	stats := &ServiceStats{
		PacketsReceived: 100,
		BytesReceived:   5000,
	}
	agg.UpdateStats("service1", stats)

	aggStats := agg.GetStats()

	if aggStats.Profile != "test_profile" {
		t.Errorf("Profile = %s, want test_profile", aggStats.Profile)
	}
	if aggStats.LinkDomain != "test_domain" {
		t.Errorf("LinkDomain = %s, want test_domain", aggStats.LinkDomain)
	}

	svc, ok := aggStats.Services["service1"]
	if !ok {
		t.Fatal("Service stats not included")
	}
	if svc.PacketsReceived != 100 {
		t.Errorf("PacketsReceived = %d, want 100", svc.PacketsReceived)
	}
}

func TestStatsAggregatorCallback(t *testing.T) {
	agg := NewStatsAggregator(StatsAggregatorConfig{
		LogInterval: 50 * time.Millisecond,
	})

	var received *AggregatedStats
	var mu sync.Mutex

	agg.AddCallback(func(stats *AggregatedStats) {
		mu.Lock()
		defer mu.Unlock()
		received = stats
	})

	// Add some stats
	agg.UpdateStats("svc1", &ServiceStats{PacketsReceived: 10})

	// Start and wait for callback
	agg.Start()
	time.Sleep(100 * time.Millisecond)
	agg.Stop()

	mu.Lock()
	defer mu.Unlock()

	if received == nil {
		t.Fatal("Callback not invoked")
	}
	if _, ok := received.Services["svc1"]; !ok {
		t.Error("Service stats not in callback")
	}
}

func TestStatsAggregatorRateCalculation(t *testing.T) {
	agg := NewStatsAggregator(StatsAggregatorConfig{
		LogInterval: time.Second, // 1 second interval
	})

	// Set up initial stats
	agg.UpdateStats("svc1", &ServiceStats{
		PacketsReceived: 100,
		BytesReceived:   1000,
		PacketsInjected: 50,
	})

	// Initialize lastStats to simulate previous aggregation
	agg.mu.Lock()
	agg.lastStats["svc1"] = &ServiceStats{
		PacketsReceived: 100,
		BytesReceived:   1000,
		PacketsInjected: 50,
	}
	agg.mu.Unlock()

	// Update with new stats (50 more packets)
	agg.UpdateStats("svc1", &ServiceStats{
		PacketsReceived: 150,
		BytesReceived:   1500,
		PacketsInjected: 100,
	})

	stats := agg.GetStats()
	svc := stats.Services["svc1"]

	// With 1 second logInterval and 50 packets delta, rate = 50 / 1 = 50/s
	// Allow some tolerance
	if svc.RxRate < 40 || svc.RxRate > 60 {
		t.Errorf("RxRate = %f, expected ~50", svc.RxRate)
	}
}

func TestStatsAggregatorAntennaAggregation(t *testing.T) {
	agg := NewStatsAggregator(StatsAggregatorConfig{})

	// Create stats with antenna info
	antKey := uint32(0)<<8 | uint32(0) // wlan 0, antenna 0
	stats := &ServiceStats{
		PacketsReceived: 100,
		AntennaStats: map[uint32]*AntennaStats{
			antKey: {
				WlanIdx:         0,
				Antenna:         0,
				PacketsReceived: 100,
				RSSIMin:         -60,
				RSSIAvg:         -50,
				RSSIMax:         -40,
				SNRMin:          10,
				SNRAvg:          15,
				SNRMax:          20,
			},
		},
	}

	agg.UpdateStats("svc1", stats)

	aggStats := agg.GetStats()

	if len(aggStats.Antennas) != 1 {
		t.Errorf("Antenna count = %d, want 1", len(aggStats.Antennas))
	}

	ant, ok := aggStats.Antennas[antKey]
	if !ok {
		t.Fatal("Antenna stats not aggregated")
	}
	if ant.RSSIAvg != -50 {
		t.Errorf("RSSIAvg = %d, want -50", ant.RSSIAvg)
	}
	if ant.PacketsTotal != 100 {
		t.Errorf("PacketsTotal = %d, want 100", ant.PacketsTotal)
	}
}

func TestStatsAggregatorTXLatency(t *testing.T) {
	agg := NewStatsAggregator(StatsAggregatorConfig{})

	latency := map[uint32]LatencyStatsData{
		0: {
			LatencyAvg: 5500, // microseconds
			LatencyMin: 2000,
			LatencyMax: 10000,
		},
	}

	agg.UpdateLatencyStats(latency)

	stats := agg.GetStats()

	if len(stats.TXLatency) != 1 {
		t.Errorf("TXLatency count = %d, want 1", len(stats.TXLatency))
	}

	lat, ok := stats.TXLatency[0]
	if !ok {
		t.Fatal("Latency stats not included")
	}
	if lat.LatencyAvg != 5500 {
		t.Errorf("LatencyAvg = %d, want 5500", lat.LatencyAvg)
	}
}

func TestStatsAggregatorRFTemperature(t *testing.T) {
	agg := NewStatsAggregator(StatsAggregatorConfig{})

	temps := map[uint32]int{
		0: 45,
		1: 50,
	}

	agg.UpdateRFTemperature(temps)

	stats := agg.GetStats()

	if len(stats.RFTemperature) != 2 {
		t.Errorf("RFTemperature count = %d, want 2", len(stats.RFTemperature))
	}
	if stats.RFTemperature[0] != 45 {
		t.Errorf("RFTemperature[0] = %d, want 45", stats.RFTemperature[0])
	}
}

func TestStatsAggregatorWithTXSelector(t *testing.T) {
	selector := NewTXAntennaSelector(TXAntennaSelectorConfig{})

	agg := NewStatsAggregator(StatsAggregatorConfig{
		LogInterval: 50 * time.Millisecond,
		TXSelector:  selector,
	})

	// Add stats with antenna info
	antKey := uint32(0)<<8 | uint32(0)
	stats := &ServiceStats{
		PacketsReceived: 100,
		AntennaStats: map[uint32]*AntennaStats{
			antKey: {
				WlanIdx:         0,
				Antenna:         0,
				PacketsReceived: 100,
				RSSIAvg:         -50,
			},
		},
	}

	agg.UpdateStats("svc1", stats)

	// Start and let it aggregate
	agg.Start()
	time.Sleep(100 * time.Millisecond)
	agg.Stop()

	// Check that TX selector was updated
	selected := selector.GetSelectedWlan()
	if selected == nil {
		t.Error("TX selector should have a selection")
	} else if *selected != 0 {
		t.Errorf("Selected wlan = %d, want 0", *selected)
	}
}

func TestStatsAggregatorStartStop(t *testing.T) {
	agg := NewStatsAggregator(StatsAggregatorConfig{
		LogInterval: 10 * time.Millisecond,
	})

	var callCount int
	var mu sync.Mutex

	agg.AddCallback(func(stats *AggregatedStats) {
		mu.Lock()
		callCount++
		mu.Unlock()
	})

	agg.Start()
	time.Sleep(50 * time.Millisecond)
	agg.Stop()

	mu.Lock()
	count1 := callCount
	mu.Unlock()

	// After stop, callbacks should stop
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	count2 := callCount
	mu.Unlock()

	if count2 != count1 {
		t.Error("Callbacks should stop after Stop()")
	}
}

func TestServiceStatsSnapshot(t *testing.T) {
	agg := NewStatsAggregator(StatsAggregatorConfig{})

	stats := &ServiceStats{
		PacketsReceived: 100,
		BytesReceived:   5000,
		PacketsDecErr:   5,
		PacketsBad:      3,
		PacketsFECRec:   10,
		PacketsLost:     2,
		PacketsOutgoing: 90,
		BytesOutgoing:   4500,
		PacketsIncoming: 80,
		BytesIncoming:   4000,
		PacketsInjected: 70,
		BytesInjected:   3500,
		PacketsDropped:  1,
		SessionEpoch:    12345,
		SessionFecK:     8,
		SessionFecN:     12,
	}

	agg.UpdateStats("svc1", stats)

	aggStats := agg.GetStats()
	snap := aggStats.Services["svc1"]

	// Verify all fields are copied
	if snap.PacketsReceived != 100 {
		t.Errorf("PacketsReceived = %d", snap.PacketsReceived)
	}
	if snap.BytesReceived != 5000 {
		t.Errorf("BytesReceived = %d", snap.BytesReceived)
	}
	if snap.PacketsDecErr != 5 {
		t.Errorf("PacketsDecErr = %d", snap.PacketsDecErr)
	}
	if snap.PacketsBad != 3 {
		t.Errorf("PacketsBad = %d", snap.PacketsBad)
	}
	if snap.PacketsFECRec != 10 {
		t.Errorf("PacketsFECRec = %d", snap.PacketsFECRec)
	}
	if snap.PacketsLost != 2 {
		t.Errorf("PacketsLost = %d", snap.PacketsLost)
	}
	if snap.SessionEpoch != 12345 {
		t.Errorf("SessionEpoch = %d", snap.SessionEpoch)
	}
	if snap.SessionFecK != 8 || snap.SessionFecN != 12 {
		t.Errorf("SessionFec = (%d, %d)", snap.SessionFecK, snap.SessionFecN)
	}
}

func TestStatsAggregatorAntennaStatsFlow(t *testing.T) {
	selector := NewTXAntennaSelector(TXAntennaSelectorConfig{
		RssiDelta: 3,
	})

	agg := NewStatsAggregator(StatsAggregatorConfig{
		LogInterval: 10 * time.Millisecond,
		TXSelector:  selector,
	})

	// Simulate service stats with antenna info from two wlans
	stats := &ServiceStats{
		PacketsReceived: 200,
		AntennaStats: map[uint32]*AntennaStats{
			// wlan0/ant0 - better signal
			uint32(0)<<8 | uint32(0): {
				WlanIdx:         0,
				Antenna:         0,
				PacketsReceived: 100,
				RSSIMin:         -45,
				RSSIAvg:         -40,
				RSSIMax:         -35,
			},
			// wlan1/ant0 - worse signal
			uint32(1)<<8 | uint32(0): {
				WlanIdx:         1,
				Antenna:         0,
				PacketsReceived: 100,
				RSSIMin:         -65,
				RSSIAvg:         -60,
				RSSIMax:         -55,
			},
		},
	}

	agg.UpdateStats("video", stats)

	// Get aggregated stats - this should trigger TX selection
	aggStats := agg.GetStats()

	// Should have both antennas
	if len(aggStats.Antennas) != 2 {
		t.Errorf("len(Antennas) = %d, want 2", len(aggStats.Antennas))
	}

	// TX selector should pick wlan0 (better RSSI)
	selected := selector.GetSelectedWlan()
	if selected == nil {
		t.Error("TX selector should have a selection")
	} else if *selected != 0 {
		t.Errorf("Selected wlan = %d, want 0 (better RSSI)", *selected)
	}

	// TXWlanIdx should be set in aggregated stats
	if aggStats.TXWlanIdx == nil {
		t.Error("TXWlanIdx should be set")
	} else if *aggStats.TXWlanIdx != 0 {
		t.Errorf("TXWlanIdx = %d, want 0", *aggStats.TXWlanIdx)
	}
}

func TestStatsAggregatorAntennaStatsSwitching(t *testing.T) {
	var selectedWlan uint8 = 255
	var callbackCount int

	selector := NewTXAntennaSelector(TXAntennaSelectorConfig{
		RssiDelta: 3,
	})
	selector.AddCallback(func(wlanIdx uint8) {
		selectedWlan = wlanIdx
		callbackCount++
	})

	agg := NewStatsAggregator(StatsAggregatorConfig{
		LogInterval: 10 * time.Millisecond,
		TXSelector:  selector,
	})

	// First update: wlan0 is better
	stats1 := &ServiceStats{
		PacketsReceived: 100,
		AntennaStats: map[uint32]*AntennaStats{
			uint32(0)<<8 | uint32(0): {
				WlanIdx:         0,
				Antenna:         0,
				PacketsReceived: 100,
				RSSIAvg:         -40,
			},
			uint32(1)<<8 | uint32(0): {
				WlanIdx:         1,
				Antenna:         0,
				PacketsReceived: 100,
				RSSIAvg:         -60,
			},
		},
	}

	agg.UpdateStats("video", stats1)
	agg.GetStats() // Trigger selection

	if selectedWlan != 0 {
		t.Errorf("After first update: selectedWlan = %d, want 0", selectedWlan)
	}
	if callbackCount != 1 {
		t.Errorf("After first update: callbackCount = %d, want 1", callbackCount)
	}

	// Second update: wlan1 is now much better (exceeds hysteresis)
	stats2 := &ServiceStats{
		PacketsReceived: 200,
		AntennaStats: map[uint32]*AntennaStats{
			uint32(0)<<8 | uint32(0): {
				WlanIdx:         0,
				Antenna:         0,
				PacketsReceived: 100,
				RSSIAvg:         -70, // Got much worse
			},
			uint32(1)<<8 | uint32(0): {
				WlanIdx:         1,
				Antenna:         0,
				PacketsReceived: 100,
				RSSIAvg:         -40, // Got much better
			},
		},
	}

	agg.UpdateStats("video", stats2)
	agg.GetStats() // Trigger selection

	if selectedWlan != 1 {
		t.Errorf("After second update: selectedWlan = %d, want 1", selectedWlan)
	}
	if callbackCount != 2 {
		t.Errorf("After second update: callbackCount = %d, want 2", callbackCount)
	}
}
