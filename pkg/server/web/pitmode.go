// Package web provides pit mode functionality.
package web

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// Default drone address for pit mode (tunnel IP)
const defaultPitModeDroneHost = "10.5.0.10"

// PitModeState holds the current pit mode state.
type PitModeState struct {
	mu sync.Mutex

	Enabled         bool `json:"enabled"`
	SavedGSPower    int  `json:"saved_gs_power"`
	SavedDronePower int  `json:"saved_drone_power"`

	// Callbacks for GS power control
	getGSPower func() int
	setGSPower func(power int) error
}

// PitModeResponse is the API response.
type PitModeResponse struct {
	Enabled         bool   `json:"enabled"`
	SavedGSPower    int    `json:"saved_gs_power,omitempty"`
	SavedDronePower int    `json:"saved_drone_power,omitempty"`
	Error           string `json:"error,omitempty"`
}

// NewPitModeState creates a new pit mode state.
func NewPitModeState() *PitModeState {
	return &PitModeState{}
}

// SetGSPowerCallbacks sets the callbacks for GS power control.
func (p *PitModeState) SetGSPowerCallbacks(get func() int, set func(power int) error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.getGSPower = get
	p.setGSPower = set
}

// GetState returns the current pit mode state.
func (p *PitModeState) GetState() PitModeResponse {
	p.mu.Lock()
	defer p.mu.Unlock()

	return PitModeResponse{
		Enabled:         p.Enabled,
		SavedGSPower:    p.SavedGSPower,
		SavedDronePower: p.SavedDronePower,
	}
}

// getDroneSSHConfig returns the drone SSH configuration using defaults.
func (p *PitModeState) getDroneSSHConfig() (host string, sshConfig *ssh.ClientConfig, ok bool) {
	sshConfig = &ssh.ClientConfig{
		User: defaultSSHUser,
		Auth: []ssh.AuthMethod{
			ssh.Password(defaultSSHPassword),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	return defaultPitModeDroneHost, sshConfig, true
}

// Enable activates pit mode - saves current power and sets to minimum.
func (p *PitModeState) Enable() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.Enabled {
		return nil // Already enabled
	}

	// Save and set GS power
	if p.getGSPower != nil && p.setGSPower != nil {
		p.SavedGSPower = p.getGSPower()
		if p.SavedGSPower == 0 {
			p.SavedGSPower = 20 // Default fallback
		}
		if err := p.setGSPower(1); err != nil {
			return fmt.Errorf("set GS power: %w", err)
		}
		log.Printf("[pitmode] GS power: %d dBm -> 1 dBm", p.SavedGSPower)
	}

	// Save and set drone power via SSH
	host, sshConfig, ok := p.getDroneSSHConfig()
	if ok {
		dronePower, err := p.getDroneTXPower(host, sshConfig)
		if err != nil {
			log.Printf("[pitmode] Warning: failed to read drone power: %v", err)
			p.SavedDronePower = 20 // Default fallback
		} else {
			p.SavedDronePower = dronePower
		}

		if err := p.setDroneTXPower(host, sshConfig, 1); err != nil {
			// Rollback GS power
			if p.setGSPower != nil && p.SavedGSPower > 0 {
				p.setGSPower(p.SavedGSPower)
			}
			return fmt.Errorf("set drone power: %w", err)
		}
		log.Printf("[pitmode] Drone power: %d dBm -> 1 dBm", p.SavedDronePower)
	}

	p.Enabled = true
	log.Printf("[pitmode] Enabled")
	return nil
}

// Disable deactivates pit mode - restores saved power values.
func (p *PitModeState) Disable() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.Enabled {
		return nil // Already disabled
	}

	var lastErr error

	// Restore GS power
	if p.setGSPower != nil && p.SavedGSPower > 0 {
		if err := p.setGSPower(p.SavedGSPower); err != nil {
			log.Printf("[pitmode] Warning: failed to restore GS power: %v", err)
			lastErr = err
		} else {
			log.Printf("[pitmode] GS power restored: %d dBm", p.SavedGSPower)
		}
	}

	// Restore drone power via SSH
	host, sshConfig, ok := p.getDroneSSHConfig()
	if ok && p.SavedDronePower > 0 {
		if err := p.setDroneTXPower(host, sshConfig, p.SavedDronePower); err != nil {
			log.Printf("[pitmode] Warning: failed to restore drone power: %v", err)
			lastErr = err
		} else {
			log.Printf("[pitmode] Drone power restored: %d dBm", p.SavedDronePower)
		}
	}

	p.Enabled = false
	log.Printf("[pitmode] Disabled")
	return lastErr
}

// getDroneTXPower reads current TX power from drone via SSH.
func (p *PitModeState) getDroneTXPower(host string, sshConfig *ssh.ClientConfig) (int, error) {
	addr := net.JoinHostPort(host, "22")
	client, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		return 0, fmt.Errorf("SSH connect: %w", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return 0, fmt.Errorf("SSH session: %w", err)
	}
	defer session.Close()

	// Try to read current TX power from iw
	output, err := session.CombinedOutput("iw dev wlan0 info 2>/dev/null | grep txpower | awk '{print int($2)}'")
	if err != nil {
		return 0, fmt.Errorf("read txpower: %w", err)
	}

	powerStr := strings.TrimSpace(string(output))
	if powerStr == "" {
		return 0, fmt.Errorf("no txpower in output")
	}

	var power int
	if _, err := fmt.Sscanf(powerStr, "%d", &power); err != nil {
		return 0, fmt.Errorf("parse txpower: %w", err)
	}

	return power, nil
}

// setDroneTXPower sets TX power on drone via SSH.
func (p *PitModeState) setDroneTXPower(host string, sshConfig *ssh.ClientConfig, powerDbm int) error {
	addr := net.JoinHostPort(host, "22")
	client, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		return fmt.Errorf("SSH connect: %w", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("SSH session: %w", err)
	}
	defer session.Close()

	// Set TX power via iw (power in mBm = dBm * 100)
	cmd := fmt.Sprintf("iw dev wlan0 set txpower fixed %d", powerDbm*100)
	if output, err := session.CombinedOutput(cmd); err != nil {
		return fmt.Errorf("set txpower: %w: %s", err, string(output))
	}

	return nil
}

// IsEnabled returns whether pit mode is currently enabled.
func (p *PitModeState) IsEnabled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.Enabled
}

// handlePitModeAPI handles GET and POST requests for pit mode.
func (s *Server) handlePitModeAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		state := s.pitMode.GetState()
		json.NewEncoder(w).Encode(state)

	case http.MethodPost:
		var req struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(PitModeResponse{Error: "invalid request body"})
			return
		}

		var err error
		if req.Enabled {
			err = s.pitMode.Enable()
		} else {
			err = s.pitMode.Disable()
		}

		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(PitModeResponse{
				Enabled: s.pitMode.IsEnabled(),
				Error:   err.Error(),
			})
			return
		}

		json.NewEncoder(w).Encode(s.pitMode.GetState())

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(PitModeResponse{Error: "method not allowed"})
	}
}
