package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"
)

const (
	defaultSSHUser     = "root"
	defaultSSHPassword = "12345"
	defaultSSHPort     = "22"
	sshTimeout         = 10 * time.Second
)

// DroneSSHClient handles SSH-based configuration for legacy wfb-ng drones.
type DroneSSHClient struct {
	host     string
	port     string
	user     string
	password string
}

// NewDroneSSHClient creates a new SSH client for drone configuration.
func NewDroneSSHClient(addr string) *DroneSSHClient {
	host, port := addr, defaultSSHPort
	if h, p, err := net.SplitHostPort(addr); err == nil {
		host = h
		if p != "" {
			port = p
		}
	}
	// If port looks like HTTP port, use SSH port
	if port == "8080" || port == "80" {
		port = defaultSSHPort
	}

	return &DroneSSHClient{
		host:     host,
		port:     port,
		user:     defaultSSHUser,
		password: defaultSSHPassword,
	}
}

// connect establishes an SSH connection to the drone.
func (c *DroneSSHClient) connect() (*ssh.Client, error) {
	config := &ssh.ClientConfig{
		User: c.user,
		Auth: []ssh.AuthMethod{
			ssh.Password(c.password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         sshTimeout,
	}

	addr := net.JoinHostPort(c.host, c.port)
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s: %w", addr, err)
	}
	return client, nil
}

// runCommand executes a command on the drone and returns stdout.
func (c *DroneSSHClient) runCommand(client *ssh.Client, cmd string) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("new session: %w", err)
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	if err := session.Run(cmd); err != nil {
		return "", fmt.Errorf("run %q: %w (stderr: %s)", cmd, err, stderr.String())
	}
	return stdout.String(), nil
}

// WfbConfig represents the wfb-ng YAML configuration structure.
type WfbConfig struct {
	Wireless  WfbWireless  `yaml:"wireless" json:"wireless"`
	Broadcast WfbBroadcast `yaml:"broadcast" json:"broadcast"`
	Telemetry WfbTelemetry `yaml:"telemetry,omitempty" json:"telemetry,omitempty"`
	Link      WfbLink      `yaml:"link,omitempty" json:"link,omitempty"`
}

type WfbWireless struct {
	Channel int    `yaml:"channel" json:"channel"`
	Width   int    `yaml:"width" json:"bandwidth"`
	TXPower int    `yaml:"txpower" json:"tx_power"`
	Region  string `yaml:"region,omitempty" json:"region,omitempty"`
	Regdom  string `yaml:"regdom,omitempty" json:"-"` // Alternative name for region
}

type WfbBroadcast struct {
	MCSIndex int  `yaml:"mcs_index" json:"mcs"`
	STBC     bool `yaml:"stbc" json:"stbc"`
	LDPC     bool `yaml:"ldpc" json:"ldpc"`
	ShortGI  bool `yaml:"short_gi" json:"short_gi"`
	FecK     int  `yaml:"fec_k" json:"fec_k"`
	FecN     int  `yaml:"fec_n" json:"fec_n"`
	LinkID   int  `yaml:"link_id" json:"link_id"`
}

type WfbTelemetry struct {
	Serial string `yaml:"serial,omitempty" json:"serial,omitempty"`
	Router string `yaml:"router,omitempty" json:"router,omitempty"`
}

type WfbLink struct {
	Domain string `yaml:"link_domain,omitempty" json:"domain,omitempty"`
	Key    string `yaml:"key,omitempty" json:"key,omitempty"`
}

// DroneConfigSSH represents the drone configuration in a format compatible with our UI.
type DroneConfigSSH struct {
	// Hardware settings
	Hardware struct {
		Channel   int    `json:"channel"`
		Bandwidth int    `json:"bandwidth"`
		TXPower   *int   `json:"tx_power,omitempty"`
		Region    string `json:"region,omitempty"`
	} `json:"hardware"`

	// Link settings
	Link *DroneLinkConfig `json:"link,omitempty"`

	// Stream settings (from broadcast section)
	Streams map[string]DroneStreamConfigSSH `json:"streams,omitempty"`

	// Camera settings (from majestic.yaml)
	Camera *DroneCameraConfig `json:"camera,omitempty"`

	// Adaptive link settings (from alink.conf)
	Adaptive *DroneAdaptiveConfig `json:"adaptive,omitempty"`

	// Raw wfb.yaml for reference
	WfbRaw *WfbConfig `json:"_wfb_raw,omitempty"`
}

// DroneLinkConfig holds link/encryption settings.
type DroneLinkConfig struct {
	Domain    string `json:"domain,omitempty"`
	ID        int    `json:"id,omitempty"`         // Link ID from broadcast.link_id
	KeyBase64 string `json:"key_base64,omitempty"` // Key in base64 format
}

// DroneCameraConfig holds camera (majestic) settings.
type DroneCameraConfig struct {
	Bitrate  int     `json:"bitrate,omitempty"`
	GOP      float64 `json:"gop,omitempty"` // in seconds (e.g., 0.5, 1, 2)
	FPS      int     `json:"fps,omitempty"`
	Codec    string  `json:"codec,omitempty"`
	Size     string  `json:"size,omitempty"`
	RCMode   string  `json:"rc_mode,omitempty"`
	QPDelta  int     `json:"qp_delta,omitempty"`
	Mirror   bool    `json:"mirror"`
	Flip     bool    `json:"flip"`
}

// DroneAdaptiveConfig holds adaptive link settings.
// Field names and units match the GS adaptive config for UI compatibility.
type DroneAdaptiveConfig struct {
	Enabled    bool `json:"enabled"`
	ListenPort int  `json:"listen_port,omitempty"` // Default 9999
	// Timing
	FallbackTimeout   int64 `json:"fallback_timeout,omitempty"`    // nanoseconds (from fallback_ms)
	FallbackHold      int64 `json:"fallback_hold,omitempty"`       // nanoseconds (from hold_fallback_mode_s)
	HoldUp            int64 `json:"hold_up,omitempty"`             // nanoseconds (from hold_modes_down_s)
	MinBetweenChanges int64 `json:"min_between_changes,omitempty"` // nanoseconds (from min_between_changes_ms)
	// Smoothing/Hysteresis
	Hysteresis     float64 `json:"hysteresis,omitempty"`      // from hysteresis_percent
	HysteresisDown float64 `json:"hysteresis_down,omitempty"` // from hysteresis_percent_down
	Smoothing      float64 `json:"smoothing,omitempty"`       // from exp_smoothing_factor
	SmoothingDown  float64 `json:"smoothing_down,omitempty"`  // from exp_smoothing_factor_down
	// Keyframe control
	AllowKeyframe    bool  `json:"allow_keyframe"`              // from allow_request_keyframe
	KeyframeInterval int64 `json:"keyframe_interval,omitempty"` // nanoseconds (from request_keyframe_interval_ms)
	IDROnChange      bool  `json:"idr_on_change"`               // from idr_every_change
	// TX drop recovery
	TXDropKeyframe      bool    `json:"tx_drop_keyframe"`                 // from allow_rq_kf_by_tx_d
	TXDropReduceBitrate bool    `json:"tx_drop_reduce_bitrate"`           // from allow_xtx_reduce_bitrate
	TXDropCheckInterval int64   `json:"tx_drop_check_interval,omitempty"` // nanoseconds (from check_xtx_period_ms)
	TXDropBitrateFactor float64 `json:"tx_drop_bitrate_factor,omitempty"` // from xtx_reduce_bitrate_factor
	// Dynamic FEC
	DynamicFEC    bool `json:"dynamic_fec"`     // from allow_dynamic_fec
	FECKAdjust    bool `json:"fec_k_adjust"`    // from fec_k_adjust
	SpikeFix      bool `json:"spike_fix"`       // from spike_fix_dynamic_fec - disable dynamic FEC at low bitrate
	AllowSpikeFPS bool `json:"allow_spike_fps"` // from allow_spike_fix_fps - allow FPS reduction on spikes
	// Power control
	AllowSetPower    bool `json:"allow_set_power"`     // from allow_set_power - enable TX power control
	Use04TXPower     bool `json:"use_04_txpower"`      // from use_0_to_4_txpower - use card power tables
	PowerLevel04     *int `json:"power_level_04"`      // from power_level_0_to_4 - power level 0-4 scale
	// Video quality
	ROIFocusMode bool `json:"roi_focus_mode"` // from roi_focus_mode - higher quality in center
	OSDLevel     *int `json:"osd_level"`      // from osd_level - OSD verbosity 0-6
	// Profiles loaded from /etc/txprofiles.conf
	Profiles []DroneAdaptiveProfile `json:"profiles,omitempty"`
}

// DroneAdaptiveProfile represents a TX profile for adaptive link.
type DroneAdaptiveProfile struct {
	Range     [2]int  `json:"range"`
	MCS       int     `json:"mcs"`
	FEC       [2]int  `json:"fec"`
	Bitrate   int     `json:"bitrate"`
	GOP       float64 `json:"gop"`
	Power     int     `json:"power"`
	Bandwidth int     `json:"bandwidth"`
	ShortGI   bool    `json:"short_gi"`
	QPDelta   int     `json:"qp_delta"`
}

type DroneStreamConfigSSH struct {
	ServiceType string `json:"service_type"`
	StreamTX    *int   `json:"stream_tx,omitempty"`
	MCS         int    `json:"mcs"`
	STBC        int    `json:"stbc"`
	LDPC        int    `json:"ldpc"`
	ShortGI     bool   `json:"short_gi"`
	FEC         []int  `json:"fec,omitempty"`
}

// GetConfig retrieves the drone configuration via SSH.
// All config files are read in a single SSH call for performance.
func (c *DroneSSHClient) GetConfig() (*DroneConfigSSH, error) {
	client, err := c.connect()
	if err != nil {
		return nil, err
	}
	defer client.Close()

	// Read all config files in a single SSH command for performance
	combinedCmd := `echo '===WFB===' && (cat /etc/wfb.yaml 2>/dev/null || cat /etc/wifibroadcast.cfg 2>/dev/null || echo '{}') && ` +
		`echo '===MAJESTIC===' && (cat /etc/majestic.yaml 2>/dev/null || echo '{}') && ` +
		`echo '===ALINK===' && (cat /etc/alink.conf 2>/dev/null || echo '') && ` +
		`echo '===ALINK_STATUS===' && (pgrep -x alink_drone >/dev/null && echo 1 || echo 0) && ` +
		`echo '===PROFILES===' && (cat /etc/txprofiles.conf 2>/dev/null || echo '')`

	output, err := c.runCommand(client, combinedCmd)
	if err != nil {
		return nil, fmt.Errorf("read drone config: %w", err)
	}

	// Parse the combined output
	sections := c.parseCombinedOutput(output)

	// Parse wfb.yaml
	var wfbCfg WfbConfig
	if wfbYaml := sections["WFB"]; wfbYaml != "" {
		if err := yaml.Unmarshal([]byte(wfbYaml), &wfbCfg); err != nil {
			log.Printf("[drone-ssh] Warning: failed to parse wfb.yaml: %v", err)
		}
	}

	// Build our config format
	cfg := &DroneConfigSSH{}
	cfg.Hardware.Channel = wfbCfg.Wireless.Channel
	cfg.Hardware.Bandwidth = wfbCfg.Wireless.Width
	if wfbCfg.Wireless.TXPower != 0 {
		txp := wfbCfg.Wireless.TXPower
		cfg.Hardware.TXPower = &txp
	}
	// Region can be stored as "region" or "regdom" in wfb.yaml
	cfg.Hardware.Region = wfbCfg.Wireless.Region
	if cfg.Hardware.Region == "" {
		cfg.Hardware.Region = wfbCfg.Wireless.Regdom
	}

	// Link settings (from link section and broadcast.link_id)
	cfg.Link = &DroneLinkConfig{
		Domain:    wfbCfg.Link.Domain,
		ID:        wfbCfg.Broadcast.LinkID,
		KeyBase64: wfbCfg.Link.Key,
	}

	// Create a "video" stream with broadcast settings
	streamTX := 0
	cfg.Streams = map[string]DroneStreamConfigSSH{
		"video": {
			ServiceType: "udp_direct_tx",
			StreamTX:    &streamTX,
			MCS:         wfbCfg.Broadcast.MCSIndex,
			STBC:        boolToInt(wfbCfg.Broadcast.STBC),
			LDPC:        boolToInt(wfbCfg.Broadcast.LDPC),
			ShortGI:     wfbCfg.Broadcast.ShortGI,
			FEC:         []int{wfbCfg.Broadcast.FecK, wfbCfg.Broadcast.FecN},
		},
	}

	cfg.WfbRaw = &wfbCfg

	// Parse majestic.yaml for camera settings
	cfg.Camera = c.parseCameraConfig(sections["MAJESTIC"])

	// Parse alink.conf for adaptive settings
	cfg.Adaptive = c.parseAdaptiveConfig(sections["ALINK"], sections["ALINK_STATUS"], sections["PROFILES"])

	return cfg, nil
}

// parseCombinedOutput splits the combined SSH output into sections by delimiter.
func (c *DroneSSHClient) parseCombinedOutput(output string) map[string]string {
	sections := make(map[string]string)
	lines := strings.Split(output, "\n")

	var currentSection string
	var currentContent strings.Builder

	for _, line := range lines {
		if strings.HasPrefix(line, "===") && strings.HasSuffix(line, "===") {
			// Save previous section
			if currentSection != "" {
				sections[currentSection] = strings.TrimSpace(currentContent.String())
			}
			// Start new section
			currentSection = strings.Trim(line, "=")
			currentContent.Reset()
		} else if currentSection != "" {
			currentContent.WriteString(line)
			currentContent.WriteString("\n")
		}
	}
	// Save last section
	if currentSection != "" {
		sections[currentSection] = strings.TrimSpace(currentContent.String())
	}

	return sections
}

// parseCameraConfig parses camera settings from majestic.yaml content.
func (c *DroneSSHClient) parseCameraConfig(majesticYaml string) *DroneCameraConfig {
	if majesticYaml == "" || majesticYaml == "{}" {
		return nil
	}

	var majestic map[string]interface{}
	if err := yaml.Unmarshal([]byte(majesticYaml), &majestic); err != nil {
		log.Printf("[drone-ssh] Warning: failed to parse majestic.yaml: %v", err)
		return nil
	}

	cam := &DroneCameraConfig{}

	// Extract video0 settings
	if video0, ok := majestic["video0"].(map[string]interface{}); ok {
		if v, ok := video0["bitrate"].(int); ok {
			cam.Bitrate = v
		} else if v, ok := video0["bitrate"].(float64); ok {
			cam.Bitrate = int(v)
		}
		if v, ok := video0["gopSize"].(float64); ok {
			cam.GOP = v
		} else if v, ok := video0["gopSize"].(int); ok {
			cam.GOP = float64(v)
		}
		if v, ok := video0["fps"].(int); ok {
			cam.FPS = v
		} else if v, ok := video0["fps"].(float64); ok {
			cam.FPS = int(v)
		}
		if v, ok := video0["codec"].(string); ok {
			cam.Codec = v
		}
		if v, ok := video0["size"].(string); ok {
			cam.Size = v
		}
		if v, ok := video0["rcMode"].(string); ok {
			cam.RCMode = v
		}
		if v, ok := video0["qpDelta"].(int); ok {
			cam.QPDelta = v
		} else if v, ok := video0["qpDelta"].(float64); ok {
			cam.QPDelta = int(v)
		}
	}

	// Extract image settings (handle bool, string "true"/"false", or int 0/1)
	if image, ok := majestic["image"].(map[string]interface{}); ok {
		cam.Mirror = parseBoolValue(image["mirror"])
		cam.Flip = parseBoolValue(image["flip"])
	}

	return cam
}

// parseAdaptiveConfig parses adaptive link settings from alink.conf content.
func (c *DroneSSHClient) parseAdaptiveConfig(alinkConf, alinkStatus, profilesConf string) *DroneAdaptiveConfig {
	if strings.TrimSpace(alinkConf) == "" {
		return nil
	}

	adaptive := &DroneAdaptiveConfig{
		ListenPort: 9999, // Default listen port
	}

	// Parse key=value format
	lines := strings.Split(alinkConf, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		var intVal int
		var floatVal float64

		switch key {
		// Timing
		case "fallback_ms":
			fmt.Sscanf(value, "%d", &intVal)
			adaptive.FallbackTimeout = int64(intVal) * 1e6 // ms to ns
		case "hold_fallback_mode_s":
			fmt.Sscanf(value, "%d", &intVal)
			adaptive.FallbackHold = int64(intVal) * 1e9 // s to ns
		case "hold_modes_down_s":
			fmt.Sscanf(value, "%d", &intVal)
			adaptive.HoldUp = int64(intVal) * 1e9 // s to ns
		case "min_between_changes_ms":
			fmt.Sscanf(value, "%d", &intVal)
			adaptive.MinBetweenChanges = int64(intVal) * 1e6 // ms to ns
		// Smoothing/Hysteresis
		case "hysteresis_percent":
			fmt.Sscanf(value, "%f", &floatVal)
			adaptive.Hysteresis = floatVal
		case "hysteresis_percent_down":
			fmt.Sscanf(value, "%f", &floatVal)
			adaptive.HysteresisDown = floatVal
		case "exp_smoothing_factor":
			fmt.Sscanf(value, "%f", &floatVal)
			adaptive.Smoothing = floatVal
		case "exp_smoothing_factor_down":
			fmt.Sscanf(value, "%f", &floatVal)
			adaptive.SmoothingDown = floatVal
		// Keyframe control
		case "allow_request_keyframe":
			adaptive.AllowKeyframe = value == "1"
		case "request_keyframe_interval_ms":
			fmt.Sscanf(value, "%d", &intVal)
			adaptive.KeyframeInterval = int64(intVal) * 1e6 // ms to ns
		case "idr_every_change":
			adaptive.IDROnChange = value == "1"
		// TX drop recovery
		case "allow_rq_kf_by_tx_d":
			adaptive.TXDropKeyframe = value == "1"
		case "allow_xtx_reduce_bitrate":
			adaptive.TXDropReduceBitrate = value == "1"
		case "check_xtx_period_ms":
			fmt.Sscanf(value, "%d", &intVal)
			adaptive.TXDropCheckInterval = int64(intVal) * 1e6 // ms to ns
		case "xtx_reduce_bitrate_factor":
			fmt.Sscanf(value, "%f", &floatVal)
			adaptive.TXDropBitrateFactor = floatVal
		// Dynamic FEC
		case "allow_dynamic_fec":
			adaptive.DynamicFEC = value == "1"
		case "fec_k_adjust":
			adaptive.FECKAdjust = value == "1"
		case "spike_fix_dynamic_fec":
			adaptive.SpikeFix = value == "1"
		case "allow_spike_fix_fps":
			adaptive.AllowSpikeFPS = value == "1"
		// Power control
		case "allow_set_power":
			adaptive.AllowSetPower = value == "1"
		case "use_0_to_4_txpower":
			adaptive.Use04TXPower = value == "1"
		case "power_level_0_to_4":
			fmt.Sscanf(value, "%d", &intVal)
			adaptive.PowerLevel04 = &intVal
		// Video quality
		case "roi_focus_mode":
			adaptive.ROIFocusMode = value == "1"
		case "osd_level":
			fmt.Sscanf(value, "%d", &intVal)
			adaptive.OSDLevel = &intVal
		}
	}

	// Check if adaptive link is enabled (alink_drone running)
	adaptive.Enabled = strings.TrimSpace(alinkStatus) == "1"

	// Parse profiles from txprofiles.conf content
	adaptive.Profiles = c.parseTXProfiles(profilesConf)

	return adaptive
}

// parseTXProfiles parses TX profiles from txprofiles.conf content.
func (c *DroneSSHClient) parseTXProfiles(profilesConf string) []DroneAdaptiveProfile {
	if strings.TrimSpace(profilesConf) == "" {
		return nil
	}

	var profiles []DroneAdaptiveProfile
	lines := strings.Split(profilesConf, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Format: <rangeMin> - <rangeMax> <gi> <mcs> <fecK> <fecN> <bitrate> <gop> <power> <roiQP> <bandwidth> <qpDelta>
		// Example: 999  -  999  long 0 2 3    1000 10 30   0,0,0,0 20 -12
		fields := strings.Fields(line)
		if len(fields) < 12 {
			continue
		}

		var profile DroneAdaptiveProfile
		var rangeMin, rangeMax int
		var gi string
		var mcs, fecK, fecN, bitrate, power, bandwidth, qpDelta int
		var gop float64

		fmt.Sscanf(fields[0], "%d", &rangeMin)
		// fields[1] is "-"
		fmt.Sscanf(fields[2], "%d", &rangeMax)
		gi = fields[3]
		fmt.Sscanf(fields[4], "%d", &mcs)
		fmt.Sscanf(fields[5], "%d", &fecK)
		fmt.Sscanf(fields[6], "%d", &fecN)
		fmt.Sscanf(fields[7], "%d", &bitrate)
		fmt.Sscanf(fields[8], "%f", &gop)
		fmt.Sscanf(fields[9], "%d", &power)
		// fields[10] is roiQP, skip for now
		fmt.Sscanf(fields[11], "%d", &bandwidth)
		if len(fields) > 12 {
			fmt.Sscanf(fields[12], "%d", &qpDelta)
		}

		profile.Range = [2]int{rangeMin, rangeMax}
		profile.ShortGI = gi == "short"
		profile.MCS = mcs
		profile.FEC = [2]int{fecK, fecN}
		profile.Bitrate = bitrate
		profile.GOP = gop
		profile.Power = power
		profile.Bandwidth = bandwidth
		profile.QPDelta = qpDelta

		profiles = append(profiles, profile)
	}

	return profiles
}

// SetConfig applies configuration changes to the drone via SSH.
func (c *DroneSSHClient) SetConfig(changes map[string]interface{}) error {
	client, err := c.connect()
	if err != nil {
		return err
	}
	defer client.Close()

	var wfbCommands []string
	var cameraCommands []string
	var alinkCommands []string
	restartWfb := false
	restartMajestic := false
	restartAlink := false

	// Process hardware changes
	if hw, ok := changes["hardware"].(map[string]interface{}); ok {
		if ch, ok := hw["channel"]; ok {
			wfbCommands = append(wfbCommands, fmt.Sprintf("wifibroadcast cli -s .wireless.channel %v", ch))
			restartWfb = true
		}
		if bw, ok := hw["bandwidth"]; ok {
			wfbCommands = append(wfbCommands, fmt.Sprintf("wifibroadcast cli -s .wireless.width %v", bw))
			restartWfb = true
		}
		if txp, ok := hw["tx_power"]; ok {
			wfbCommands = append(wfbCommands, fmt.Sprintf("wifibroadcast cli -s .wireless.txpower %v", txp))
			restartWfb = true
		}
		if region, ok := hw["region"].(string); ok && region != "" {
			wfbCommands = append(wfbCommands, fmt.Sprintf("wifibroadcast cli -s .wireless.region %v", region))
			restartWfb = true
		}
	}

	// Process link changes
	if link, ok := changes["link"].(map[string]interface{}); ok {
		if domain, ok := link["domain"].(string); ok {
			wfbCommands = append(wfbCommands, fmt.Sprintf("wifibroadcast cli -s .link.link_domain %q", domain))
			restartWfb = true
		}
		if id, ok := link["id"]; ok {
			wfbCommands = append(wfbCommands, fmt.Sprintf("wifibroadcast cli -s .broadcast.link_id %v", toInt(id)))
			restartWfb = true
		}
		// Note: key_base64 not supported via SSH - would need to write file on drone
	}

	// Process stream changes (apply to broadcast section)
	if streams, ok := changes["streams"].(map[string]interface{}); ok {
		for _, streamData := range streams {
			if stream, ok := streamData.(map[string]interface{}); ok {
				if mcs, ok := stream["mcs"]; ok {
					wfbCommands = append(wfbCommands, fmt.Sprintf("wifibroadcast cli -s .broadcast.mcs_index %v", mcs))
					restartWfb = true
				}
				if stbc, ok := stream["stbc"]; ok {
					wfbCommands = append(wfbCommands, fmt.Sprintf("wifibroadcast cli -s .broadcast.stbc %v", intToBoolStr(stbc)))
					restartWfb = true
				}
				if ldpc, ok := stream["ldpc"]; ok {
					wfbCommands = append(wfbCommands, fmt.Sprintf("wifibroadcast cli -s .broadcast.ldpc %v", intToBoolStr(ldpc)))
					restartWfb = true
				}
				if sgi, ok := stream["short_gi"]; ok {
					wfbCommands = append(wfbCommands, fmt.Sprintf("wifibroadcast cli -s .broadcast.short_gi %v", intToBoolStr(sgi)))
					restartWfb = true
				}
				if fec, ok := stream["fec"].([]interface{}); ok && len(fec) >= 2 {
					wfbCommands = append(wfbCommands, fmt.Sprintf("wifibroadcast cli -s .broadcast.fec_k %v", fec[0]))
					wfbCommands = append(wfbCommands, fmt.Sprintf("wifibroadcast cli -s .broadcast.fec_n %v", fec[1]))
					restartWfb = true
				}
			}
		}
	}

	// Process camera changes (majestic.yaml)
	if cam, ok := changes["camera"].(map[string]interface{}); ok {
		if v, ok := cam["bitrate"]; ok {
			cameraCommands = append(cameraCommands, fmt.Sprintf("cli -s .video0.bitrate %v", v))
			restartMajestic = true
		}
		if v, ok := cam["gop"]; ok {
			cameraCommands = append(cameraCommands, fmt.Sprintf("cli -s .video0.gopSize %v", v))
			restartMajestic = true
		}
		if v, ok := cam["fps"]; ok {
			cameraCommands = append(cameraCommands, fmt.Sprintf("cli -s .video0.fps %v", v))
			restartMajestic = true
		}
		if v, ok := cam["codec"]; ok {
			cameraCommands = append(cameraCommands, fmt.Sprintf("cli -s .video0.codec %v", v))
			restartMajestic = true
		}
		if v, ok := cam["size"]; ok {
			cameraCommands = append(cameraCommands, fmt.Sprintf("cli -s .video0.size %v", v))
			restartMajestic = true
		}
		if v, ok := cam["rc_mode"]; ok {
			cameraCommands = append(cameraCommands, fmt.Sprintf("cli -s .video0.rcMode %v", v))
			restartMajestic = true
		}
		if v, ok := cam["qp_delta"]; ok {
			cameraCommands = append(cameraCommands, fmt.Sprintf("cli -s .video0.qpDelta %v", v))
			restartMajestic = true
		}
		if v, ok := cam["mirror"]; ok {
			cameraCommands = append(cameraCommands, fmt.Sprintf("cli -s .image.mirror %v", v))
			restartMajestic = true
		}
		if v, ok := cam["flip"]; ok {
			cameraCommands = append(cameraCommands, fmt.Sprintf("cli -s .image.flip %v", v))
			restartMajestic = true
		}
	}

	// Process adaptive link changes (alink.conf)
	// UI sends values in nanoseconds, convert to ms/s for alink.conf
	if al, ok := changes["adaptive"].(map[string]interface{}); ok {
		if v, ok := al["fallback_timeout"]; ok {
			ms := nsToMs(v)
			alinkCommands = append(alinkCommands, fmt.Sprintf("sed -i 's/fallback_ms=.*/fallback_ms=%d/' /etc/alink.conf", ms))
			restartAlink = true
		}
		if v, ok := al["fallback_hold"]; ok {
			s := nsToS(v)
			alinkCommands = append(alinkCommands, fmt.Sprintf("sed -i 's/hold_fallback_mode_s=.*/hold_fallback_mode_s=%d/' /etc/alink.conf", s))
			restartAlink = true
		}
		if v, ok := al["hold_up"]; ok {
			s := nsToS(v)
			alinkCommands = append(alinkCommands, fmt.Sprintf("sed -i 's/hold_modes_down_s=.*/hold_modes_down_s=%d/' /etc/alink.conf", s))
			restartAlink = true
		}
		if v, ok := al["min_between_changes"]; ok {
			ms := nsToMs(v)
			alinkCommands = append(alinkCommands, fmt.Sprintf("sed -i 's/min_between_changes_ms=.*/min_between_changes_ms=%d/' /etc/alink.conf", ms))
			restartAlink = true
		}
		if v, ok := al["hysteresis"]; ok {
			alinkCommands = append(alinkCommands, fmt.Sprintf("sed -i 's/hysteresis_percent=.*/hysteresis_percent=%v/' /etc/alink.conf", v))
			restartAlink = true
		}
		if v, ok := al["hysteresis_down"]; ok {
			alinkCommands = append(alinkCommands, fmt.Sprintf("sed -i 's/hysteresis_percent_down=.*/hysteresis_percent_down=%v/' /etc/alink.conf", v))
			restartAlink = true
		}
		if v, ok := al["smoothing"]; ok {
			alinkCommands = append(alinkCommands, fmt.Sprintf("sed -i 's/exp_smoothing_factor=.*/exp_smoothing_factor=%v/' /etc/alink.conf", v))
			restartAlink = true
		}
		if v, ok := al["smoothing_down"]; ok {
			alinkCommands = append(alinkCommands, fmt.Sprintf("sed -i 's/exp_smoothing_factor_down=.*/exp_smoothing_factor_down=%v/' /etc/alink.conf", v))
			restartAlink = true
		}
		// Keyframe control
		if v, ok := al["allow_keyframe"]; ok {
			alinkCommands = append(alinkCommands, fmt.Sprintf("sed -i 's/allow_request_keyframe=.*/allow_request_keyframe=%s/' /etc/alink.conf", boolToStr(v)))
			restartAlink = true
		}
		if v, ok := al["keyframe_interval"]; ok {
			ms := nsToMs(v)
			alinkCommands = append(alinkCommands, fmt.Sprintf("sed -i 's/request_keyframe_interval_ms=.*/request_keyframe_interval_ms=%d/' /etc/alink.conf", ms))
			restartAlink = true
		}
		if v, ok := al["idr_on_change"]; ok {
			alinkCommands = append(alinkCommands, fmt.Sprintf("sed -i 's/idr_every_change=.*/idr_every_change=%s/' /etc/alink.conf", boolToStr(v)))
			restartAlink = true
		}
		// TX drop recovery
		if v, ok := al["tx_drop_keyframe"]; ok {
			alinkCommands = append(alinkCommands, fmt.Sprintf("sed -i 's/allow_rq_kf_by_tx_d=.*/allow_rq_kf_by_tx_d=%s/' /etc/alink.conf", boolToStr(v)))
			restartAlink = true
		}
		if v, ok := al["tx_drop_reduce_bitrate"]; ok {
			alinkCommands = append(alinkCommands, fmt.Sprintf("sed -i 's/allow_xtx_reduce_bitrate=.*/allow_xtx_reduce_bitrate=%s/' /etc/alink.conf", boolToStr(v)))
			restartAlink = true
		}
		if v, ok := al["tx_drop_check_interval"]; ok {
			ms := nsToMs(v)
			alinkCommands = append(alinkCommands, fmt.Sprintf("sed -i 's/check_xtx_period_ms=.*/check_xtx_period_ms=%d/' /etc/alink.conf", ms))
			restartAlink = true
		}
		if v, ok := al["tx_drop_bitrate_factor"]; ok {
			alinkCommands = append(alinkCommands, fmt.Sprintf("sed -i 's/xtx_reduce_bitrate_factor=.*/xtx_reduce_bitrate_factor=%v/' /etc/alink.conf", v))
			restartAlink = true
		}
		// Dynamic FEC
		if v, ok := al["dynamic_fec"]; ok {
			alinkCommands = append(alinkCommands, fmt.Sprintf("sed -i 's/allow_dynamic_fec=.*/allow_dynamic_fec=%s/' /etc/alink.conf", boolToStr(v)))
			restartAlink = true
		}
		if v, ok := al["fec_k_adjust"]; ok {
			alinkCommands = append(alinkCommands, fmt.Sprintf("sed -i 's/fec_k_adjust=.*/fec_k_adjust=%s/' /etc/alink.conf", boolToStr(v)))
			restartAlink = true
		}
		if v, ok := al["spike_fix"]; ok {
			alinkCommands = append(alinkCommands, fmt.Sprintf("sed -i 's/spike_fix_dynamic_fec=.*/spike_fix_dynamic_fec=%s/' /etc/alink.conf", boolToStr(v)))
			restartAlink = true
		}
		if v, ok := al["allow_spike_fps"]; ok {
			alinkCommands = append(alinkCommands, fmt.Sprintf("sed -i 's/allow_spike_fix_fps=.*/allow_spike_fix_fps=%s/' /etc/alink.conf", boolToStr(v)))
			restartAlink = true
		}
		// Power control
		if v, ok := al["allow_set_power"]; ok {
			alinkCommands = append(alinkCommands, fmt.Sprintf("sed -i 's/allow_set_power=.*/allow_set_power=%s/' /etc/alink.conf", boolToStr(v)))
			restartAlink = true
		}
		if v, ok := al["use_04_txpower"]; ok {
			alinkCommands = append(alinkCommands, fmt.Sprintf("sed -i 's/use_0_to_4_txpower=.*/use_0_to_4_txpower=%s/' /etc/alink.conf", boolToStr(v)))
			restartAlink = true
		}
		if v, ok := al["power_level_04"]; ok {
			alinkCommands = append(alinkCommands, fmt.Sprintf("sed -i 's/power_level_0_to_4=.*/power_level_0_to_4=%v/' /etc/alink.conf", toInt(v)))
			restartAlink = true
		}
		// Video quality
		if v, ok := al["roi_focus_mode"]; ok {
			alinkCommands = append(alinkCommands, fmt.Sprintf("sed -i 's/roi_focus_mode=.*/roi_focus_mode=%s/' /etc/alink.conf", boolToStr(v)))
			restartAlink = true
		}
		if v, ok := al["osd_level"]; ok {
			alinkCommands = append(alinkCommands, fmt.Sprintf("sed -i 's/osd_level=.*/osd_level=%v/' /etc/alink.conf", toInt(v)))
			restartAlink = true
		}
		// Profiles
		if profiles, ok := al["profiles"].([]interface{}); ok && len(profiles) > 0 {
			profilesContent := c.formatTXProfiles(profiles)
			if profilesContent != "" {
				// Write profiles to /etc/txprofiles.conf
				alinkCommands = append(alinkCommands, fmt.Sprintf("cat > /etc/txprofiles.conf << 'EOF'\n%sEOF", profilesContent))
				restartAlink = true
			}
		}
	}

	// Execute WFB commands in a single SSH call
	if len(wfbCommands) > 0 {
		combined := strings.Join(wfbCommands, " && ")
		log.Printf("[drone-ssh] Running WFB commands: %s", combined)
		if _, err := c.runCommand(client, combined); err != nil {
			log.Printf("[drone-ssh] Warning: WFB commands failed: %v", err)
		}
	}

	// Execute camera commands in a single SSH call
	if len(cameraCommands) > 0 {
		combined := strings.Join(cameraCommands, " && ")
		log.Printf("[drone-ssh] Running camera commands: %s", combined)
		if _, err := c.runCommand(client, combined); err != nil {
			log.Printf("[drone-ssh] Warning: camera commands failed: %v", err)
		}
	}

	// Execute adaptive link commands in a single SSH call
	if len(alinkCommands) > 0 {
		combined := strings.Join(alinkCommands, " && ")
		log.Printf("[drone-ssh] Running alink commands: %s", combined)
		if _, err := c.runCommand(client, combined); err != nil {
			log.Printf("[drone-ssh] Warning: alink commands failed: %v", err)
		}
	}

	// Restart services as needed
	// Use nohup/setsid to ensure processes survive SSH session close
	if restartWfb {
		log.Printf("[drone-ssh] Restarting wfb-ng...")
		c.runCommand(client, "nohup sh -c 'wifibroadcast stop; sleep 1; wifibroadcast start' > /dev/null 2>&1 &")
	}
	if restartMajestic {
		log.Printf("[drone-ssh] Reloading majestic...")
		c.runCommand(client, "killall -1 majestic")
	}
	if restartAlink {
		log.Printf("[drone-ssh] Restarting alink_drone...")
		c.runCommand(client, "killall -9 alink_drone 2>/dev/null; sleep 0.5; nohup alink_drone > /dev/null 2>&1 &")
	}

	return nil
}

// GetConfigJSON returns the config as JSON bytes.
func (c *DroneSSHClient) GetConfigJSON() ([]byte, error) {
	cfg, err := c.GetConfig()
	if err != nil {
		return nil, err
	}
	return json.Marshal(cfg)
}

// SetConfigJSON applies config changes from JSON.
func (c *DroneSSHClient) SetConfigJSON(data []byte) error {
	var changes map[string]interface{}
	if err := json.Unmarshal(data, &changes); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	return c.SetConfig(changes)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// nsToMs converts nanoseconds (as interface{}) to milliseconds.
func nsToMs(v interface{}) int64 {
	switch val := v.(type) {
	case float64:
		return int64(val / 1e6)
	case int64:
		return val / 1e6
	case int:
		return int64(val) / 1e6
	default:
		return 0
	}
}

// nsToS converts nanoseconds (as interface{}) to seconds.
func nsToS(v interface{}) int64 {
	switch val := v.(type) {
	case float64:
		return int64(val / 1e9)
	case int64:
		return val / 1e9
	case int:
		return int64(val) / 1e9
	default:
		return 0
	}
}

func intToBoolStr(v interface{}) string {
	switch val := v.(type) {
	case bool:
		return fmt.Sprintf("%v", val)
	case int:
		return fmt.Sprintf("%v", val != 0)
	case float64:
		return fmt.Sprintf("%v", val != 0)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// parseYAMLPath extracts a value from YAML using a dot-separated path.
func parseYAMLPath(data map[string]interface{}, path string) interface{} {
	parts := strings.Split(strings.TrimPrefix(path, "."), ".")
	current := data
	for i, part := range parts {
		if i == len(parts)-1 {
			return current[part]
		}
		if next, ok := current[part].(map[string]interface{}); ok {
			current = next
		} else {
			return nil
		}
	}
	return nil
}

// boolToStr converts a bool-like interface value to "1" or "0".
func boolToStr(v interface{}) string {
	switch val := v.(type) {
	case bool:
		if val {
			return "1"
		}
		return "0"
	case int:
		if val != 0 {
			return "1"
		}
		return "0"
	case float64:
		if val != 0 {
			return "1"
		}
		return "0"
	default:
		return "0"
	}
}

// formatTXProfiles formats profiles for /etc/txprofiles.conf.
// Format: <rangeMin> - <rangeMax> <gi> <mcs> <fecK> <fecN> <bitrate> <gop> <power> <roiQP> <bandwidth> <qpDelta>
func (c *DroneSSHClient) formatTXProfiles(profiles []interface{}) string {
	var lines []string
	lines = append(lines, "# TX profiles for adaptive link")
	lines = append(lines, "# <ra - nge> <gi> <mcs> <fecK> <fecN> <bitrate> <gop> <Pwr> <roiQP> <bandwidth> <qpDelta>")

	for _, p := range profiles {
		profile, ok := p.(map[string]interface{})
		if !ok {
			continue
		}

		// Extract range
		var rangeMin, rangeMax int
		if r, ok := profile["range"].([]interface{}); ok && len(r) >= 2 {
			rangeMin = toInt(r[0])
			rangeMax = toInt(r[1])
		}

		// Extract GI
		gi := "long"
		if shortGI, ok := profile["short_gi"].(bool); ok && shortGI {
			gi = "short"
		}

		// Extract FEC
		var fecK, fecN int
		if fec, ok := profile["fec"].([]interface{}); ok && len(fec) >= 2 {
			fecK = toInt(fec[0])
			fecN = toInt(fec[1])
		}

		mcs := toInt(profile["mcs"])
		bitrate := toInt(profile["bitrate"])
		gop := toFloat(profile["gop"])
		power := toInt(profile["power"])
		bandwidth := toInt(profile["bandwidth"])
		qpDelta := toInt(profile["qp_delta"])

		// roiQP is not currently supported in UI, use default
		roiQP := "0,0,0,0"

		line := fmt.Sprintf("%d  -  %d  %s %d %d %d    %d %.1f %d   %s %d %d",
			rangeMin, rangeMax, gi, mcs, fecK, fecN, bitrate, gop, power, roiQP, bandwidth, qpDelta)
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n") + "\n"
}

// toInt converts interface to int.
func toInt(v interface{}) int {
	switch val := v.(type) {
	case int:
		return val
	case float64:
		return int(val)
	case int64:
		return int(val)
	default:
		return 0
	}
}

// toFloat converts interface to float64.
func toFloat(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	default:
		return 0
	}
}

// parseBoolValue converts various types to bool (handles bool, string, int).
func parseBoolValue(v interface{}) bool {
	if v == nil {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val == "true" || val == "1" || val == "yes"
	case int:
		return val != 0
	case float64:
		return val != 0
	default:
		return false
	}
}
