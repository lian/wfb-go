// Package web provides channel scanner API functionality.
package web

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/lian/wfb-go/pkg/wifi/adapter"
)

// ScannerState holds the channel scanner state.
type ScannerState struct {
	mu       sync.Mutex
	scanning bool
}

// NewScannerState creates a new scanner state.
func NewScannerState() *ScannerState {
	return &ScannerState{}
}

// IsScanning returns whether a scan is currently in progress.
func (sc *ScannerState) IsScanning() bool {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.scanning
}

// ScanRequest is the request body for starting a scan.
type ScanRequest struct {
	Interface string `json:"interface"`
}

// ScanWithInterface performs a channel scan on a specific interface.
func (sc *ScannerState) ScanWithInterface(iface string) (*adapter.ScanResult, error) {
	sc.mu.Lock()
	if sc.scanning {
		sc.mu.Unlock()
		return &adapter.ScanResult{Error: "scan already in progress"}, nil
	}
	sc.scanning = true
	sc.mu.Unlock()

	defer func() {
		sc.mu.Lock()
		sc.scanning = false
		sc.mu.Unlock()
	}()

	log.Printf("[scanner] Starting channel scan on %s...", iface)
	result, err := adapter.ScanOnInterface(iface)
	if err != nil {
		log.Printf("[scanner] Scan failed: %v", err)
		return &adapter.ScanResult{Error: err.Error()}, nil
	}

	log.Printf("[scanner] Scan complete: found %d channels", len(result.Channels))
	return result, nil
}

// handleScannerAPI handles GET and POST requests for channel scanning.
func (s *Server) handleScannerAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		// Return current status and list of interfaces
		interfaces := adapter.ListWifiInterfaces()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"scanning":   s.scanner.IsScanning(),
			"interfaces": interfaces,
		})

	case http.MethodPost:
		// Parse request body for interface selection
		var req ScanRequest
		if r.Body != nil {
			json.NewDecoder(r.Body).Decode(&req)
		}

		if req.Interface == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(adapter.ScanResult{Error: "interface required"})
			return
		}

		result, err := s.scanner.ScanWithInterface(req.Interface)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(adapter.ScanResult{Error: err.Error()})
			return
		}
		json.NewEncoder(w).Encode(result)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(adapter.ScanResult{Error: "method not allowed"})
	}
}
