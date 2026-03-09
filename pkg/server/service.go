package server

import (
	"context"
	"crypto/sha1"
	"encoding/binary"
	"sync"

	"github.com/lian/wfb-go/pkg/config"
	"github.com/lian/wfb-go/pkg/rx"
	"github.com/lian/wfb-go/pkg/server/mavlink"
)


// ServiceType defines the type of service.
type ServiceType string

const (
	ServiceUDPDirectTX ServiceType = "udp_direct_tx"
	ServiceUDPDirectRX ServiceType = "udp_direct_rx"
	ServiceMavlink     ServiceType = "mavlink"
	ServiceTunnel      ServiceType = "tunnel"
	ServiceUDPProxy    ServiceType = "udp_proxy"
)

// Service is the interface all service types implement.
type Service interface {
	Name() string
	Type() ServiceType
	Start(ctx context.Context) error
	Stop() error
	Stats() *ServiceStats
}

// AntennaRoutable is implemented by services that support TX antenna selection.
type AntennaRoutable interface {
	SetAntenna(wlanIdx uint8)
}

// LatencyProvider is implemented by services that track TX latency stats.
type LatencyProvider interface {
	// GetLatencyStats returns latency stats keyed by (wlan_idx << 8) | antenna_id
	GetLatencyStats() map[uint32]LatencyStatsData
}

// TXConfigurable is implemented by services that support runtime TX parameter updates.
type TXConfigurable interface {
	// SetTXParams updates TX parameters dynamically.
	SetTXParams(params TXParams)
}

// TXParams contains dynamically adjustable TX parameters (matches alink_drone).
type TXParams struct {
	MCS       int  // MCS index
	ShortGI   bool // Short guard interval
	Bandwidth int  // 20 or 40 MHz
	STBC      int  // Space-time block coding (0-2)
	LDPC      bool // Low-density parity check
	FecK      int  // FEC data packets
	FecN      int  // FEC total packets
	TXPower   int  // TX power in dBm (0 = don't change)
}

// LatencyStatsData holds injection latency data.
type LatencyStatsData struct {
	PacketsInjected uint64
	PacketsDropped  uint64
	LatencyMin      uint64 // microseconds
	LatencyMax      uint64 // microseconds
	LatencyAvg      uint64 // microseconds
}

// ServiceStats contains runtime statistics for a service.
type ServiceStats struct {
	mu sync.RWMutex

	// RX stats
	PacketsReceived uint64
	BytesReceived   uint64
	PacketsDecErr   uint64
	PacketsBad      uint64
	PacketsFECRec   uint64
	PacketsLost     uint64
	PacketsOutgoing uint64
	BytesOutgoing   uint64

	// TX stats
	PacketsIncoming uint64
	BytesIncoming   uint64
	PacketsInjected uint64
	BytesInjected   uint64
	PacketsDropped  uint64
	FECTimeouts     uint64

	// Session info
	SessionEpoch uint64
	SessionFecK  int
	SessionFecN  int
	SessionMCS   int

	// Antenna stats
	AntennaStats map[uint32]*AntennaStats
}

// AntennaStats holds per-antenna statistics.
type AntennaStats struct {
	WlanIdx   uint8
	Antenna   uint8
	Freq      uint16
	MCSIndex  uint8
	Bandwidth uint8

	PacketsReceived uint64
	RSSIMin         int8
	RSSIAvg         int8
	RSSIMax         int8
	SNRMin          int8
	SNRAvg          int8
	SNRMax          int8
}

// ServiceConfig holds configuration for creating a service.
type ServiceConfig struct {
	Name        string
	ServiceType ServiceType
	StreamRX    *uint8
	StreamTX    *uint8
	Wlans       []string
	LinkID      uint32
	Epoch       uint64

	// Connection settings
	Peer    string // listen://addr:port, connect://addr:port, unix://path
	KeyData []byte // Key data (64 bytes: secret key + peer public key)

	// Radio settings
	Bandwidth int
	ForceVHT  bool
	ShortGI   bool
	STBC      int
	LDPC      int
	MCSIndex  int
	FrameType string // "data" or "rts"

	// FEC settings
	FecK       int
	FecN       int
	FecTimeout int
	FecDelay   int

	// Buffer sizes
	RcvBufSize int
	SndBufSize int

	// Tunnel settings
	IfName       string
	IfAddr       string
	DefaultRoute bool
	TunName      string
	KeepaliveMS  int

	// Feature flags
	InjectRSSI     bool   // Inject RSSI into mavlink RADIO_STATUS
	Mirror         bool   // Send packets to ALL antennas (for multi-frequency setups)
	MavlinkSysID   uint8  // System ID for injected RSSI messages (default: 3)
	MavlinkCompID  uint8  // Component ID for injected RSSI messages (default: 68 = telemetry radio)
	OSD            string // OSD mirror address (e.g., "connect://192.168.1.100:14550")
	MavlinkTCPPort int    // Additional TCP port for mavlink (e.g., 5760 for QGC)

	// QoS settings
	UseQdisc bool   // Use kernel qdisc (false = bypass for lower latency)
	Fwmark   uint32 // Packet mark for tc qdisc rules (only when UseQdisc=true)

	// Aggregation settings
	AggTimeout float64 // Aggregation timeout in seconds (0 = disabled)
	AggFramed  bool    // Use 2-byte length framing (for tunnel)
	AggMaxSize int     // Max aggregated packet size (from radio_mtu)

	// Mavlink ARM callbacks
	CallOnArm    string // Shell command to run when armed
	CallOnDisarm string // Shell command to run when disarmed

	// Logging
	LogMessages   bool   // Log mavlink messages to binary log file
	BinaryLogFile string // Path to binary log file (from common config)

	// Capture settings (RX)
	CaptureManager *rx.CaptureManager // Shared capture manager (nil = create dedicated)

	// Video callback for web UI integration
	VideoCallback func(data []byte) // Called with raw video data (nil = disabled)
}

// NewServiceConfig creates a ServiceConfig from config types.
func NewServiceConfig(name string, stream *config.StreamConfig, cfg *config.Config, wlans []string, linkID uint32) (*ServiceConfig, error) {
	// Load key data
	keyData, err := cfg.GetStreamKeyData(stream)
	if err != nil {
		return nil, err
	}

	// Get FEC parameters
	fecK, fecN := stream.GetFEC()
	if fecK == 0 {
		fecK = 8
	}
	if fecN == 0 {
		fecN = 12
	}

	// Get radio settings from stream (with defaults)
	bandwidth := stream.Bandwidth
	if bandwidth == 0 {
		bandwidth = cfg.Hardware.Bandwidth // Fall back to hardware bandwidth
	}
	mcs := stream.GetMCS()
	shortGI := stream.ShortGI
	stbc := stream.GetSTBC()
	ldpc := stream.GetLDPC()

	// Get buffer sizes from common config
	rcvBufSize := 2097152 // 2MB default
	sndBufSize := 2097152
	radioMTU := 1445
	mavlinkAggTimeout := 0.1
	tunnelAggTimeout := 0.005
	logInterval := 1000

	if cfg.Common != nil {
		if cfg.Common.TxRcvBufSize > 0 {
			rcvBufSize = cfg.Common.TxRcvBufSize
		}
		if cfg.Common.RxSndBufSize > 0 {
			sndBufSize = cfg.Common.RxSndBufSize
		}
		if cfg.Common.RadioMTU > 0 {
			radioMTU = cfg.Common.RadioMTU
		}
		if cfg.Common.MavlinkAggTimeout > 0 {
			mavlinkAggTimeout = cfg.Common.MavlinkAggTimeout
		}
		if cfg.Common.TunnelAggTimeout > 0 {
			tunnelAggTimeout = cfg.Common.TunnelAggTimeout
		}
		if cfg.Common.LogInterval > 0 {
			logInterval = cfg.Common.LogInterval
		}
	}

	svcCfg := &ServiceConfig{
		Name:        name,
		ServiceType: ServiceType(stream.ServiceType),
		StreamRX:    stream.StreamRX,
		StreamTX:    stream.StreamTX,
		Wlans:       wlans,
		LinkID:      linkID,
		Peer:        stream.Peer,
		KeyData:     keyData,
		Bandwidth:   bandwidth,
		ShortGI:     shortGI,
		STBC:        stbc,
		LDPC:        ldpc,
		MCSIndex:    mcs,
		FrameType:   stream.FrameType,
		FecK:        fecK,
		FecN:        fecN,
		FecTimeout:  stream.FECTimeout,
		FecDelay:    stream.FECDelay,
		RcvBufSize:  rcvBufSize,
		SndBufSize:  sndBufSize,
		Mirror:   stream.Mirror,
		UseQdisc: stream.UseQdisc,
		Fwmark:   uint32(stream.FWMark),
	}

	// Apply frame type default
	if svcCfg.FrameType == "" {
		svcCfg.FrameType = "data"
	}

	// Tunnel settings
	if stream.Tunnel != nil {
		svcCfg.IfName = stream.Tunnel.Ifname
		svcCfg.IfAddr = stream.Tunnel.Ifaddr
		svcCfg.DefaultRoute = stream.Tunnel.DefaultRoute
		svcCfg.TunName = stream.Tunnel.Ifname
	}

	// Mavlink settings
	if stream.Mavlink != nil {
		svcCfg.CallOnArm = stream.Mavlink.CallOnArm
		svcCfg.CallOnDisarm = stream.Mavlink.CallOnDisarm
		svcCfg.LogMessages = stream.Mavlink.LogMessages
		if stream.Mavlink.TCPPort != nil {
			svcCfg.MavlinkTCPPort = *stream.Mavlink.TCPPort
		}
		svcCfg.InjectRSSI = stream.Mavlink.InjectRSSI
		if stream.Mavlink.SysID > 0 {
			svcCfg.MavlinkSysID = uint8(stream.Mavlink.SysID)
		}
		if stream.Mavlink.CompID > 0 {
			svcCfg.MavlinkCompID = uint8(stream.Mavlink.CompID)
		}
		svcCfg.OSD = stream.Mavlink.OSD
	}

	// Set aggregation settings based on service type
	switch svcCfg.ServiceType {
	case ServiceMavlink:
		svcCfg.AggTimeout = mavlinkAggTimeout
		svcCfg.AggMaxSize = radioMTU
	case ServiceTunnel:
		svcCfg.AggTimeout = tunnelAggTimeout
		svcCfg.AggMaxSize = radioMTU
		svcCfg.AggFramed = true
		if svcCfg.KeepaliveMS == 0 {
			svcCfg.KeepaliveMS = logInterval / 2
		}
	}

	return svcCfg, nil
}

// CreateService creates a service based on the service type.
// All service types use the unified StreamService - the type just sets defaults.
func CreateService(cfg *ServiceConfig) (Service, error) {
	// Apply service-type-specific defaults
	switch cfg.ServiceType {
	case ServiceMavlink:
		cfg.InjectRSSI = true // Mavlink gets RSSI injection by default
		// Set default mavlink identity for RSSI injection
		if cfg.MavlinkSysID == 0 {
			cfg.MavlinkSysID = mavlink.DefaultSysID
		}
		if cfg.MavlinkCompID == 0 {
			cfg.MavlinkCompID = mavlink.DefaultCompID
		}
	case ServiceTunnel:
		// Convert IfName to tun: peer format if peer not set
		if cfg.Peer == "" && cfg.TunName != "" {
			cfg.Peer = "tun:" + cfg.TunName
		} else if cfg.Peer == "" {
			cfg.Peer = "tun:wfb-tun"
		}
	}

	return NewStreamService(cfg)
}

// UnsupportedServiceError is returned when a service type is not supported.
type UnsupportedServiceError struct {
	Type string
}

func (e *UnsupportedServiceError) Error() string {
	return "unsupported service type: " + e.Type
}

// HashLinkDomain converts a link domain string to a 24-bit link ID.
// Uses SHA1 hash (first 3 bytes) to match wfb-ng behavior.
// Example: "default" -> 7669206 (0x7505d6)
func HashLinkDomain(domain string) uint32 {
	if domain == "" {
		domain = "default"
	}
	hash := sha1.Sum([]byte(domain))
	// Use first 3 bytes as link ID (same as wfb-ng)
	return uint32(hash[0])<<16 | uint32(hash[1])<<8 | uint32(hash[2])
}

// MakeChannelID creates a channel ID from link ID and port.
func MakeChannelID(linkID uint32, port uint8) uint32 {
	return (linkID << 8) | uint32(port)
}

// NewServiceStats creates a new ServiceStats instance.
func NewServiceStats() *ServiceStats {
	return &ServiceStats{
		AntennaStats: make(map[uint32]*AntennaStats),
	}
}

// Clone returns a copy of the stats.
func (s *ServiceStats) Clone() *ServiceStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	clone := &ServiceStats{
		PacketsReceived: s.PacketsReceived,
		BytesReceived:   s.BytesReceived,
		PacketsDecErr:   s.PacketsDecErr,
		PacketsBad:      s.PacketsBad,
		PacketsFECRec:   s.PacketsFECRec,
		PacketsLost:     s.PacketsLost,
		PacketsOutgoing: s.PacketsOutgoing,
		BytesOutgoing:   s.BytesOutgoing,
		PacketsIncoming: s.PacketsIncoming,
		BytesIncoming:   s.BytesIncoming,
		PacketsInjected: s.PacketsInjected,
		BytesInjected:   s.BytesInjected,
		PacketsDropped:  s.PacketsDropped,
		FECTimeouts:     s.FECTimeouts,
		SessionEpoch:    s.SessionEpoch,
		SessionFecK:     s.SessionFecK,
		SessionFecN:     s.SessionFecN,
		SessionMCS:      s.SessionMCS,
		AntennaStats:    make(map[uint32]*AntennaStats),
	}

	for k, v := range s.AntennaStats {
		clone.AntennaStats[k] = &AntennaStats{
			WlanIdx:         v.WlanIdx,
			Antenna:         v.Antenna,
			Freq:            v.Freq,
			MCSIndex:        v.MCSIndex,
			Bandwidth:       v.Bandwidth,
			PacketsReceived: v.PacketsReceived,
			RSSIMin:         v.RSSIMin,
			RSSIAvg:         v.RSSIAvg,
			RSSIMax:         v.RSSIMax,
			SNRMin:          v.SNRMin,
			SNRAvg:          v.SNRAvg,
			SNRMax:          v.SNRMax,
		}
	}

	return clone
}

// MakeAntennaKey creates a unique key for antenna stats.
func MakeAntennaKey(wlanIdx, antenna uint8, freq uint16) uint32 {
	return uint32(wlanIdx)<<24 | uint32(antenna)<<16 | uint32(freq)
}

// ParseAntennaKey extracts wlanIdx, antenna, freq from key.
func ParseAntennaKey(key uint32) (wlanIdx, antenna uint8, freq uint16) {
	wlanIdx = uint8(key >> 24)
	antenna = uint8(key >> 16)
	freq = uint16(key)
	return
}

// ChannelIDBytes returns the channel ID as bytes for MAC address.
func ChannelIDBytes(channelID uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, channelID)
	return b
}
