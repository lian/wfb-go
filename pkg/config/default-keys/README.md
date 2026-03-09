# Default Keys

These are the default WFB keys shipped with standard VTX firmwares (OpenIPC, etc.).

**WARNING:** These keys are public and provide no security. Generate your own keys for production use.

## Files

- `drone.key` - Default key for drone/TX side
- `gs.key` - Default key for ground station/RX side

## Base64 Format

For use with `key_base64` in config files:

```
u7ftboOkaoqbihKg+Y7OK9yXhwW4IEcBsghfooyse0YOBcSKYZX7cJIcdHpm6DwC5kC9a761slFTepiidBaiYw==
```

## Generating Custom Keys

Generate a new keypair:

```bash
wfb_keygen -o /path/to/keys/
```

Or derive from a password (reproducible across devices):

```bash
wfb_keygen -o /path/to/keys/ -p "your-secret-password"
```
