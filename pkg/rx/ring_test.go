package rx

import (
	"testing"
)

func TestNewRing(t *testing.T) {
	ring, err := NewRing(8, 12)
	if err != nil {
		t.Fatalf("NewRing failed: %v", err)
	}

	if ring.fecK != 8 || ring.fecN != 12 {
		t.Errorf("FEC params = (%d, %d), want (8, 12)", ring.fecK, ring.fecN)
	}

	alloc, lastKnown := ring.Stats()
	if alloc != 0 || lastKnown != noLastKnownBlock {
		t.Errorf("Initial stats = (%d, %d), want (0, noLastKnownBlock)", alloc, lastKnown)
	}
}

func TestRingGetBlockRingIdx(t *testing.T) {
	ring, _ := NewRing(4, 6)

	// First packet should allocate at front
	idx, _, valid := ring.GetBlockRingIdx(100)
	if !valid || idx != 0 {
		t.Errorf("First block: idx=%d, valid=%v, want idx=0, valid=true", idx, valid)
	}

	alloc, lastKnown := ring.Stats()
	if alloc != 1 || lastKnown != 100 {
		t.Errorf("After first: alloc=%d, lastKnown=%d, want 1, 100", alloc, lastKnown)
	}

	// Same block should return same index
	idx2, _, valid2 := ring.GetBlockRingIdx(100)
	if !valid2 || idx2 != idx {
		t.Errorf("Same block: idx=%d, valid=%v, want idx=%d, valid=true", idx2, valid2, idx)
	}

	// Next block should allocate next slot
	idx3, _, valid3 := ring.GetBlockRingIdx(101)
	if !valid3 {
		t.Error("Block 101 should be valid")
	}
	if idx3 == idx {
		t.Error("Block 101 should get different slot than 100")
	}

	alloc, _ = ring.Stats()
	if alloc != 2 {
		t.Errorf("After second: alloc=%d, want 2", alloc)
	}

	// Old block should be rejected
	idx4, _, valid4 := ring.GetBlockRingIdx(99)
	if valid4 {
		t.Error("Block 99 should be invalid (too old)")
	}
	_ = idx4
}

func TestRingGetBlockRingIdxSequential(t *testing.T) {
	ring, _ := NewRing(4, 6)

	// Sequential allocation within ring window
	for i := uint64(0); i < 30; i++ {
		idx, _, valid := ring.GetBlockRingIdx(i)
		if !valid {
			t.Errorf("Block %d should be valid", i)
		}
		_ = idx
	}

	alloc, lastKnown := ring.Stats()
	if lastKnown != 29 {
		t.Errorf("lastKnown = %d, want 29", lastKnown)
	}
	if alloc != 30 {
		t.Errorf("alloc = %d, want 30", alloc)
	}

	// Block before the front should be invalid
	_, _, valid := ring.GetBlockRingIdx(0)
	// Front moved forward as we allocated more blocks
	// After 30 allocations, front is still 0, so block 0 is at front - still valid

	// Allocate enough to push front forward
	for i := uint64(30); i < 50; i++ {
		ring.GetBlockRingIdx(i)
	}

	// Now block 0 should be invalid (ring only holds 40 blocks)
	_, _, valid = ring.GetBlockRingIdx(0)
	if valid {
		t.Error("Block 0 should be invalid after being pushed out of ring")
	}
}

func TestRingGetBlockRingIdxGap(t *testing.T) {
	ring, _ := NewRing(4, 6)

	// Allocate block 0
	ring.GetBlockRingIdx(0)

	// Skip some blocks (simulating packet loss)
	ring.GetBlockRingIdx(5)

	alloc, _ := ring.Stats()
	// Should have allocated slots for 0-5
	if alloc != 6 {
		t.Errorf("alloc = %d, want 6 (0 through 5)", alloc)
	}

	// Block 3 should be valid (in the gap)
	_, _, valid := ring.GetBlockRingIdx(3)
	if !valid {
		t.Error("Block 3 should be valid (within allocated range)")
	}
}

func TestRingAddFragment(t *testing.T) {
	ring, _ := NewRing(4, 6)

	idx, _, _ := ring.GetBlockRingIdx(0)

	// Add fragments one by one
	for i := uint8(0); i < 3; i++ {
		data := make([]byte, 100)
		data[0] = i
		complete := ring.AddFragment(idx, i, data)
		if complete {
			t.Errorf("Fragment %d: should not be complete yet", i)
		}
	}

	// Fourth fragment should complete the block (k=4)
	data := make([]byte, 100)
	complete := ring.AddFragment(idx, 3, data)
	if !complete {
		t.Error("Block should be complete after k fragments")
	}

	// Duplicate fragment should not change completeness
	complete2 := ring.AddFragment(idx, 0, data)
	if !complete2 {
		t.Error("Block should still be complete")
	}
}

func TestRingAddFragmentInvalid(t *testing.T) {
	ring, _ := NewRing(4, 6)

	// Invalid ring index
	complete := ring.AddFragment(-1, 0, []byte{1})
	if complete {
		t.Error("Invalid ring index should return false")
	}

	complete = ring.AddFragment(100, 0, []byte{1})
	if complete {
		t.Error("Out of bounds ring index should return false")
	}

	// Invalid fragment index
	idx, _, _ := ring.GetBlockRingIdx(0)
	complete = ring.AddFragment(idx, 10, []byte{1}) // n=6, so 10 is invalid
	if complete {
		t.Error("Invalid fragment index should return false")
	}
}

func TestRingCanRecover(t *testing.T) {
	ring, _ := NewRing(4, 6)

	idx, _, _ := ring.GetBlockRingIdx(0)

	// Not enough fragments
	if ring.CanRecover(idx) {
		t.Error("Should not be able to recover with 0 fragments")
	}

	// Add k-1 fragments
	for i := uint8(0); i < 3; i++ {
		ring.AddFragment(idx, i, make([]byte, 100))
	}
	if ring.CanRecover(idx) {
		t.Error("Should not be able to recover with k-1 fragments")
	}

	// Add k-th fragment
	ring.AddFragment(idx, 3, make([]byte, 100))
	if !ring.CanRecover(idx) {
		t.Error("Should be able to recover with k fragments")
	}
}

func TestRingNeedsRecovery(t *testing.T) {
	ring, _ := NewRing(4, 6)

	idx, _, _ := ring.GetBlockRingIdx(0)

	// Add first 4 data shards (0-3)
	for i := uint8(0); i < 4; i++ {
		ring.AddFragment(idx, i, make([]byte, 100))
	}
	// All data shards present - no recovery needed
	if ring.NeedsRecovery(idx) {
		t.Error("Should not need recovery when all data shards present")
	}

	// New block with missing data shard
	idx2, _, _ := ring.GetBlockRingIdx(1)
	// Add shards 0, 1, 2, 4 (missing 3)
	ring.AddFragment(idx2, 0, make([]byte, 100))
	ring.AddFragment(idx2, 1, make([]byte, 100))
	ring.AddFragment(idx2, 2, make([]byte, 100))
	ring.AddFragment(idx2, 4, make([]byte, 100)) // Parity shard

	if !ring.NeedsRecovery(idx2) {
		t.Error("Should need recovery when data shard 3 is missing")
	}
}

func TestRingRecover(t *testing.T) {
	ring, _ := NewRing(4, 6)

	idx, _, _ := ring.GetBlockRingIdx(0)

	// Create data with recognizable patterns
	for i := uint8(0); i < 4; i++ {
		data := make([]byte, 100)
		for j := range data {
			data[j] = i
		}
		ring.AddFragment(idx, i, data)
	}

	// Block is complete with all data shards - no recovery needed
	recovered, err := ring.Recover(idx)
	if err != nil {
		t.Fatalf("Recover failed: %v", err)
	}
	if recovered != 0 {
		t.Errorf("Recovered = %d, want 0 (no missing data shards)", recovered)
	}
}

func TestRingGetFragment(t *testing.T) {
	ring, _ := NewRing(4, 6)

	idx, _, _ := ring.GetBlockRingIdx(0)

	// No fragment yet
	data, size := ring.GetFragment(idx, 0)
	if data != nil || size != 0 {
		t.Error("Should return nil for missing fragment")
	}

	// Add fragment
	testData := []byte{1, 2, 3, 4, 5}
	ring.AddFragment(idx, 0, testData)

	data, size = ring.GetFragment(idx, 0)
	if size != len(testData) {
		t.Errorf("Size = %d, want %d", size, len(testData))
	}
	for i, b := range testData {
		if data[i] != b {
			t.Errorf("Data[%d] = %d, want %d", i, data[i], b)
		}
	}

	// Invalid indices
	data, size = ring.GetFragment(-1, 0)
	if data != nil || size != 0 {
		t.Error("Invalid ring index should return nil")
	}

	data, size = ring.GetFragment(idx, 10)
	if data != nil || size != 0 {
		t.Error("Invalid fragment index should return nil")
	}
}

func TestRingGetNextToSend(t *testing.T) {
	ring, _ := NewRing(4, 6)

	idx, _, _ := ring.GetBlockRingIdx(0)

	// Add all data fragments
	for i := uint8(0); i < 4; i++ {
		ring.AddFragment(idx, i, make([]byte, 100))
	}

	// Should get fragments 0, 1, 2, 3 in order
	for expected := uint8(0); expected < 4; expected++ {
		fragIdx, ok := ring.GetNextToSend(idx)
		if !ok {
			t.Errorf("Fragment %d: expected ok=true", expected)
		}
		if fragIdx != expected {
			t.Errorf("Fragment = %d, want %d", fragIdx, expected)
		}
	}

	// No more fragments
	_, ok := ring.GetNextToSend(idx)
	if ok {
		t.Error("Should return ok=false when all fragments sent")
	}
}

func TestRingIsComplete(t *testing.T) {
	ring, _ := NewRing(4, 6)

	idx, _, _ := ring.GetBlockRingIdx(0)

	if ring.IsComplete(idx) {
		t.Error("Should not be complete before any fragments sent")
	}

	// Add all data fragments
	for i := uint8(0); i < 4; i++ {
		ring.AddFragment(idx, i, make([]byte, 100))
	}

	// Send all fragments
	for i := 0; i < 4; i++ {
		ring.GetNextToSend(idx)
	}

	if !ring.IsComplete(idx) {
		t.Error("Should be complete after all data fragments sent")
	}
}

func TestRingAdvance(t *testing.T) {
	ring, _ := NewRing(4, 6)

	// Allocate a few blocks
	_, _, _ = ring.GetBlockRingIdx(0)
	_, _, _ = ring.GetBlockRingIdx(1)
	_, _, _ = ring.GetBlockRingIdx(2)

	alloc, _ := ring.Stats()
	if alloc != 3 {
		t.Errorf("Initial alloc = %d, want 3", alloc)
	}

	// Check front
	if ring.FrontBlockIdx() != 0 {
		t.Errorf("Initial front = %d, want 0", ring.FrontBlockIdx())
	}

	// Advance
	ring.Advance()

	alloc, _ = ring.Stats()
	if alloc != 2 {
		t.Errorf("After advance: alloc = %d, want 2", alloc)
	}

	if ring.FrontBlockIdx() != 1 {
		t.Errorf("After advance: front = %d, want 1", ring.FrontBlockIdx())
	}
}

func TestRingReset(t *testing.T) {
	ring, _ := NewRing(4, 6)

	// Use the ring
	_, _, _ = ring.GetBlockRingIdx(0)
	_, _, _ = ring.GetBlockRingIdx(1)

	// Reset with new FEC params
	err := ring.Reset(8, 12)
	if err != nil {
		t.Fatalf("Reset failed: %v", err)
	}

	if ring.fecK != 8 || ring.fecN != 12 {
		t.Errorf("FEC params after reset = (%d, %d), want (8, 12)", ring.fecK, ring.fecN)
	}

	alloc, lastKnown := ring.Stats()
	if alloc != 0 || lastKnown != noLastKnownBlock {
		t.Errorf("Stats after reset = (%d, %d), want (0, noLastKnownBlock)", alloc, lastKnown)
	}
}

func TestModN(t *testing.T) {
	tests := []struct {
		x, base, want int
	}{
		{0, 10, 0},
		{5, 10, 5},
		{10, 10, 0},
		{-1, 10, 9},
		{-5, 10, 5},
		{15, 10, 5},
	}

	for _, tt := range tests {
		got := modN(tt.x, tt.base)
		if got != tt.want {
			t.Errorf("modN(%d, %d) = %d, want %d", tt.x, tt.base, got, tt.want)
		}
	}
}
