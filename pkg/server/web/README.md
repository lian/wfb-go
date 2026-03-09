# Web Package

Browser-based ground station for WFB - video player, stats dashboard, and configuration editor.

## What is this?

This package provides a complete web-based ground station interface:
- **Video player** - Watch your WFB stream directly in the browser (H.264/H.265)
- **Stats dashboard** - Real-time RSSI, SNR, FEC recovery, per-antenna metrics
- **Configuration panel** - Edit ground station and drone settings
- **Drone SSH support** - Configure legacy wfb-ng drones via SSH

It's useful for:
- Quick testing without setting up a full ground station
- Viewing video on devices where installing software is inconvenient
- Configuring drone settings remotely without direct access
- Sharing video with others on the same network

**About latency:** The web player may have higher latency than native decoders. In practice, latency is pretty good for most use cases, but your results will vary depending on browser, hardware, and network conditions. Test it yourself and see if it works for your needs.

## Quick Start

1. Enable the web UI in your config:

```yaml
web:
  enabled: true
  port: 8080
  video_stream: video   # Name of your video stream
```

2. Start wfb_server with your config
3. Open `http://<your-ip>:8080` in a browser

That's it. The page shows your video stream with real-time stats (RSSI, SNR, FEC recovery, etc.).

## Browser Compatibility

Browser support for video codecs changes frequently. Safari on macOS/iOS generally has the best HEVC/H.265 support. For other browsers, your mileage may vary - try it and see what works.

## How It Works

The web server receives video data from the RX service and streams it to browsers via WebSocket. Stats are also pushed via WebSocket for real-time display.

### Video Modes

- **Direct sink mode** - Video data comes directly from the RX service (default when using wfb_server)
- **UDP mode** - Video received via UDP (for standalone use)

### Architecture

```
                    ┌─────────────────────────────────────────┐
                    │              Web Server                  │
                    │                                          │
Video Data ────────>│  NALParser ──> broadcastNALU() ────────>│──> WebSocket ──> Browser
(RX callback        │       │                                  │      (MSE)
 or UDP)            │       └── Annex B start code parsing     │
                    │                                          │
Stats Data ────────>│  UpdateStats() ──────────────────────>│──> WebSocket ──> Browser
                    │                                          │      (JSON)
                    └─────────────────────────────────────────┘
```

## Configuration

```yaml
web:
  enabled: true
  port: 8080                  # HTTP server port
  video_stream: video         # Stream name to display
```

## Endpoints

| Endpoint | Description |
|----------|-------------|
| `/` | Web UI (video player + stats dashboard) |
| `/ws/video` | WebSocket for video NAL units |
| `/ws/stats` | WebSocket for stats JSON |
| `/api/stats` | REST endpoint for current stats |
| `/api/config` | REST endpoint for GS configuration (GET/PUT) |
| `/api/drone/config` | REST endpoint for drone configuration via SSH or HTTP |

## Configuration Panel

Click the gear icon in the top-right to open the configuration panel. The panel has two modes:

### Ground Station Mode

Edit local wfb_server settings:
- **General** - Channel, bandwidth, TX power, link domain
- **Streams** - Service types, ports, FEC settings
- **Adaptive** - Link quality parameters, score weights
- **Advanced** - Buffer sizes, timeouts, debug options

Changes are saved to the running server and take effect immediately (some require restart).

### Drone Mode

Configure remote drones via SSH (legacy wfb-ng) or HTTP (wfb-go):

1. Enter the drone IP address (default: 10.5.0.10)
2. Select connection mode:
   - **SSH** - For wfb-ng drones (reads/writes /etc/wfb.yaml, /etc/majestic.yaml, /etc/alink.conf)
   - **HTTP** - For wfb-go drones running wfb_server
3. Click Connect

Available settings for drones:
- **General** - Channel, bandwidth, TX power
- **Streams** - MCS, STBC, LDPC, Short GI, FEC
- **Camera** - Bitrate, GOP, FPS, codec, resolution, mirror/flip
- **Adaptive** - All alink.conf settings including TX profiles

### SSH Drone Configuration

For legacy wfb-ng drones, the web UI connects via SSH (root@drone, password: 12345) and:

**Reads:**
- `/etc/wfb.yaml` - WFB settings (channel, MCS, FEC, etc.)
- `/etc/majestic.yaml` - Camera settings (bitrate, GOP, codec, etc.)
- `/etc/alink.conf` - Adaptive link settings
- `/etc/txprofiles.conf` - TX profiles for adaptive link

**Writes:**
- Uses `wifibroadcast cli -s` for WFB settings
- Uses `cli -s` for majestic camera settings
- Uses `sed -i` for alink.conf settings
- Writes complete txprofiles.conf for profile changes

Services are automatically restarted after changes (wifibroadcast, majestic, alink_drone).

## Standalone Usage

The web server can also run standalone via `wfb_web`:

```bash
wfb_web --http :8080 --video :5600
```

This listens for video on UDP port 5600 and serves the web UI on port 8080.

## Technical Details

### Integration with wfb_server

When `web.enabled: true`, the server:

1. Creates a buffered channel for video data
2. Creates a non-blocking callback for the video stream
3. Starts a goroutine to drain the channel and call `WriteVideo()`
4. Starts the web server

The callback never blocks the RX packet processing path:

```go
// Non-blocking send - drops if channel full
select {
case videoChan <- dataCopy:
default:
    // Channel full, drop packet
}
```

### Video Streaming

- **H.264/H.265 NAL unit parsing** - Extracts complete NAL units from Annex B byte stream
- **Accumulating parser** - Handles NAL units that span multiple packets
- **Low-latency flush** - Timeout-based flush to minimize buffering delay
- **Length-prefixed frames** - Each NAL unit is sent with a 4-byte big-endian length prefix

### NAL Unit Format

Video is sent to browsers as binary WebSocket messages:

```
┌─────────────────┬─────────────────────────────┐
│  Length (4B)    │  NAL Unit Data              │
│  Big-endian     │  (without start code)       │
└─────────────────┴─────────────────────────────┘
```

The browser JavaScript reconstructs the Annex B stream by prepending start codes before feeding to MSE.

### Stats Dashboard

- **Real-time stats** - RSSI, SNR, FEC recovery, packet loss, decode errors
- **Per-antenna stats** - Individual RSSI/SNR/MCS/frequency for each adapter
- **Per-stream stats** - Packet rates and FEC stats per stream
- **TX stats** - Injected and dropped packet counts
- **Bitrate graph** - 60-second time-series with auto-scaling
- **Link quality gauge** - Visual indicator based on RSSI/SNR/FEC
- **Latency display** - Real-time latency measurement
- **WebSocket push** - Stats pushed to browser as JSON
- **REST API** - `/api/stats` endpoint for polling

## Files

| File | Description |
|------|-------------|
| `server.go` | HTTP server, WebSocket handlers, NAL parser, config API |
| `drone_ssh.go` | SSH client for legacy wfb-ng drone configuration |
| `sink.go` | VideoSink interface for direct integration |
| `static/index.html` | Main HTML shell with CSS styles |
| `static/js/app.js` | Vue.js application entry point |
| `static/js/components/VideoPlayer.js` | H.265/HEVC video decoder and player |
| `static/js/components/StatusBar.js` | Main status bar with key metrics |
| `static/js/components/AntennaPanel.js` | Per-antenna stats display |
| `static/js/components/StreamPanel.js` | Per-stream stats display |
| `static/js/components/BitrateGraph.js` | Time-series bitrate visualization |
| `static/js/components/LinkQuality.js` | Link quality gauge and latency display |
| `static/js/components/AdaptiveLinkPanel.js` | Adaptive link status and profile info |
| `static/js/components/ConfigPanel.js` | Configuration modal with device selector |
| `static/js/components/configHelp.js` | Help text for all config fields |
| `static/js/components/tabs/GeneralTab.js` | General settings tab |
| `static/js/components/tabs/StreamsTab.js` | Stream settings tab |
| `static/js/components/tabs/CameraTab.js` | Camera settings tab (drone only) |
| `static/js/components/tabs/AdaptiveTab.js` | Adaptive link settings tab |
| `static/js/components/tabs/AdvancedTab.js` | Advanced settings tab |
| `static/js/vue.global.prod.js` | Vue.js (bundled for offline use) |
| `static/favicon.svg` | Favicon |
