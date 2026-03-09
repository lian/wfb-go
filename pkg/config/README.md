# Config Package

YAML configuration system for wfb_server.

## Overview

This package provides:

- YAML configuration parsing
- Hardware, link, and stream configuration
- Adaptive link profile configuration
- Example configurations for drone and ground station

## Configuration Structure

```yaml
hardware:
  wlans: [wlan0, wlan1]
  region: BO
  channel: 149
  bandwidth: 20
  tx_power: 58
  capture_mode: shared

link:
  domain: "my-drone"
  key: /etc/wfb/drone.key           # Key file path
  # key_base64: "..."               # Or embed key as base64

streams:
  video:
    service_type: udp_direct_tx
    stream_tx: 0
    peer: "listen://0.0.0.0:5600"
    fec: [8, 12]
    # Radio params (per-stream, for TX direction)
    mcs: 1
    stbc: 1
    ldpc: 1
    short_gi: false

  tunnel:
    service_type: tunnel
    stream_tx: 0x20
    stream_rx: 0xa0
    fec: [1, 2]
    tunnel:
      ifname: wfb-tun
      ifaddr: 10.5.0.10/24     # Drone IP (GS uses 10.5.0.1/24)

adaptive:
  enabled: true
  mode: drone
  listen_port: 9999            # Receives stats from GS via tunnel
  profiles: [...]

api:
  enabled: true
  stats_port: 8002
  json_port: 8102
```

## Service Types

| Type | Description | Streams |
|------|-------------|---------|
| `udp_direct_tx` | TX only (video) | stream_tx |
| `udp_direct_rx` | RX only | stream_rx |
| `udp_proxy` | Bidirectional UDP | stream_tx + stream_rx |
| `mavlink` | MAVLink with RSSI injection | stream_tx + stream_rx |
| `tunnel` | IP tunnel (TUN device) | stream_tx + stream_rx |

## Peer Connection Formats

```yaml
peer: "listen://0.0.0.0:5600"      # UDP server
peer: "connect://127.0.0.1:5600"   # UDP client
peer: "serial:/dev/ttyUSB0:115200" # Serial port
peer: "tcp://0.0.0.0:5760"         # TCP server
```

## Usage

```go
import "github.com/lian/wfb-go/pkg/config"

// Load configuration
cfg, err := config.Load("/etc/wfb/drone.yaml")

// Access settings
for _, wlan := range cfg.Hardware.WLANs {
    fmt.Println(wlan)
}

// Iterate streams
for name, stream := range cfg.Streams {
    fmt.Printf("%s: %s\n", name, stream.ServiceType)
}
```

## Hardware Configuration

```go
type HardwareConfig struct {
    WLANs     []string          // WiFi interfaces
    Region    string            // Regulatory region
    Channel   int               // WiFi channel
    Bandwidth int               // 20 or 40 MHz
    TXPower   *int              // TX power (nil = default)
    CaptureMode string          // "dedicated" or "shared"

    // Per-interface overrides
    ChannelOverrides map[string]int
    TXPowerOverrides map[string]int
}
```

Note: Radio parameters (MCS, ShortGI, STBC, LDPC) are configured per-stream,
not at the hardware level. See StreamConfig below.

## Stream Configuration

```go
type StreamConfig struct {
    ServiceType string   // Service type
    StreamRX    *uint8   // RX stream ID (0-255)
    StreamTX    *uint8   // TX stream ID (0-255)
    Peer        string   // Connection string
    FEC         []int    // [k, n] FEC parameters
    Mirror      bool     // TX on all interfaces

    // Radio params for TX (per-packet via radiotap injection)
    // Only used for streams with TX direction
    MCS       *int    // MCS index (nil = default 1)
    ShortGI   bool    // Short guard interval (default: false)
    STBC      *int    // Space-time block coding (nil = default 1, 0 = disabled)
    LDPC      *int    // LDPC coding (nil = default 1, 0 = disabled)
    Bandwidth int     // 20 or 40 MHz (default: from hardware)

    // MAVLink specific
    MAVLink *MAVLinkConfig
}
```

## Example Files

See the [examples/](../../examples/) directory for complete configuration examples:

| File | Description |
|------|-------------|
| [drone.yaml](../../examples/drone.yaml) | Complete drone configuration |
| [gs.yaml](../../examples/gs.yaml) | Complete ground station configuration |

## Key Configuration

Keys can be specified in two ways:

### File path (traditional)
```yaml
link:
  key: /etc/wfb/drone.key
```

### Base64 embedded (for containerized deployments)
```yaml
link:
  key_base64: "u7ftboOkaoqbihKg+Y7OK9yXhwW4IEcBsghfooyse0YOBcSKYZX7cJIcdHpm6DwC5kC9a761slFTepiidBaiYw=="
```

Generate base64 from key file:
```bash
base64 -w0 /etc/wfb/drone.key
```

Priority: `key_base64` takes precedence over `key` if both are specified.

Per-stream key overrides are also supported:
```yaml
streams:
  video:
    key: /etc/wfb/video.key        # File path
    # key_base64: "..."            # Or base64
```

## Files

| File | Description |
|------|-------------|
| `config.go` | Configuration structures and loading |
