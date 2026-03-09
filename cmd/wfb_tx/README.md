# wfb_tx - Transmitter

Receives UDP/Unix socket data and transmits over WiFi with FEC encoding and encryption.

## Usage

```
wfb_tx [options] interface1 [interface2] ...
```

## Options

| Flag | Description | Default |
|------|-------------|---------|
| `-K` | TX keypair path | `drone.key` |
| `-k` | FEC data shards | 8 |
| `-n` | FEC total shards | 12 |
| `-u` | UDP listen port | 5600 |
| `-U` | Unix socket path (alternative to UDP) | |
| `-M` | MCS index | 1 |
| `-B` | Bandwidth 20/40/80 MHz | 20 |
| `-G` | Guard interval short/long | long |
| `-S` | STBC streams 0-2 | 0 |
| `-L` | LDPC 0=off, 1=on | 0 |
| `-i` | Link ID (24-bit) | 0 |
| `-p` | Radio port / stream number | 0 |
| `-e` | Session epoch | 0 |
| `-m` | Mirror mode (send on all interfaces) | false |
| `-C` | Control port for wfb_tx_cmd (0=disabled) | 0 |
| `-F` | FEC delay between packets [us] | 0 |
| `-T` | FEC timeout [ms] (0=disabled) | 0 |
| `-P` | fwmark for traffic shaping | 0 |

## Examples

```bash
# Basic usage - receive video on UDP 5600, transmit on wlan0
wfb_tx -K drone.key -u 5600 wlan0

# With FEC 8/12 and MCS 3
wfb_tx -K drone.key -k 8 -n 12 -M 3 wlan0

# Enable runtime control on port 8000
wfb_tx -K drone.key -C 8000 wlan0

# Mirror mode - transmit on multiple interfaces
wfb_tx -K drone.key -m wlan0 wlan1
```

## See Also

- [wfb_tx_cmd](../wfb_tx_cmd/README.md) - Runtime control of TX parameters
- [wfb_rx](../wfb_rx/README.md) - Receiver
