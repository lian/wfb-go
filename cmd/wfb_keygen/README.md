# wfb_keygen - Key Generator

Generates matched keypairs for drone and ground station. Keys are compatible with wfb-ng.

## Usage

```
wfb_keygen [options]
```

## Options

| Flag | Description | Default |
|------|-------------|---------|
| `-o` | Output directory for key files | `.` |
| `-p` | Derive keys from password (Argon2i) | random |
| `-hex` | Also print keys in hex format | false |

## Output Files

- `drone.key` - Use with wfb_tx on the drone
- `gs.key` - Use with wfb_rx on the ground station

Both files are 64 bytes (same format as wfb-ng).

## Examples

```bash
# Generate random keypairs in /etc/wfb
wfb_keygen -o /etc/wfb

# Derive keys from password (reproducible across devices)
wfb_keygen -o /etc/wfb -p "your-secret-password"

# Print hex representation
wfb_keygen -o . -hex
```

## Security Notes

- Password-derived keys use Argon2i with fixed salt for reproducibility
- Random keys are generated using crypto/rand
- Keys are X25519 keypairs for session key exchange
