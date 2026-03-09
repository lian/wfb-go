// Package config provides a unified YAML configuration system for wfb-go.
package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the root configuration structure.
type Config struct {
	Hardware HardwareConfig           `yaml:"hardware" json:"hardware"`
	Link     LinkConfig               `yaml:"link" json:"link"`
	Streams  map[string]StreamConfig  `yaml:"streams" json:"streams"`
	Adaptive *AdaptiveConfig          `yaml:"adaptive,omitempty" json:"adaptive,omitempty"`
	API      *APIConfig               `yaml:"api,omitempty" json:"api,omitempty"`
	Web      *WebConfig               `yaml:"web,omitempty" json:"web,omitempty"`
	Paths    *PathsConfig             `yaml:"paths,omitempty" json:"paths,omitempty"`
	Common   *CommonConfig            `yaml:"common,omitempty" json:"common,omitempty"`
}

// HardwareConfig defines WiFi hardware settings.
type HardwareConfig struct {
	WLANs     []string `yaml:"wlans" json:"wlans"`
	Region    string   `yaml:"region" json:"region"`
	Channel   int      `yaml:"channel" json:"channel"`
	Bandwidth int      `yaml:"bandwidth" json:"bandwidth"` // 20 or 40
	TXPower   *int     `yaml:"tx_power,omitempty" json:"tx_power,omitempty"` // TX power in dBm (nil=driver default)

	// Capture mode for RX: "dedicated" or "shared"
	//   dedicated: one pcap handle per stream (default, better isolation)
	//   shared: one pcap handle per interface (lower latency, less kernel overhead)
	CaptureMode string `yaml:"capture_mode,omitempty" json:"capture_mode,omitempty"`

	// Per-interface overrides
	ChannelOverrides map[string]int `yaml:"channel_overrides,omitempty" json:"channel_overrides,omitempty"`
	TXPowerOverrides map[string]int `yaml:"tx_power_overrides,omitempty" json:"tx_power_overrides,omitempty"` // in dBm
}

// LinkConfig defines link identity.
type LinkConfig struct {
	Domain    string `yaml:"domain" json:"domain"`                         // Domain string (hashed to link_id)
	ID        uint32 `yaml:"id,omitempty" json:"id,omitempty"`             // Numeric link_id (overrides domain if set)
	Key       string `yaml:"key,omitempty" json:"key,omitempty"`           // Path to key file
	KeyBase64 string `yaml:"key_base64,omitempty" json:"key_base64,omitempty"` // Key content as base64 (alternative to key file)
}

// StreamConfig defines a data stream.
// Streams are unidirectional - use separate stream_rx and stream_tx for bidirectional.
// Per wfb-ng standard:
//   Downlink (drone -> GS): stream IDs 0-127
//   Uplink (GS -> drone):   stream IDs 128-255
type StreamConfig struct {
	// Service type determines behavior:
	//   udp_direct_tx - TX only, no antenna selection feedback
	//   udp_direct_rx - RX only
	//   udp_proxy     - Bidirectional UDP proxy
	//   mavlink       - Bidirectional MAVLink with RSSI injection
	//   tunnel        - Bidirectional IP tunnel (TUN device)
	ServiceType string `yaml:"service_type" json:"service_type"`

	// Stream IDs (0-255). Use nil for unused direction.
	// For bidirectional services, both must be set.
	// Downlink: 0-127, Uplink: 128-255
	StreamRX *uint8 `yaml:"stream_rx,omitempty" json:"stream_rx,omitempty"` // RX stream ID (receive from radio)
	StreamTX *uint8 `yaml:"stream_tx,omitempty" json:"stream_tx,omitempty"` // TX stream ID (send to radio)

	// Peer connection (required for most services):
	//   listen://0.0.0.0:5600     - UDP server (receive from local app)
	//   connect://127.0.0.1:5600  - UDP client (send to local app)
	//   serial:/dev/ttyUSB0:115200 - Serial port
	//   tcp://0.0.0.0:5760        - TCP server (for QGC etc)
	Peer string `yaml:"peer" json:"peer"`

	// FEC parameters: [k, n] where k = data packets, n = total packets
	FEC [2]int `yaml:"fec" json:"fec"`

	// Optional key override (default: from link config)
	Key       string `yaml:"key,omitempty" json:"key,omitempty"`               // Path to key file
	KeyBase64 string `yaml:"key_base64,omitempty" json:"key_base64,omitempty"` // Key content as base64

	// Radio settings for TX (per-packet via radiotap)
	// Use pointers so nil = "use default" vs 0 = "explicitly disabled"
	MCS       *int `yaml:"mcs,omitempty" json:"mcs,omitempty"`             // MCS index (nil = default 1)
	ShortGI   bool `yaml:"short_gi,omitempty" json:"short_gi,omitempty"`   // Short guard interval
	STBC      *int `yaml:"stbc,omitempty" json:"stbc,omitempty"`           // Space-time block coding (nil = default 1, 0 = disabled)
	LDPC      *int `yaml:"ldpc,omitempty" json:"ldpc,omitempty"`           // LDPC coding (nil = default 1, 0 = disabled)
	Bandwidth int  `yaml:"bandwidth,omitempty" json:"bandwidth,omitempty"` // 20 or 40 MHz (default: from hardware)

	// Advanced FEC
	FECTimeout int `yaml:"fec_timeout,omitempty" json:"fec_timeout,omitempty"` // ms, 0 to disable
	FECDelay   int `yaml:"fec_delay,omitempty" json:"fec_delay,omitempty"`     // us, inter-packet delay

	// Tunnel-specific (service_type: tunnel)
	Tunnel *TunnelConfig `yaml:"tunnel,omitempty" json:"tunnel,omitempty"`

	// Mavlink-specific (service_type: mavlink)
	Mavlink *MavlinkConfig `yaml:"mavlink,omitempty" json:"mavlink,omitempty"`

	// Advanced TX settings
	Mirror           bool   `yaml:"mirror,omitempty" json:"mirror,omitempty"`                       // TX via all antennas
	ControlPort      int    `yaml:"control_port,omitempty" json:"control_port,omitempty"`           // wfb_tx_cmd control
	UseQdisc         bool   `yaml:"use_qdisc,omitempty" json:"use_qdisc,omitempty"`                 // Use kernel qdisc
	FWMark           int    `yaml:"fwmark,omitempty" json:"fwmark,omitempty"`                       // Packet mark for tc
	FrameType        string `yaml:"frame_type,omitempty" json:"frame_type,omitempty"`               // data or rts
	InjectionRetries int    `yaml:"injection_retries,omitempty" json:"injection_retries,omitempty"`
}

// TunnelConfig defines tunnel-specific settings.
type TunnelConfig struct {
	Ifname       string `yaml:"ifname" json:"ifname"`
	Ifaddr       string `yaml:"ifaddr" json:"ifaddr"` // CIDR notation
	DefaultRoute bool   `yaml:"default_route,omitempty" json:"default_route,omitempty"`
}

// MavlinkConfig defines mavlink-specific settings.
type MavlinkConfig struct {
	InjectRSSI   bool   `yaml:"inject_rssi,omitempty" json:"inject_rssi,omitempty"`
	SysID        int    `yaml:"sys_id,omitempty" json:"sys_id,omitempty"`
	CompID       int    `yaml:"comp_id,omitempty" json:"comp_id,omitempty"`
	OSD          string `yaml:"osd,omitempty" json:"osd,omitempty"`
	TCPPort      *int   `yaml:"tcp_port,omitempty" json:"tcp_port,omitempty"`
	LogMessages  bool   `yaml:"log_messages,omitempty" json:"log_messages,omitempty"`
	CallOnArm    string `yaml:"call_on_arm,omitempty" json:"call_on_arm,omitempty"`
	CallOnDisarm string `yaml:"call_on_disarm,omitempty" json:"call_on_disarm,omitempty"`
}

// AdaptiveConfig defines adaptive link settings.
type AdaptiveConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Mode    string `yaml:"mode" json:"mode"` // "drone" or "gs"

	// Drone mode: listen for GS stats
	ListenPort int `yaml:"listen_port,omitempty" json:"listen_port,omitempty"`

	// GS mode: send stats to drone
	SendAddr string `yaml:"send_addr,omitempty" json:"send_addr,omitempty"`

	// Profiles
	ProfilesFile string          `yaml:"profiles_file,omitempty" json:"profiles_file,omitempty"`
	Profiles     []ProfileConfig `yaml:"profiles,omitempty" json:"profiles,omitempty"`

	// === Timing ===
	FallbackTimeout   Duration `yaml:"fallback_timeout,omitempty" json:"fallback_timeout,omitempty"`
	FallbackHold      Duration `yaml:"fallback_hold,omitempty" json:"fallback_hold,omitempty"`
	HoldUp            Duration `yaml:"hold_up,omitempty" json:"hold_up,omitempty"`
	HoldDown          Duration `yaml:"hold_down,omitempty" json:"hold_down,omitempty"`
	MinBetweenChanges Duration `yaml:"min_between_changes,omitempty" json:"min_between_changes,omitempty"`

	// === Smoothing/Hysteresis ===
	Smoothing      float64 `yaml:"smoothing,omitempty" json:"smoothing,omitempty"`
	SmoothingDown  float64 `yaml:"smoothing_down,omitempty" json:"smoothing_down,omitempty"`
	Hysteresis     float64 `yaml:"hysteresis,omitempty" json:"hysteresis,omitempty"`
	HysteresisDown float64 `yaml:"hysteresis_down,omitempty" json:"hysteresis_down,omitempty"`

	// === Keyframe ===
	AllowKeyframe    bool     `yaml:"allow_keyframe,omitempty" json:"allow_keyframe,omitempty"`
	KeyframeInterval Duration `yaml:"keyframe_interval,omitempty" json:"keyframe_interval,omitempty"`
	IDROnChange      bool     `yaml:"idr_on_change,omitempty" json:"idr_on_change,omitempty"`

	// === TX Dropped ===
	TXDropKeyframe      bool     `yaml:"tx_drop_keyframe,omitempty" json:"tx_drop_keyframe,omitempty"`
	TXDropReduceBitrate bool     `yaml:"tx_drop_reduce_bitrate,omitempty" json:"tx_drop_reduce_bitrate,omitempty"`
	TXDropCheckInterval Duration `yaml:"tx_drop_check_interval,omitempty" json:"tx_drop_check_interval,omitempty"`
	TXDropBitrateFactor float64  `yaml:"tx_drop_bitrate_factor,omitempty" json:"tx_drop_bitrate_factor,omitempty"`

	// === Dynamic FEC ===
	DynamicFEC bool `yaml:"dynamic_fec,omitempty" json:"dynamic_fec,omitempty"`
	FECKAdjust bool `yaml:"fec_k_adjust,omitempty" json:"fec_k_adjust,omitempty"`
	SpikeFix   bool `yaml:"spike_fix,omitempty" json:"spike_fix,omitempty"`

	// === Power ===
	MaxPowerLevel *int `yaml:"max_power_level,omitempty" json:"max_power_level,omitempty"` // 0-4 scale

	// === Commands ===
	Commands *AdaptiveCommands `yaml:"commands,omitempty" json:"commands,omitempty"`

	// === GS-specific (score calculation) ===
	ScoreWeights *ScoreWeights `yaml:"score_weights,omitempty" json:"score_weights,omitempty"`
	ScoreRanges  *ScoreRanges  `yaml:"score_ranges,omitempty" json:"score_ranges,omitempty"`
	Kalman       *KalmanConfig `yaml:"kalman,omitempty" json:"kalman,omitempty"`
}

// AdaptiveCommands defines command templates.
type AdaptiveCommands struct {
	Keyframe string `yaml:"keyframe,omitempty" json:"keyframe,omitempty"`
	Bitrate  string `yaml:"bitrate,omitempty" json:"bitrate,omitempty"`
	FEC      string `yaml:"fec,omitempty" json:"fec,omitempty"`
	MCS      string `yaml:"mcs,omitempty" json:"mcs,omitempty"`
	Power    string `yaml:"power,omitempty" json:"power,omitempty"`
	GOP      string `yaml:"gop,omitempty" json:"gop,omitempty"`
	FPS      string `yaml:"fps,omitempty" json:"fps,omitempty"`
	ROI      string `yaml:"roi,omitempty" json:"roi,omitempty"`
	QPDelta  string `yaml:"qp_delta,omitempty" json:"qp_delta,omitempty"`
}

// ScoreWeights for GS mode.
type ScoreWeights struct {
	SNR  float64 `yaml:"snr" json:"snr"`
	RSSI float64 `yaml:"rssi" json:"rssi"`
}

// ScoreRanges for GS mode.
type ScoreRanges struct {
	SNRMin  int `yaml:"snr_min" json:"snr_min"`
	SNRMax  int `yaml:"snr_max" json:"snr_max"`
	RSSIMin int `yaml:"rssi_min" json:"rssi_min"`
	RSSIMax int `yaml:"rssi_max" json:"rssi_max"`
}

// KalmanConfig for GS error estimation.
type KalmanConfig struct {
	Estimate            float64 `yaml:"estimate" json:"estimate"`
	ErrorEstimate       float64 `yaml:"error_estimate" json:"error_estimate"`
	ProcessVariance     float64 `yaml:"process_variance" json:"process_variance"`
	MeasurementVariance float64 `yaml:"measurement_variance" json:"measurement_variance"`
}

// ProfileConfig defines a TX profile.
type ProfileConfig struct {
	Range     [2]int  `yaml:"range" json:"range"`               // [min, max] score (1000-2000)
	MCS       int     `yaml:"mcs" json:"mcs"`
	ShortGI   bool    `yaml:"short_gi,omitempty" json:"short_gi,omitempty"`
	FEC       [2]int  `yaml:"fec" json:"fec"`                   // [k, n]
	Bitrate   int     `yaml:"bitrate" json:"bitrate"`           // kbps
	GOP       float64 `yaml:"gop,omitempty" json:"gop,omitempty"`
	Power     int     `yaml:"power,omitempty" json:"power,omitempty"`
	Bandwidth int     `yaml:"bandwidth,omitempty" json:"bandwidth,omitempty"`
	QPDelta   int     `yaml:"qp_delta,omitempty" json:"qp_delta,omitempty"`
	ROIQP     string  `yaml:"roi_qp,omitempty" json:"roi_qp,omitempty"`
}

// APIConfig defines API settings.
type APIConfig struct {
	Enabled     bool `yaml:"enabled" json:"enabled"`
	StatsPort   int  `yaml:"stats_port,omitempty" json:"stats_port,omitempty"`
	JSONPort    int  `yaml:"json_port,omitempty" json:"json_port,omitempty"`
	LogInterval int  `yaml:"log_interval,omitempty" json:"log_interval,omitempty"` // ms
}

// WebConfig defines web UI settings.
type WebConfig struct {
	Enabled     bool   `yaml:"enabled" json:"enabled"`                             // Enable web UI
	Port        int    `yaml:"port,omitempty" json:"port,omitempty"`               // HTTP port (default: 8080)
	VideoStream string `yaml:"video_stream,omitempty" json:"video_stream,omitempty"` // Stream name to display (e.g., "video")
}

// PathsConfig for system paths.
type PathsConfig struct {
	ConfDir string `yaml:"conf_dir,omitempty" json:"conf_dir,omitempty"`
	BinDir  string `yaml:"bin_dir,omitempty" json:"bin_dir,omitempty"`
	TmpDir  string `yaml:"tmp_dir,omitempty" json:"tmp_dir,omitempty"`
	LogDir  string `yaml:"log_dir,omitempty" json:"log_dir,omitempty"`
}

// CommonConfig for global settings.
type CommonConfig struct {
	Debug              bool    `yaml:"debug,omitempty" json:"debug,omitempty"`
	Primary            bool    `yaml:"primary,omitempty" json:"primary,omitempty"`
	RadioMTU           int     `yaml:"radio_mtu,omitempty" json:"radio_mtu,omitempty"`
	TunnelAggTimeout   float64 `yaml:"tunnel_agg_timeout,omitempty" json:"tunnel_agg_timeout,omitempty"`
	MavlinkAggTimeout  float64 `yaml:"mavlink_agg_timeout,omitempty" json:"mavlink_agg_timeout,omitempty"`
	LogInterval        int     `yaml:"log_interval,omitempty" json:"log_interval,omitempty"`
	TxSelRssiDelta     int     `yaml:"tx_sel_rssi_delta,omitempty" json:"tx_sel_rssi_delta,omitempty"`
	TxRcvBufSize       int     `yaml:"tx_rcv_buf_size,omitempty" json:"tx_rcv_buf_size,omitempty"`
	RxSndBufSize       int     `yaml:"rx_snd_buf_size,omitempty" json:"rx_snd_buf_size,omitempty"`
}

// Duration wraps time.Duration for YAML parsing.
type Duration time.Duration

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	// Try integer first (treated as milliseconds)
	var ms int
	if err := node.Decode(&ms); err == nil {
		*d = Duration(time.Duration(ms) * time.Millisecond)
		return nil
	}

	// Try string (e.g., "500ms", "2s")
	var s string
	if err := node.Decode(&s); err != nil {
		return err
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(dur)
	return nil
}

func (d Duration) MarshalYAML() (interface{}, error) {
	return time.Duration(d).String(), nil
}

func (d Duration) Dur() time.Duration {
	return time.Duration(d)
}

// DefaultConfig returns config with alink.conf defaults.
func DefaultConfig() *Config {
	return &Config{
		Hardware: HardwareConfig{
			Region:    "BO",
			Channel:   165,
			Bandwidth: 20,
		},
		Link: LinkConfig{
			Domain: "default",
		},
		Streams: make(map[string]StreamConfig),
	}
}

// ApplyDefaults applies default values to a stream config.
// Only sets defaults for nil pointers, preserving explicit 0 values.
func (s *StreamConfig) ApplyDefaults(hardwareBandwidth int) {
	if s.MCS == nil {
		defaultMCS := 1
		s.MCS = &defaultMCS
	}
	if s.STBC == nil {
		defaultSTBC := 1
		s.STBC = &defaultSTBC
	}
	if s.LDPC == nil {
		defaultLDPC := 1
		s.LDPC = &defaultLDPC
	}
	if s.Bandwidth == 0 {
		s.Bandwidth = hardwareBandwidth
	}
}

// GetMCS returns MCS value with default.
func (s *StreamConfig) GetMCS() int {
	if s.MCS != nil {
		return *s.MCS
	}
	return 1
}

// GetSTBC returns STBC value with default.
func (s *StreamConfig) GetSTBC() int {
	if s.STBC != nil {
		return *s.STBC
	}
	return 1
}

// GetLDPC returns LDPC value with default.
func (s *StreamConfig) GetLDPC() int {
	if s.LDPC != nil {
		return *s.LDPC
	}
	return 1
}

// DefaultAdaptive returns adaptive config with alink.conf defaults.
func DefaultAdaptive() *AdaptiveConfig {
	return &AdaptiveConfig{
		Enabled:             false,
		Mode:                "drone",
		ListenPort:          9999,
		FallbackTimeout:     Duration(1 * time.Second),
		FallbackHold:        Duration(1 * time.Second),
		HoldUp:              Duration(3 * time.Second),
		HoldDown:            Duration(0),
		MinBetweenChanges:   Duration(200 * time.Millisecond),
		Smoothing:           0.1,
		SmoothingDown:       1.0,
		Hysteresis:          5,
		HysteresisDown:      5,
		AllowKeyframe:       true,
		KeyframeInterval:    Duration(1112 * time.Millisecond),
		TXDropKeyframe:      true,
		TXDropReduceBitrate: true,
		TXDropCheckInterval: Duration(2250 * time.Millisecond),
		TXDropBitrateFactor: 0.8,
		DynamicFEC:          false,
		FECKAdjust:          true,
		SpikeFix:            false,
		ScoreWeights:        &ScoreWeights{SNR: 0.5, RSSI: 0.5},
		ScoreRanges:         &ScoreRanges{SNRMin: 12, SNRMax: 38, RSSIMin: -80, RSSIMax: -30},
		Kalman: &KalmanConfig{
			Estimate:            0.005,
			ErrorEstimate:       0.1,
			ProcessVariance:     1e-5,
			MeasurementVariance: 0.01,
		},
	}
}

// Load reads config from YAML file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Apply adaptive defaults if enabled but not fully specified
	if cfg.Adaptive != nil && cfg.Adaptive.Enabled {
		defaults := DefaultAdaptive()
		applyAdaptiveDefaults(cfg.Adaptive, defaults)
	}

	// Load profiles from file if specified
	if cfg.Adaptive != nil && cfg.Adaptive.ProfilesFile != "" {
		profiles, err := LoadProfiles(cfg.Adaptive.ProfilesFile)
		if err != nil {
			return nil, fmt.Errorf("load profiles: %w", err)
		}
		cfg.Adaptive.Profiles = profiles
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate: %w", err)
	}

	return cfg, nil
}

func applyAdaptiveDefaults(cfg, defaults *AdaptiveConfig) {
	if cfg.ListenPort == 0 {
		cfg.ListenPort = defaults.ListenPort
	}
	if cfg.FallbackTimeout == 0 {
		cfg.FallbackTimeout = defaults.FallbackTimeout
	}
	if cfg.FallbackHold == 0 {
		cfg.FallbackHold = defaults.FallbackHold
	}
	if cfg.HoldUp == 0 {
		cfg.HoldUp = defaults.HoldUp
	}
	if cfg.MinBetweenChanges == 0 {
		cfg.MinBetweenChanges = defaults.MinBetweenChanges
	}
	if cfg.Smoothing == 0 {
		cfg.Smoothing = defaults.Smoothing
	}
	if cfg.SmoothingDown == 0 {
		cfg.SmoothingDown = defaults.SmoothingDown
	}
	if cfg.Hysteresis == 0 {
		cfg.Hysteresis = defaults.Hysteresis
	}
	if cfg.HysteresisDown == 0 {
		cfg.HysteresisDown = defaults.HysteresisDown
	}
	if cfg.KeyframeInterval == 0 {
		cfg.KeyframeInterval = defaults.KeyframeInterval
	}
	if cfg.TXDropCheckInterval == 0 {
		cfg.TXDropCheckInterval = defaults.TXDropCheckInterval
	}
	if cfg.TXDropBitrateFactor == 0 {
		cfg.TXDropBitrateFactor = defaults.TXDropBitrateFactor
	}
	if cfg.ScoreWeights == nil {
		cfg.ScoreWeights = defaults.ScoreWeights
	}
	if cfg.ScoreRanges == nil {
		cfg.ScoreRanges = defaults.ScoreRanges
	}
	if cfg.Kalman == nil {
		cfg.Kalman = defaults.Kalman
	}
}

// LoadProfiles reads profiles from YAML or txprofiles.conf format.
func LoadProfiles(path string) ([]ProfileConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Try YAML first
	var profiles []ProfileConfig
	if err := yaml.Unmarshal(data, &profiles); err == nil && len(profiles) > 0 {
		return profiles, nil
	}

	// Fall back to txprofiles.conf format
	return ParseTxProfilesConf(string(data))
}

// ParseTxProfilesConf parses txprofiles.conf format.
func ParseTxProfilesConf(data string) ([]ProfileConfig, error) {
	var profiles []ProfileConfig

	for _, line := range strings.Split(data, "\n") {
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = line[:idx]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 12 {
			continue
		}

		p := ProfileConfig{}
		var err error

		if p.Range[0], err = strconv.Atoi(fields[0]); err != nil {
			continue
		}
		if p.Range[1], err = strconv.Atoi(fields[2]); err != nil {
			continue
		}
		p.ShortGI = strings.ToLower(fields[3]) == "short"
		if p.MCS, err = strconv.Atoi(fields[4]); err != nil {
			continue
		}
		if p.FEC[0], err = strconv.Atoi(fields[5]); err != nil {
			continue
		}
		if p.FEC[1], err = strconv.Atoi(fields[6]); err != nil {
			continue
		}
		if p.Bitrate, err = strconv.Atoi(fields[7]); err != nil {
			continue
		}
		if p.GOP, err = strconv.ParseFloat(fields[8], 64); err != nil {
			continue
		}
		if p.Power, err = strconv.Atoi(fields[9]); err != nil {
			continue
		}
		p.ROIQP = fields[10]
		if p.Bandwidth, err = strconv.Atoi(fields[11]); err != nil {
			continue
		}
		if len(fields) > 12 {
			p.QPDelta, _ = strconv.Atoi(fields[12])
		}

		profiles = append(profiles, p)
	}

	if len(profiles) == 0 {
		return nil, fmt.Errorf("no valid profiles")
	}
	return profiles, nil
}

// Valid service types
var validServiceTypes = map[string]bool{
	"udp_direct_tx": true,
	"udp_direct_rx": true,
	"udp_proxy":     true,
	"mavlink":       true,
	"tunnel":        true,
}

// Validate checks config for errors.
func (c *Config) Validate() error {
	if len(c.Hardware.WLANs) == 0 {
		return fmt.Errorf("no wlans specified")
	}
	if c.Hardware.Channel < 1 || c.Hardware.Channel > 200 {
		return fmt.Errorf("invalid channel: %d", c.Hardware.Channel)
	}
	if c.Hardware.Bandwidth != 20 && c.Hardware.Bandwidth != 40 {
		return fmt.Errorf("invalid bandwidth: %d", c.Hardware.Bandwidth)
	}

	for name, s := range c.Streams {
		if !validServiceTypes[s.ServiceType] {
			return fmt.Errorf("stream %s: invalid service_type %q", name, s.ServiceType)
		}

		// Validate stream IDs based on service type
		switch s.ServiceType {
		case "udp_direct_tx":
			if s.StreamTX == nil {
				return fmt.Errorf("stream %s: udp_direct_tx requires stream_tx", name)
			}
		case "udp_direct_rx":
			if s.StreamRX == nil {
				return fmt.Errorf("stream %s: udp_direct_rx requires stream_rx", name)
			}
		case "udp_proxy", "mavlink", "tunnel":
			if s.StreamRX == nil || s.StreamTX == nil {
				return fmt.Errorf("stream %s: %s requires both stream_rx and stream_tx", name, s.ServiceType)
			}
		}

		// Validate peer format
		if s.Peer != "" {
			if !strings.HasPrefix(s.Peer, "listen://") &&
				!strings.HasPrefix(s.Peer, "connect://") &&
				!strings.HasPrefix(s.Peer, "serial:") &&
				!strings.HasPrefix(s.Peer, "tcp://") &&
				!strings.HasPrefix(s.Peer, "tun:") {
				return fmt.Errorf("stream %s: invalid peer format %q", name, s.Peer)
			}
		}
	}

	if c.Adaptive != nil {
		for i, p := range c.Adaptive.Profiles {
			if p.Range[0] < 999 || p.Range[1] > 2001 {
				return fmt.Errorf("profile %d: invalid range", i)
			}
		}
	}

	return nil
}

// GetStreamFEC returns FEC k,n for a stream (with defaults).
func (s *StreamConfig) GetFEC() (int, int) {
	if s.FEC[0] > 0 && s.FEC[1] > 0 {
		return s.FEC[0], s.FEC[1]
	}
	return 1, 2 // default
}


// GetKeyData returns the key bytes for the link config.
// Prefers key_base64 if set, otherwise reads from key file path.
func (l *LinkConfig) GetKeyData() ([]byte, error) {
	if l.KeyBase64 != "" {
		data, err := base64.StdEncoding.DecodeString(l.KeyBase64)
		if err != nil {
			return nil, fmt.Errorf("decode key_base64: %w", err)
		}
		return data, nil
	}
	if l.Key != "" {
		data, err := os.ReadFile(l.Key)
		if err != nil {
			return nil, fmt.Errorf("read key file %s: %w", l.Key, err)
		}
		return data, nil
	}
	return nil, fmt.Errorf("no key or key_base64 specified")
}

// GetKeyData returns the key bytes for the stream config.
// Prefers key_base64 if set, otherwise reads from key file path.
// If neither is set, returns nil (caller should fall back to link config).
func (s *StreamConfig) GetKeyData() ([]byte, error) {
	if s.KeyBase64 != "" {
		data, err := base64.StdEncoding.DecodeString(s.KeyBase64)
		if err != nil {
			return nil, fmt.Errorf("decode key_base64: %w", err)
		}
		return data, nil
	}
	if s.Key != "" {
		data, err := os.ReadFile(s.Key)
		if err != nil {
			return nil, fmt.Errorf("read key file %s: %w", s.Key, err)
		}
		return data, nil
	}
	return nil, nil // No stream-specific key, use link config
}

// GetStreamKeyData returns key data for a stream, falling back to link config.
func (c *Config) GetStreamKeyData(s *StreamConfig) ([]byte, error) {
	// Try stream-specific key first
	data, err := s.GetKeyData()
	if err != nil {
		return nil, err
	}
	if data != nil {
		return data, nil
	}
	// Fall back to link config
	return c.Link.GetKeyData()
}

// Save writes the config to a YAML file.
func (c *Config) Save(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

// Clone creates a deep copy of the config for safe modification.
func (c *Config) Clone() *Config {
	data, _ := yaml.Marshal(c)
	clone := &Config{}
	yaml.Unmarshal(data, clone)
	return clone
}

