# wfb_server - Service Orchestrator

Runs multiple WFB streams from a YAML configuration file. This is the recommended way to run wfb-go in production.

## Usage

```
wfb_server --config <config.yaml> [options]
```

## Configuration

See the example configs to get started:
- **[examples/gs.yaml](../../examples/gs.yaml)** - Ground station (recommended starting point)
- **[examples/drone.yaml](../../examples/drone.yaml)** - Drone/air unit

## Options

| Flag | Description | Default |
|------|-------------|---------|
| `--config` | Config file path (YAML) | required |
| `--wlans` | WiFi interfaces (comma-separated) | from config |
| `--skip-wlan-init` | Skip WiFi interface initialization | false |
| `--log-interval` | Stats log interval in ms (0 to disable) | 1000 |
| `--json-port` | JSON API port | from config |
| `--msgpack-port` | MsgPack API port | from config |
| `--capture-mode` | Capture mode: dedicated, shared, or libpcap | from config |
| `--version` | Show version information | - |

## Features

- Runs all streams in-process (no external wfb_tx/wfb_rx)
- Automatic WiFi interface setup (monitor mode, channel, power)
- TX antenna selection based on RX signal quality
- Per-antenna RSSI/SNR statistics
- Adaptive link integration (OpenIPC compatible)
- MsgPack API for wfb_cli
- JSON API for integrations

### Web UI

When enabled in the config, wfb_server provides a browser-based ground station interface:

- **Live video player** - H.264/H.265 decoding via WebCodecs (Safari/Chrome)
- **Real-time stats** - RSSI, SNR, FEC recovery, per-antenna metrics
- **Stream monitoring** - Packet rates, dropped packets, FEC status per stream
- **Configuration panel** - Edit ground station and drone settings
- **Drone SSH support** - Configure legacy wfb-ng drones via SSH (camera, adaptive link, profiles)

Enable in config:
```yaml
web:
  enabled: true
  port: 8080
  video_stream: video    # Stream name for browser video player
```

Then open http://localhost:8080 in your browser.

## Examples

```bash
# Run ground station with config file
wfb_server --config /etc/wfb/gs.yaml

# Override interfaces
wfb_server --config gs.yaml --wlans wlan0,wlan1

# Skip WiFi init (already configured)
wfb_server --config gs.yaml --skip-wlan-init

# Use shared capture mode for lower latency
wfb_server --config gs.yaml --capture-mode shared

# Show version
wfb_server --version
```

## See Also

- [pkg/server](../../pkg/server/README.md) - Server package documentation
- [pkg/config](../../pkg/config/README.md) - Configuration reference
