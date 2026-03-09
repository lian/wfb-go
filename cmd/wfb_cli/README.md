# wfb_cli - Stats Viewer

Real-time statistics display from a running wfb_server. Shows per-antenna RSSI/SNR, packet rates, FEC recovery, and more.

## Usage

```
wfb_cli [options] [profile]
```

## Options

| Flag | Description | Default |
|------|-------------|---------|
| `-host` | WFB server host | `127.0.0.1` |
| `-port` | Stats port (MsgPack API) | 8003 |

## Display

Shows live updating stats including:
- Per-antenna signal quality (RSSI, SNR)
- Packet rates (RX/TX per second)
- FEC recovery statistics
- Session information

## Examples

```bash
# Connect to local wfb_server
wfb_cli

# Connect to remote server
wfb_cli -host 192.168.1.100

# Connect to specific port
wfb_cli -port 8002
```

## See Also

- [wfb_server](../wfb_server/README.md) - Service orchestrator
