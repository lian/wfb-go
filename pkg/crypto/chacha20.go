// Package crypto provides cryptographic functions for WFB.
// Implements the original ChaCha20-Poly1305 AEAD with 8-byte nonce,
// compatible with libsodium's crypto_aead_chacha20poly1305.
package crypto

import (
	"encoding/binary"
	"math/bits"
)

const (
	// ChaCha20 constants
	chacha20KeySize   = 32
	chacha20NonceSize = 8 // Original variant (not IETF 12-byte)
	chacha20BlockSize = 64

	// State positions
	stateWords = 16
)

// ChaCha20 constants: "expand 32-byte k"
var sigma = [4]uint32{
	0x61707865, // "expa"
	0x3320646e, // "nd 3"
	0x79622d32, // "2-by"
	0x6b206574, // "te k"
}

// chacha20State holds the ChaCha20 cipher state.
type chacha20State struct {
	state [stateWords]uint32
}

// newChaCha20 creates a new ChaCha20 cipher with the original state layout.
// key: 32 bytes, nonce: 8 bytes, counter: initial 64-bit counter value
//
// State layout (original DJB variant):
//
//	[0-3]:   Constants
//	[4-11]:  Key (8 x uint32)
//	[12-13]: Counter (64-bit, little-endian)
//	[14-15]: Nonce (64-bit, little-endian)
func newChaCha20(key, nonce []byte, counter uint64) *chacha20State {
	if len(key) != chacha20KeySize {
		panic("chacha20: invalid key size")
	}
	if len(nonce) != chacha20NonceSize {
		panic("chacha20: invalid nonce size")
	}

	s := &chacha20State{}

	// Constants
	s.state[0] = sigma[0]
	s.state[1] = sigma[1]
	s.state[2] = sigma[2]
	s.state[3] = sigma[3]

	// Key (little-endian)
	s.state[4] = binary.LittleEndian.Uint32(key[0:4])
	s.state[5] = binary.LittleEndian.Uint32(key[4:8])
	s.state[6] = binary.LittleEndian.Uint32(key[8:12])
	s.state[7] = binary.LittleEndian.Uint32(key[12:16])
	s.state[8] = binary.LittleEndian.Uint32(key[16:20])
	s.state[9] = binary.LittleEndian.Uint32(key[20:24])
	s.state[10] = binary.LittleEndian.Uint32(key[24:28])
	s.state[11] = binary.LittleEndian.Uint32(key[28:32])

	// Counter (64-bit, little-endian split into two 32-bit words)
	s.state[12] = uint32(counter)
	s.state[13] = uint32(counter >> 32)

	// Nonce (little-endian)
	s.state[14] = binary.LittleEndian.Uint32(nonce[0:4])
	s.state[15] = binary.LittleEndian.Uint32(nonce[4:8])

	return s
}

// quarterRound performs the ChaCha20 quarter round.
func quarterRound(a, b, c, d uint32) (uint32, uint32, uint32, uint32) {
	a += b
	d ^= a
	d = bits.RotateLeft32(d, 16)

	c += d
	b ^= c
	b = bits.RotateLeft32(b, 12)

	a += b
	d ^= a
	d = bits.RotateLeft32(d, 8)

	c += d
	b ^= c
	b = bits.RotateLeft32(b, 7)

	return a, b, c, d
}

// block generates a 64-byte keystream block.
func (s *chacha20State) block() [chacha20BlockSize]byte {
	// Working state
	var x [stateWords]uint32
	copy(x[:], s.state[:])

	// 20 rounds (10 double-rounds)
	for i := 0; i < 10; i++ {
		// Column rounds
		x[0], x[4], x[8], x[12] = quarterRound(x[0], x[4], x[8], x[12])
		x[1], x[5], x[9], x[13] = quarterRound(x[1], x[5], x[9], x[13])
		x[2], x[6], x[10], x[14] = quarterRound(x[2], x[6], x[10], x[14])
		x[3], x[7], x[11], x[15] = quarterRound(x[3], x[7], x[11], x[15])

		// Diagonal rounds
		x[0], x[5], x[10], x[15] = quarterRound(x[0], x[5], x[10], x[15])
		x[1], x[6], x[11], x[12] = quarterRound(x[1], x[6], x[11], x[12])
		x[2], x[7], x[8], x[13] = quarterRound(x[2], x[7], x[8], x[13])
		x[3], x[4], x[9], x[14] = quarterRound(x[3], x[4], x[9], x[14])
	}

	// Add original state
	for i := 0; i < stateWords; i++ {
		x[i] += s.state[i]
	}

	// Serialize to bytes (little-endian)
	var out [chacha20BlockSize]byte
	for i := 0; i < stateWords; i++ {
		binary.LittleEndian.PutUint32(out[i*4:], x[i])
	}

	// Increment 64-bit counter
	s.state[12]++
	if s.state[12] == 0 {
		s.state[13]++
	}

	return out
}

// xorKeyStream XORs src with the keystream and writes to dst.
func (s *chacha20State) xorKeyStream(dst, src []byte) {
	for len(src) > 0 {
		block := s.block()
		n := len(src)
		if n > chacha20BlockSize {
			n = chacha20BlockSize
		}
		for i := 0; i < n; i++ {
			dst[i] = src[i] ^ block[i]
		}
		dst = dst[n:]
		src = src[n:]
	}
}

// keyStream generates n bytes of keystream.
func (s *chacha20State) keyStream(n int) []byte {
	out := make([]byte, n)
	for i := 0; i < n; {
		block := s.block()
		copied := copy(out[i:], block[:])
		i += copied
	}
	return out
}
