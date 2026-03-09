package crypto

import (
	"crypto/sha1"
	"encoding/hex"
	"testing"
)

// TestKeyDerivationCompatibility tests that our key derivation produces
// the same output as wfb-ng's wfb_keygen.
// Test vectors from wfb-ng/wfb_ng/tests/test_txrx.py:KeyDerivationTestCase
func TestKeyDerivationCompatibility(t *testing.T) {
	password := "secret password"

	droneKey, gsKey, err := DeriveKeysFromPassword(password)
	if err != nil {
		t.Fatalf("DeriveKeysFromPassword failed: %v", err)
	}

	// Expected SHA1 hashes from wfb-ng test vectors
	// gs.key SHA1: cb8d52ca7602928f67daba6ba1f308f4cfc88aa7
	// drone.key SHA1: 7a6ffb44cebc53b4538d20bdcaba8d70c9cf4095
	expectedGSKeySHA1 := "cb8d52ca7602928f67daba6ba1f308f4cfc88aa7"
	expectedDroneKeySHA1 := "7a6ffb44cebc53b4538d20bdcaba8d70c9cf4095"

	gsKeySHA1 := sha1.Sum(gsKey)
	droneKeySHA1 := sha1.Sum(droneKey)

	gotGSKeySHA1 := hex.EncodeToString(gsKeySHA1[:])
	gotDroneKeySHA1 := hex.EncodeToString(droneKeySHA1[:])

	if gotGSKeySHA1 != expectedGSKeySHA1 {
		t.Errorf("gs.key SHA1 mismatch:\n  got:      %s\n  expected: %s", gotGSKeySHA1, expectedGSKeySHA1)
	}

	if gotDroneKeySHA1 != expectedDroneKeySHA1 {
		t.Errorf("drone.key SHA1 mismatch:\n  got:      %s\n  expected: %s", gotDroneKeySHA1, expectedDroneKeySHA1)
	}

	// Verify key file sizes
	if len(gsKey) != KeyFileSize {
		t.Errorf("gs.key size = %d, want %d", len(gsKey), KeyFileSize)
	}
	if len(droneKey) != KeyFileSize {
		t.Errorf("drone.key size = %d, want %d", len(droneKey), KeyFileSize)
	}
}

// TestChaCha20Poly1305NonceSize verifies we're using 8-byte nonces
// as required by wfb-ng (original ChaCha20-Poly1305, not IETF variant).
func TestChaCha20Poly1305NonceSize(t *testing.T) {
	if NonceSize != 8 {
		t.Errorf("NonceSize = %d, want 8 (original ChaCha20-Poly1305)", NonceSize)
	}
}

// TestChaCha20Poly1305TagSize verifies the authentication tag size.
func TestChaCha20Poly1305TagSize(t *testing.T) {
	if TagSize != 16 {
		t.Errorf("TagSize = %d, want 16 (Poly1305)", TagSize)
	}
}

// TestChaCha20Poly1305KeySize verifies the key size.
func TestChaCha20Poly1305KeySize(t *testing.T) {
	if KeySize != 32 {
		t.Errorf("KeySize = %d, want 32", KeySize)
	}
}
