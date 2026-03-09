package util

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RFTempMeter reads WiFi chip temperatures from Linux hwmon sysfs.
type RFTempMeter struct {
	mu    sync.RWMutex
	wlans []string
	temps map[uint32]int // antenna_id -> temperature in Celsius

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewRFTempMeter creates a new RF temperature meter.
func NewRFTempMeter(wlans []string, interval time.Duration) *RFTempMeter {
	m := &RFTempMeter{
		wlans:  wlans,
		temps:  make(map[uint32]int),
		stopCh: make(chan struct{}),
	}

	// Start background reader
	m.wg.Add(1)
	go m.readLoop(interval)

	return m
}

// GetTemperatures returns a copy of the current temperature readings.
// Map keys are (wlan_idx << 8) | rf_path.
func (m *RFTempMeter) GetTemperatures() map[uint32]int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[uint32]int, len(m.temps))
	for k, v := range m.temps {
		result[k] = v
	}
	return result
}

// Stop stops the temperature meter.
func (m *RFTempMeter) Stop() {
	close(m.stopCh)
	m.wg.Wait()
}

func (m *RFTempMeter) readLoop(interval time.Duration) {
	defer m.wg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Read immediately on start
	m.readTemperatures()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.readTemperatures()
		}
	}
}

func (m *RFTempMeter) readTemperatures() {
	temps := make(map[uint32]int)

	for wlanIdx, wlan := range m.wlans {
		// Try to find hwmon for this interface
		// Path pattern: /sys/class/net/<wlan>/device/hwmon/hwmon*/temp*_input
		hwmonPath := filepath.Join("/sys/class/net", wlan, "device", "hwmon")

		entries, err := os.ReadDir(hwmonPath)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "hwmon") {
				continue
			}

			hwmonDir := filepath.Join(hwmonPath, entry.Name())
			tempFiles, err := filepath.Glob(filepath.Join(hwmonDir, "temp*_input"))
			if err != nil {
				continue
			}

			for rfPath, tempFile := range tempFiles {
				data, err := os.ReadFile(tempFile)
				if err != nil {
					continue
				}

				// Temperature is in millidegrees Celsius
				milliC, err := strconv.Atoi(strings.TrimSpace(string(data)))
				if err != nil {
					continue
				}

				// Convert to Celsius and store
				antID := uint32(wlanIdx)<<8 | uint32(rfPath)
				temps[antID] = milliC / 1000
			}
		}
	}

	m.mu.Lock()
	m.temps = temps
	m.mu.Unlock()
}
