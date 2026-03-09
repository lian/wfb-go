# WiFi Package

WiFi adapter management and 802.11 frame handling.

## Overview

This package provides:

- **adapter/** - WiFi adapter detection and configuration
- **radiotap/** - Radiotap header parsing and generation
- **frame/** - IEEE 802.11 frame header handling

## Subpackages

### adapter

Detects WFB-compatible WiFi adapters and manages interface configuration.

```go
import "github.com/lian/wfb-go/pkg/wifi/adapter"

// Detect compatible adapters
adapters, err := adapter.DetectAdapters()
for _, a := range adapters {
    fmt.Printf("%s: %s (PHY: %s)\n", a.Name, a.Driver, a.PHY)
}

// Initialize interfaces for WFB
mgr := adapter.NewManager(cfg, []string{"wlan0", "wlan1"})
err := mgr.InitWlans()
```

Supported drivers:
- `rtl88xxau_wfb` - RTL8812AU
- `rtl88x2eu` - RTL8812EU
- `rtl88x2cu` - RTL8812CU

### radiotap

Parses and generates radiotap headers for RX/TX.

```go
import "github.com/lian/wfb-go/pkg/wifi/radiotap"

// Parse received radiotap header
hdr, offset, err := radiotap.Parse(packet)
fmt.Printf("RSSI: %d dBm, MCS: %d\n", hdr.DBMSignal, hdr.MCSIndex)

// Extract radio info
bandwidth := hdr.Bandwidth()    // 20, 40, 80 MHz
shortGI := hdr.ShortGI()        // Guard interval
ldpc := hdr.LDPC()              // LDPC coding
stbc := hdr.STBC()              // STBC streams

// Check for self-injected packets
if hdr.IsSelfInjected() {
    // Skip our own TX packets
}
```

TX header generation:
```go
import "github.com/lian/wfb-go/pkg/wifi/radiotap/tx"

// Build radiotap header for injection
header := tx.BuildHeader(tx.Config{
    MCS:       1,
    Bandwidth: 20,
    ShortGI:   false,
    STBC:      1,
    LDPC:      true,
})
```

### frame

IEEE 802.11 frame header handling.

```go
import "github.com/lian/wfb-go/pkg/wifi/frame"

// Build 802.11 data frame header
hdr := frame.BuildDataHeader(frame.Config{
    SrcMAC:  srcMAC,
    DstMAC:  dstMAC,
    BSSID:   bssid,
})
```

## Radiotap Fields

| Field | Description |
|-------|-------------|
| TSFT | Timestamp |
| Flags | Frame flags (FCS, etc.) |
| Rate | Legacy rate (500kbps units) |
| Channel | Frequency and flags |
| DBM_ANTSIGNAL | Signal strength (dBm) |
| DBM_ANTNOISE | Noise floor (dBm) |
| Antenna | Antenna index |
| MCS | HT MCS info (known, flags, index) |
| VHT | VHT info (bandwidth, MCS, NSS) |

## Monitor Mode Setup

The adapter manager performs these steps:
1. `nmcli device set <wlan> managed no` - Unmanage from NetworkManager
2. `ip link set <wlan> down`
3. `iw dev <wlan> set monitor otherbss`
4. `ip link set <wlan> up`
5. `iw dev <wlan> set channel <ch> <HT20|HT40+|...>`
6. `iw dev <wlan> set txpower fixed <mBm>`

## Files

| File | Description |
|------|-------------|
| `adapter/manager.go` | Adapter detection and configuration |
| `radiotap/radiotap.go` | Radiotap header parsing |
| `radiotap/tx.go` | TX radiotap header generation |
| `frame/header.go` | 802.11 frame header handling |
