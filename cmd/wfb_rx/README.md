# wfb_rx - Receiver

Captures packets from WiFi, decrypts, performs FEC recovery, and outputs to UDP/Unix socket.

## Usage

```
wfb_rx [options] interface1 [interface2] ...
```

## Options

| Flag | Description | Default |
|------|-------------|---------|
| `-K` | RX keypair path | `gs.key` |
| `-c` | Client address for output | `127.0.0.1` |
| `-u` | Client port for output | 5600 |
| `-U` | Unix socket path (alternative to UDP) | |
| `-i` | Link ID (24-bit) | 0 |
| `-p` | Radio port / stream number | 0 |
| `-e` | Minimum session epoch | 0 |
| `-l` | Stats log interval [ms] | 1000 |
| `-R` | UDP receive buffer size | system default |
| `-s` | UDP send buffer size | system default |
| `--capture-mode` | Capture mode: dedicated, shared, or libpcap | dedicated |

## Output

Periodic stats are logged showing:
- Packet counts (all, unique, session, data)
- FEC recovery stats
- Per-antenna RSSI/SNR (min/avg/max)

```
RX: all=100 uniq=90 session=1 data=89 dec_err=0 bad=0 fec_rec=2 lost=0 ...
  ANT[0:0] freq=5180 mcs=3 bw=20 pkts=50 RSSI=-45/-42/-38 SNR=15/18/22
```

## Examples

```bash
# Basic usage - receive from wlan0, output to UDP 5600
wfb_rx -K gs.key -c 127.0.0.1 -u 5600 wlan0

# Multi-interface diversity reception
wfb_rx -K gs.key wlan0 wlan1

# Output to Unix socket
wfb_rx -K gs.key -U /tmp/video.sock wlan0
```

## See Also

- [wfb_tx](../wfb_tx/README.md) - Transmitter
- [pkg/rx](../../pkg/rx/README.md) - RX package documentation
