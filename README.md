# wfb-go

A pure Go implementation of [wfb-ng](https://github.com/svpcom/wfb-ng) (WiFi Broadcast Next Generation), providing low-latency video transmission over WiFi using packet injection and FEC (Forward Error Correction).

## What is wfb-go?

wfb-go is a ground-up rewrite of wfb-ng in Go. It provides the same core functionality - encrypted, FEC-protected wireless video/telemetry links - but with a native Go implementation that eliminates C/Python dependencies.

**Current Status:** wfb-go is primarily tested on the ground station side, with the VTX (drone) running the default OpenIPC firmware wfb-ng. The wire protocol is fully compatible, so you can use wfb-go GS with a standard OpenIPC drone setup out of the box.

> :warning: **Warranty/Disclaimer**
> This is free software and comes with no warranty, as stated in the MIT license. The creators and contributors of the software are not responsible for how it is used. See [LICENSE](LICENSE) for details.

The project implements:
- **1:1 mapping of UDP to IEEE 802.11 packets** - Minimum latency (no byte-stream serialization)
- **Reed-Solomon FEC** - Pure Go implementation bit-identical to wfb-ng's zfex
- **Smart FEC** - Latency reduction on packet loss through efficient block handling
- **ChaCha20-Poly1305 encryption** - Pure Go with 8-byte nonces (original DJB spec, not IETF)
- **X25519 key exchange** - crypto_box compatible session key encryption
- **TPACKET_V3 + BPF** - High-performance packet capture with kernel-level filtering
- **Adaptive link** - Dynamic MCS/FEC/bitrate based on link quality
- **Wire protocol compatibility** - Interoperates with wfb-ng transmitters and receivers

## Roadmap

Beyond wfb-ng compatibility, wfb-go aims to integrate the complete FPV stack:
- **[Adaptive Link](pkg/adaptive/README.md)** - Integrated (OpenIPC Adaptive-Link compatible)
- **[Web Ground Station](pkg/server/web/README.md)** - Browser-based video player, stats dashboard, and configuration editor
- **MSPOSD** - On-screen display overlay (planned)

## How It Works

```
Camera --[UDP/RTP]--> wfb_tx --//--[ RADIO ]--//--> wfb_rx --[UDP/RTP]--> Decoder
                         │                            │
                    [FEC encode]                 [FEC decode]
                    [Encrypt]                    [Decrypt]
                    [Inject]                     [Capture]
```

WiFi cards are put into monitor mode, allowing transmission and reception of raw 802.11 frames without association. This bypasses normal WiFi overhead (ACKs, retries, association) for minimum latency.

## Why Go?

- **Single static binary** - No Python, no shared libraries, just deploy and run
- **Memory safety** - No buffer overflows or memory leaks
- **Native concurrency** - Efficient handling of multiple streams and interfaces
- **Cross-compilation** - Build for ARM, x86, or any Go-supported platform from any host
- **Simpler deployment** - No virtualenv, no pip, no dependency hell

## Features

| Feature | Status |
|---------|--------|
| FEC encoding/decoding | ✅ Pure Go, bit-identical to wfb-ng |
| ChaCha20-Poly1305 encryption | ✅ Pure Go, 8-byte nonce (DJB spec) |
| Session key exchange (crypto_box) | ✅ |
| Multi-interface RX diversity | ✅ |
| Automatic TX antenna selection | ✅ Based on RX RSSI |
| Per-antenna RSSI/SNR stats | ✅ |
| BPF kernel filtering | ✅ |
| MAVLink RSSI injection | ✅ |
| MAVLink/tunnel packet aggregation | ✅ |
| IP tunnel (TUN device) | ✅ |
| Adaptive link | ✅ Integrated OpenIPC Adaptive-Link |
| Dynamic FEC and modulation | ✅ Runtime changes via wfb_tx_cmd |
| Traffic shaper support (fwmark) | ✅ |
| Distributed operation (cluster) | ⏸️ Removed for now, planned |
| wfb_cli stats viewer | ✅ |
| MsgPack + JSON APIs | ✅ |
| Web UI video player | ✅ Browser-based H.264/H.265 viewer |
| Web UI configuration | ✅ GS + drone config editor with SSH support |

## Package Documentation

| Package | Description |
|---------|-------------|
| [pkg/rx](pkg/rx/README.md) | Packet reception, FEC recovery, per-antenna stats |
| [pkg/tx](pkg/tx/README.md) | Packet transmission, FEC encoding, radiotap injection |
| [pkg/server](pkg/server/README.md) | Service orchestrator, TX antenna selection, APIs |
| [pkg/server/web](pkg/server/web/README.md) | Web UI, video player, stats dashboard, config panel |
| [pkg/crypto](pkg/crypto/README.md) | ChaCha20-Poly1305, X25519, session key exchange |
| [pkg/protocol](pkg/protocol/README.md) | Wire protocol, packet formats, radiotap |
| [pkg/config](pkg/config/README.md) | YAML configuration parsing |
| [pkg/adaptive](pkg/adaptive/README.md) | Adaptive link quality monitoring |
| [pkg/wifi](pkg/wifi/README.md) | WiFi adapter detection and management |

## Installation

### From Source

```bash
git clone https://github.com/lian/wfb-go
cd wfb-go

# Main tools (recommended)
go build -o wfb_server ./cmd/wfb_server
go build -o wfb_keygen ./cmd/wfb_keygen

# Optional: standalone tools (for migration from wfb-ng or standalone use)
go build -o wfb_rx ./cmd/wfb_rx
go build -o wfb_tx ./cmd/wfb_tx
go build -o wfb_cli ./cmd/wfb_cli
go build -o wfb_tx_cmd ./cmd/wfb_tx_cmd
go build -o wfb_tun ./cmd/wfb_tun
```

### Cross-compile for ARM

```bash
# Using build script (builds all binaries for all targets)
./build.sh

# Or build specific target/binary
./build.sh --target linux/arm64 --bin wfb_server

# Manual cross-compile
GOOS=linux GOARCH=arm64 go build -o wfb_server ./cmd/wfb_server
GOOS=linux GOARCH=arm GOARM=7 go build -o wfb_server ./cmd/wfb_server
```

Output goes to `dist/<os>_<arch>/`.

## Requirements

- Linux kernel 4.x+ with monitor mode support
- WiFi adapter with monitor mode and packet injection
- Root privileges (for raw sockets and monitor mode)

### Supported WiFi Hardware

wfb-go auto-detects adapters using these drivers:
- **RTL8812AU** - `rtl88xxau_wfb` driver ([patched driver required](https://github.com/svpcom/rtl8812au))
- **RTL8812EU** - `rtl88x2eu` driver ([patched driver required](https://github.com/svpcom/rtl8812eu))
- **RTL8812CU** - `rtl88x2cu` driver

These are 802.11ac capable cards supporting 5GHz with good TX power control.

## Quick Start

### 1. Generate Keys

```bash
# Generate matching keypairs
wfb_keygen -o /etc/wfb

# Or derive from a password (reproducible across devices)
wfb_keygen -o /etc/wfb -p "your-secret-password"
```

This creates:
- `drone.key` - Use on the drone (TX)
- `gs.key` - Use on the ground station (RX)

### 2. Configure and Run wfb_server

The recommended way to run wfb-go is with `wfb_server` using a YAML config file. It handles WiFi setup, runs all streams in-process, and provides stats APIs.

```bash
# On drone
sudo wfb_server --config drone.yaml --wlans wlan0

# On ground station
sudo wfb_server --config gs.yaml --wlans wlan0,wlan1
```

See the [Configuration](#wfb_server-configuration) section below for config file details.

### 3. Receive Video (GS)

```bash
# GStreamer H.265 decode with VA-API hardware acceleration (lowest latency)
gst-launch-1.0 \
  udpsrc port=5600 buffer-size=0 do-timestamp=false \
  ! application/x-rtp,encoding-name=H265 \
  ! rtph265depay \
  ! h265parse config-interval=-1 \
  ! vah265dec \
  ! queue max-size-buffers=1 leaky=downstream \
  ! glimagesink sync=false render-delay=0
```

Alternatively, enable the built-in web UI for browser-based viewing and configuration:
```yaml
web:
  enabled: true
  port: 8080
  video_stream: video
```
Then open http://localhost:8080 in Safari (for HEVC) or Chrome. The web UI provides:
- **Live video player** - H.264/H.265 decoding in browser
- **Real-time stats** - RSSI, SNR, FEC recovery, per-antenna metrics
- **Configuration panel** - Edit ground station and drone settings
- **Drone SSH support** - Configure legacy wfb-ng drones via SSH (camera, adaptive link, profiles)

## wfb_server Configuration

wfb_server uses YAML configuration files. See the complete examples:
- [examples/drone.yaml](examples/drone.yaml) - Drone configuration
- [examples/gs.yaml](examples/gs.yaml) - Ground station configuration

### Drone Configuration

```yaml
hardware:
  wlans: [wlan0]
  region: BO
  channel: 161
  bandwidth: 20
  tx_power: 20
  mcs: 3
  stbc: 1
  ldpc: 1

link:
  domain: default          # OpenIPC standard (hashed to link_id)
  key: /etc/wfb/drone.key

streams:
  video:
    service_type: udp_direct_tx
    stream_tx: 0
    peer: "listen://0.0.0.0:5600"
    fec: [8, 12]

  tunnel:
    service_type: tunnel
    stream_tx: 0x20
    stream_rx: 0xa0
    fec: [1, 2]
    tunnel:
      ifname: wfb-tun
      ifaddr: 10.5.0.10/24       # Drone IP

adaptive:
  enabled: true
  mode: drone
  listen_port: 9999
  profiles:
    - range: [999, 999]
      mcs: 0
      fec: [2, 6]
      bitrate: 2000
    - range: [1000, 1200]
      mcs: 1
      fec: [8, 12]
      bitrate: 4000
    - range: [1201, 2000]
      mcs: 3
      fec: [8, 12]
      bitrate: 10000
```

### Ground Station Configuration

```yaml
hardware:
  wlans: [wlan0, wlan1]
  region: BO
  channel: 161
  bandwidth: 20
  tx_power: 20
  mcs: 3
  stbc: 1
  ldpc: 1

link:
  domain: default          # Must match drone (OpenIPC standard)
  key: /etc/wfb/gs.key

streams:
  video:
    service_type: udp_direct_rx
    stream_rx: 0
    peer: "connect://127.0.0.1:5600"

  tunnel:
    service_type: tunnel
    stream_tx: 0xa0              # Swapped from drone
    stream_rx: 0x20
    fec: [1, 2]
    tunnel:
      ifname: wfb-tun
      ifaddr: 10.5.0.1/24        # GS IP

adaptive:
  enabled: true
  mode: gs
  send_addr: "10.5.0.10:9999"    # Drone's tunnel IP

api:
  stats_port: 8002
  json_port: 8102

web:
  enabled: true
  port: 8080
  video_stream: video          # Stream name for browser video player
```

### Service Types

| Type | Description |
|------|-------------|
| `udp_direct_tx` | TX only - sends UDP input to radio |
| `udp_direct_rx` | RX only - receives from radio, outputs UDP |
| `udp_proxy` | Bidirectional UDP proxy (protocol agnostic) |
| `mavlink` | Bidirectional MAVLink with RSSI injection |
| `tunnel` | IP tunnel (creates TUN device) |

### Stream ID Allocation

Per wfb-ng standard:
- **Downlink (drone → GS):** 0-127
- **Uplink (GS → drone):** 128-255

Ranges:
- 0-15 / 128-143: Video streams
- 16-31 / 144-159: Telemetry (MAVLink)
- 32-47 / 160-175: Tunnel
- 48-63 / 176-191: MSP/custom

## CLI Tools

| Tool | Description |
|------|-------------|
| [wfb_server](cmd/wfb_server/README.md) | Service orchestrator with built-in web UI (recommended) |
| [wfb_keygen](cmd/wfb_keygen/README.md) | Generate matched keypairs |
| [wfb_tx](cmd/wfb_tx/README.md) | Standalone transmitter |
| [wfb_rx](cmd/wfb_rx/README.md) | Standalone receiver |
| [wfb_cli](cmd/wfb_cli/README.md) | Real-time stats viewer |
| [wfb_tx_cmd](cmd/wfb_tx_cmd/README.md) | Runtime TX parameter control |
| [wfb_tun](cmd/wfb_tun/README.md) | IP tunnel over WFB |

The standalone tools (wfb_tx, wfb_rx, etc.) are useful for migration from wfb-ng or simple single-stream setups.

## Migration from wfb-ng

### CLI Compatibility

wfb-go CLI tools use the same flags as wfb-ng:

| wfb-ng | wfb-go | Notes |
|--------|--------|-------|
| `wfb_tx` | `wfb_tx` | Same flags: -K, -k, -n, -u, -p, -M, -B, etc. |
| `wfb_rx` | `wfb_rx` | Same flags: -K, -c, -u, -p, etc. |
| `wfb_keygen` | `wfb_keygen` | Same key file format (64 bytes) |
| `wfb_tx_cmd` | `wfb_tx_cmd` | Same commands: set_fec, get_fec, set_radio, get_radio |
| `wfb_tun` | `wfb_tun` | IP tunnel over WFB |

### Key Differences

1. **Configuration Format**
   - wfb-ng: INI config with Python syntax (`True`, `None`, `[list]`)
   - wfb-go: YAML config with explicit types

2. **Key Files**
   - Fully compatible - same 64-byte format
   - Generate once, use with either implementation

3. **Dependencies**
   - wfb-ng: Python 3, Twisted, libsodium, libpcap
   - wfb-go: None (statically linked Go binary)

4. **Process Model**
   - wfb-ng: Separate wfb_tx/wfb_rx processes per stream
   - wfb-go: `wfb_server` runs all streams in-process with shared capture

5. **Adaptive Link**
   - wfb-ng: Separate alink_drone/alink_gs processes
   - wfb-go: Integrated into wfb_server configuration

## Testing

```bash
# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run benchmarks
go test -bench=. ./...
```

The test suite covers:
- **pkg/crypto** - ChaCha20-Poly1305 encryption, compatibility with libsodium
- **pkg/fec** - Reed-Solomon encoding/decoding, bit-identical to wfb-ng's zfex
- **pkg/rx** - Packet aggregation, FEC recovery, ring buffers
- **pkg/tx** - Transmission, FEC encoding
- **pkg/config** - YAML configuration parsing
- **pkg/adaptive** - Link quality monitoring, profile selection
- **pkg/server** - Stats aggregation, TX antenna selection, MAVLink handling

## Performance

### Benchmarks (ARM64)

| Component | Operation | Throughput | Notes |
|-----------|-----------|------------|-------|
| ChaCha20-Poly1305 | Encrypt 1500B | 454 MB/s | Pure Go, 8-byte nonce |
| ChaCha20-Poly1305 | Decrypt 1500B | 452 MB/s | ~300K packets/sec capacity |
| FEC Encode | 8/12 1400B | ~800 MB/s | Pure Go |
| FEC Reconstruct | 8/12 | ~60 GB/s | Matrix inversion cached |
| Packet Capture | TPACKET_V3 | 200K+ pps | Kernel ring buffer |

### Capacity vs Typical Video

| Metric | Capacity | 30 Mbps Video (~2.5K pps) | Headroom |
|--------|----------|---------------------------|----------|
| Crypto | ~300K pps | 2.5K pps | ~120x |
| FEC | ~290K pps | 2.5K pps | ~116x |
| Capture | 200K+ pps | 2.5K pps | ~80x |

### Capture Modes

| Mode | Description | Use Case |
|------|-------------|----------|
| `dedicated` | One pcap handle per stream | Better isolation |
| `shared` | One handle per interface, demux in userspace | Lower latency, less overhead |

See [pkg/rx/README.md](pkg/rx/README.md) for details on BPF filtering and capture internals.

## FEC Details

Reed-Solomon erasure coding over GF(2^8), bit-identical to wfb-ng's zfex. With default 8/12 FEC, any 8 of 12 packets can reconstruct the original data (33% loss tolerance with 50% overhead).

See [pkg/tx/README.md](pkg/tx/README.md) for encoding details.

## Protocol

wfb-go implements the wfb-ng wire protocol. See [pkg/protocol/README.md](pkg/protocol/README.md) for packet formats and [wfb-ng-std-draft.md](https://github.com/svpcom/wfb-ng/blob/master/doc/wfb-ng-std-draft.md) for the full specification.

Channel ID: `channel_id = (link_id << 8) | port` where link_id (24-bit) is hashed from the link domain and port (8-bit) is the stream number.

## FAQ

**Q: What type of data can be transmitted?**

A: Any UDP with packet size ≤ 3993 bytes. For example: H.264/H.265 in RTP, MAVLink, or generic IPv4 via tunnel.

**Q: What are the transmission guarantees?**

A: WFB uses FEC which can recover lost packets. With default 8/12 settings, it can recover any 4 lost packets from a 12-packet block. You can tune k/n to fit your needs.

**Q: Is this compatible with wfb-ng?**

A: Yes, wfb-go uses the same wire protocol and key format. You can mix wfb-go TX with wfb-ng RX or vice versa.

## Acknowledgments

wfb-go builds on the excellent work of:

- **[wfb-ng](https://github.com/svpcom/wfb-ng)** by Vasily Evseenko - The original WiFi Broadcast NG project. The FEC implementation (zfex) and wire protocol are from wfb-ng.
- **[OpenIPC Adaptive-Link](https://github.com/sickgreg/OpenIPC-Adaptive-Link)** by sickgreg - Adaptive bitrate algorithm for dynamic link quality adjustment.
- **[libsodium](https://libsodium.org/)** - Cryptographic primitives reference (ChaCha20-Poly1305, crypto_box API compatibility).
- **Screaming GPUs in a datacenter somewhere**

## License

GPLv3 - see [LICENSE](LICENSE) for details.

## Contributing

Contributions are welcome. Please open an issue to discuss significant changes before submitting a PR.

## Community

- wfb-ng Telegram: https://t.me/wfb_ng
- OpenIPC community: https://openipc.org
