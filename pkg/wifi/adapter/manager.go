package adapter

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/lian/wfb-go/pkg/config"
)

// SupportedDrivers lists WiFi drivers known to work with WFB injection.
var SupportedDrivers = []string{
	"rtl88xxau_wfb", // RTL8812AU
	"rtl88x2eu",     // RTL8812EU
	"rtl88x2cu",     // RTL8812CU
}

// AdapterInfo holds information about a detected WiFi adapter.
type AdapterInfo struct {
	Name   string // Interface name (e.g., "wlan0")
	Driver string // USB driver name
	PHY    string // PHY name (e.g., "phy0")
	MAC    string // Hardware MAC address
}

// DetectAdapters finds all WiFi adapters with WFB-compatible drivers.
func DetectAdapters() ([]AdapterInfo, error) {
	netDir := "/sys/class/net"
	entries, err := os.ReadDir(netDir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", netDir, err)
	}

	var adapters []AdapterInfo

	// Sort for consistent ordering
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		ifacePath := filepath.Join(netDir, entry.Name())

		// Only check symlinks (real interfaces)
		fi, err := os.Lstat(ifacePath)
		if err != nil || fi.Mode()&os.ModeSymlink == 0 {
			continue
		}

		// Get driver from udevadm
		driver := getUSBDriver(entry.Name())
		if driver == "" {
			continue
		}

		// Check if supported
		if !isDriverSupported(driver) {
			continue
		}

		info := AdapterInfo{
			Name:   entry.Name(),
			Driver: driver,
		}

		// Get PHY name
		phyPath := filepath.Join(ifacePath, "phy80211", "name")
		if data, err := os.ReadFile(phyPath); err == nil {
			info.PHY = strings.TrimSpace(string(data))
		}

		// Get MAC address
		macPath := filepath.Join(ifacePath, "address")
		if data, err := os.ReadFile(macPath); err == nil {
			info.MAC = strings.TrimSpace(string(data))
		}

		adapters = append(adapters, info)
	}

	return adapters, nil
}

// getUSBDriver returns the USB driver for a network interface.
func getUSBDriver(ifaceName string) string {
	cmd := exec.Command("udevadm", "info", "/sys/class/net/"+ifaceName)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "E: ID_USB_DRIVER=") {
			return strings.TrimPrefix(line, "E: ID_USB_DRIVER=")
		}
	}
	return ""
}

// isDriverSupported checks if the driver is WFB-compatible.
func isDriverSupported(driver string) bool {
	for _, supported := range SupportedDrivers {
		if strings.EqualFold(driver, supported) {
			return true
		}
	}
	return false
}

// Manager handles WiFi interface configuration.
type Manager struct {
	config *config.Config
	wlans  []string
}

// NewManager creates a new WiFi adapter manager.
func NewManager(cfg *config.Config, wlans []string) *Manager {
	return &Manager{
		config: cfg,
		wlans:  wlans,
	}
}

// InitWlans initializes all WiFi interfaces for monitor mode.
func (m *Manager) InitWlans() error {
	// Check if we should skip (non-primary instance)
	if m.config.Common != nil && !m.config.Common.Primary {
		return nil
	}

	// Set regulatory domain
	if err := m.setRegion(m.config.Hardware.Region); err != nil {
		return fmt.Errorf("set region: %w", err)
	}

	for _, wlan := range m.wlans {
		if err := m.initWlan(wlan); err != nil {
			return fmt.Errorf("init %s: %w", wlan, err)
		}
	}

	// If TX power not set in config, read from first interface
	if m.config.Hardware.TXPower == nil && len(m.wlans) > 0 {
		if power := m.readTXPower(m.wlans[0]); power > 0 {
			m.config.Hardware.TXPower = &power
		}
	}

	return nil
}

func (m *Manager) initWlan(wlan string) error {
	// Set NetworkManager unmanaged (always try, ignore errors)
	m.setNMUnmanaged(wlan)

	// Bring interface down
	if err := m.runIP("link", "set", wlan, "down"); err != nil {
		return fmt.Errorf("link down: %w", err)
	}

	// Set monitor mode with otherbss
	if err := m.runIW("dev", wlan, "set", "monitor", "otherbss"); err != nil {
		return fmt.Errorf("set monitor: %w", err)
	}

	// Bring interface up
	if err := m.runIP("link", "set", wlan, "up"); err != nil {
		return fmt.Errorf("link up: %w", err)
	}

	// Set channel/frequency (check per-interface override first)
	channel := m.config.Hardware.Channel
	if override, ok := m.config.Hardware.ChannelOverrides[wlan]; ok {
		channel = override
	}
	htMode := bandwidthToHTMode(m.config.Hardware.Bandwidth)

	if channel > 2000 {
		// Frequency in MHz
		if err := m.runIW("dev", wlan, "set", "freq", strconv.Itoa(channel), htMode); err != nil {
			return fmt.Errorf("set freq: %w", err)
		}
	} else {
		// Channel number
		if err := m.runIW("dev", wlan, "set", "channel", strconv.Itoa(channel), htMode); err != nil {
			return fmt.Errorf("set channel: %w", err)
		}
	}

	// Set TX power (check per-interface override first)
	// Config values are in dBm, iw expects mBm (multiply by 100)
	var txpowerDbm *int
	if override, ok := m.config.Hardware.TXPowerOverrides[wlan]; ok {
		txpowerDbm = &override
	} else {
		txpowerDbm = m.config.Hardware.TXPower
	}
	if txpowerDbm != nil && *txpowerDbm > 0 {
		txpowerMbm := *txpowerDbm * 100 // Convert dBm to mBm
		if err := m.runIW("dev", wlan, "set", "txpower", "fixed", strconv.Itoa(txpowerMbm)); err != nil {
			return fmt.Errorf("set txpower: %w", err)
		}
	}

	return nil
}

func (m *Manager) setRegion(region string) error {
	return m.runIW("reg", "set", region)
}

func (m *Manager) setNMUnmanaged(wlan string) {
	// Try to set interface as unmanaged in NetworkManager
	// Ignore errors as NM may not be present
	exec.Command("nmcli", "device", "set", wlan, "managed", "no").Run()
}

func (m *Manager) runIP(args ...string) error {
	cmd := exec.Command("ip", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (m *Manager) runIW(args ...string) error {
	cmd := exec.Command("iw", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// bandwidthToHTMode converts bandwidth to iw HT mode string.
func bandwidthToHTMode(bandwidth int) string {
	switch bandwidth {
	case 5:
		return "5MHz"
	case 10:
		return "10MHz"
	case 20:
		return "HT20"
	case 40:
		return "HT40+"
	case 80:
		return "80MHz"
	case 160:
		return "160MHz"
	default:
		return "HT20"
	}
}

// GetWlans returns the list of all wlans.
func (m *Manager) GetWlans() []string {
	return m.wlans
}

// SetTXPower sets TX power on all wlans and updates config state.
// Power is in dBm.
func (m *Manager) SetTXPower(powerDbm int) error {
	if powerDbm <= 0 {
		return nil // 0 means don't change
	}

	powerMbm := powerDbm * 100 // Convert dBm to mBm for iw
	for _, wlan := range m.wlans {
		if err := m.runIW("dev", wlan, "set", "txpower", "fixed", strconv.Itoa(powerMbm)); err != nil {
			return fmt.Errorf("set txpower on %s: %w", wlan, err)
		}
	}

	// Update config to reflect current state (in dBm)
	m.config.Hardware.TXPower = &powerDbm
	return nil
}

// GetTXPower returns current TX power from config in dBm.
// Returns 0 if not set.
func (m *Manager) GetTXPower() int {
	if m.config.Hardware.TXPower != nil {
		return *m.config.Hardware.TXPower
	}
	return 0
}

// SetChannel sets channel/frequency on all wlans.
// Channel can be a channel number (1-200) or frequency in MHz (>2000).
func (m *Manager) SetChannel(channel, bandwidth int) error {
	htMode := bandwidthToHTMode(bandwidth)

	for _, wlan := range m.wlans {
		var err error
		if channel > 2000 {
			// Frequency in MHz
			err = m.runIW("dev", wlan, "set", "freq", strconv.Itoa(channel), htMode)
		} else {
			// Channel number
			err = m.runIW("dev", wlan, "set", "channel", strconv.Itoa(channel), htMode)
		}
		if err != nil {
			return fmt.Errorf("set channel on %s: %w", wlan, err)
		}
	}

	// Update config
	m.config.Hardware.Channel = channel
	m.config.Hardware.Bandwidth = bandwidth
	return nil
}

// readTXPower reads current TX power from interface using iw.
// Returns power in dBm, or 0 on error.
func (m *Manager) readTXPower(wlan string) int {
	out, err := exec.Command("iw", "dev", wlan, "info").Output()
	if err != nil {
		return 0
	}

	// Parse "txpower X.XX dBm" line
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "txpower ") {
			// Format: "txpower 20.00 dBm"
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				if dbm, err := strconv.ParseFloat(parts[1], 64); err == nil {
					return int(dbm)
				}
			}
		}
	}
	return 0
}
