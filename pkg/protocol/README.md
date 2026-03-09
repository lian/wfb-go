# Protocol Package

Defines WFB-NG packet structures and constants.

## Overview

This package provides:

- Wire protocol constants (packet types, sizes, limits)
- Packet header serialization/deserialization
- Channel ID encoding/decoding
- Stream ID allocation conventions

Reference: [wfb-ng-std-draft.md](https://github.com/svpcom/wfb-ng/blob/master/doc/wfb-ng-std-draft.md)

## Packet Types

| Type | Value | Description |
|------|-------|-------------|
| `WFB_PACKET_DATA` | 0x01 | Encrypted data packet |
| `WFB_PACKET_SESSION` | 0x02 | Session key announcement |

## Packet Structure

### Data Packet

```
[Radiotap Header]
[IEEE 802.11 Header (24 bytes)]
[packet_type (1)]
[data_nonce (8)]           ← (block_idx << 8) | fragment_idx
[encrypted payload]
  [flags (1)]
  [packet_size (2)]        ← Big-endian, actual payload size
  [payload...]
  [Poly1305 tag (16)]
```

### Session Packet

```
[Radiotap Header]
[IEEE 802.11 Header (24 bytes)]
[packet_type (1)]
[session_nonce (24)]       ← Random, for crypto_box
[encrypted payload]
  [epoch (8)]              ← Reject old sessions
  [channel_id (4)]         ← (link_id << 8) | port
  [fec_type (1)]           ← 0x01 = Reed-Solomon
  [k (1)]                  ← FEC data shards
  [n (1)]                  ← FEC total shards
  [session_key (32)]       ← ChaCha20-Poly1305 key
  [crypto_box tag (16)]
```

## Channel ID

```go
channel_id = (link_id << 8) | port

// link_id: 24-bit identifier derived from link domain
// port:    8-bit stream number (0-255)

channelID := protocol.MakeChannelID(linkID, port)
linkID, port := protocol.ParseChannelID(channelID)
```

## Stream ID Allocation

| Range | Direction | Use |
|-------|-----------|-----|
| 0-15 | Downlink | Video |
| 16-31 | Downlink | Telemetry (MAVLink) |
| 32-47 | Downlink | Tunnel |
| 48-63 | Downlink | MSP/custom |
| 128-143 | Uplink | Video (rare) |
| 144-159 | Uplink | Telemetry |
| 160-175 | Uplink | Tunnel |
| 176-191 | Uplink | MSP/custom |

## Size Limits

| Constant | Value | Description |
|----------|-------|-------------|
| `WIFI_MTU` | 4045 | Max injected packet size |
| `MAX_PAYLOAD_SIZE` | 3993 | Max user payload per packet |
| `MAX_FEC_PAYLOAD` | 3996 | Max FEC shard size |
| `IEEE80211_HDR_SIZE` | 24 | 802.11 header size |

## Usage

```go
import "github.com/lian/wfb-go/pkg/protocol"

// Create channel ID
channelID := protocol.MakeChannelID(0x123456, 0) // video stream

// Create data nonce
nonce := protocol.MakeDataNonce(blockIdx, fragmentIdx)

// Parse received nonce
blockIdx, fragmentIdx := protocol.ParseDataNonce(nonce)

// Marshal session data
sessionData := &protocol.SessionData{
    Epoch:      1,
    ChannelID:  channelID,
    FECType:    protocol.WFB_FEC_VDM_RS,
    K:          8,
    N:          12,
    SessionKey: sessionKey,
}
data := sessionData.Marshal()
```

## Files

| File | Description |
|------|-------------|
| `constants.go` | Protocol constants and stream allocation |
| `packets.go` | Packet header structures and marshaling |
| `command.go` | TX control command protocol |
