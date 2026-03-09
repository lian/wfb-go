# Adaptive Package

Implements adaptive link quality monitoring and profile selection, compatible with [OpenIPC Adaptive-Link](https://github.com/sickgreg/OpenIPC-Adaptive-Link).

## Overview

This package provides two complementary components:

- **LinkMonitor** (GS side) - Monitors RSSI/SNR and sends link quality scores to the drone
- **LinkReceiver** (Drone side) - Receives scores and selects appropriate TX profiles

**Important:** Adaptive link requires a bidirectional tunnel between GS and drone. When using `wfb_server`, configure a `tunnel` service type in your YAML config. The GS sends link quality updates to the drone via this tunnel (e.g., to `10.5.0.10:9999` where `10.5.0.10` is the drone's TUN interface IP).

## Usage with wfb_server

When using `wfb_server`, all services run in-process (no separate `wfb_tx`/`wfb_rx` binaries). Configure the tunnel and adaptive link in your YAML:

```yaml
streams:
  tunnel:
    service_type: tunnel
    stream_tx: 32      # Downlink tunnel
    stream_rx: 160     # Uplink tunnel
    fec: [1, 2]

adaptive:
  enabled: true
  mode: drone          # or "gs"
  listen_port: 9999
  profiles:
    - range: [1000, 1200]
      mcs: 0
      fec: [8, 12]
      bitrate: 3000
    # ... more profiles
```

## Components

### LinkMonitor

Runs on the ground station. Collects per-antenna RSSI/SNR statistics, calculates weighted link quality scores (1000-2000), and sends updates to the drone over the tunnel.

Features:
- Kalman filter for noise estimation
- Configurable RSSI/SNR ranges and weights
- Keyframe request triggering on packet loss
- Dynamic FEC suggestions based on link quality
- Penalty calculation for noisy links

```go
monitor, err := adaptive.NewLinkMonitor(adaptive.LinkConfig{
    DroneAddr:      "10.5.0.10:9999",  // Drone's TUN IP
    UpdateInterval: 100 * time.Millisecond,
    RSSIMin:        -85,
    RSSIMax:        -40,
    SNRMin:         10,
    SNRMax:         36,
    SNRWeight:      0.5,
    RSSIWeight:     0.5,
}, getStatsFunc)

monitor.Start()
defer monitor.Stop()
```

### LinkReceiver

Runs on the drone. Receives link quality messages from the GS (via the tunnel) and selects TX profiles based on score ranges.

Features:
- Profile selection with hysteresis to prevent oscillation
- Exponential smoothing for score stability
- Hold-down timers for quality transitions
- Fallback mode when GS heartbeat is lost
- TX dropped packet monitoring
- Dynamic FEC adjustment from GS suggestions
- External command execution for encoder/radio control

```go
receiver, err := adaptive.NewLinkReceiver(adaptive.LinkReceiverConfig{
    Port: 9999,
    Profiles: []adaptive.TXProfile{
        {RangeMin: 1000, RangeMax: 1200, MCS: 0, FecK: 8, FecN: 12, Bitrate: 3000},
        {RangeMin: 1201, RangeMax: 1500, MCS: 1, FecK: 8, FecN: 12, Bitrate: 6000},
        {RangeMin: 1501, RangeMax: 2000, MCS: 2, FecK: 8, FecN: 12, Bitrate: 10000},
    },
    OnProfileChange: func(profile *adaptive.TXProfile, msg *adaptive.LinkMessage) {
        // Adjust TX parameters
    },
})

receiver.Start()
defer receiver.Stop()
```

## Data Flow

```
GS (wfb_server)                                  Drone (wfb_server)
    │                                                  │
    ├─ Collect RSSI/SNR from radiotap headers         │
    │                                                  │
    ▼                                                  │
LinkMonitor                                            │
    │                                                  │
    ├─ Calculate link quality score (1000-2000)       │
    │                                                  │
    ▼                                                  │
UDP to 10.5.0.10:9999                                  │
    │                                                  │
    └──► TUN ──► tunnel TX ──//──► tunnel RX ──► TUN ─┘
                                                       │
                                                       ▼
                                               LinkReceiver
                                                       │
                                                       ├─ Select TX profile
                                                       │
                                                       ▼
                                               Adjust MCS/FEC/Bitrate
```

## Wire Protocol

Messages are sent as length-prefixed UTF-8 strings over UDP:

```
[4-byte big-endian length][message]
```

Message format (colon-separated):
```
timestamp:rssi_score:snr_score:fec_rec:lost:best_rssi:best_snr:num_ant:penalty:fec_change[:keyframe_code]
```

Special commands:
```
special:request_keyframe:code
special:pause_adaptive
special:resume_adaptive
```

## Profile Format

Compatible with alink's `txprofiles.conf`:

```
# rangeMin - rangeMax setGI setMCS setFecK setFecN setBitrate setGop wfbPower ROIqp bandwidth setQpDelta
999 - 999 long 0 8 12 2000 0.5 58 0,0,0,0 20 -12
1000 - 1200 long 1 8 12 4000 1.0 56 0,0,0,0 20 -12
1201 - 1800 long 2 8 12 8000 2.0 50 12,6,6,12 20 -12
```

## Files

| File | Description |
|------|-------------|
| `link.go` | LinkMonitor - GS side link quality sender |
| `receiver.go` | LinkReceiver - Drone side profile selector |
