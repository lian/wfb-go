# Server Package

Service orchestrator for running multiple WFB streams in-process.

## Overview

This package provides:

- **Server** - Main orchestrator that manages all services
- **Services** - TX, RX, MAVLink, Tunnel stream handlers
- **Stats aggregation** - Per-antenna statistics collection
- **TX antenna selection** - Automatic antenna switching based on RSSI
- **APIs** - MsgPack and JSON statistics APIs
- **Web UI** - Browser-based video player, stats dashboard, and configuration panel (see [web/README.md](web/README.md))

## Components

### Server

The main orchestrator that:
- Loads YAML configuration
- Initializes WiFi interfaces (monitor mode, channel, power)
- Starts all configured stream services
- Manages shared packet capture
- Aggregates statistics
- Provides API endpoints

```go
srv, err := server.New(server.Config{
    ConfigPath:  "/etc/wfb/drone.yaml",
    WLANs:       []string{"wlan0"},
    CaptureMode: "shared",
})

srv.Run(ctx)
```

### Stream Services

| Service | Description |
|---------|-------------|
| `StreamTX` | Receives UDP, encodes FEC, encrypts, injects |
| `StreamRX` | Captures, decrypts, FEC recovers, outputs UDP |
| `StreamMAVLink` | Bidirectional MAVLink with RSSI injection |
| `StreamTunnel` | Bidirectional IP tunnel (creates TUN device) |

### Statistics Aggregation

Collects per-antenna stats from all RX streams. Stats are tracked per packet with a separate lock to avoid contention with the FEC decode path.

```go
type AggregatedStats struct {
    Timestamp   time.Time
    Profile     string
    LinkDomain  string
    Interval    time.Duration

    // Per-service stats
    Services map[string]*ServiceStatsSnapshot

    // Per-antenna stats (keyed by wlanIdx<<8 | antennaIdx)
    Antennas map[uint32]*AggregatedAntennaStats

    // Selected TX antenna (nil if single adapter)
    TXWlanIdx *uint8
}

type AggregatedAntennaStats struct {
    WlanIdx   uint8
    Antenna   uint8
    Freq      uint16  // MHz
    MCSIndex  uint8
    Bandwidth uint8   // MHz

    PacketsTotal uint64
    RSSIMin      int8  // dBm
    RSSIAvg      int8
    RSSIMax      int8
    SNRMin       int8  // dB
    SNRAvg       int8
    SNRMax       int8
}
```

### TX Antenna Selection

When running with multiple WiFi adapters, the server automatically selects which adapter to use for transmission based on received signal quality. This is a **local decision** - each side (GS and drone) independently picks its best TX antenna based on what it receives.

**How it works:**
1. RX stats are collected from all adapters (RSSI, packet counts)
2. The selector picks the adapter with the strongest current signal
3. TX services are switched to transmit on that adapter
4. Hysteresis prevents rapid switching between similar adapters

**Logging:** Selection events are logged with RSSI details:
```
TX antenna selected: wlan0 (RSSI=-45dB, pkts=100)
TX antenna switch: wlan0 (RSSI=-70dB) -> wlan1 (RSSI=-40dB, delta=+30dB)
```

**Single adapter:** When only one adapter is configured, the selector is disabled entirely - there's no selection to make and no overhead.

**No cross-link feedback:** The GS does not tell the drone which antenna to use (or vice versa). Each side optimizes its own TX based on its own RX quality.

**Configuration:**
- `tx_sel_rssi_delta` - RSSI difference threshold to switch (default: 3 dB)
- `tx_sel_counter_abs_delta` - Absolute packet count difference (default: 3)
- `tx_sel_counter_rel_delta` - Relative packet count difference (default: 0.1)

### Web UI

Optional browser-based ground station with video player, stats dashboard, and configuration panel. Video data flows directly from the RX service to the web server via a non-blocking callback.

```yaml
web:
  enabled: true
  port: 8080
  video_stream: video
```

See [web/README.md](web/README.md) for full documentation.

### APIs

**MsgPack API** (for wfb_cli):
```go
// Connect to stats_port (default 8002)
// Receives binary msgpack messages:
// - cli_title: Profile name
// - rx: RX statistics
// - tx: TX statistics
// - settings: Current configuration
```

**JSON API** (for integrations):
```go
// Connect to json_port (default 8102)
// Receives line-delimited JSON:
{"type":"rx","data":{...}}
{"type":"tx","data":{...}}
```

## Service Lifecycle

```
Server.New()
    │
    ├─ Load config
    ├─ Initialize WLANs (if not skipped)
    │     ├─ Set regulatory region
    │     ├─ Set monitor mode
    │     ├─ Set channel/bandwidth
    │     └─ Set TX power
    │
    ├─ Create shared capture (if mode=shared)
    │
    └─ Create services for each stream

Server.Run(ctx)
    │
    ├─ Start all services
    ├─ Start stats aggregation
    ├─ Start API servers
    ├─ Start adaptive link (if enabled)
    │
    └─ Wait for context cancellation
          │
          └─ Graceful shutdown
```

## Files

| File | Description |
|------|-------------|
| `server.go` | Main orchestrator |
| `service.go` | Service interface and registry |
| `service_stream.go` | Base stream service |
| `service_stream_tx.go` | TX service |
| `service_stream_rx.go` | RX service |
| `service_stream_mavlink.go` | MAVLink service |
| `service_stream_tun.go` | Tunnel service |
| `stats.go` | Statistics aggregation |
| `antenna.go` | TX antenna selection |
| `api.go` | MsgPack and JSON APIs |
| `web/` | Web UI server (see [web/README.md](web/README.md)) |

## See Also

- [examples/drone.yaml](../../examples/drone.yaml) - Complete drone configuration
- [examples/gs.yaml](../../examples/gs.yaml) - Complete ground station configuration
- [pkg/config](../config/README.md) - Configuration format documentation
