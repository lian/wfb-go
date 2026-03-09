# wfb_tx_cmd - TX Control

Runtime control of wfb_tx parameters without restarting. Connects to a running wfb_tx instance via its control port.

## Usage

```
wfb_tx_cmd <port> <command> [options]
```

## Commands

| Command | Description |
|---------|-------------|
| `set_fec` | Set FEC parameters |
| `get_fec` | Get current FEC parameters |
| `set_radio` | Set radio parameters |
| `get_radio` | Get current radio parameters |

## FEC Options

| Flag | Description |
|------|-------------|
| `-k` | FEC data shards |
| `-n` | FEC total shards |

## Radio Options

| Flag | Description |
|------|-------------|
| `-M` | MCS index |
| `-B` | Bandwidth (20/40/80) |
| `-G` | Guard interval (short/long) |
| `-S` | STBC streams |
| `-L` | LDPC (0/1) |

## Examples

```bash
# Set FEC to 4/8 (50% overhead, smaller blocks)
wfb_tx_cmd 8000 set_fec -k 4 -n 8

# Get current FEC settings
wfb_tx_cmd 8000 get_fec

# Increase MCS for better throughput
wfb_tx_cmd 8000 set_radio -M 3

# Set 40MHz bandwidth
wfb_tx_cmd 8000 set_radio -B 40

# Get current radio settings
wfb_tx_cmd 8000 get_radio
```

## Notes

- Requires wfb_tx started with `-C <port>` to enable control
- Changes take effect immediately
- Used by adaptive link for dynamic adjustments

## See Also

- [wfb_tx](../wfb_tx/README.md) - Transmitter
