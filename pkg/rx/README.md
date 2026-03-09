# RX Package

Implements WFB-NG packet reception, decryption, and FEC recovery.

## Overview

This package provides:

- **Aggregator** - Session management, decryption, FEC recovery
- **Receiver/Forwarder** - Packet capture and output
- **AF_PACKET capture** - TPACKET_V3 with BPF filtering

## Components

### Aggregator

Core receiver logic: processes raw packets, manages sessions, decrypts data, and performs FEC recovery.

```go
// Load key data from file
keyData, _ := os.ReadFile("/etc/wfb/gs.key")

agg, err := rx.NewAggregator(rx.AggregatorConfig{
    ChannelID: channelID,
    Epoch:     0,
    KeyData:   keyData,        // 64 bytes: secret key + peer public key
    OutputFn: func(data []byte) error {
        // Handle reassembled packet
        return conn.Write(data)
    },
})

// Process incoming packet (after radiotap/802.11 stripped)
agg.ProcessPacket(wfbPayload, radioInfo)

// Get statistics
stats := agg.Stats()
```

### Receiver

High-level receiver that captures from interfaces and outputs to UDP/Unix socket.

```go
receiver, err := rx.NewReceiver(rx.ReceiverConfig{
    Interfaces:  []string{"wlan0", "wlan1"},
    ChannelID:   channelID,
    KeyPath:     "/etc/wfb/gs.key",
    OutputAddr:  "127.0.0.1:5600",
    CaptureMode: "shared",
})

receiver.Run(ctx)
```

### Forwarder

Captures packets and forwards them raw (with radio metadata) to a remote aggregator for cluster/distributed operation.

```go
forwarder, err := rx.NewForwarder(rx.ForwarderConfig{
    Interface:    "wlan0",
    ChannelID:    channelID,
    AggregatorAddr: "192.168.1.100:5800",
})

forwarder.Run(ctx)
```

## Capture Modes

| Mode | Description | Use Case |
|------|-------------|----------|
| `dedicated` | One pcap handle per stream | Better isolation |
| `shared` | One handle per interface, demux by channel ID | Lower latency |

## BPF Filter

Packets are filtered in kernel before reaching userspace:

```
Load radiotap length (LE16 at offset 2-3)
Load WFB magic at radiotap_len + 10
If magic != 0x5742 ("WB"), drop
Load channel_id at radiotap_len + 12
If channel_id != expected, drop
Accept
```

## Statistics

```go
stats := agg.Stats()

stats.PacketsAll      // Total received
stats.PacketsUniq     // Unique (non-duplicate)
stats.PacketsDecErr   // Decryption failures
stats.PacketsFECRec   // FEC recovered
stats.PacketsLost     // Unrecoverable losses
stats.PacketsOverride // Ring buffer overflows
```

## FEC Recovery

The aggregator maintains a ring buffer of FEC blocks. When k unique fragments arrive for a block, it attempts reconstruction:

1. If all k data fragments present → output directly
2. If some data fragments missing but k total present → FEC reconstruct
3. If block incomplete when ring advances → count as lost

## Per-Antenna Stats

Radio metadata (RSSI, SNR, antenna) is tracked per wlan+antenna combination:

```go
stats := agg.Stats()

// AntennaStats keyed by (wlanIdx << 8 | antennaIdx)
for key, ant := range stats.AntennaStats {
    fmt.Printf("wlan%d ant%d: RSSI=%d/%d/%d SNR=%d/%d/%d pkts=%d\n",
        ant.WlanIdx, ant.Antenna,
        ant.RSSIMin, ant.RSSISum/int64(ant.PacketsReceived), ant.RSSIMax,
        ant.SNRMin, ant.SNRSum/int64(ant.PacketsReceived), ant.SNRMax,
        ant.PacketsReceived)
}
```

The `AntennaStats` struct contains:
- `WlanIdx`, `Antenna` - Interface and antenna indices
- `Freq`, `MCSIndex`, `Bandwidth` - Radio parameters
- `PacketsReceived` - Packet count
- `RSSIMin`, `RSSIMax`, `RSSISum` - RSSI range and sum (for average)
- `SNRMin`, `SNRMax`, `SNRSum` - SNR range and sum (for average)

## Files

| File | Description |
|------|-------------|
| `aggregator.go` | Session, decryption, FEC recovery |
| `receiver.go` | High-level receiver with UDP output |
| `ring.go` | FEC block ring buffer |
| `afpacket.go` | TPACKET_V3 capture with BPF |
| `capture.go` | Capture mode abstraction |
