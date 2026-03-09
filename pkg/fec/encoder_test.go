package fec

import (
	"bytes"
	"crypto/rand"
	"fmt"
	mathrand "math/rand"
	"testing"
)

// TestPureRoundTrip performs a full encode-decode round trip with pure Go implementation
func TestPureRoundTrip(t *testing.T) {
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
			testPureRoundTrip(t, tc.k, tc.n, tc.shardSize, tc.lostCount)
		})
	}
}

func testPureRoundTrip(t *testing.T, k, n, shardSize, lostCount int) {
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

// TestGFTables verifies GF tables match expected values
func TestGFTables(t *testing.T) {
	initGF()

	// Verify some known values
	// gfExp[0] = 1 (α^0 = 1)
	if gfExp[0] != 1 {
		t.Errorf("gfExp[0] = %d, want 1", gfExp[0])
	}

	// gfExp[1] = 2 (α^1 = α = x)
	if gfExp[1] != 2 {
		t.Errorf("gfExp[1] = %d, want 2", gfExp[1])
	}

	// gfLog[1] = 0 (log(1) = 0)
	if gfLog[1] != 0 {
		t.Errorf("gfLog[1] = %d, want 0", gfLog[1])
	}

	// gfLog[2] = 1 (log(α) = 1)
	if gfLog[2] != 1 {
		t.Errorf("gfLog[2] = %d, want 1", gfLog[2])
	}

	// gfInverse[1] = 1 (1 * 1 = 1)
	if gfInverse[1] != 1 {
		t.Errorf("gfInverse[1] = %d, want 1", gfInverse[1])
	}

	// Verify gfExp is extended correctly
	for i := 0; i < 255; i++ {
		if gfExp[i] != gfExp[i+255] {
			t.Errorf("gfExp[%d] = %d != gfExp[%d] = %d", i, gfExp[i], i+255, gfExp[i+255])
		}
	}

	// Verify multiplication table consistency
	for a := 0; a < 256; a++ {
		for b := 0; b < 256; b++ {
			expected := byte(0)
			if a != 0 && b != 0 {
				expected = gfExp[modnn(gfLog[a]+gfLog[b])]
			}
			if gfMulTable[a][b] != expected {
				t.Errorf("gfMulTable[%d][%d] = %d, want %d", a, b, gfMulTable[a][b], expected)
			}
		}
	}
}

// BenchmarkPureEncode benchmarks pure Go FEC encoding
func BenchmarkPureEncode(b *testing.B) {
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

// BenchmarkPureReconstruct benchmarks pure Go FEC reconstruction
func BenchmarkPureReconstruct(b *testing.B) {
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

// TestAllConfigs tests many k,n configurations
func TestAllConfigs(t *testing.T) {
	configs := []struct{ k, n int }{
		{1, 2}, {1, 3}, {1, 4}, {1, 5},
		{2, 3}, {2, 4}, {2, 5}, {2, 6},
		{3, 4}, {3, 5}, {3, 6}, {3, 7},
		{4, 5}, {4, 6}, {4, 7}, {4, 8},
		{5, 6}, {5, 7}, {5, 8}, {5, 10},
		{6, 7}, {6, 8}, {6, 9}, {6, 12},
		{7, 8}, {7, 10}, {7, 12}, {7, 14},
		{8, 9}, {8, 10}, {8, 12}, {8, 16},
		{10, 12}, {10, 15}, {10, 20},
		{12, 16}, {12, 20}, {12, 24},
		{16, 20}, {16, 24}, {16, 32},
		{32, 40}, {32, 48}, {32, 64},
		{64, 80}, {64, 96}, {64, 128},
		{128, 160}, {128, 192}, {128, 255},
	}

	shardSize := 256

	for _, cfg := range configs {
		name := fmt.Sprintf("k=%d,n=%d", cfg.k, cfg.n)
		t.Run(name, func(t *testing.T) {
			testRoundTripConfig(t, cfg.k, cfg.n, shardSize)
		})
	}
}

func testRoundTripConfig(t *testing.T, k, n, shardSize int) {
	numParity := n - k

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

	// Save original
	originalData := make([][]byte, k)
	for i := 0; i < k; i++ {
		originalData[i] = make([]byte, shardSize)
		copy(originalData[i], dataShards[i])
	}

	// Encode
	parityShards := make([][]byte, numParity)
	for i := 0; i < numParity; i++ {
		parityShards[i] = make([]byte, shardSize)
	}

	err = encoder.Encode(dataShards, parityShards)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Build shards with some loss (lose half the recoverable amount)
	shards := make([][]byte, n)
	for i := 0; i < k; i++ {
		shards[i] = make([]byte, shardSize)
		copy(shards[i], dataShards[i])
	}
	for i := 0; i < numParity; i++ {
		shards[k+i] = make([]byte, shardSize)
		copy(shards[k+i], parityShards[i])
	}

	// Lose half the parity count (or at least 1) of data shards
	lossCount := numParity / 2
	if lossCount < 1 {
		lossCount = 1
	}
	for i := 0; i < lossCount; i++ {
		shards[i] = nil
	}

	// Decode
	err = decoder.Reconstruct(shards)
	if err != nil {
		t.Fatalf("Reconstruct failed: %v", err)
	}

	// Verify
	for i := 0; i < k; i++ {
		if !bytes.Equal(shards[i], originalData[i]) {
			t.Errorf("shard %d mismatch", i)
		}
	}
}

// TestVariousLossPatterns tests decoding with various loss patterns
func TestVariousLossPatterns(t *testing.T) {
	testCases := []struct {
		k, n      int
		shardSize int
		lossCount int
	}{
		{4, 6, 128, 1},
		{4, 6, 128, 2},
		{8, 12, 256, 1},
		{8, 12, 256, 2},
		{8, 12, 256, 3},
		{8, 12, 256, 4},
		{8, 12, 1400, 4}, // typical wfb-ng config
		{16, 24, 512, 4},
		{16, 24, 512, 8},
	}

	for _, tc := range testCases {
		// Test losing first N shards
		name := fmt.Sprintf("k=%d,n=%d,lose_first_%d", tc.k, tc.n, tc.lossCount)
		t.Run(name, func(t *testing.T) {
			lostIndices := make([]int, tc.lossCount)
			for i := 0; i < tc.lossCount; i++ {
				lostIndices[i] = i
			}
			testLossPattern(t, tc.k, tc.n, tc.shardSize, lostIndices)
		})

		// Test losing last N shards
		name = fmt.Sprintf("k=%d,n=%d,lose_last_%d", tc.k, tc.n, tc.lossCount)
		t.Run(name, func(t *testing.T) {
			lostIndices := make([]int, tc.lossCount)
			for i := 0; i < tc.lossCount; i++ {
				lostIndices[i] = tc.k - tc.lossCount + i
			}
			testLossPattern(t, tc.k, tc.n, tc.shardSize, lostIndices)
		})

		// Test losing alternating shards
		if tc.lossCount <= tc.k/2 {
			name = fmt.Sprintf("k=%d,n=%d,lose_alternating_%d", tc.k, tc.n, tc.lossCount)
			t.Run(name, func(t *testing.T) {
				lostIndices := make([]int, tc.lossCount)
				for i := 0; i < tc.lossCount; i++ {
					lostIndices[i] = i * 2
				}
				testLossPattern(t, tc.k, tc.n, tc.shardSize, lostIndices)
			})
		}
	}
}

func testLossPattern(t *testing.T, k, n, shardSize int, lostIndices []int) {
	numParity := n - k

	encoder, _ := NewEncoder(k, n)
	defer encoder.Close()
	decoder, _ := NewDecoder(k, n)
	defer decoder.Close()

	// Create random test data
	dataShards := make([][]byte, k)
	for i := 0; i < k; i++ {
		dataShards[i] = make([]byte, shardSize)
		rand.Read(dataShards[i])
	}

	// Save original
	originalData := make([][]byte, k)
	for i := 0; i < k; i++ {
		originalData[i] = make([]byte, shardSize)
		copy(originalData[i], dataShards[i])
	}

	// Encode
	parityShards := make([][]byte, numParity)
	for i := 0; i < numParity; i++ {
		parityShards[i] = make([]byte, shardSize)
	}
	encoder.Encode(dataShards, parityShards)

	// Build shards
	shards := make([][]byte, n)
	for i := 0; i < k; i++ {
		shards[i] = make([]byte, shardSize)
		copy(shards[i], dataShards[i])
	}
	for i := 0; i < numParity; i++ {
		shards[k+i] = make([]byte, shardSize)
		copy(shards[k+i], parityShards[i])
	}

	// Apply loss pattern
	for _, idx := range lostIndices {
		shards[idx] = nil
	}

	// Decode
	err := decoder.Reconstruct(shards)
	if err != nil {
		t.Errorf("Reconstruct failed: %v", err)
		return
	}

	// Verify
	for i := 0; i < k; i++ {
		if !bytes.Equal(shards[i], originalData[i]) {
			t.Errorf("shard %d mismatch", i)
		}
	}
}

// TestSpecialDataPatterns tests with special data patterns
func TestSpecialDataPatterns(t *testing.T) {
	k := 8
	n := 12
	shardSize := 256

	patterns := []struct {
		name    string
		pattern func([]byte)
	}{
		{"zeros", func(b []byte) { for i := range b { b[i] = 0 } }},
		{"ones", func(b []byte) { for i := range b { b[i] = 0xFF } }},
		{"ascending", func(b []byte) { for i := range b { b[i] = byte(i) } }},
		{"descending", func(b []byte) { for i := range b { b[i] = byte(255 - i) } }},
		{"alternating", func(b []byte) { for i := range b { b[i] = byte(i % 2 * 255) } }},
		{"random", func(b []byte) { rand.Read(b) }},
	}

	for _, p := range patterns {
		t.Run(p.name, func(t *testing.T) {
			encoder, _ := NewEncoder(k, n)
			defer encoder.Close()
			decoder, _ := NewDecoder(k, n)
			defer decoder.Close()

			dataShards := make([][]byte, k)
			for i := 0; i < k; i++ {
				dataShards[i] = make([]byte, shardSize)
				p.pattern(dataShards[i])
			}

			// Save original
			originalData := make([][]byte, k)
			for i := 0; i < k; i++ {
				originalData[i] = make([]byte, shardSize)
				copy(originalData[i], dataShards[i])
			}

			parityShards := make([][]byte, n-k)
			for i := 0; i < n-k; i++ {
				parityShards[i] = make([]byte, shardSize)
			}

			encoder.Encode(dataShards, parityShards)

			// Build shards with loss
			shards := make([][]byte, n)
			for i := 0; i < k; i++ {
				shards[i] = make([]byte, shardSize)
				copy(shards[i], dataShards[i])
			}
			for i := 0; i < n-k; i++ {
				shards[k+i] = make([]byte, shardSize)
				copy(shards[k+i], parityShards[i])
			}

			// Lose first 2 data shards
			shards[0] = nil
			shards[1] = nil

			// Decode
			err := decoder.Reconstruct(shards)
			if err != nil {
				t.Fatalf("Reconstruct failed: %v", err)
			}

			// Verify
			for i := 0; i < k; i++ {
				if !bytes.Equal(shards[i], originalData[i]) {
					t.Errorf("shard %d mismatch for pattern %s", i, p.name)
				}
			}
		})
	}
}

// TestVariousShardSizes tests various shard sizes
func TestVariousShardSizes(t *testing.T) {
	k := 8
	n := 12

	// Pure Go implementation handles any shard size (no SIMD alignment requirements)
	sizes := []int{1, 2, 3, 7, 13, 16, 17, 31, 32, 33, 63, 64, 65, 100, 127, 128, 129, 255, 256, 257, 500, 1000, 1400, 1500, 2048, 4096}

	for _, size := range sizes {
		t.Run(fmt.Sprintf("size=%d", size), func(t *testing.T) {
			numParity := n - k

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
				dataShards[i] = make([]byte, size)
				rand.Read(dataShards[i])
			}

			// Save original
			originalData := make([][]byte, k)
			for i := 0; i < k; i++ {
				originalData[i] = make([]byte, size)
				copy(originalData[i], dataShards[i])
			}

			// Encode
			parityShards := make([][]byte, numParity)
			for i := 0; i < numParity; i++ {
				parityShards[i] = make([]byte, size)
			}

			err = encoder.Encode(dataShards, parityShards)
			if err != nil {
				t.Fatalf("Encode failed: %v", err)
			}

			// Build shards with loss
			shards := make([][]byte, n)
			for i := 0; i < k; i++ {
				shards[i] = make([]byte, size)
				copy(shards[i], dataShards[i])
			}
			for i := 0; i < numParity; i++ {
				shards[k+i] = make([]byte, size)
				copy(shards[k+i], parityShards[i])
			}

			// Lose first 2 data shards
			shards[0] = nil
			shards[1] = nil

			// Decode
			err = decoder.Reconstruct(shards)
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

// TestStressRandom runs many random encode/decode cycles
func TestStressRandom(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	iterations := 100
	k := 8
	n := 12

	encoder, _ := NewEncoder(k, n)
	defer encoder.Close()
	decoder, _ := NewDecoder(k, n)
	defer decoder.Close()

	for iter := 0; iter < iterations; iter++ {
		// Random shard size between 1 and 2000
		shardSize := 1 + int(mathrand.Int31n(2000))
		// Random number of lost shards (1 to n-k)
		lossCount := 1 + int(mathrand.Int31n(int32(n-k)))

		// Random loss pattern
		perm := mathrand.Perm(k)
		lostIndices := perm[:lossCount]

		numParity := n - k

		// Create random test data
		dataShards := make([][]byte, k)
		for i := 0; i < k; i++ {
			dataShards[i] = make([]byte, shardSize)
			rand.Read(dataShards[i])
		}

		// Save original
		originalData := make([][]byte, k)
		for i := 0; i < k; i++ {
			originalData[i] = make([]byte, shardSize)
			copy(originalData[i], dataShards[i])
		}

		// Encode
		parityShards := make([][]byte, numParity)
		for i := 0; i < numParity; i++ {
			parityShards[i] = make([]byte, shardSize)
		}
		encoder.Encode(dataShards, parityShards)

		// Build shards
		shards := make([][]byte, n)
		for i := 0; i < k; i++ {
			shards[i] = make([]byte, shardSize)
			copy(shards[i], dataShards[i])
		}
		for i := 0; i < numParity; i++ {
			shards[k+i] = make([]byte, shardSize)
			copy(shards[k+i], parityShards[i])
		}

		// Apply loss pattern
		for _, idx := range lostIndices {
			shards[idx] = nil
		}

		// Decode
		err := decoder.Reconstruct(shards)
		if err != nil {
			t.Errorf("iter %d: Reconstruct failed: %v", iter, err)
			continue
		}

		// Verify
		for i := 0; i < k; i++ {
			if !bytes.Equal(shards[i], originalData[i]) {
				t.Errorf("iter %d: shard %d mismatch", iter, i)
			}
		}
	}
}
