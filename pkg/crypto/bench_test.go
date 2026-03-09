package crypto

import (
	"crypto/rand"
	"fmt"
	"testing"

	"golang.org/x/crypto/chacha20"
	"golang.org/x/crypto/chacha20poly1305"
)

// Benchmark our custom ChaCha20-Poly1305 (8-byte nonce, pure Go)
func BenchmarkOurChaCha20Poly1305Encrypt(b *testing.B) {
	sizes := []int{64, 256, 512, 1024, 1500}

	key := make([]byte, 32)
	rand.Read(key)

	aead, _ := NewAEAD(key)
	nonce := make([]byte, 8)

	for _, size := range sizes {
		plaintext := make([]byte, size)
		rand.Read(plaintext)

		b.Run(fmt.Sprintf("%dB", size), func(b *testing.B) {
			b.SetBytes(int64(size))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_ = aead.Seal(nil, nonce, plaintext, nil)
			}
		})
	}
}

// Benchmark Go's optimized ChaCha20-Poly1305 (IETF 12-byte nonce, assembly)
func BenchmarkGoChaCha20Poly1305Encrypt(b *testing.B) {
	sizes := []int{64, 256, 512, 1024, 1500}

	key := make([]byte, 32)
	rand.Read(key)

	aead, _ := chacha20poly1305.New(key)
	nonce := make([]byte, 12)

	for _, size := range sizes {
		plaintext := make([]byte, size)
		rand.Read(plaintext)

		b.Run(fmt.Sprintf("%dB", size), func(b *testing.B) {
			b.SetBytes(int64(size))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_ = aead.Seal(nil, nonce, plaintext, nil)
			}
		})
	}
}

// Benchmark our custom ChaCha20-Poly1305 decryption
func BenchmarkOurChaCha20Poly1305Decrypt(b *testing.B) {
	sizes := []int{64, 256, 512, 1024, 1500}

	key := make([]byte, 32)
	rand.Read(key)

	aead, _ := NewAEAD(key)
	nonce := make([]byte, 8)

	for _, size := range sizes {
		plaintext := make([]byte, size)
		rand.Read(plaintext)
		ciphertext := aead.Seal(nil, nonce, plaintext, nil)

		b.Run(fmt.Sprintf("%dB", size), func(b *testing.B) {
			b.SetBytes(int64(size))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_, _ = aead.Open(nil, nonce, ciphertext, nil)
			}
		})
	}
}

// Benchmark Go's optimized ChaCha20-Poly1305 decryption
func BenchmarkGoChaCha20Poly1305Decrypt(b *testing.B) {
	sizes := []int{64, 256, 512, 1024, 1500}

	key := make([]byte, 32)
	rand.Read(key)

	aead, _ := chacha20poly1305.New(key)
	nonce := make([]byte, 12)

	for _, size := range sizes {
		plaintext := make([]byte, size)
		rand.Read(plaintext)
		ciphertext := aead.Seal(nil, nonce, plaintext, nil)

		b.Run(fmt.Sprintf("%dB", size), func(b *testing.B) {
			b.SetBytes(int64(size))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_, _ = aead.Open(nil, nonce, ciphertext, nil)
			}
		})
	}
}

// Benchmark our ChaCha20 stream generation (8-byte nonce, pure Go)
func BenchmarkOurChaCha20Only(b *testing.B) {
	key := make([]byte, 32)
	nonce := make([]byte, 8)
	rand.Read(key)

	sizes := []int{64, 512, 1500}
	for _, size := range sizes {
		b.Run(fmt.Sprintf("%dB", size), func(b *testing.B) {
			b.SetBytes(int64(size))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				chacha := newChaCha20(key, nonce, 1)
				_ = chacha.keyStream(size)
			}
		})
	}
}

// Benchmark Go's optimized ChaCha20 (12-byte nonce, assembly)
func BenchmarkGoChaCha20Only(b *testing.B) {
	key := make([]byte, 32)
	nonce := make([]byte, 12)
	rand.Read(key)

	sizes := []int{64, 512, 1500}
	for _, size := range sizes {
		plaintext := make([]byte, size)
		ciphertext := make([]byte, size)

		b.Run(fmt.Sprintf("%dB", size), func(b *testing.B) {
			cipher, _ := chacha20.NewUnauthenticatedCipher(key, nonce)
			b.SetBytes(int64(size))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				cipher.XORKeyStream(ciphertext, plaintext)
			}
		})
	}
}

// Benchmark just Poly1305 MAC computation
func BenchmarkPoly1305Only(b *testing.B) {
	key := make([]byte, 32)
	rand.Read(key)

	sizes := []int{64, 512, 1500}
	for _, size := range sizes {
		data := make([]byte, size)
		rand.Read(data)

		b.Run(fmt.Sprintf("%dB", size), func(b *testing.B) {
			b.SetBytes(int64(size))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_ = poly1305Sum(data, key)
			}
		})
	}
}

// Benchmark throughput in packets/second (typical video packet ~1400 bytes)
func BenchmarkPacketThroughput(b *testing.B) {
	key := make([]byte, 32)
	rand.Read(key)

	plaintext := make([]byte, 1400) // typical video packet
	rand.Read(plaintext)

	b.Run("Our_1400B", func(b *testing.B) {
		aead, _ := NewAEAD(key)
		nonce := make([]byte, 8)
		b.SetBytes(1400)
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			_ = aead.Seal(nil, nonce, plaintext, nil)
		}
	})

	b.Run("Go_1400B", func(b *testing.B) {
		aead, _ := chacha20poly1305.New(key)
		nonce := make([]byte, 12)
		b.SetBytes(1400)
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			_ = aead.Seal(nil, nonce, plaintext, nil)
		}
	})
}
