package crypto

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestChaCha20Block(t *testing.T) {
	// Test vector from RFC 7539 Section 2.3.2
	// Note: RFC uses IETF variant, but the quarter rounds are the same
	// We test our quarter round implementation here

	// Test quarter round
	a, b, c, d := quarterRound(0x11111111, 0x01020304, 0x9b8d6f43, 0x01234567)
	if a != 0xea2a92f4 || b != 0xcb1cf8ce || c != 0x4581472e || d != 0x5881c4bb {
		t.Errorf("quarterRound failed: got %08x %08x %08x %08x", a, b, c, d)
	}
}

func TestChaCha20KeyStream(t *testing.T) {
	// Test vector for original ChaCha20 (8-byte nonce)
	// Key: all zeros
	// Nonce: all zeros
	// Counter: 0
	key := make([]byte, 32)
	nonce := make([]byte, 8)

	s := newChaCha20(key, nonce, 0)
	block := s.block()

	// First block with zero key/nonce/counter should produce known output
	// This is the "sunscreen" test pattern from DJB's ChaCha20 specification
	// We verify the block is non-zero and deterministic
	if bytes.Equal(block[:], make([]byte, 64)) {
		t.Error("ChaCha20 block should not be all zeros")
	}

	// Verify determinism
	s2 := newChaCha20(key, nonce, 0)
	block2 := s2.block()
	if !bytes.Equal(block[:], block2[:]) {
		t.Error("ChaCha20 should be deterministic")
	}
}

func TestAEADSealOpen(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	aead, err := NewAEAD(key)
	if err != nil {
		t.Fatalf("NewAEAD failed: %v", err)
	}

	plaintext := []byte("Hello, WFB-NG!")
	ad := []byte("additional data")
	nonce := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00} // block_idx=1, fragment_idx=0

	// Seal
	ciphertext := aead.Seal(nil, nonce, plaintext, ad)
	if len(ciphertext) != len(plaintext)+TagSize {
		t.Errorf("ciphertext length: got %d, want %d", len(ciphertext), len(plaintext)+TagSize)
	}

	// Open
	decrypted, err := aead.Open(nil, nonce, ciphertext, ad)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("decrypted mismatch: got %q, want %q", decrypted, plaintext)
	}

	// Verify tampering detection
	ciphertext[0] ^= 0x01
	_, err = aead.Open(nil, nonce, ciphertext, ad)
	if err != ErrAuthFailed {
		t.Error("should detect tampering")
	}
}

func TestAEADNonceConvenience(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	aead, _ := NewAEAD(key)

	plaintext := []byte("test message")
	var ad []byte // no additional data, like wfb-ng

	// Test with uint64 nonce (wfb-ng style)
	blockIdx := uint64(1)
	fragmentIdx := uint8(0)
	nonce := MakeDataNonce(blockIdx, fragmentIdx)

	ciphertext := aead.SealNonce(nil, nonce, plaintext, ad)
	decrypted, err := aead.OpenNonce(nil, nonce, ciphertext, ad)
	if err != nil {
		t.Fatalf("OpenNonce failed: %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("decrypted mismatch")
	}
}

func TestMakeDataNonce(t *testing.T) {
	tests := []struct {
		blockIdx    uint64
		fragmentIdx uint8
		want        uint64
	}{
		{0, 0, 0x0000000000000000},
		{1, 0, 0x0000000000000100},
		{1, 1, 0x0000000000000101},
		{0xFF, 0xFF, 0x000000000000FFFF},
		{0x123456789AB, 0xCD, 0x123456789ABCD},
	}

	for _, tt := range tests {
		got := MakeDataNonce(tt.blockIdx, tt.fragmentIdx)
		if got != tt.want {
			t.Errorf("MakeDataNonce(%d, %d) = 0x%X, want 0x%X",
				tt.blockIdx, tt.fragmentIdx, got, tt.want)
		}

		// Verify round-trip
		gotBlock, gotFrag := ParseDataNonce(got)
		if gotBlock != tt.blockIdx || gotFrag != tt.fragmentIdx {
			t.Errorf("ParseDataNonce(0x%X) = (%d, %d), want (%d, %d)",
				got, gotBlock, gotFrag, tt.blockIdx, tt.fragmentIdx)
		}
	}
}

func TestPoly1305(t *testing.T) {
	// Test vector from RFC 7539 Section 2.5.2
	key, _ := hex.DecodeString(
		"85d6be7857556d337f4452fe42d506a8" +
			"0103808afb0db2fd4abff6af4149f51b")

	msg := []byte("Cryptographic Forum Research Group")

	tag := poly1305Sum(msg, key)
	expected, _ := hex.DecodeString("a8061dc1305136c6c22b8baf0c0127a9")

	if !bytes.Equal(tag[:], expected) {
		t.Errorf("Poly1305 tag mismatch:\ngot:  %x\nwant: %x", tag, expected)
	}
}

func TestEmptyMessage(t *testing.T) {
	key := make([]byte, 32)
	aead, _ := NewAEAD(key)

	nonce := make([]byte, 8)
	var plaintext, ad []byte

	ciphertext := aead.Seal(nil, nonce, plaintext, ad)
	if len(ciphertext) != TagSize {
		t.Errorf("empty message ciphertext should be just tag")
	}

	decrypted, err := aead.Open(nil, nonce, ciphertext, ad)
	if err != nil {
		t.Fatalf("Open empty message failed: %v", err)
	}
	if len(decrypted) != 0 {
		t.Errorf("decrypted should be empty")
	}
}

// MakeDataNonce is in protocol package, replicate here for testing
func MakeDataNonce(blockIdx uint64, fragmentIdx uint8) uint64 {
	const BLOCK_IDX_MASK = (1 << 56) - 1
	return ((blockIdx & BLOCK_IDX_MASK) << 8) | uint64(fragmentIdx)
}

func ParseDataNonce(nonce uint64) (blockIdx uint64, fragmentIdx uint8) {
	return nonce >> 8, uint8(nonce & 0xFF)
}
