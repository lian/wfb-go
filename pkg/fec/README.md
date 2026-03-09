# FEC Package

Pure Go implementation of Reed-Solomon Forward Error Correction (FEC) compatible with wfb-ng.

## Overview

This package provides FEC encoding/decoding that produces bit-identical output to wfb-ng's zfex C library. It uses:

- **GF(2^8)** Galois Field arithmetic with primitive polynomial x^8 + x^4 + x^3 + x^2 + 1 (0x11D)
- **Vandermonde matrix** construction matching zfex's algorithm
- **Systematic encoding** where data shards pass through unchanged

## Usage

```go
import "github.com/lian/wfb-go/pkg/fec"

// Create encoder/decoder (k=8 data shards, n=12 total shards)
encoder, err := fec.NewEncoder(8, 12)
decoder, err := fec.NewDecoder(8, 12)
defer encoder.Close()
defer decoder.Close()

// Encode: generate 4 parity shards from 8 data shards
dataShards := make([][]byte, 8)    // fill with data
parityShards := make([][]byte, 4)  // pre-allocate
for i := range parityShards {
    parityShards[i] = make([]byte, shardSize)
}
err = encoder.Encode(dataShards, parityShards)

// Or use EncodeInPlace with a single slice of n shards
shards := make([][]byte, 12)  // first 8 are data, last 4 are parity (pre-allocated)
err = encoder.EncodeInPlace(shards)

// Decode: reconstruct missing data shards
shards := make([][]byte, 12)  // all 12 shards, nil for missing
// ... fill in received shards ...
shards[0] = nil  // simulate loss
shards[1] = nil  // simulate loss
err = decoder.Reconstruct(shards)
// shards[0] and shards[1] are now reconstructed
```

## Performance

Benchmarks on ARM64 (k=8, n=12, 1400 byte shards):

| Implementation | Encode | Reconstruct (1 lost) |
|----------------|--------|----------------------|
| CGO zfex (SIMD) | ~930 MB/s | ~60 GB/s |
| Pure Go | ~790 MB/s | ~60 GB/s |

The pure Go implementation is ~15% slower for encoding but matches CGO for reconstruction. The high reconstruction throughput is due to matrix inversion caching when few shards are lost.

Note: CGO zfex required 16-byte aligned shard sizes due to SIMD. Pure Go handles any shard size.

## Compatibility

The implementation has been verified against the original C zfex library:

- 50+ k,n configurations from (1,2) to (128,255)
- Multiple loss patterns: first N, last N, alternating, random
- Special data patterns: zeros, ones, ascending, descending, random
- Various shard sizes: 1 byte to 4KB+
- Stress testing: 100+ iterations with random parameters

All tests confirmed bit-identical encoding output and successful cross-decoding.

## Files

| File | Description |
|------|-------------|
| `zfex.go` | Encoder and Decoder implementation |
| `gf256.go` | GF(2^8) Galois Field arithmetic tables and operations |
| `encoder_test.go` | Comprehensive test suite |
| `fec_test.go` | API-level tests |

## Testing

```bash
# Run all tests
go test ./pkg/fec/...

# Run with verbose output
go test ./pkg/fec/... -v

# Run benchmarks
go test ./pkg/fec/... -bench=. -benchmem
```

## Technical Details

### Encoding Matrix

The encoding matrix is constructed as:
1. Generate Vandermonde matrix with element (row, col) = α^(row*col)
2. Invert the top k×k submatrix
3. Multiply the remaining rows by the inverse
4. Replace top k×k with identity matrix (systematic encoding)

### GF(2^8) Arithmetic

- Primitive polynomial: x^8 + x^4 + x^3 + x^2 + 1
- Lookup tables for exp, log, inverse, and full multiplication
- Extended exp table (510 elements) for fast multiplication without modulo
