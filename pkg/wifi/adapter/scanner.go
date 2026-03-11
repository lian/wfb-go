package adapter

import (
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

// NetworkInfo holds information about a detected WiFi network.
type NetworkInfo struct {
	SSID      string `json:"ssid"`
	BSSID     string `json:"bssid"`
	Channel   int    `json:"channel"`
	Frequency int    `json:"frequency"`
	Signal    int    `json:"signal"` // dBm
}

// ChannelInfo holds aggregated information about a channel.
type ChannelInfo struct {
	Channel     int           `json:"channel"`
	Frequency   int           `json:"frequency"`
	Networks    []NetworkInfo `json:"networks"`
	Count       int           `json:"count"`
	MaxSignal   int           `json:"max_signal"`
	Recommended bool          `json:"recommended"`
}

// ScanResult holds the results of a channel scan.
type ScanResult struct {
	Channels []ChannelInfo `json:"channels"`
	Wlan     string        `json:"wlan"`
	Error    string        `json:"error,omitempty"`
}

// WifiInterface holds info about a WiFi interface.
type WifiInterface struct {
	Name    string `json:"name"`
	Type    string `json:"type"`    // managed, monitor, etc.
	Channel int    `json:"channel"` // 0 if not set
	IsUp    bool   `json:"is_up"`
}

// ListWifiInterfaces returns all WiFi interfaces on the system.
func ListWifiInterfaces() []WifiInterface {
	var interfaces []WifiInterface

	// Run "iw dev" to list all wireless interfaces
	cmd := exec.Command("iw", "dev")
	output, err := cmd.Output()
	if err != nil {
		log.Printf("[scanner] Failed to list interfaces: %v", err)
		return interfaces
	}

	var current *WifiInterface
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "Interface ") {
			if current != nil {
				interfaces = append(interfaces, *current)
			}
			current = &WifiInterface{
				Name: strings.TrimPrefix(line, "Interface "),
			}
		}
		if current == nil {
			continue
		}

		if strings.HasPrefix(line, "type ") {
			current.Type = strings.TrimPrefix(line, "type ")
		}
		if strings.HasPrefix(line, "channel ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				if ch, err := strconv.Atoi(parts[1]); err == nil {
					current.Channel = ch
				}
			}
		}
	}
	if current != nil {
		interfaces = append(interfaces, *current)
	}

	// Check which interfaces are up
	for i := range interfaces {
		cmd := exec.Command("ip", "link", "show", interfaces[i].Name)
		out, _ := cmd.Output()
		interfaces[i].IsUp = strings.Contains(string(out), "state UP") || strings.Contains(string(out), ",UP")
	}

	return interfaces
}

// findScanInterface looks for a WiFi interface that can be used for scanning
// without disrupting the WFB link. Returns empty string if none found.
func findScanInterface(wfbWlans []string) string {
	// Build set of WFB interfaces to exclude
	wfbSet := make(map[string]bool)
	for _, w := range wfbWlans {
		wfbSet[w] = true
	}

	// Get all interfaces and find one in managed mode
	interfaces := ListWifiInterfaces()
	for _, iface := range interfaces {
		if wfbSet[iface.Name] {
			continue // Skip WFB interfaces
		}
		if iface.Type == "managed" {
			log.Printf("[scanner] Found managed interface: %s", iface.Name)
			return iface.Name
		}
	}

	// No managed interface, try any non-WFB interface
	for _, iface := range interfaces {
		if !wfbSet[iface.Name] {
			log.Printf("[scanner] Found secondary interface: %s (type=%s)", iface.Name, iface.Type)
			return iface.Name
		}
	}

	return ""
}

// ScanOnInterface scans using a specific interface, handling mode switching if needed.
func ScanOnInterface(iface string) (*ScanResult, error) {
	// Get current interface state
	interfaces := ListWifiInterfaces()
	var ifaceInfo *WifiInterface
	for _, i := range interfaces {
		if i.Name == iface {
			ifaceInfo = &i
			break
		}
	}

	if ifaceInfo == nil {
		return &ScanResult{
			Channels: aggregateByChannel(nil),
			Wlan:     iface,
			Error:    fmt.Sprintf("Interface %s not found", iface),
		}, nil
	}

	// Save original state for restoration
	origType := ifaceInfo.Type
	origUp := ifaceInfo.IsUp
	origChannel := ifaceInfo.Channel
	needsRestore := false

	log.Printf("[scanner] Interface %s: type=%s up=%v channel=%d", iface, origType, origUp, origChannel)

	// Switch to managed mode if needed
	if origType != "managed" {
		needsRestore = true
		log.Printf("[scanner] Switching %s from %s to managed mode", iface, origType)

		// Bring down
		cmd := exec.Command("ip", "link", "set", iface, "down")
		if out, err := cmd.CombinedOutput(); err != nil {
			return &ScanResult{
				Channels: aggregateByChannel(nil),
				Wlan:     iface,
				Error:    fmt.Sprintf("Failed to bring interface down: %v: %s", err, string(out)),
			}, nil
		}

		// Set managed mode
		cmd = exec.Command("iw", "dev", iface, "set", "type", "managed")
		if out, err := cmd.CombinedOutput(); err != nil {
			return &ScanResult{
				Channels: aggregateByChannel(nil),
				Wlan:     iface,
				Error:    fmt.Sprintf("Failed to set managed mode: %v: %s", err, string(out)),
			}, nil
		}

		// Bring up
		cmd = exec.Command("ip", "link", "set", iface, "up")
		if out, err := cmd.CombinedOutput(); err != nil {
			return &ScanResult{
				Channels: aggregateByChannel(nil),
				Wlan:     iface,
				Error:    fmt.Sprintf("Failed to bring interface up: %v: %s", err, string(out)),
			}, nil
		}

		// Wait for interface to be ready
		time.Sleep(500 * time.Millisecond)
	} else if !origUp {
		// Interface is managed but down, bring it up
		needsRestore = true
		cmd := exec.Command("ip", "link", "set", iface, "up")
		if out, err := cmd.CombinedOutput(); err != nil {
			return &ScanResult{
				Channels: aggregateByChannel(nil),
				Wlan:     iface,
				Error:    fmt.Sprintf("Failed to bring interface up: %v: %s", err, string(out)),
			}, nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Run the scan
	output, scanErr := runScanOnInterface(iface)

	// Restore original state if needed
	if needsRestore {
		log.Printf("[scanner] Restoring %s to type=%s up=%v", iface, origType, origUp)

		// Bring down first
		exec.Command("ip", "link", "set", iface, "down").Run()

		// Restore type
		if origType == "monitor" {
			exec.Command("iw", "dev", iface, "set", "monitor", "otherbss").Run()
		} else if origType != "managed" {
			exec.Command("iw", "dev", iface, "set", "type", origType).Run()
		}

		// Restore up state
		if origUp {
			exec.Command("ip", "link", "set", iface, "up").Run()
			// Restore channel if it was set
			if origChannel > 0 {
				exec.Command("iw", "dev", iface, "set", "channel", strconv.Itoa(origChannel)).Run()
			}
		}
	}

	if scanErr != nil || len(output) == 0 {
		errMsg := "Scan returned no results"
		if scanErr != nil {
			errMsg = fmt.Sprintf("Scan failed: %v", scanErr)
		}
		return &ScanResult{
			Channels: aggregateByChannel(nil),
			Wlan:     iface,
			Error:    errMsg,
		}, nil
	}

	// Parse and aggregate
	networks := parseIwScan(string(output))
	log.Printf("[scanner] Parsed %d networks", len(networks))
	channels := aggregateByChannel(networks)

	return &ScanResult{
		Channels: channels,
		Wlan:     iface,
	}, nil
}

// runScanOnInterface runs iw scan on the given interface with retries.
func runScanOnInterface(iface string) ([]byte, error) {
	var output []byte
	var err error

	for attempt := 0; attempt < 5; attempt++ {
		log.Printf("[scanner] Scan attempt %d: iw dev %s scan", attempt+1, iface)
		cmd := exec.Command("iw", "dev", iface, "scan")
		output, err = cmd.CombinedOutput()
		log.Printf("[scanner] Scan result: err=%v len=%d", err, len(output))

		if err == nil && len(output) > 0 {
			return output, nil
		}

		// Check if device is busy, retry after a delay
		if strings.Contains(string(output), "busy") || strings.Contains(string(output), "(-16)") {
			log.Printf("[scanner] Device busy, waiting...")
			time.Sleep(2 * time.Second)
			continue
		}

		// Some other error
		if err != nil {
			return output, err
		}
		break
	}

	return output, err
}

// ScanChannels performs a WiFi scan to detect nearby networks.
// It tries to use a secondary interface (like wlan0) for scanning to avoid disrupting the WFB link.
// Falls back to switching the WFB interface to managed mode if no other interface is available.
func (m *Manager) ScanChannels() (*ScanResult, error) {
	// Try to find a secondary interface for scanning (not used by WFB)
	scanIface := findScanInterface(m.wlans)

	var output []byte
	var err error
	var needsRestore bool
	var savedChannel, savedBandwidth int

	if scanIface != "" {
		// Use secondary interface - no need to disrupt WFB link
		log.Printf("[scanner] Using secondary interface %s for scanning", scanIface)
		output, err = runScanOnInterface(scanIface)
	} else {
		// Fall back to using WFB interface - need to switch modes
		if len(m.wlans) == 0 {
			return nil, fmt.Errorf("no wlan interfaces available")
		}

		wlan := m.wlans[0]
		savedChannel = m.config.Hardware.Channel
		savedBandwidth = m.config.Hardware.Bandwidth
		needsRestore = true

		log.Printf("[scanner] No secondary interface found, using WFB interface %s", wlan)

		// Switch to managed mode
		if err := m.runIP("link", "set", wlan, "down"); err != nil {
			return nil, fmt.Errorf("link down: %w", err)
		}
		if err := m.runIW("dev", wlan, "set", "type", "managed"); err != nil {
			m.restoreMonitorMode(wlan, savedChannel, savedBandwidth)
			return nil, fmt.Errorf("set managed mode: %w", err)
		}
		if err := m.runIP("link", "set", wlan, "up"); err != nil {
			m.restoreMonitorMode(wlan, savedChannel, savedBandwidth)
			return nil, fmt.Errorf("link up: %w", err)
		}

		time.Sleep(1 * time.Second)
		output, err = runScanOnInterface(wlan)
		scanIface = wlan
	}

	// Restore WFB interface if needed
	if needsRestore && len(m.wlans) > 0 {
		wlan := m.wlans[0]
		if restoreErr := m.restoreMonitorMode(wlan, savedChannel, savedBandwidth); restoreErr != nil {
			log.Printf("[scanner] Warning: failed to restore monitor mode: %v", restoreErr)
		}
	}

	if err != nil {
		return &ScanResult{
			Channels: aggregateByChannel(nil),
			Wlan:     scanIface,
			Error:    fmt.Sprintf("Scan failed: %v", err),
		}, nil
	}

	if len(output) == 0 {
		return &ScanResult{
			Channels: aggregateByChannel(nil),
			Wlan:     scanIface,
			Error:    "Scan returned no results. This adapter may not support scanning.",
		}, nil
	}

	// Parse scan output
	networks := parseIwScan(string(output))
	log.Printf("[scanner] Parsed %d networks", len(networks))

	channels := aggregateByChannel(networks)

	return &ScanResult{
		Channels: channels,
		Wlan:     scanIface,
	}, nil
}

// restoreMonitorMode restores the interface to monitor mode with original settings.
func (m *Manager) restoreMonitorMode(wlan string, channel, bandwidth int) error {
	// Bring down
	if err := m.runIP("link", "set", wlan, "down"); err != nil {
		return fmt.Errorf("link down: %w", err)
	}

	// Set monitor mode
	if err := m.runIW("dev", wlan, "set", "monitor", "otherbss"); err != nil {
		return fmt.Errorf("set monitor: %w", err)
	}

	// Bring up
	if err := m.runIP("link", "set", wlan, "up"); err != nil {
		return fmt.Errorf("link up: %w", err)
	}

	// Restore channel
	htMode := bandwidthToHTMode(bandwidth)
	if channel > 2000 {
		if err := m.runIW("dev", wlan, "set", "freq", strconv.Itoa(channel), htMode); err != nil {
			return fmt.Errorf("set freq: %w", err)
		}
	} else {
		if err := m.runIW("dev", wlan, "set", "channel", strconv.Itoa(channel), htMode); err != nil {
			return fmt.Errorf("set channel: %w", err)
		}
	}

	return nil
}

// parseIwScan parses the output of "iw dev wlanX scan".
func parseIwScan(output string) []NetworkInfo {
	var networks []NetworkInfo
	var current *NetworkInfo

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)

		// New BSS entry
		if strings.HasPrefix(line, "BSS ") {
			if current != nil {
				networks = append(networks, *current)
			}
			current = &NetworkInfo{}
			// Parse BSSID from "BSS aa:bb:cc:dd:ee:ff(on wlan0)"
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				bssid := parts[1]
				if idx := strings.Index(bssid, "("); idx > 0 {
					bssid = bssid[:idx]
				}
				current.BSSID = bssid
			}
			continue
		}

		if current == nil {
			continue
		}

		// Parse fields
		if strings.HasPrefix(line, "freq: ") {
			// Frequency can be "2412" or "2412.0" - handle both
			freqStr := strings.TrimPrefix(line, "freq: ")
			freqStr = strings.TrimSuffix(freqStr, ".0") // Remove .0 if present
			if dotIdx := strings.Index(freqStr, "."); dotIdx > 0 {
				freqStr = freqStr[:dotIdx] // Truncate at decimal point
			}
			if freq, err := strconv.Atoi(freqStr); err == nil {
				current.Frequency = freq
				current.Channel = frequencyToChannel(freq)
			}
		} else if strings.HasPrefix(line, "signal: ") {
			// Format: "signal: -45.00 dBm"
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				if sig, err := strconv.ParseFloat(parts[1], 64); err == nil {
					current.Signal = int(sig)
				}
			}
		} else if strings.HasPrefix(line, "SSID: ") {
			current.SSID = strings.TrimPrefix(line, "SSID: ")
		}
	}

	// Don't forget the last entry
	if current != nil {
		networks = append(networks, *current)
	}

	return networks
}

// frequencyToChannel converts a frequency in MHz to a channel number.
func frequencyToChannel(freq int) int {
	// 2.4 GHz band
	if freq >= 2412 && freq <= 2484 {
		if freq == 2484 {
			return 14
		}
		return (freq - 2407) / 5
	}
	// 5 GHz band
	if freq >= 5170 && freq <= 5835 {
		return (freq - 5000) / 5
	}
	// 6 GHz band (WiFi 6E)
	if freq >= 5955 && freq <= 7115 {
		return (freq - 5950) / 5
	}
	return 0
}

// aggregateByChannel groups networks by channel and calculates statistics.
func aggregateByChannel(networks []NetworkInfo) []ChannelInfo {
	// Map channel -> networks
	channelMap := make(map[int][]NetworkInfo)
	freqMap := make(map[int]int) // channel -> frequency

	for _, n := range networks {
		if n.Channel > 0 {
			channelMap[n.Channel] = append(channelMap[n.Channel], n)
			freqMap[n.Channel] = n.Frequency
		}
	}

	// Common 2.4 GHz channels to always show
	common24 := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13}
	for _, ch := range common24 {
		if _, exists := channelMap[ch]; !exists {
			channelMap[ch] = []NetworkInfo{} // Empty slice, not nil
			freqMap[ch] = channelToFrequency(ch)
		}
	}

	// Common 5 GHz channels to always show
	common5g := []int{36, 40, 44, 48, 52, 56, 60, 64, 100, 104, 108, 112, 116, 120, 124, 128, 132, 136, 140, 144, 149, 153, 157, 161, 165}
	for _, ch := range common5g {
		if _, exists := channelMap[ch]; !exists {
			channelMap[ch] = []NetworkInfo{} // Empty slice, not nil
			freqMap[ch] = channelToFrequency(ch)
		}
	}

	// Build result
	var channels []ChannelInfo
	for ch, nets := range channelMap {
		info := ChannelInfo{
			Channel:   ch,
			Frequency: freqMap[ch],
			Networks:  nets,
			Count:     len(nets),
		}

		// Find max signal
		if len(nets) > 0 {
			info.MaxSignal = -999
			for _, n := range nets {
				if n.Signal > info.MaxSignal {
					info.MaxSignal = n.Signal
				}
			}
		}

		// Recommend channels with no networks
		info.Recommended = len(nets) == 0

		channels = append(channels, info)
	}

	// Sort by channel number
	sort.Slice(channels, func(i, j int) bool {
		return channels[i].Channel < channels[j].Channel
	})

	return channels
}

// channelToFrequency converts a channel number to frequency in MHz.
func channelToFrequency(channel int) int {
	// 2.4 GHz
	if channel >= 1 && channel <= 13 {
		return 2407 + channel*5
	}
	if channel == 14 {
		return 2484
	}
	// 5 GHz
	if channel >= 36 && channel <= 177 {
		return 5000 + channel*5
	}
	return 0
}
