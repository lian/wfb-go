package crypto

// Poly1305 implementation adapted from golang.org/x/crypto/internal/poly1305
// Uses 64-bit limbs for better performance on 64-bit architectures.

import (
	"encoding/binary"
	"math/bits"
)

const (
	poly1305KeySize = 32
	poly1305TagSize = 16
)

// [rMask0, rMask1] is the specified Poly1305 clamping mask in little-endian.
const (
	rMask0 = 0x0FFFFFFC0FFFFFFF
	rMask1 = 0x0FFFFFFC0FFFFFFC
)

const (
	maskLow2Bits    uint64 = 0x0000000000000003
	maskNotLow2Bits uint64 = ^maskLow2Bits
)

// [p0, p1, p2] is 2¹³⁰ - 5 in little endian order.
const (
	p0 = 0xFFFFFFFFFFFFFFFB
	p1 = 0xFFFFFFFFFFFFFFFF
	p2 = 0x0000000000000003
)

// macState holds numbers in saturated 64-bit little-endian limbs.
type macState struct {
	h [3]uint64 // accumulator
	r [2]uint64 // r (clamped)
	s [2]uint64 // s (final addition)
}

// poly1305MAC implements the Poly1305 message authentication code.
type poly1305MAC struct {
	macState
	buffer [poly1305TagSize]byte
	offset int
}

// newPoly1305 creates a new Poly1305 MAC with the given 32-byte key.
func newPoly1305(key []byte) *poly1305MAC {
	if len(key) != poly1305KeySize {
		panic("poly1305: invalid key size")
	}
	m := &poly1305MAC{}
	m.r[0] = binary.LittleEndian.Uint64(key[0:8]) & rMask0
	m.r[1] = binary.LittleEndian.Uint64(key[8:16]) & rMask1
	m.s[0] = binary.LittleEndian.Uint64(key[16:24])
	m.s[1] = binary.LittleEndian.Uint64(key[24:32])
	return m
}

// update processes message bytes.
func (m *poly1305MAC) update(msg []byte) {
	// Handle buffered data first
	if m.offset > 0 {
		n := copy(m.buffer[m.offset:], msg)
		if m.offset+n < poly1305TagSize {
			m.offset += n
			return
		}
		msg = msg[n:]
		m.offset = 0
		m.processBlock(m.buffer[:], true)
	}

	// Process full blocks
	for len(msg) >= poly1305TagSize {
		m.processBlock(msg[:poly1305TagSize], true)
		msg = msg[poly1305TagSize:]
	}

	// Buffer remaining
	if len(msg) > 0 {
		m.offset = copy(m.buffer[:], msg)
	}
}

// processBlock processes a single 16-byte block.
// fullBlock should be true for full 16-byte blocks, false for the final partial block.
func (m *poly1305MAC) processBlock(block []byte, fullBlock bool) {
	h0, h1, h2 := m.h[0], m.h[1], m.h[2]
	r0, r1 := m.r[0], m.r[1]

	var c uint64

	if fullBlock && len(block) == poly1305TagSize {
		// Full block: h += block with hibit
		h0, c = bits.Add64(h0, binary.LittleEndian.Uint64(block[0:8]), 0)
		h1, c = bits.Add64(h1, binary.LittleEndian.Uint64(block[8:16]), c)
		h2 += c + 1 // hibit
	} else {
		// Partial block: pad with 0x01 and zeros, no hibit
		var buf [poly1305TagSize]byte
		copy(buf[:], block)
		buf[len(block)] = 1

		h0, c = bits.Add64(h0, binary.LittleEndian.Uint64(buf[0:8]), 0)
		h1, c = bits.Add64(h1, binary.LittleEndian.Uint64(buf[8:16]), c)
		h2 += c
	}

	// h *= r
	h0r0 := mul64(h0, r0)
	h1r0 := mul64(h1, r0)
	h2r0 := mul64(h2, r0)
	h0r1 := mul64(h0, r1)
	h1r1 := mul64(h1, r1)
	h2r1 := mul64(h2, r1)

	m0 := h0r0
	m1 := add128(h1r0, h0r1)
	m2 := add128(h2r0, h1r1)
	m3 := h2r1

	t0 := m0.lo
	t1, c := bits.Add64(m1.lo, m0.hi, 0)
	t2, c := bits.Add64(m2.lo, m1.hi, c)
	t3, _ := bits.Add64(m3.lo, m2.hi, c)

	// Partial reduction: h mod 2^130-5
	h0, h1, h2 = t0, t1, t2&maskLow2Bits
	cc := uint128{t2 & maskNotLow2Bits, t3}

	h0, c = bits.Add64(h0, cc.lo, 0)
	h1, c = bits.Add64(h1, cc.hi, c)
	h2 += c

	cc = shiftRightBy2(cc)

	h0, c = bits.Add64(h0, cc.lo, 0)
	h1, c = bits.Add64(h1, cc.hi, c)
	h2 += c

	m.h[0], m.h[1], m.h[2] = h0, h1, h2
}

// finish computes the final MAC tag.
func (m *poly1305MAC) finish() [poly1305TagSize]byte {
	// Process any remaining buffered data as partial block
	if m.offset > 0 {
		m.processBlock(m.buffer[:m.offset], false)
	}

	h0, h1, h2 := m.h[0], m.h[1], m.h[2]

	// Final reduction: h mod 2^130-5
	// Compute h - p, select h if underflow
	hMinusP0, b := bits.Sub64(h0, p0, 0)
	hMinusP1, b := bits.Sub64(h1, p1, b)
	_, b = bits.Sub64(h2, p2, b)

	h0 = select64(b, h0, hMinusP0)
	h1 = select64(b, h1, hMinusP1)

	// tag = h + s mod 2^128
	h0, c := bits.Add64(h0, m.s[0], 0)
	h1, _ = bits.Add64(h1, m.s[1], c)

	var tag [poly1305TagSize]byte
	binary.LittleEndian.PutUint64(tag[0:8], h0)
	binary.LittleEndian.PutUint64(tag[8:16], h1)
	return tag
}

// Helper types and functions

type uint128 struct {
	lo, hi uint64
}

func mul64(a, b uint64) uint128 {
	hi, lo := bits.Mul64(a, b)
	return uint128{lo, hi}
}

func add128(a, b uint128) uint128 {
	lo, c := bits.Add64(a.lo, b.lo, 0)
	hi, _ := bits.Add64(a.hi, b.hi, c)
	return uint128{lo, hi}
}

func shiftRightBy2(a uint128) uint128 {
	return uint128{
		lo: a.lo>>2 | (a.hi&3)<<62,
		hi: a.hi >> 2,
	}
}

func select64(v, x, y uint64) uint64 {
	return ^(v-1)&x | (v-1)&y
}

// poly1305Sum computes the Poly1305 MAC of msg using the given key.
func poly1305Sum(msg, key []byte) [poly1305TagSize]byte {
	p := newPoly1305(key)
	p.update(msg)
	return p.finish()
}
