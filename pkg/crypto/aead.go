package crypto

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"errors"

	"golang.org/x/crypto/blake2b"
)

const (
	// KeySize is the size of the ChaCha20-Poly1305 key.
	KeySize = 32
	// NonceSize is the size of the nonce (8 bytes for original variant).
	NonceSize = 8
	// TagSize is the size of the Poly1305 authentication tag.
	TagSize = 16
)

var (
	ErrAuthFailed = errors.New("chacha20poly1305: authentication failed")
	ErrInvalidKey = errors.New("chacha20poly1305: invalid key size")
)

// AEAD implements the original ChaCha20-Poly1305 AEAD with 8-byte nonce.
// This is compatible with libsodium's crypto_aead_chacha20poly1305.
type AEAD struct {
	key [KeySize]byte
}

// NewAEAD creates a new ChaCha20-Poly1305 AEAD cipher.
func NewAEAD(key []byte) (*AEAD, error) {
	if len(key) != KeySize {
		return nil, ErrInvalidKey
	}
	a := &AEAD{}
	copy(a.key[:], key)
	return a, nil
}

// Seal encrypts and authenticates plaintext with additional data.
// The nonce must be 8 bytes and unique for each message with the same key.
// Returns ciphertext || tag.
//
// Uses the ORIGINAL ChaCha20-Poly1305 construction (NaCl/libsodium), NOT IETF RFC 8439.
// Tag is computed as: Poly1305(key, ad || le64(adlen) || ct || le64(ctlen))
func (a *AEAD) Seal(dst, nonce, plaintext, additionalData []byte) []byte {
	if len(nonce) != NonceSize {
		panic("chacha20poly1305: invalid nonce size")
	}

	// Allocate output: ciphertext + tag
	ret := make([]byte, len(plaintext)+TagSize)
	if dst != nil {
		ret = append(dst[:0], make([]byte, len(plaintext)+TagSize)...)
	}

	ciphertext := ret[:len(plaintext)]
	tag := ret[len(plaintext):]

	// Generate Poly1305 key using first block (counter=0)
	chacha := newChaCha20(a.key[:], nonce, 0)
	polyKey := chacha.keyStream(32)

	// Encrypt plaintext with counter starting at 1
	chacha = newChaCha20(a.key[:], nonce, 1)
	chacha.xorKeyStream(ciphertext, plaintext)

	// Compute Poly1305 tag using ORIGINAL libsodium construction:
	// ad || le64(adlen) || ciphertext || le64(ctlen)
	p := newPoly1305(polyKey)

	// AD data
	p.update(additionalData)

	// Length of AD as 64-bit little-endian
	var adLen [8]byte
	binary.LittleEndian.PutUint64(adLen[:], uint64(len(additionalData)))
	p.update(adLen[:])

	// Ciphertext
	p.update(ciphertext)

	// Length of ciphertext as 64-bit little-endian
	var ctLen [8]byte
	binary.LittleEndian.PutUint64(ctLen[:], uint64(len(ciphertext)))
	p.update(ctLen[:])

	computedTag := p.finish()
	copy(tag, computedTag[:])

	return ret
}

// Open decrypts and verifies ciphertext with additional data.
// The ciphertext must include the tag (ciphertext || tag).
// Returns plaintext or error if authentication fails.
//
// Uses the ORIGINAL ChaCha20-Poly1305 construction (NaCl/libsodium), NOT IETF RFC 8439.
// Tag is verified as: Poly1305(key, ad || le64(adlen) || ct || le64(ctlen))
func (a *AEAD) Open(dst, nonce, ciphertext, additionalData []byte) ([]byte, error) {
	if len(nonce) != NonceSize {
		panic("chacha20poly1305: invalid nonce size")
	}
	if len(ciphertext) < TagSize {
		return nil, ErrAuthFailed
	}

	// Split ciphertext and tag
	tag := ciphertext[len(ciphertext)-TagSize:]
	ciphertext = ciphertext[:len(ciphertext)-TagSize]

	// Generate Poly1305 key using first block (counter=0)
	chacha := newChaCha20(a.key[:], nonce, 0)
	polyKey := chacha.keyStream(32)

	// Compute expected tag using ORIGINAL libsodium construction:
	// ad || le64(adlen) || ciphertext || le64(ctlen)
	p := newPoly1305(polyKey)

	// AD data
	p.update(additionalData)

	// Length of AD as 64-bit little-endian
	var adLen [8]byte
	binary.LittleEndian.PutUint64(adLen[:], uint64(len(additionalData)))
	p.update(adLen[:])

	// Ciphertext
	p.update(ciphertext)

	// Length of ciphertext as 64-bit little-endian
	var ctLen [8]byte
	binary.LittleEndian.PutUint64(ctLen[:], uint64(len(ciphertext)))
	p.update(ctLen[:])

	expectedTag := p.finish()

	// Constant-time comparison
	if subtle.ConstantTimeCompare(tag, expectedTag[:]) != 1 {
		return nil, ErrAuthFailed
	}

	// Decrypt into dst if provided and large enough, otherwise allocate
	var plaintext []byte
	if dst != nil && cap(dst) >= len(ciphertext) {
		plaintext = dst[:len(ciphertext)]
	} else {
		plaintext = make([]byte, len(ciphertext))
	}

	chacha = newChaCha20(a.key[:], nonce, 1)
	chacha.xorKeyStream(plaintext, ciphertext)

	return plaintext, nil
}

// SealNonce is a convenience function that takes the nonce as a uint64.
// Nonce format: (block_idx << 8) | fragment_idx.
func (a *AEAD) SealNonce(dst []byte, nonce uint64, plaintext, additionalData []byte) []byte {
	var nonceBytes [NonceSize]byte
	binary.BigEndian.PutUint64(nonceBytes[:], nonce)
	return a.Seal(dst, nonceBytes[:], plaintext, additionalData)
}

// OpenNonce is a convenience function that takes the nonce as a uint64.
func (a *AEAD) OpenNonce(dst []byte, nonce uint64, ciphertext, additionalData []byte) ([]byte, error) {
	var nonceBytes [NonceSize]byte
	binary.BigEndian.PutUint64(nonceBytes[:], nonce)
	return a.Open(dst, nonceBytes[:], ciphertext, additionalData)
}

// Hash computes SHA-256 hash of data.
// Deprecated: Use KeyedHash for session deduplication to match C++ behavior.
func Hash(data []byte) [32]byte {
	return sha256.Sum256(data)
}

// KeyedHash computes BLAKE2b keyed hash of data.
// Compatible with libsodium's crypto_generichash.
// The key should be the session nonce (24 bytes).
func KeyedHash(data, key []byte) [32]byte {
	// BLAKE2b with key - matches crypto_generichash(out, 32, data, len, key, keylen)
	h, err := blake2b.New(32, key)
	if err != nil {
		// Fallback to unkeyed if key is too long (shouldn't happen with 24-byte nonce)
		h, _ = blake2b.New256(nil)
	}
	h.Write(data)
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}
