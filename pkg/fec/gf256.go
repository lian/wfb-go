// Package fec provides Reed-Solomon FEC encoding/decoding.
// This file implements GF(2^8) Galois Field arithmetic compatible with zfex.
package fec

// Galois Field GF(2^8) with primitive polynomial x^8 + x^4 + x^3 + x^2 + 1 (0x11D).
// This matches the zfex library used by wfb-ng.

// gfExp is the exponent table: gfExp[i] = α^i where α is the primitive element.
// Extended to 510 elements for fast multiplication without modulo.
var gfExp [510]byte

// gfLog is the log table: gfLog[α^i] = i. gfLog[0] = 255 (undefined).
var gfLog [256]int

// gfInverse is the multiplicative inverse table.
var gfInverse [256]byte

// gfMulTable is the full multiplication table for fast lookups.
var gfMulTable [256][256]byte

// gfInitialized tracks if tables have been generated.
var gfInitialized bool

// initGF generates all GF(2^8) lookup tables.
func initGF() {
	if gfInitialized {
		return
	}

	// Primitive polynomial: x^8 + x^4 + x^3 + x^2 + 1
	// Represented as "101110001" in zfex (Pp string)
	// String index: 0='1', 1='0', 2='1', 3='1', 4='1', 5='0', 6='0', 7='0', 8='1'
	// Pp[i] = '1' for i in {0, 2, 3, 4} (ignoring bit 8 which is handled implicitly)
	pp := []int{0, 2, 3, 4} // positions of '1' in lower 8 bits

	// Generate gfExp and gfLog tables
	var mask byte = 1
	gfExp[8] = 0 // will be updated

	// First 8 powers are simply bits shifted left
	for i := 0; i < 8; i++ {
		gfExp[i] = mask
		gfLog[gfExp[i]] = i

		// If Pp[i] == '1', add to gfExp[8]
		for _, p := range pp {
			if p == i {
				gfExp[8] ^= mask
				break
			}
		}
		mask <<= 1
	}
	gfLog[gfExp[8]] = 8

	// Generate remaining powers
	mask = 1 << 7
	for i := 9; i < 255; i++ {
		if gfExp[i-1] >= mask {
			gfExp[i] = gfExp[8] ^ ((gfExp[i-1] ^ mask) << 1)
		} else {
			gfExp[i] = gfExp[i-1] << 1
		}
		gfLog[gfExp[i]] = i
	}

	// log(0) is undefined, use special value
	gfLog[0] = 255

	// Extended exp table for fast multiply
	for i := 0; i < 255; i++ {
		gfExp[i+255] = gfExp[i]
	}

	// Generate inverse table
	gfInverse[0] = 0
	gfInverse[1] = 1
	for i := 2; i <= 255; i++ {
		gfInverse[i] = gfExp[255-gfLog[i]]
	}

	// Generate multiplication table
	for i := 0; i < 256; i++ {
		for j := 0; j < 256; j++ {
			if i == 0 || j == 0 {
				gfMulTable[i][j] = 0
			} else {
				gfMulTable[i][j] = gfExp[modnn(gfLog[i]+gfLog[j])]
			}
		}
	}

	gfInitialized = true
}

// modnn computes x % 255 efficiently without division.
func modnn(x int) int {
	for x >= 255 {
		x -= 255
		x = (x >> 8) + (x & 255)
	}
	return x
}

// gfMul multiplies two GF(2^8) elements.
func gfMul(a, b byte) byte {
	return gfMulTable[a][b]
}

// gfAdd adds two GF(2^8) elements (XOR).
func gfAdd(a, b byte) byte {
	return a ^ b
}

// gfInv returns the multiplicative inverse of a GF(2^8) element.
func gfInv(a byte) byte {
	return gfInverse[a]
}

// gfAddMul computes dst ^= c * src for each byte.
func gfAddMul(dst, src []byte, c byte) {
	if c == 0 {
		return
	}
	mulRow := &gfMulTable[c]
	for i := range dst {
		dst[i] ^= mulRow[src[i]]
	}
}
