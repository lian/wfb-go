package fec

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"testing"
)

// TestFECRoundTrip performs a full encode-decode round trip
func TestFECRoundTrip(t *testing.T) {
	testCases := []struct {
		k, n      int
		shardSize int
		lostCount int // number of data shards to lose
	}{
		{4, 6, 16, 1},     // lose 1 of 4 data shards
		{4, 6, 16, 2},     // lose 2 of 4 data shards (max recoverable)
		{8, 12, 1024, 1},  // typical wfb-ng config, lose 1
		{8, 12, 1024, 4},  // typical wfb-ng config, lose 4 (max)
		{8, 12, 1400, 2},  // MTU-sized packets
		{1, 2, 100, 1},    // minimal config
		{2, 4, 64, 2},     // lose all data, recover from parity
	}

	for _, tc := range testCases {
		name := fmt.Sprintf("k=%d,n=%d,size=%d,lost=%d", tc.k, tc.n, tc.shardSize, tc.lostCount)
		t.Run(name, func(t *testing.T) {
			testRoundTrip(t, tc.k, tc.n, tc.shardSize, tc.lostCount)
		})
	}
}

func testRoundTrip(t *testing.T, k, n, shardSize, lostCount int) {
	numParity := n - k

	// Create encoder and decoder
	encoder, err := NewEncoder(k, n)
	if err != nil {
		t.Fatalf("NewEncoder failed: %v", err)
	}
	defer encoder.Close()

	decoder, err := NewDecoder(k, n)
	if err != nil {
		t.Fatalf("NewDecoder failed: %v", err)
	}
	defer decoder.Close()

	// Create random test data
	dataShards := make([][]byte, k)
	for i := 0; i < k; i++ {
		dataShards[i] = make([]byte, shardSize)
		rand.Read(dataShards[i])
	}

	// Save original data for comparison
	originalData := make([][]byte, k)
	for i := 0; i < k; i++ {
		originalData[i] = make([]byte, shardSize)
		copy(originalData[i], dataShards[i])
	}

	// Allocate parity shards
	parityShards := make([][]byte, numParity)
	for i := 0; i < numParity; i++ {
		parityShards[i] = make([]byte, shardSize)
	}

	// Encode
	err = encoder.Encode(dataShards, parityShards)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Build shards array for decoder (simulating received packets)
	shards := make([][]byte, n)

	// Copy all data shards first
	for i := 0; i < k; i++ {
		shards[i] = make([]byte, shardSize)
		copy(shards[i], dataShards[i])
	}

	// Copy all parity shards
	for i := 0; i < numParity; i++ {
		shards[k+i] = make([]byte, shardSize)
		copy(shards[k+i], parityShards[i])
	}

	// Simulate packet loss - remove first 'lostCount' data shards
	for i := 0; i < lostCount; i++ {
		shards[i] = nil
	}

	// Reconstruct
	err = decoder.Reconstruct(shards)
	if err != nil {
		t.Fatalf("Reconstruct failed: %v", err)
	}

	// Verify all data shards match original
	for i := 0; i < k; i++ {
		if !bytes.Equal(shards[i], originalData[i]) {
			t.Errorf("shard %d mismatch:\ngot:  %x\nwant: %x", i, shards[i], originalData[i])
		}
	}
}

// TestFECRoundTripVariousLossPatterns tests different loss patterns
func TestFECRoundTripVariousLossPatterns(t *testing.T) {
	k := 4
	n := 6
	shardSize := 64

	encoder, err := NewEncoder(k, n)
	if err != nil {
		t.Fatalf("NewEncoder failed: %v", err)
	}
	defer encoder.Close()

	decoder, err := NewDecoder(k, n)
	if err != nil {
		t.Fatalf("NewDecoder failed: %v", err)
	}
	defer decoder.Close()

	// Create test data
	dataShards := make([][]byte, k)
	for i := 0; i < k; i++ {
		dataShards[i] = make([]byte, shardSize)
		rand.Read(dataShards[i])
	}

	originalData := make([][]byte, k)
	for i := 0; i < k; i++ {
		originalData[i] = make([]byte, shardSize)
		copy(originalData[i], dataShards[i])
	}

	parityShards := make([][]byte, n-k)
	for i := 0; i < n-k; i++ {
		parityShards[i] = make([]byte, shardSize)
	}

	err = encoder.Encode(dataShards, parityShards)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Test different loss patterns
	lossPatterns := [][]int{
		{0},       // lose first data shard
		{3},       // lose last data shard
		{1, 2},    // lose middle data shards
		{0, 3},    // lose first and last
		{0, 1},    // lose first two
		{2, 3},    // lose last two
	}

	for _, lostIndices := range lossPatterns {
		name := fmt.Sprintf("lost=%v", lostIndices)
		t.Run(name, func(t *testing.T) {
			// Build shards array
			shards := make([][]byte, n)
			for i := 0; i < k; i++ {
				shards[i] = make([]byte, shardSize)
				copy(shards[i], originalData[i])
			}
			for i := 0; i < n-k; i++ {
				shards[k+i] = make([]byte, shardSize)
				copy(shards[k+i], parityShards[i])
			}

			// Apply loss pattern
			for _, idx := range lostIndices {
				shards[idx] = nil
			}

			// Reconstruct
			err := decoder.Reconstruct(shards)
			if err != nil {
				t.Fatalf("Reconstruct failed: %v", err)
			}

			// Verify
			for i := 0; i < k; i++ {
				if !bytes.Equal(shards[i], originalData[i]) {
					t.Errorf("shard %d mismatch", i)
				}
			}
		})
	}
}

// TestDecoderCreateClose tests decoder creation and cleanup
func TestDecoderCreateClose(t *testing.T) {
	testCases := []struct {
		k, n    int
		wantErr bool
	}{
		{1, 2, false},
		{8, 12, false},
		{4, 8, false},
		{1, 1, false},  // edge case: no parity
		{0, 1, true},   // invalid k
		{2, 1, true},   // k > n
		{1, 256, true}, // n too large (max 255)
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("k=%d,n=%d", tc.k, tc.n), func(t *testing.T) {
			decoder, err := NewDecoder(tc.k, tc.n)
			if tc.wantErr {
				if err == nil {
					decoder.Close()
					t.Errorf("expected error for k=%d, n=%d", tc.k, tc.n)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewDecoder failed: %v", err)
			}
			defer decoder.Close()

			if decoder.K() != tc.k {
				t.Errorf("K() = %d, want %d", decoder.K(), tc.k)
			}
			if decoder.N() != tc.n {
				t.Errorf("N() = %d, want %d", decoder.N(), tc.n)
			}
		})
	}
}

// TestEncoderCreateClose tests encoder creation and cleanup
func TestEncoderCreateClose(t *testing.T) {
	testCases := []struct {
		k, n    int
		wantErr bool
	}{
		{1, 2, false},
		{8, 12, false},
		{4, 8, false},
		{0, 1, true},   // invalid k
		{2, 1, true},   // k > n
		{1, 256, true}, // n too large (max 255)
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("k=%d,n=%d", tc.k, tc.n), func(t *testing.T) {
			encoder, err := NewEncoder(tc.k, tc.n)
			if tc.wantErr {
				if err == nil {
					encoder.Close()
					t.Errorf("expected error for k=%d, n=%d", tc.k, tc.n)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewEncoder failed: %v", err)
			}
			defer encoder.Close()

			if encoder.K() != tc.k {
				t.Errorf("K() = %d, want %d", encoder.K(), tc.k)
			}
			if encoder.N() != tc.n {
				t.Errorf("N() = %d, want %d", encoder.N(), tc.n)
			}
		})
	}
}

// TestFECReconstructAllPresent tests that Reconstruct is a no-op when all data shards present
func TestFECReconstructAllPresent(t *testing.T) {
	k := 4
	n := 6

	decoder, err := NewDecoder(k, n)
	if err != nil {
		t.Fatalf("NewDecoder failed: %v", err)
	}
	defer decoder.Close()

	shardSize := 256
	shards := make([][]byte, n)

	// Fill all data shards
	for i := 0; i < k; i++ {
		shards[i] = make([]byte, shardSize)
		rand.Read(shards[i])
	}
	// Leave parity shards nil

	// Copy original data for comparison
	originalData := make([][]byte, k)
	for i := 0; i < k; i++ {
		originalData[i] = make([]byte, shardSize)
		copy(originalData[i], shards[i])
	}

	// Reconstruct should be a no-op
	err = decoder.Reconstruct(shards)
	if err != nil {
		t.Fatalf("Reconstruct failed: %v", err)
	}

	// Verify data unchanged
	for i := 0; i < k; i++ {
		if !bytes.Equal(shards[i], originalData[i]) {
			t.Errorf("shard %d was modified", i)
		}
	}
}

// TestFECReconstructTooFewShards tests error when not enough shards
func TestFECReconstructTooFewShards(t *testing.T) {
	k := 4
	n := 6

	decoder, err := NewDecoder(k, n)
	if err != nil {
		t.Fatalf("NewDecoder failed: %v", err)
	}
	defer decoder.Close()

	shardSize := 256
	shards := make([][]byte, n)

	// Only provide k-1 shards (not enough)
	for i := 0; i < k-1; i++ {
		shards[i] = make([]byte, shardSize)
		rand.Read(shards[i])
	}

	err = decoder.Reconstruct(shards)
	if err == nil {
		t.Error("expected error for too few shards")
	}
}

// TestFECDeterministic verifies that encoding is deterministic
func TestFECDeterministic(t *testing.T) {
	k := 4
	n := 6
	shardSize := 64

	encoder, err := NewEncoder(k, n)
	if err != nil {
		t.Fatalf("NewEncoder failed: %v", err)
	}
	defer encoder.Close()

	// Fixed test data
	dataShards := make([][]byte, k)
	for i := 0; i < k; i++ {
		dataShards[i] = make([]byte, shardSize)
		for j := 0; j < shardSize; j++ {
			dataShards[i][j] = byte(i*shardSize + j)
		}
	}

	// Encode twice
	parity1 := make([][]byte, n-k)
	parity2 := make([][]byte, n-k)
	for i := 0; i < n-k; i++ {
		parity1[i] = make([]byte, shardSize)
		parity2[i] = make([]byte, shardSize)
	}

	err = encoder.Encode(dataShards, parity1)
	if err != nil {
		t.Fatalf("First encode failed: %v", err)
	}

	err = encoder.Encode(dataShards, parity2)
	if err != nil {
		t.Fatalf("Second encode failed: %v", err)
	}

	// Verify parity is identical
	for i := 0; i < n-k; i++ {
		if !bytes.Equal(parity1[i], parity2[i]) {
			t.Errorf("parity shard %d differs between encodes", i)
		}
	}
}

// BenchmarkFECEncode benchmarks FEC encoding
func BenchmarkFECEncode(b *testing.B) {
	k := 8
	n := 12
	shardSize := 1400

	encoder, err := NewEncoder(k, n)
	if err != nil {
		b.Fatalf("NewEncoder failed: %v", err)
	}
	defer encoder.Close()

	dataShards := make([][]byte, k)
	for i := 0; i < k; i++ {
		dataShards[i] = make([]byte, shardSize)
		rand.Read(dataShards[i])
	}

	parityShards := make([][]byte, n-k)
	for i := 0; i < n-k; i++ {
		parityShards[i] = make([]byte, shardSize)
	}

	b.ResetTimer()
	b.SetBytes(int64(k * shardSize))

	for i := 0; i < b.N; i++ {
		err := encoder.Encode(dataShards, parityShards)
		if err != nil {
			b.Fatalf("Encode failed: %v", err)
		}
	}
}

// BenchmarkFECReconstruct benchmarks FEC reconstruction
func BenchmarkFECReconstruct(b *testing.B) {
	k := 8
	n := 12
	shardSize := 1400

	encoder, err := NewEncoder(k, n)
	if err != nil {
		b.Fatalf("NewEncoder failed: %v", err)
	}
	defer encoder.Close()

	decoder, err := NewDecoder(k, n)
	if err != nil {
		b.Fatalf("NewDecoder failed: %v", err)
	}
	defer decoder.Close()

	// Create and encode data
	dataShards := make([][]byte, k)
	for i := 0; i < k; i++ {
		dataShards[i] = make([]byte, shardSize)
		rand.Read(dataShards[i])
	}

	parityShards := make([][]byte, n-k)
	for i := 0; i < n-k; i++ {
		parityShards[i] = make([]byte, shardSize)
	}

	err = encoder.Encode(dataShards, parityShards)
	if err != nil {
		b.Fatalf("Encode failed: %v", err)
	}

	// Prepare template with one lost shard
	template := make([][]byte, n)
	for i := 1; i < k; i++ { // skip shard 0
		template[i] = dataShards[i]
	}
	for i := 0; i < n-k; i++ {
		template[k+i] = parityShards[i]
	}

	b.ResetTimer()
	b.SetBytes(int64(k * shardSize))

	for i := 0; i < b.N; i++ {
		// Make a copy of template
		shards := make([][]byte, n)
		for j := 1; j < n; j++ {
			if template[j] != nil {
				shards[j] = template[j]
			}
		}
		shards[0] = make([]byte, shardSize) // allocate for recovered shard

		err := decoder.Reconstruct(shards)
		if err != nil {
			b.Fatalf("Reconstruct failed: %v", err)
		}
	}
}
