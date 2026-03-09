//go:build libpcap

package rx

import (
	"fmt"
	"time"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/pcap"
)

// LibpcapSource wraps gopacket/pcap to match vendor wfb-ng behavior.
// Uses pcap_set_immediate_mode for low latency and BPF filtering.
type LibpcapSource struct {
	handle    *pcap.Handle
	channelID uint32
	wlanIdx   uint8
}

// NewLibpcapSource creates a packet source matching vendor wfb-ng settings:
// - pcap_set_immediate_mode(1) for low latency
// - pcap_set_timeout(-1) / BlockForever
// - BPF filter for WFB magic and channel ID
// - pcap_setnonblock after activation
func NewLibpcapSource(iface string, channelID uint32, wlanIdx uint8, rcvBufSize int) (*LibpcapSource, error) {
	// Use InactiveHandle for full control (like pcap_create + pcap_set_* + pcap_activate)
	inactive, err := pcap.NewInactiveHandle(iface)
	if err != nil {
		return nil, fmt.Errorf("pcap_create failed: %w", err)
	}
	defer inactive.CleanUp()

	// Match vendor wfb-ng settings:
	// pcap_set_snaplen(ppcap, MAX_PCAP_PACKET_SIZE)
	if err := inactive.SetSnapLen(65535); err != nil {
		return nil, fmt.Errorf("set_snaplen failed: %w", err)
	}

	// pcap_set_promisc(ppcap, 1)
	if err := inactive.SetPromisc(true); err != nil {
		return nil, fmt.Errorf("set_promisc failed: %w", err)
	}

	// pcap_set_timeout(ppcap, -1)
	// BlockForever = -10ms in gopacket, which maps to no timeout
	if err := inactive.SetTimeout(pcap.BlockForever); err != nil {
		return nil, fmt.Errorf("set_timeout failed: %w", err)
	}

	// pcap_set_immediate_mode(ppcap, 1) - CRITICAL for low latency
	if err := inactive.SetImmediateMode(true); err != nil {
		return nil, fmt.Errorf("set_immediate_mode failed: %w", err)
	}

	// pcap_set_buffer_size if specified
	if rcvBufSize > 0 {
		if err := inactive.SetBufferSize(rcvBufSize); err != nil {
			return nil, fmt.Errorf("set_buffer_size failed: %w", err)
		}
	}

	// pcap_activate(ppcap)
	handle, err := inactive.Activate()
	if err != nil {
		return nil, fmt.Errorf("pcap_activate failed: %w", err)
	}

	// Verify link type is radiotap
	if handle.LinkType() != 127 { // DLT_IEEE802_11_RADIO = 127
		handle.Close()
		return nil, fmt.Errorf("interface %s not in monitor mode (link type %d, want 127)", iface, handle.LinkType())
	}

	// Set BPF filter matching vendor:
	// "ether[0x0a:2]==0x5742 && ether[0x0c:4] == 0x%08x"
	// This filters for WFB magic (0x5742 = "WB") and specific channel ID
	filter := fmt.Sprintf("ether[0x0a:2]==0x5742 && ether[0x0c:4]==0x%08x", channelID)
	if err := handle.SetBPFFilter(filter); err != nil {
		handle.Close()
		return nil, fmt.Errorf("set BPF filter failed: %w", err)
	}

	return &LibpcapSource{
		handle:    handle,
		channelID: channelID,
		wlanIdx:   wlanIdx,
	}, nil
}

// ReadPacket reads the next packet from the capture.
func (s *LibpcapSource) ReadPacket() ([]byte, gopacket.CaptureInfo, error) {
	data, ci, err := s.handle.ReadPacketData()
	if err != nil {
		return nil, gopacket.CaptureInfo{}, err
	}
	return data, ci, nil
}

// Close releases the pcap handle.
func (s *LibpcapSource) Close() error {
	s.handle.Close()
	return nil
}

// Stats returns capture statistics.
func (s *LibpcapSource) Stats() (*pcap.Stats, error) {
	return s.handle.Stats()
}

// LibpcapSourceWithTimeout creates a source with a specific timeout for polling.
// This is useful when you want periodic control (e.g., stats logging).
func NewLibpcapSourceWithTimeout(iface string, channelID uint32, wlanIdx uint8, rcvBufSize int, timeout time.Duration) (*LibpcapSource, error) {
	inactive, err := pcap.NewInactiveHandle(iface)
	if err != nil {
		return nil, fmt.Errorf("pcap_create failed: %w", err)
	}
	defer inactive.CleanUp()

	if err := inactive.SetSnapLen(65535); err != nil {
		return nil, fmt.Errorf("set_snaplen failed: %w", err)
	}

	if err := inactive.SetPromisc(true); err != nil {
		return nil, fmt.Errorf("set_promisc failed: %w", err)
	}

	// Use specified timeout instead of BlockForever
	if err := inactive.SetTimeout(timeout); err != nil {
		return nil, fmt.Errorf("set_timeout failed: %w", err)
	}

	if err := inactive.SetImmediateMode(true); err != nil {
		return nil, fmt.Errorf("set_immediate_mode failed: %w", err)
	}

	if rcvBufSize > 0 {
		if err := inactive.SetBufferSize(rcvBufSize); err != nil {
			return nil, fmt.Errorf("set_buffer_size failed: %w", err)
		}
	}

	handle, err := inactive.Activate()
	if err != nil {
		return nil, fmt.Errorf("pcap_activate failed: %w", err)
	}

	if handle.LinkType() != 127 {
		handle.Close()
		return nil, fmt.Errorf("interface %s not in monitor mode (link type %d, want 127)", iface, handle.LinkType())
	}

	filter := fmt.Sprintf("ether[0x0a:2]==0x5742 && ether[0x0c:4]==0x%08x", channelID)
	if err := handle.SetBPFFilter(filter); err != nil {
		handle.Close()
		return nil, fmt.Errorf("set BPF filter failed: %w", err)
	}

	return &LibpcapSource{
		handle:    handle,
		channelID: channelID,
		wlanIdx:   wlanIdx,
	}, nil
}
