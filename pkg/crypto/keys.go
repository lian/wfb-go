package crypto

import (
	"crypto/rand"
	"crypto/sha512"
	"encoding/binary"
	"errors"
	"os"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/nacl/box"
)

const (
	// X25519 key sizes
	PublicKeySize  = 32
	PrivateKeySize = 32

	// Key file size: secret key + peer public key
	KeyFileSize = PrivateKeySize + PublicKeySize
)

var (
	ErrInvalidKeyFile = errors.New("invalid key file size")
)

// KeyPair holds an X25519 keypair.
type KeyPair struct {
	PublicKey  [PublicKeySize]byte
	PrivateKey [PrivateKeySize]byte
}

// GenerateKeyPair generates a new X25519 keypair.
func GenerateKeyPair() (*KeyPair, error) {
	pub, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	kp := &KeyPair{}
	copy(kp.PublicKey[:], pub[:])
	copy(kp.PrivateKey[:], priv[:])
	return kp, nil
}

// TXKey holds keys for the transmitter side.
// Contains our secret key and the receiver's public key.
type TXKey struct {
	SecretKey [PrivateKeySize]byte
	RXPubKey  [PublicKeySize]byte
}

// RXKey holds keys for the receiver side.
// Contains our secret key and the transmitter's public key.
type RXKey struct {
	SecretKey [PrivateKeySize]byte
	TXPubKey  [PublicKeySize]byte
}

// ParseTXKey parses TX key data (drone.key format).
// Format: tx_secretkey (32 bytes) + rx_publickey (32 bytes)
func ParseTXKey(data []byte) (*TXKey, error) {
	if len(data) != KeyFileSize {
		return nil, ErrInvalidKeyFile
	}
	key := &TXKey{}
	copy(key.SecretKey[:], data[0:32])
	copy(key.RXPubKey[:], data[32:64])
	return key, nil
}

// LoadTXKey loads a TX key file (drone.key format).
// Format: tx_secretkey (32 bytes) + rx_publickey (32 bytes)
func LoadTXKey(path string) (*TXKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseTXKey(data)
}

// ParseRXKey parses RX key data (gs.key format).
// Format: rx_secretkey (32 bytes) + tx_publickey (32 bytes)
func ParseRXKey(data []byte) (*RXKey, error) {
	if len(data) != KeyFileSize {
		return nil, ErrInvalidKeyFile
	}
	key := &RXKey{}
	copy(key.SecretKey[:], data[0:32])
	copy(key.TXPubKey[:], data[32:64])
	return key, nil
}

// LoadRXKey loads an RX key file (gs.key format).
// Format: rx_secretkey (32 bytes) + tx_publickey (32 bytes)
func LoadRXKey(path string) (*RXKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseRXKey(data)
}

// SaveKeyFile writes a key file.
func SaveKeyFile(path string, secretKey, peerPubKey []byte) error {
	if len(secretKey) != PrivateKeySize || len(peerPubKey) != PublicKeySize {
		return ErrInvalidKeyFile
	}
	data := make([]byte, KeyFileSize)
	copy(data[0:32], secretKey)
	copy(data[32:64], peerPubKey)
	return os.WriteFile(path, data, 0600)
}

// GenerateWFBKeys generates a matched pair of drone.key and gs.key.
func GenerateWFBKeys() (droneKey, gsKey []byte, err error) {
	// Generate drone keypair
	dronePub, dronePriv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	// Generate GS keypair
	gsPub, gsPriv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	// drone.key = drone_secretkey + gs_publickey
	droneKey = make([]byte, KeyFileSize)
	copy(droneKey[0:32], dronePriv[:])
	copy(droneKey[32:64], gsPub[:])

	// gs.key = gs_secretkey + drone_publickey
	gsKey = make([]byte, KeyFileSize)
	copy(gsKey[0:32], gsPriv[:])
	copy(gsKey[32:64], dronePub[:])

	return droneKey, gsKey, nil
}

// DeriveKeysFromPassword derives drone and GS keypairs from a password using Argon2i.
func DeriveKeysFromPassword(password string) (droneKey, gsKey []byte, err error) {
	// Salt: "wifibroadcastkey" (16 bytes)
	salt := []byte("wifibroadcastkey")

	// Derive 64 bytes of seed using Argon2i
	// Parameters match libsodium's current (v1.0.18+) values:
	// crypto_pwhash_argon2i_OPSLIMIT_INTERACTIVE = 4
	// crypto_pwhash_argon2i_MEMLIMIT_INTERACTIVE = 33554432 (32 MB = 32768 KiB)
	seed := argon2.Key([]byte(password), salt, 4, 32*1024, 1, 64)

	// First 32 bytes → drone keypair seed
	// Second 32 bytes → GS keypair seed
	var droneSeed, gsSeed [32]byte
	copy(droneSeed[:], seed[0:32])
	copy(gsSeed[:], seed[32:64])

	// Generate keypairs from seeds using crypto_box_seed_keypair equivalent
	// libsodium's crypto_box_seed_keypair:
	// 1. Hash seed with SHA-512
	// 2. Take first 32 bytes as secret key
	// 3. Derive public key via scalar multiplication
	dronePriv, dronePub := seedToKeypair(droneSeed[:])
	gsPriv, gsPub := seedToKeypair(gsSeed[:])

	// drone.key = drone_secretkey + gs_publickey
	droneKey = make([]byte, KeyFileSize)
	copy(droneKey[0:32], dronePriv[:])
	copy(droneKey[32:64], gsPub[:])

	// gs.key = gs_secretkey + drone_publickey
	gsKey = make([]byte, KeyFileSize)
	copy(gsKey[0:32], gsPriv[:])
	copy(gsKey[32:64], dronePub[:])

	return droneKey, gsKey, nil
}

// seedToKeypair derives a keypair from a seed using libsodium's
// crypto_box_seed_keypair algorithm:
// 1. Hash seed with SHA-512
// 2. Take first 32 bytes as secret key
// 3. Derive public key via curve25519 scalar multiplication
func seedToKeypair(seed []byte) (secretKey, publicKey [32]byte) {
	// Hash the seed with SHA-512
	hash := sha512.Sum512(seed)

	// First 32 bytes become the secret key
	copy(secretKey[:], hash[:32])

	// Derive public key
	curve25519.ScalarBaseMult(&publicKey, &secretKey)

	return secretKey, publicKey
}

// GenerateSessionKey generates a random 32-byte session key.
func GenerateSessionKey() ([KeySize]byte, error) {
	var key [KeySize]byte
	_, err := rand.Read(key[:])
	return key, err
}

// BoxSeal encrypts a message using crypto_box_easy.
// Uses the TX secret key and RX public key.
func BoxSeal(message []byte, nonce *[24]byte, rxPubKey, txSecKey *[32]byte) []byte {
	return box.Seal(nil, message, nonce, rxPubKey, txSecKey)
}

// BoxOpen decrypts a message using crypto_box_open_easy.
// Uses the RX secret key and TX public key.
func BoxOpen(ciphertext []byte, nonce *[24]byte, txPubKey, rxSecKey *[32]byte) ([]byte, bool) {
	return box.Open(nil, ciphertext, nonce, txPubKey, rxSecKey)
}

// GenerateBoxNonce generates a random 24-byte nonce for crypto_box.
func GenerateBoxNonce() ([24]byte, error) {
	var nonce [24]byte
	_, err := rand.Read(nonce[:])
	return nonce, err
}

// NonceFromUint64 creates an 8-byte nonce from a uint64 (big-endian).
// Used for data packet nonces: (block_idx << 8) | fragment_idx
func NonceFromUint64(val uint64) [NonceSize]byte {
	var nonce [NonceSize]byte
	binary.BigEndian.PutUint64(nonce[:], val)
	return nonce
}
