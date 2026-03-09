package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfig(t *testing.T) {
	yaml := `
hardware:
  wlans: [wlan0, wlan1]
  region: US
  channel: 149
  bandwidth: 20
  tx_power: 30
  mcs: 2

link:
  domain: "my-drone"
  key: /etc/wfb/drone.key

streams:
  video:
    service_type: udp_direct_tx
    stream_tx: 0
    peer: "listen://0.0.0.0:5600"
    fec: [8, 12]
  telemetry:
    service_type: udp_proxy
    stream_tx: 16
    stream_rx: 144
    peer: "listen://0.0.0.0:14550"
    fec: [1, 2]

adaptive:
  enabled: true
  mode: drone
  listen_port: 9999
  profiles:
    - range: [1000, 1200]
      mcs: 1
      fec: [2, 3]
      bitrate: 4000
    - range: [1201, 1600]
      mcs: 3
      fec: [8, 12]
      bitrate: 12000
    - range: [1601, 2000]
      mcs: 4
      short_gi: true
      fec: [10, 15]
      bitrate: 18000

api:
  enabled: true
  stats_port: 8002
  json_port: 8102
`

	// Write temp file
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Check hardware
	if len(cfg.Hardware.WLANs) != 2 {
		t.Errorf("WLANs = %v, want 2 items", cfg.Hardware.WLANs)
	}
	if cfg.Hardware.Channel != 149 {
		t.Errorf("Channel = %d, want 149", cfg.Hardware.Channel)
	}
	if cfg.Hardware.TXPower == nil || *cfg.Hardware.TXPower != 30 {
		t.Errorf("TXPower = %v, want 30", cfg.Hardware.TXPower)
	}

	// Check link
	if cfg.Link.Domain != "my-drone" {
		t.Errorf("Domain = %q, want my-drone", cfg.Link.Domain)
	}

	// Check streams
	if len(cfg.Streams) != 2 {
		t.Errorf("Streams = %d, want 2", len(cfg.Streams))
	}
	video := cfg.Streams["video"]
	if video.ServiceType != "udp_direct_tx" {
		t.Errorf("video.ServiceType = %q, want udp_direct_tx", video.ServiceType)
	}
	if video.FEC[0] != 8 || video.FEC[1] != 12 {
		t.Errorf("video.FEC = %v, want [8, 12]", video.FEC)
	}

	// Check adaptive
	if cfg.Adaptive == nil || !cfg.Adaptive.Enabled {
		t.Fatal("Adaptive not enabled")
	}
	if len(cfg.Adaptive.Profiles) != 3 {
		t.Errorf("Profiles = %d, want 3", len(cfg.Adaptive.Profiles))
	}
	if cfg.Adaptive.Profiles[2].ShortGI != true {
		t.Errorf("Profile[2].ShortGI = false, want true")
	}

	// Check API
	if cfg.API == nil || cfg.API.StatsPort != 8002 {
		t.Errorf("API.StatsPort = %v, want 8002", cfg.API)
	}
}

func TestLoadProfiles(t *testing.T) {
	// Test txprofiles.conf format
	conf := `# Comment
999  -  999  long 0 2 3    1000 10 30   0,0,0,0 20 -12
1000 - 1200  long 1 2 3    4000 10 30   0,0,0,0 20 -12
1201 - 1600 short 3 8 12  12000  5 30   0,0,0,0 20 -12
`
	dir := t.TempDir()
	path := filepath.Join(dir, "profiles.conf")
	if err := os.WriteFile(path, []byte(conf), 0644); err != nil {
		t.Fatal(err)
	}

	profiles, err := LoadProfiles(path)
	if err != nil {
		t.Fatalf("LoadProfiles() error: %v", err)
	}

	if len(profiles) != 3 {
		t.Fatalf("profiles = %d, want 3", len(profiles))
	}

	// Check first profile
	p := profiles[0]
	if p.Range[0] != 999 || p.Range[1] != 999 {
		t.Errorf("profile[0].Range = %v, want [999, 999]", p.Range)
	}
	if p.ShortGI != false {
		t.Errorf("profile[0].ShortGI = true, want false")
	}

	// Check third profile
	p = profiles[2]
	if p.Range[0] != 1201 || p.Range[1] != 1600 {
		t.Errorf("profile[2].Range = %v, want [1201, 1600]", p.Range)
	}
	if p.ShortGI != true {
		t.Errorf("profile[2].ShortGI = false, want true")
	}
	if p.FEC[0] != 8 || p.FEC[1] != 12 {
		t.Errorf("profile[2].FEC = %v, want [8, 12]", p.FEC)
	}
}

func TestDefaultAdaptive(t *testing.T) {
	defaults := DefaultAdaptive()

	// Check alink.conf defaults
	if defaults.FallbackTimeout.Dur() != 1*time.Second {
		t.Errorf("FallbackTimeout = %v, want 1s", defaults.FallbackTimeout)
	}
	if defaults.HoldUp.Dur() != 3*time.Second {
		t.Errorf("HoldUp = %v, want 3s", defaults.HoldUp)
	}
	if defaults.Smoothing != 0.1 {
		t.Errorf("Smoothing = %v, want 0.1", defaults.Smoothing)
	}
	if defaults.SmoothingDown != 1.0 {
		t.Errorf("SmoothingDown = %v, want 1.0", defaults.SmoothingDown)
	}
	if defaults.Hysteresis != 5 {
		t.Errorf("Hysteresis = %v, want 5", defaults.Hysteresis)
	}
	if defaults.TXDropBitrateFactor != 0.8 {
		t.Errorf("TXDropBitrateFactor = %v, want 0.8", defaults.TXDropBitrateFactor)
	}
	if !defaults.FECKAdjust {
		t.Error("FECKAdjust = false, want true")
	}
}

func TestDuration(t *testing.T) {
	yaml := `
hardware:
  wlans: [wlan0]
  channel: 165
  bandwidth: 20

link:
  domain: test

streams: {}

adaptive:
  enabled: true
  fallback_timeout: 500ms
  hold_up: 2s
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Adaptive.FallbackTimeout.Dur() != 500*time.Millisecond {
		t.Errorf("FallbackTimeout = %v, want 500ms", cfg.Adaptive.FallbackTimeout)
	}
	if cfg.Adaptive.HoldUp.Dur() != 2*time.Second {
		t.Errorf("HoldUp = %v, want 2s", cfg.Adaptive.HoldUp)
	}
}

func TestValidation(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name:    "no wlans",
			yaml:    "hardware:\n  channel: 165\n  bandwidth: 20\nlink:\n  domain: test\nstreams: {}",
			wantErr: "no wlans",
		},
		{
			name:    "invalid channel",
			yaml:    "hardware:\n  wlans: [wlan0]\n  channel: 999\n  bandwidth: 20\nlink:\n  domain: test\nstreams: {}",
			wantErr: "invalid channel",
		},
		{
			name:    "invalid bandwidth",
			yaml:    "hardware:\n  wlans: [wlan0]\n  channel: 165\n  bandwidth: 30\nlink:\n  domain: test\nstreams: {}",
			wantErr: "invalid bandwidth",
		},
		{
			name:    "invalid service_type",
			yaml:    "hardware:\n  wlans: [wlan0]\n  channel: 165\n  bandwidth: 20\nlink:\n  domain: test\nstreams:\n  foo:\n    service_type: invalid\n    peer: \"listen://0.0.0.0:5600\"",
			wantErr: "invalid service_type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(path, []byte(tt.yaml), 0644); err != nil {
				t.Fatal(err)
			}

			_, err := Load(path)
			if err == nil {
				t.Fatal("expected error")
			}
			if !contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestHardwareOverrides(t *testing.T) {
	yaml := `
hardware:
  wlans: [wlan0, wlan1, wlan2]
  region: US
  channel: 149
  bandwidth: 20
  tx_power: 30
  channel_overrides:
    wlan1: 36
    wlan2: 161
  tx_power_overrides:
    wlan1: 20
    wlan2: 25

link:
  domain: test

streams: {}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Check overrides
	if cfg.Hardware.ChannelOverrides["wlan1"] != 36 {
		t.Errorf("ChannelOverrides[wlan1] = %d, want 36", cfg.Hardware.ChannelOverrides["wlan1"])
	}
	if cfg.Hardware.TXPowerOverrides["wlan2"] != 25 {
		t.Errorf("TXPowerOverrides[wlan2] = %d, want 25", cfg.Hardware.TXPowerOverrides["wlan2"])
	}
}

func TestStreamOverrides(t *testing.T) {
	yaml := `
hardware:
  wlans: [wlan0]
  channel: 165
  bandwidth: 20

link:
  domain: test

streams:
  video:
    service_type: udp_direct_tx
    stream_tx: 0
    peer: "listen://0.0.0.0:5600"
    fec: [8, 12]
    mcs: 4
    stbc: 2
    ldpc: 0
    short_gi: true
    bandwidth: 40
    fec_timeout: 50
    fec_delay: 1000
    control_port: 9876
    mirror: true
    frame_type: data
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	video := cfg.Streams["video"]

	// Check stream-level overrides
	if video.GetMCS() != 4 {
		t.Errorf("video.GetMCS() = %d, want 4", video.GetMCS())
	}
	if video.GetSTBC() != 2 {
		t.Errorf("video.GetSTBC() = %d, want 2", video.GetSTBC())
	}
	if video.GetLDPC() != 0 {
		t.Errorf("video.GetLDPC() = %d, want 0 (disabled)", video.GetLDPC())
	}
	if !video.ShortGI {
		t.Errorf("video.ShortGI = %v, want true", video.ShortGI)
	}
	if video.Bandwidth != 40 {
		t.Errorf("video.Bandwidth = %v, want 40", video.Bandwidth)
	}
	if video.FECTimeout != 50 {
		t.Errorf("video.FECTimeout = %d, want 50", video.FECTimeout)
	}
	if video.FECDelay != 1000 {
		t.Errorf("video.FECDelay = %d, want 1000", video.FECDelay)
	}
	if video.ControlPort != 9876 {
		t.Errorf("video.ControlPort = %d, want 9876", video.ControlPort)
	}
	if !video.Mirror {
		t.Error("video.Mirror = false, want true")
	}
}

func TestTunnelConfig(t *testing.T) {
	yaml := `
hardware:
  wlans: [wlan0]
  channel: 165
  bandwidth: 20

link:
  domain: test

streams:
  wfb-tunnel:
    service_type: tunnel
    stream_tx: 32
    stream_rx: 160
    fec: [1, 2]
    tunnel:
      ifname: wfb-tunnel
      ifaddr: 10.5.0.1/24
      default_route: true
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	tunnel := cfg.Streams["wfb-tunnel"]
	if tunnel.Tunnel == nil {
		t.Fatal("tunnel config is nil")
	}
	if tunnel.Tunnel.Ifname != "wfb-tunnel" {
		t.Errorf("Ifname = %q, want wfb-tunnel", tunnel.Tunnel.Ifname)
	}
	if tunnel.Tunnel.Ifaddr != "10.5.0.1/24" {
		t.Errorf("Ifaddr = %q, want 10.5.0.1/24", tunnel.Tunnel.Ifaddr)
	}
	if !tunnel.Tunnel.DefaultRoute {
		t.Error("DefaultRoute = false, want true")
	}
}

func TestMavlinkConfig(t *testing.T) {
	yaml := `
hardware:
  wlans: [wlan0]
  channel: 165
  bandwidth: 20

link:
  domain: test

streams:
  mavlink:
    service_type: mavlink
    stream_tx: 16
    stream_rx: 144
    fec: [1, 2]
    peer: "connect://127.0.0.1:14551"
    mavlink:
      inject_rssi: true
      sys_id: 3
      comp_id: 68
      osd: "127.0.0.1:14560"
      tcp_port: 5790
      log_messages: true
      call_on_arm: "/usr/bin/arm-script.sh"
      call_on_disarm: "/usr/bin/disarm-script.sh"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	mav := cfg.Streams["mavlink"]
	if mav.Peer != "connect://127.0.0.1:14551" {
		t.Errorf("Peer = %q, want connect://127.0.0.1:14551", mav.Peer)
	}
	if mav.Mavlink == nil {
		t.Fatal("mavlink config is nil")
	}
	if !mav.Mavlink.InjectRSSI {
		t.Error("InjectRSSI = false, want true")
	}
	if mav.Mavlink.SysID != 3 {
		t.Errorf("SysID = %d, want 3", mav.Mavlink.SysID)
	}
	if mav.Mavlink.CompID != 68 {
		t.Errorf("CompID = %d, want 68", mav.Mavlink.CompID)
	}
	if mav.Mavlink.TCPPort == nil || *mav.Mavlink.TCPPort != 5790 {
		t.Errorf("TCPPort = %v, want 5790", mav.Mavlink.TCPPort)
	}
}

func TestAdaptiveCommands(t *testing.T) {
	yaml := `
hardware:
  wlans: [wlan0]
  channel: 165
  bandwidth: 20

link:
  domain: test

streams: {}

adaptive:
  enabled: true
  mode: drone
  commands:
    keyframe: "curl -s http://127.0.0.1:8080/idr"
    bitrate: "curl -s http://127.0.0.1:8080/bitrate?value={}"
    fec: "echo {} {} > /tmp/fec.txt"
    mcs: "iw dev wlan0 set bitrates ht-mcs-5 {}"
    power: "/usr/bin/set-power.sh {}"
    gop: "curl -s http://127.0.0.1:8080/gop?value={}"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Adaptive.Commands == nil {
		t.Fatal("Commands is nil")
	}
	if cfg.Adaptive.Commands.Keyframe != "curl -s http://127.0.0.1:8080/idr" {
		t.Errorf("Keyframe = %q", cfg.Adaptive.Commands.Keyframe)
	}
	if cfg.Adaptive.Commands.Bitrate != "curl -s http://127.0.0.1:8080/bitrate?value={}" {
		t.Errorf("Bitrate = %q", cfg.Adaptive.Commands.Bitrate)
	}
}

func TestAdaptiveGSMode(t *testing.T) {
	yaml := `
hardware:
  wlans: [wlan0]
  channel: 165
  bandwidth: 20

link:
  domain: test

streams: {}

adaptive:
  enabled: true
  mode: gs
  send_addr: "10.5.0.10:9999"
  score_weights:
    snr: 0.6
    rssi: 0.4
  score_ranges:
    snr_min: 10
    snr_max: 40
    rssi_min: -85
    rssi_max: -25
  kalman:
    estimate: 0.01
    error_estimate: 0.2
    process_variance: 0.00001
    measurement_variance: 0.02
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Adaptive.Mode != "gs" {
		t.Errorf("Mode = %q, want gs", cfg.Adaptive.Mode)
	}
	if cfg.Adaptive.SendAddr != "10.5.0.10:9999" {
		t.Errorf("SendAddr = %q, want 10.5.0.10:9999", cfg.Adaptive.SendAddr)
	}
	if cfg.Adaptive.ScoreWeights.SNR != 0.6 {
		t.Errorf("ScoreWeights.SNR = %v, want 0.6", cfg.Adaptive.ScoreWeights.SNR)
	}
	if cfg.Adaptive.ScoreRanges.SNRMin != 10 {
		t.Errorf("ScoreRanges.SNRMin = %d, want 10", cfg.Adaptive.ScoreRanges.SNRMin)
	}
	if cfg.Adaptive.Kalman.Estimate != 0.01 {
		t.Errorf("Kalman.Estimate = %v, want 0.01", cfg.Adaptive.Kalman.Estimate)
	}
}

func TestPathsAndCommonConfig(t *testing.T) {
	yaml := `
hardware:
  wlans: [wlan0]
  channel: 165
  bandwidth: 20

link:
  domain: test

streams: {}

paths:
  conf_dir: /etc/wfb
  bin_dir: /usr/local/bin
  tmp_dir: /var/tmp
  log_dir: /var/log/wfb

common:
  debug: true
  primary: true
  radio_mtu: 1445
  tunnel_agg_timeout: 0.005
  mavlink_agg_timeout: 0.1
  log_interval: 1000
  tx_sel_rssi_delta: 3
  tx_rcv_buf_size: 2097152
  rx_snd_buf_size: 2097152
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Paths == nil {
		t.Fatal("Paths is nil")
	}
	if cfg.Paths.ConfDir != "/etc/wfb" {
		t.Errorf("ConfDir = %q, want /etc/wfb", cfg.Paths.ConfDir)
	}
	if cfg.Paths.LogDir != "/var/log/wfb" {
		t.Errorf("LogDir = %q, want /var/log/wfb", cfg.Paths.LogDir)
	}

	if cfg.Common == nil {
		t.Fatal("Common is nil")
	}
	if !cfg.Common.Debug {
		t.Error("Debug = false, want true")
	}
	if cfg.Common.RadioMTU != 1445 {
		t.Errorf("RadioMTU = %d, want 1445", cfg.Common.RadioMTU)
	}
	if cfg.Common.TxSelRssiDelta != 3 {
		t.Errorf("TxSelRssiDelta = %d, want 3", cfg.Common.TxSelRssiDelta)
	}
}

func TestAPIConfig(t *testing.T) {
	yaml := `
hardware:
  wlans: [wlan0]
  channel: 165
  bandwidth: 20

link:
  domain: test

streams: {}

api:
  enabled: true
  stats_port: 8002
  json_port: 8102
  log_interval: 500
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.API == nil {
		t.Fatal("API is nil")
	}
	if cfg.API.StatsPort != 8002 {
		t.Errorf("StatsPort = %d, want 8002", cfg.API.StatsPort)
	}
	if cfg.API.JSONPort != 8102 {
		t.Errorf("JSONPort = %d, want 8102", cfg.API.JSONPort)
	}
	if cfg.API.LogInterval != 500 {
		t.Errorf("LogInterval = %d, want 500", cfg.API.LogInterval)
	}
}

func TestYAMLProfiles(t *testing.T) {
	// Test YAML format profiles (alternative to txprofiles.conf)
	yaml := `
- range: [1000, 1200]
  mcs: 1
  fec: [2, 3]
  bitrate: 4000
  short_gi: false
  gop: 10
  power: 30
  bandwidth: 20
  qp_delta: -12
  roi_qp: "0,0,0,0"
- range: [1201, 1600]
  mcs: 3
  fec: [8, 12]
  bitrate: 12000
  short_gi: true
`
	dir := t.TempDir()
	path := filepath.Join(dir, "profiles.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	profiles, err := LoadProfiles(path)
	if err != nil {
		t.Fatalf("LoadProfiles() error: %v", err)
	}

	if len(profiles) != 2 {
		t.Fatalf("profiles = %d, want 2", len(profiles))
	}

	p := profiles[0]
	if p.Range[0] != 1000 || p.Range[1] != 1200 {
		t.Errorf("profile[0].Range = %v, want [1000, 1200]", p.Range)
	}
	if p.GOP != 10 {
		t.Errorf("profile[0].GOP = %v, want 10", p.GOP)
	}
	if p.Power != 30 {
		t.Errorf("profile[0].Power = %d, want 30", p.Power)
	}
	if p.QPDelta != -12 {
		t.Errorf("profile[0].QPDelta = %d, want -12", p.QPDelta)
	}
}

func TestProfilesFileReference(t *testing.T) {
	// Create profiles file
	dir := t.TempDir()
	profilesPath := filepath.Join(dir, "profiles.conf")
	profiles := `1000 - 1200  long 1 2 3 4000 10 30 0,0,0,0 20 -12
1201 - 1600 short 3 8 12 12000 5 30 0,0,0,0 20 -12
`
	if err := os.WriteFile(profilesPath, []byte(profiles), 0644); err != nil {
		t.Fatal(err)
	}

	// Create config that references profiles file
	yaml := `
hardware:
  wlans: [wlan0]
  channel: 165
  bandwidth: 20

link:
  domain: test

streams: {}

adaptive:
  enabled: true
  mode: drone
  profiles_file: ` + profilesPath + `
`
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if len(cfg.Adaptive.Profiles) != 2 {
		t.Fatalf("Profiles = %d, want 2", len(cfg.Adaptive.Profiles))
	}
	if cfg.Adaptive.Profiles[0].MCS != 1 {
		t.Errorf("Profile[0].MCS = %d, want 1", cfg.Adaptive.Profiles[0].MCS)
	}
}

func TestDurationInteger(t *testing.T) {
	// Test that integer values are treated as milliseconds
	yaml := `
hardware:
  wlans: [wlan0]
  channel: 165
  bandwidth: 20

link:
  domain: test

streams: {}

adaptive:
  enabled: true
  fallback_timeout: 2000
  hold_up: 5000
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Adaptive.FallbackTimeout.Dur() != 2*time.Second {
		t.Errorf("FallbackTimeout = %v, want 2s", cfg.Adaptive.FallbackTimeout)
	}
	if cfg.Adaptive.HoldUp.Dur() != 5*time.Second {
		t.Errorf("HoldUp = %v, want 5s", cfg.Adaptive.HoldUp)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Hardware.Region != "BO" {
		t.Errorf("Region = %q, want BO", cfg.Hardware.Region)
	}
	if cfg.Hardware.Channel != 165 {
		t.Errorf("Channel = %d, want 165", cfg.Hardware.Channel)
	}
	if cfg.Hardware.Bandwidth != 20 {
		t.Errorf("Bandwidth = %d, want 20", cfg.Hardware.Bandwidth)
	}
	if cfg.Link.Domain != "default" {
		t.Errorf("Domain = %q, want default", cfg.Link.Domain)
	}
}

func TestGetFEC(t *testing.T) {
	// Test with FEC set
	s := StreamConfig{FEC: [2]int{8, 12}}
	k, n := s.GetFEC()
	if k != 8 || n != 12 {
		t.Errorf("GetFEC() = %d, %d, want 8, 12", k, n)
	}

	// Test with FEC not set (defaults)
	s2 := StreamConfig{}
	k, n = s2.GetFEC()
	if k != 1 || n != 2 {
		t.Errorf("GetFEC() = %d, %d, want 1, 2", k, n)
	}
}

func TestStreamRadioParamDefaults(t *testing.T) {
	// Stream without explicit values - uses defaults
	s := &StreamConfig{}
	if s.GetMCS() != 1 {
		t.Errorf("GetMCS() = %d, want 1 (default)", s.GetMCS())
	}
	if s.GetSTBC() != 1 {
		t.Errorf("GetSTBC() = %d, want 1 (default)", s.GetSTBC())
	}
	if s.GetLDPC() != 1 {
		t.Errorf("GetLDPC() = %d, want 1 (default)", s.GetLDPC())
	}

	// Stream with explicit values
	mcs := 5
	stbc := 2
	ldpc := 0 // explicitly disabled
	s2 := &StreamConfig{MCS: &mcs, STBC: &stbc, LDPC: &ldpc}
	if s2.GetMCS() != 5 {
		t.Errorf("GetMCS() = %d, want 5", s2.GetMCS())
	}
	if s2.GetSTBC() != 2 {
		t.Errorf("GetSTBC() = %d, want 2", s2.GetSTBC())
	}
	if s2.GetLDPC() != 0 {
		t.Errorf("GetLDPC() = %d, want 0 (disabled)", s2.GetLDPC())
	}
}

func TestAdaptiveDefaultsApplied(t *testing.T) {
	yaml := `
hardware:
  wlans: [wlan0]
  channel: 165
  bandwidth: 20

link:
  domain: test

streams: {}

adaptive:
  enabled: true
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Check that defaults are applied
	if cfg.Adaptive.ListenPort != 9999 {
		t.Errorf("ListenPort = %d, want 9999", cfg.Adaptive.ListenPort)
	}
	if cfg.Adaptive.FallbackTimeout.Dur() != 1*time.Second {
		t.Errorf("FallbackTimeout = %v, want 1s", cfg.Adaptive.FallbackTimeout)
	}
	if cfg.Adaptive.Smoothing != 0.1 {
		t.Errorf("Smoothing = %v, want 0.1", cfg.Adaptive.Smoothing)
	}
	if cfg.Adaptive.ScoreWeights == nil {
		t.Fatal("ScoreWeights is nil")
	}
	if cfg.Adaptive.ScoreWeights.SNR != 0.5 {
		t.Errorf("ScoreWeights.SNR = %v, want 0.5", cfg.Adaptive.ScoreWeights.SNR)
	}
}

func TestExampleDroneConfig(t *testing.T) {
	cfg, err := Load("../../examples/drone.yaml")
	if err != nil {
		t.Fatalf("drone.yaml failed to load: %v", err)
	}

	// Verify drone-specific configuration
	// "default" is the OpenIPC standard domain
	if cfg.Link.Domain != "default" {
		t.Errorf("domain = %q, want default (OpenIPC standard)", cfg.Link.Domain)
	}

	// Video stream should be udp_direct_tx with stream_tx 0
	video, ok := cfg.Streams["video"]
	if !ok {
		t.Fatal("video stream not found")
	}
	if video.ServiceType != "udp_direct_tx" {
		t.Errorf("video.ServiceType = %q, want udp_direct_tx", video.ServiceType)
	}
	if video.StreamTX == nil || *video.StreamTX != 0 {
		t.Errorf("video.StreamTX = %v, want 0", video.StreamTX)
	}
	if video.Peer != "listen://0.0.0.0:5600" {
		t.Errorf("video.Peer = %q, want listen://0.0.0.0:5600", video.Peer)
	}

	// Mavlink stream (optional) should be bidirectional with stream_tx=0x10, stream_rx=0x90
	if mavlink, ok := cfg.Streams["mavlink"]; ok {
		if mavlink.ServiceType != "mavlink" {
			t.Errorf("mavlink.ServiceType = %q, want mavlink", mavlink.ServiceType)
		}
		if mavlink.StreamTX == nil || *mavlink.StreamTX != 0x10 {
			t.Errorf("mavlink.StreamTX = %v, want 0x10 (16)", mavlink.StreamTX)
		}
		if mavlink.StreamRX == nil || *mavlink.StreamRX != 0x90 {
			t.Errorf("mavlink.StreamRX = %v, want 0x90 (144)", mavlink.StreamRX)
		}
	}

	// Adaptive should be drone mode
	if cfg.Adaptive == nil || !cfg.Adaptive.Enabled {
		t.Fatal("adaptive not enabled")
	}
	if cfg.Adaptive.Mode != "drone" {
		t.Errorf("adaptive.Mode = %q, want drone", cfg.Adaptive.Mode)
	}
	if cfg.Adaptive.ListenPort != 9999 {
		t.Errorf("adaptive.ListenPort = %d, want 9999", cfg.Adaptive.ListenPort)
	}
	if len(cfg.Adaptive.Profiles) < 3 {
		t.Errorf("adaptive.Profiles = %d, want at least 3", len(cfg.Adaptive.Profiles))
	}
}

func TestExampleGSConfig(t *testing.T) {
	cfg, err := Load("../../examples/gs.yaml")
	if err != nil {
		t.Fatalf("gs.yaml failed to load: %v", err)
	}

	// Verify GS-specific configuration
	// "default" is the OpenIPC standard domain
	if cfg.Link.Domain != "default" {
		t.Errorf("domain = %q, want default (OpenIPC standard)", cfg.Link.Domain)
	}

	// Should have multiple WLANs for diversity
	if len(cfg.Hardware.WLANs) < 2 {
		t.Errorf("WLANs = %d, want at least 2 for diversity", len(cfg.Hardware.WLANs))
	}

	// Video stream should be udp_direct_rx with stream_rx 0 (matching drone TX)
	video, ok := cfg.Streams["video"]
	if !ok {
		t.Fatal("video stream not found")
	}
	if video.ServiceType != "udp_direct_rx" {
		t.Errorf("video.ServiceType = %q, want udp_direct_rx", video.ServiceType)
	}
	if video.StreamRX == nil || *video.StreamRX != 0 {
		t.Errorf("video.StreamRX = %v, want 0 (must match drone TX)", video.StreamRX)
	}
	if video.Peer != "connect://127.0.0.1:5600" {
		t.Errorf("video.Peer = %q, want connect://127.0.0.1:5600", video.Peer)
	}

	// Mavlink stream (optional) should be bidirectional with stream_rx=0x10, stream_tx=0x90 (swapped from drone)
	if mavlink, ok := cfg.Streams["mavlink"]; ok {
		if mavlink.ServiceType != "mavlink" {
			t.Errorf("mavlink.ServiceType = %q, want mavlink", mavlink.ServiceType)
		}
		if mavlink.StreamRX == nil || *mavlink.StreamRX != 0x10 {
			t.Errorf("mavlink.StreamRX = %v, want 0x10 (16, receives drone's TX)", mavlink.StreamRX)
		}
		if mavlink.StreamTX == nil || *mavlink.StreamTX != 0x90 {
			t.Errorf("mavlink.StreamTX = %v, want 0x90 (144, sends to drone's RX)", mavlink.StreamTX)
		}
	}

	// Adaptive should be GS mode
	if cfg.Adaptive == nil || !cfg.Adaptive.Enabled {
		t.Fatal("adaptive not enabled")
	}
	if cfg.Adaptive.Mode != "gs" {
		t.Errorf("adaptive.Mode = %q, want gs", cfg.Adaptive.Mode)
	}
	if cfg.Adaptive.SendAddr == "" {
		t.Error("adaptive.SendAddr not set (required for GS mode)")
	}

	// GS uses different API ports
	if cfg.API == nil {
		t.Fatal("API not configured")
	}
	if cfg.API.StatsPort != 8003 {
		t.Errorf("API.StatsPort = %d, want 8003 (different from drone)", cfg.API.StatsPort)
	}
}

func TestDroneGSStreamIDMatch(t *testing.T) {
	// Verify that drone and GS configs have matching stream_ids
	drone, err := Load("../../examples/drone.yaml")
	if err != nil {
		t.Fatalf("drone.yaml failed to load: %v", err)
	}
	gs, err := Load("../../examples/gs.yaml")
	if err != nil {
		t.Fatalf("gs.yaml failed to load: %v", err)
	}

	// Video: drone TX stream must match GS RX stream
	droneVideo := drone.Streams["video"]
	gsVideo := gs.Streams["video"]
	if droneVideo.StreamTX == nil || gsVideo.StreamRX == nil {
		t.Fatal("video stream_tx or stream_rx is nil")
	}
	if *droneVideo.StreamTX != *gsVideo.StreamRX {
		t.Errorf("video stream mismatch: drone_tx=%d, gs_rx=%d",
			*droneVideo.StreamTX, *gsVideo.StreamRX)
	}

	// Mavlink (optional): drone TX must match GS RX, drone RX must match GS TX
	droneMav, hasDroneMav := drone.Streams["mavlink"]
	gsMav, hasGSMav := gs.Streams["mavlink"]
	if hasDroneMav && hasGSMav {
		if droneMav.StreamTX == nil || gsMav.StreamRX == nil {
			t.Error("mavlink stream_tx or stream_rx is nil")
		} else if *droneMav.StreamTX != *gsMav.StreamRX {
			t.Errorf("mavlink downlink mismatch: drone_tx=%d, gs_rx=%d",
				*droneMav.StreamTX, *gsMav.StreamRX)
		}
		if droneMav.StreamRX == nil || gsMav.StreamTX == nil {
			t.Error("mavlink stream_rx or stream_tx is nil")
		} else if *droneMav.StreamRX != *gsMav.StreamTX {
			t.Errorf("mavlink uplink mismatch: drone_rx=%d, gs_tx=%d",
				*droneMav.StreamRX, *gsMav.StreamTX)
		}
	}

	// Link domains must match
	if drone.Link.Domain != gs.Link.Domain {
		t.Errorf("link domain mismatch: drone=%q, gs=%q",
			drone.Link.Domain, gs.Link.Domain)
	}

	// Channels must match
	if drone.Hardware.Channel != gs.Hardware.Channel {
		t.Errorf("channel mismatch: drone=%d, gs=%d",
			drone.Hardware.Channel, gs.Hardware.Channel)
	}

	// Bandwidth must match
	if drone.Hardware.Bandwidth != gs.Hardware.Bandwidth {
		t.Errorf("bandwidth mismatch: drone=%d, gs=%d",
			drone.Hardware.Bandwidth, gs.Hardware.Bandwidth)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestKeyBase64(t *testing.T) {
	// Test key_base64 in link config
	keyData := []byte("this-is-a-secret-key-for-testing")
	keyBase64 := "dGhpcy1pcy1hLXNlY3JldC1rZXktZm9yLXRlc3Rpbmc="

	link := &LinkConfig{
		KeyBase64: keyBase64,
	}

	data, err := link.GetKeyData()
	if err != nil {
		t.Fatalf("GetKeyData() error: %v", err)
	}
	if string(data) != string(keyData) {
		t.Errorf("GetKeyData() = %q, want %q", data, keyData)
	}
}

func TestKeyBase64VsFile(t *testing.T) {
	// Test that key_base64 takes precedence over key file
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "key.bin")
	fileContent := []byte("file-key-content")
	if err := os.WriteFile(keyFile, fileContent, 0644); err != nil {
		t.Fatal(err)
	}

	base64Content := "YmFzZTY0LWtleS1jb250ZW50" // "base64-key-content"

	link := &LinkConfig{
		Key:       keyFile,
		KeyBase64: base64Content,
	}

	data, err := link.GetKeyData()
	if err != nil {
		t.Fatalf("GetKeyData() error: %v", err)
	}
	// key_base64 should win
	if string(data) != "base64-key-content" {
		t.Errorf("GetKeyData() = %q, want base64-key-content (key_base64 should take precedence)", data)
	}
}

func TestKeyFile(t *testing.T) {
	// Test key file reading
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "key.bin")
	keyContent := []byte("secret-key-from-file")
	if err := os.WriteFile(keyFile, keyContent, 0644); err != nil {
		t.Fatal(err)
	}

	link := &LinkConfig{
		Key: keyFile,
	}

	data, err := link.GetKeyData()
	if err != nil {
		t.Fatalf("GetKeyData() error: %v", err)
	}
	if string(data) != string(keyContent) {
		t.Errorf("GetKeyData() = %q, want %q", data, keyContent)
	}
}

func TestStreamKeyOverride(t *testing.T) {
	dir := t.TempDir()
	linkKeyFile := filepath.Join(dir, "link.key")
	if err := os.WriteFile(linkKeyFile, []byte("link-key"), 0644); err != nil {
		t.Fatal(err)
	}

	streamKeyBase64 := "c3RyZWFtLWtleQ==" // "stream-key"

	cfg := &Config{
		Link: LinkConfig{
			Key: linkKeyFile,
		},
	}

	// Stream with key_base64 override
	stream := &StreamConfig{
		KeyBase64: streamKeyBase64,
	}

	data, err := cfg.GetStreamKeyData(stream)
	if err != nil {
		t.Fatalf("GetStreamKeyData() error: %v", err)
	}
	if string(data) != "stream-key" {
		t.Errorf("GetStreamKeyData() = %q, want stream-key", data)
	}

	// Stream without override should fall back to link config
	stream2 := &StreamConfig{}
	data2, err := cfg.GetStreamKeyData(stream2)
	if err != nil {
		t.Fatalf("GetStreamKeyData() error: %v", err)
	}
	if string(data2) != "link-key" {
		t.Errorf("GetStreamKeyData() = %q, want link-key", data2)
	}
}

func TestKeyBase64InvalidEncoding(t *testing.T) {
	link := &LinkConfig{
		KeyBase64: "not-valid-base64!!!",
	}

	_, err := link.GetKeyData()
	if err == nil {
		t.Error("expected error for invalid base64, got nil")
	}
}

func TestNoKeySpecified(t *testing.T) {
	link := &LinkConfig{}

	_, err := link.GetKeyData()
	if err == nil {
		t.Error("expected error for no key specified, got nil")
	}
}
