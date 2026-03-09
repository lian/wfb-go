# TX Package

Implements WFB-NG packet transmission with FEC encoding and encryption.

## Overview

This package provides:

- **Transmitter** - FEC encoding, encryption, packet injection
- **Raw socket injection** - Direct 802.11 frame injection
- **UDP listener** - Receives data from local applications

## Components

### Transmitter

Core transmitter logic: receives UDP packets, applies FEC encoding, encrypts with ChaCha20-Poly1305, and injects via raw socket.

```go
// Load key data from file
keyData, _ := os.ReadFile("/etc/wfb/drone.key")

tx, err := tx.New(tx.Config{
    FecK:      8,              // Data shards
    FecN:      12,             // Total shards (4 parity)
    Epoch:     0,
    ChannelID: channelID,
    KeyData:   keyData,        // 64 bytes: secret key + peer public key
}, injector)

// Send a packet (will be FEC encoded and encrypted)
err := tx.Send(payload)

// Get statistics
stats := tx.Stats()
```

### FEC Block Management

Packets are grouped into FEC blocks:

1. Incoming packet → add to current block as fragment
2. When k fragments accumulated → encode parity shards
3. Encrypt and inject all n fragments
4. Advance to next block

### Session Key Announcement

The transmitter periodically broadcasts session packets (every 1 second) containing:
- Current epoch
- Channel ID
- FEC parameters (k, n)
- Session key (encrypted with crypto_box)

## Configuration

```go
type Config struct {
    FecK       int           // Data shards (default: 8)
    FecN       int           // Total shards (default: 12)
    Epoch      uint64        // Session epoch
    ChannelID  uint32        // (link_id << 8) | port
    FecDelay   time.Duration // Delay between FEC packets
    FecTimeout time.Duration // Timeout to close incomplete blocks
    KeyData    []byte        // TX key (64 bytes: secret key + peer public key)
}
```

## Radio Settings

Radio parameters are set via radiotap header injection:

```go
type RadioConfig struct {
    MCS       int  // MCS index (0-9 for HT, 0-9 for VHT)
    Bandwidth int  // 20, 40, or 80 MHz
    ShortGI   bool // Short guard interval
    STBC      int  // Space-time block coding (0-2)
    LDPC      bool // Low-density parity check
    VHT       bool // Use VHT (802.11ac) mode
    VHTNss    int  // VHT spatial streams
}
```

## Runtime Control

FEC and radio parameters can be changed at runtime via the control port:

```go
tx.SetFEC(4, 8)           // Change to 4/8 FEC
tx.SetRadio(radioConfig)  // Change radio settings
```

Compatible with `wfb_tx_cmd`:
```bash
wfb_tx_cmd 8000 set_fec -k 4 -n 8
wfb_tx_cmd 8000 set_radio -M 3 -B 40
```

## Statistics

```go
stats := tx.Stats()

stats.PacketsInjected  // Successfully injected
stats.BytesInjected    // Total bytes
stats.PacketsDropped   // Injection failures
stats.FECTimeouts      // Blocks closed by timeout
stats.SessionsStarted  // Session key rotations
```

## Injection Modes

| Mode | CLI Flag | Description |
|------|----------|-------------|
| Single | (default) | Inject on first interface only |
| Mirror | `-m` | Inject on all interfaces (spatial diversity) |

### Using Mirror Mode

CLI:
```bash
wfb_tx -m -K drone.key -u 5600 wlan0 wlan1 wlan2
```

In `wfb_server` config:
```yaml
streams:
  video:
    service_type: udp_direct_tx
    stream_tx: 0
    peer: "listen://0.0.0.0:5600"
    fec: [8, 12]
    mirror: true   # Enable mirror mode
```

## Files

| File | Description |
|------|-------------|
| `transmitter.go` | Core TX logic with FEC and encryption |
| `rawsocket.go` | Raw socket packet injection |
| `udp.go` | UDP listener for input data |
