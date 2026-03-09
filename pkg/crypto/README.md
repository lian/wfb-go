# Crypto Package

Pure Go cryptographic primitives compatible with wfb-ng and libsodium.

## Overview

This package provides:

- **ChaCha20-Poly1305 AEAD** - Original 8-byte nonce variant (not IETF 12-byte)
- **X25519 key exchange** - crypto_box compatible for session key encryption
- **Key file handling** - Load/save wfb-ng compatible 64-byte key files
- **Password derivation** - Argon2i key derivation matching wfb-ng

## Why Custom Implementation?

wfb-ng uses the **original** ChaCha20-Poly1305 with 8-byte nonces (DJB spec), not the IETF variant (RFC 8439) with 12-byte nonces. Go's `x/crypto` only provides the IETF variant, so we implement the original.

```
wfb-ng (libsodium)          Go x/crypto
├── 8-byte nonce            ├── 12-byte nonce (IETF)
├── Original DJB spec       ├── RFC 8439
└── Required for compat     └── Incompatible
```

## Usage

### AEAD Encryption/Decryption

```go
import "github.com/lian/wfb-go/pkg/crypto"

// Create AEAD cipher
aead, err := crypto.NewAEAD(sessionKey[:])

// Encrypt (nonce is 8 bytes, typically block_idx<<8 | fragment_idx)
ciphertext := aead.SealNonce(nil, nonce, plaintext, additionalData)

// Decrypt
plaintext, err := aead.OpenNonce(nil, nonce, ciphertext, additionalData)
```

### Key File Operations

```go
// Load TX key (drone.key)
txKey, err := crypto.LoadTXKey("/etc/wfb/drone.key")

// Load RX key (gs.key)
rxKey, err := crypto.LoadRXKey("/etc/wfb/gs.key")

// Generate new keypair
droneKey, gsKey, err := crypto.GenerateWFBKeys()
crypto.SaveKeyFile("drone.key", droneKey[:32], droneKey[32:])
crypto.SaveKeyFile("gs.key", gsKey[:32], gsKey[32:])

// Derive from password (reproducible)
droneKey, gsKey, err := crypto.DeriveKeysFromPassword("secret")
```

### Session Key Exchange (crypto_box)

```go
// Generate session nonce and key
nonce, _ := crypto.GenerateBoxNonce()
sessionKey, _ := crypto.GenerateSessionKey()

// Encrypt session data (TX side)
encrypted := crypto.BoxSeal(sessionData, &nonce, &rxPubKey, &txSecKey)

// Decrypt session data (RX side)
decrypted, ok := crypto.BoxOpen(encrypted, &nonce, &txPubKey, &rxSecKey)
```

## Key File Format

wfb-ng key files are 64 bytes:
- Bytes 0-31: Secret key (X25519 private key)
- Bytes 32-63: Peer's public key

```
drone.key = drone_secret_key (32) + gs_public_key (32)
gs.key    = gs_secret_key (32)    + drone_public_key (32)
```

## Performance

Benchmarks on ARM64 (1500 byte packets):

| Operation | Throughput | Notes |
|-----------|------------|-------|
| Encrypt | 454 MB/s | ~300K packets/sec |
| Decrypt | 452 MB/s | Pure Go, no assembly |
| Poly1305 | 2.8 GB/s | Uses `bits.Mul64` intrinsics |

ChaCha20 is the bottleneck (~84% of AEAD time). The pure Go implementation is ~2.7x slower than Go's assembly-optimized IETF variant, but provides ~100x headroom for typical video workloads.

## Files

| File | Description |
|------|-------------|
| `aead.go` | ChaCha20-Poly1305 AEAD implementation |
| `chacha20.go` | ChaCha20 stream cipher (8-byte nonce) |
| `poly1305.go` | Poly1305 MAC |
| `keys.go` | Key generation, loading, and crypto_box |
