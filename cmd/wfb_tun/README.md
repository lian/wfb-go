# wfb_tun - IP Tunnel (Standalone)

Standalone TUN tunnel for use with wfb_tx/wfb_rx. Creates a TUN device for bidirectional IP traffic over the WFB link.

**Note:** When using `wfb_server`, use the built-in `tunnel` service type instead - it handles TUN creation and IP configuration automatically without needing this tool.

## Usage

```
wfb_tun [options]
```

## Options

| Flag | Description | Default |
|------|-------------|---------|
| `-t` | TUN interface name | `wfb-tun` |
| `-a` | TUN interface address (CIDR) | `10.5.0.2/24` |
| `-c` | Peer UDP address | `127.0.0.1` |
| `-u` | Peer UDP port (send to) | `5801` |
| `-l` | Local UDP listen port | `5800` |
| `-T` | Aggregation timeout in ms (0=disabled) | `5` |

## How It Works

The tunnel:
1. Creates a TUN device with the specified name
2. Automatically configures IP address and MTU (no manual `ip` commands needed)
3. Brings up the interface
4. Aggregates small packets to reduce radio overhead
5. Sends/receives via UDP to/from wfb_tx/wfb_rx
6. Sends keepalive pings every 500ms

## Examples

### Drone Side (10.5.0.10)
```bash
wfb_tun -a 10.5.0.10/24 -u 5801 -l 5800
```

### Ground Station Side (10.5.0.1)
```bash
wfb_tun -a 10.5.0.1/24 -u 5800 -l 5801
```

The ports are crossed: drone listens on 5800/sends to 5801, GS listens on 5801/sends to 5800.

### Connect to Drone
```bash
# From GS
ssh user@10.5.0.10
ping 10.5.0.10
```

## Packet Aggregation

Small packets (SSH keystrokes, etc.) are aggregated before transmission to reduce radio overhead. The `-T` flag controls the aggregation timeout:
- `-T 5` (default): Wait up to 5ms to aggregate packets
- `-T 0`: Disable aggregation, send immediately

## Using wfb_server Instead

For most setups, use wfb_server's built-in tunnel service which handles everything in-process (no UDP, no separate wfb_tun process):

```yaml
# Drone config
streams:
  tunnel:
    service_type: tunnel
    stream_tx: 0x20              # WFB stream ID for TX (drone -> GS)
    stream_rx: 0xA0              # WFB stream ID for RX (GS -> drone)
    fec: [1, 2]
    tunnel:
      ifname: wfb-tunnel         # TUN interface name
      ifaddr: 10.5.0.10/24       # Drone IP
```

```yaml
# GS config (stream IDs swapped)
streams:
  tunnel:
    service_type: tunnel
    stream_rx: 0x20              # Receive from drone
    stream_tx: 0xA0              # Send to drone
    fec: [1, 2]
    tunnel:
      ifname: wfb-tunnel
      ifaddr: 10.5.0.1/24        # GS IP
```

wfb_server creates the TUN device directly - no UDP ports involved.

## Notes

- Requires root privileges for TUN device creation
- MTU is set automatically to 1443 bytes

## See Also

- [wfb_server](../wfb_server/README.md) - Recommended: runs tunnel as built-in service
