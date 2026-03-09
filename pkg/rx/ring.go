// Package rx implements the WFB receiver.
package rx

import (
	"encoding/binary"

	"github.com/lian/wfb-go/pkg/fec"
	"github.com/lian/wfb-go/pkg/protocol"
)

const (
	// RingSize is the number of FEC blocks in the receive ring buffer.
	RingSize = 40

	// noLastKnownBlock indicates no block has been seen yet.
	// Matches wfb-ng's (uint64_t)-1 initialization for last_known_block.
	noLastKnownBlock = ^uint64(0) // 0xFFFFFFFFFFFFFFFF
)

// RingItem represents a single FEC block in the ring buffer.
type RingItem struct {
	BlockIdx       uint64
	Fragments      [][]byte // Fragment data buffers
	FragmentSizes  []int    // Size of each received fragment (0 = not received)
	FragmentToSend uint8    // Next fragment index to send to output
	HasFragments   uint8    // Number of received fragments
}

// Ring is a circular buffer for FEC block reassembly.
type Ring struct {
	items      [RingSize]RingItem
	front      int    // Index of current/front block
	alloc      int    // Number of allocated entries
	lastKnown  uint64 // Last known block index
	fecK       int
	fecN       int
	decoder    *fec.Decoder
	maxPayload int
}

// NewRing creates a new receive ring buffer.
func NewRing(k, n int) (*Ring, error) {
	decoder, err := fec.NewDecoder(k, n)
	if err != nil {
		return nil, err
	}

	r := &Ring{
		fecK:       k,
		fecN:       n,
		decoder:    decoder,
		maxPayload: protocol.MAX_FEC_PAYLOAD,
		lastKnown:  noLastKnownBlock, // No blocks seen yet (matches wfb-ng)
	}

	// Initialize all ring items
	for i := 0; i < RingSize; i++ {
		r.items[i].Fragments = make([][]byte, n)
		r.items[i].FragmentSizes = make([]int, n)
		for j := 0; j < n; j++ {
			r.items[i].Fragments[j] = make([]byte, protocol.MAX_FEC_PAYLOAD)
		}
	}

	return r, nil
}

// Reset reinitializes the ring with new FEC parameters.
func (r *Ring) Reset(k, n int) error {
	// Close old decoder if it exists
	if r.decoder != nil {
		r.decoder.Close()
	}

	decoder, err := fec.NewDecoder(k, n)
	if err != nil {
		return err
	}

	r.decoder = decoder
	r.fecK = k
	r.fecN = n
	r.front = 0
	r.alloc = 0
	r.lastKnown = noLastKnownBlock

	// Reallocate fragment buffers if n changed
	for i := 0; i < RingSize; i++ {
		if len(r.items[i].Fragments) != n {
			r.items[i].Fragments = make([][]byte, n)
			r.items[i].FragmentSizes = make([]int, n)
			for j := 0; j < n; j++ {
				r.items[i].Fragments[j] = make([]byte, protocol.MAX_FEC_PAYLOAD)
			}
		}
		r.clearItem(i)
	}

	return nil
}

// clearItem resets a ring item to empty state.
func (r *Ring) clearItem(idx int) {
	r.items[idx].BlockIdx = 0
	r.items[idx].HasFragments = 0
	r.items[idx].FragmentToSend = 0
	for i := 0; i < r.fecN; i++ {
		r.items[idx].FragmentSizes[i] = 0
	}
}

// modN computes positive modulo.
func modN(x, base int) int {
	return (base + (x % base)) % base
}

// GetBlockRingIdx finds or allocates a ring slot for the given block index.
// Returns (ringIdx, evicted[], valid). Evicted contains indices of blocks that
// were evicted due to overflow - caller should flush these before they're lost.
// Matches wfb-ng's get_block_ring_idx() logic.
func (r *Ring) GetBlockRingIdx(blockIdx uint64) (int, []int, bool) {
	// Check if block is already in the ring
	for i, c := r.front, r.alloc; c > 0; i, c = modN(i+1, RingSize), c-1 {
		if r.items[i].BlockIdx == blockIdx {
			return i, nil, true
		}
	}

	// Block is already known (processed) and not in ring - reject
	if r.lastKnown != noLastKnownBlock && blockIdx <= r.lastKnown {
		return -1, nil, false
	}

	// Calculate how many new blocks to push
	var newBlocks int
	if r.lastKnown != noLastKnownBlock {
		newBlocks = int(blockIdx - r.lastKnown)
		if newBlocks > RingSize {
			newBlocks = RingSize
		}
	} else {
		newBlocks = 1
	}

	// Update last known block
	r.lastKnown = blockIdx

	// Push new blocks, collecting any evicted indices
	var evicted []int
	var ringIdx int
	for i := 0; i < newBlocks; i++ {
		var evictedIdx int
		ringIdx, evictedIdx = r.ringPush()
		if evictedIdx >= 0 {
			evicted = append(evicted, evictedIdx)
		}
		r.items[ringIdx].BlockIdx = blockIdx + uint64(i+1-newBlocks)
		r.items[ringIdx].FragmentToSend = 0
		r.items[ringIdx].HasFragments = 0
		for j := 0; j < r.fecN; j++ {
			r.items[ringIdx].FragmentSizes[j] = 0
		}
	}

	return ringIdx, evicted, true
}

// ringPush allocates a new slot in the ring.
// Returns (newIdx, evictedIdx) where evictedIdx is -1 if no eviction occurred.
func (r *Ring) ringPush() (int, int) {
	if r.alloc < RingSize {
		idx := modN(r.front+r.alloc, RingSize)
		r.alloc++
		return idx, -1
	}

	// Ring overflow - return the evicted index so caller can flush it
	evicted := r.front
	r.front = modN(r.front+1, RingSize)
	return evicted, evicted
}

// AddFragment adds a decrypted fragment to the ring.
// Returns true if block is now complete (has k fragments).
func (r *Ring) AddFragment(ringIdx int, fragmentIdx uint8, data []byte) bool {
	if ringIdx < 0 || ringIdx >= RingSize {
		return false
	}
	if int(fragmentIdx) >= r.fecN {
		return false
	}

	item := &r.items[ringIdx]

	// Already have this fragment
	if item.FragmentSizes[fragmentIdx] > 0 {
		return item.HasFragments >= uint8(r.fecK)
	}

	// Store fragment
	copy(item.Fragments[fragmentIdx], data)
	item.FragmentSizes[fragmentIdx] = len(data)
	item.HasFragments++

	return item.HasFragments >= uint8(r.fecK)
}

// CanRecover checks if a block has enough fragments for FEC recovery.
func (r *Ring) CanRecover(ringIdx int) bool {
	if ringIdx < 0 || ringIdx >= RingSize {
		return false
	}
	return r.items[ringIdx].HasFragments >= uint8(r.fecK)
}

// NeedsRecovery checks if a block needs FEC recovery (has k fragments but missing some data shards).
func (r *Ring) NeedsRecovery(ringIdx int) bool {
	if ringIdx < 0 || ringIdx >= RingSize {
		return false
	}

	item := &r.items[ringIdx]
	if item.HasFragments < uint8(r.fecK) {
		return false
	}

	// Check if any data shard is missing
	for i := 0; i < r.fecK; i++ {
		if item.FragmentSizes[i] == 0 {
			return true
		}
	}
	return false
}

// Recover performs FEC recovery on a block.
// Returns the number of recovered fragments.
func (r *Ring) Recover(ringIdx int) (int, error) {
	if ringIdx < 0 || ringIdx >= RingSize {
		return 0, nil
	}

	item := &r.items[ringIdx]

	// Find max packet size (from parity shards which are always max size)
	maxSize := 0
	for i := r.fecK; i < r.fecN; i++ {
		if item.FragmentSizes[i] > maxSize {
			maxSize = item.FragmentSizes[i]
		}
	}

	if maxSize == 0 {
		// No parity shards, check data shards
		for i := 0; i < r.fecK; i++ {
			if item.FragmentSizes[i] > maxSize {
				maxSize = item.FragmentSizes[i]
			}
		}
	}

	// Build shards array for decoder
	shards := make([][]byte, r.fecN)
	for i := 0; i < r.fecN; i++ {
		if item.FragmentSizes[i] > 0 {
			shards[i] = item.Fragments[i][:maxSize]
		} else {
			shards[i] = nil // Missing shard
		}
	}

	// Perform FEC reconstruction
	if err := r.decoder.Reconstruct(shards); err != nil {
		return 0, err
	}

	// Copy recovered fragments back to ring buffer and update sizes
	recovered := 0
	for i := 0; i < r.fecK; i++ {
		if item.FragmentSizes[i] == 0 {
			// The decoder allocated a new slice for this shard
			// Copy the reconstructed data back to our pre-allocated buffer
			copy(item.Fragments[i], shards[i])

			// Read actual packet size from the recovered packet header
			// Header format: flags (1 byte) + packet_size (2 bytes big-endian)
			actualSize := maxSize
			if len(shards[i]) >= protocol.PACKET_HDR_SIZE {
				packetSize := int(binary.BigEndian.Uint16(shards[i][1:3]))
				// Total fragment size = header (3) + payload
				actualSize = protocol.PACKET_HDR_SIZE + packetSize
				if actualSize > maxSize {
					actualSize = maxSize // Sanity check
				}
			}
			item.FragmentSizes[i] = actualSize
			recovered++
		}
	}

	return recovered, nil
}

// GetFragment returns a fragment from the ring.
func (r *Ring) GetFragment(ringIdx int, fragmentIdx uint8) ([]byte, int) {
	if ringIdx < 0 || ringIdx >= RingSize {
		return nil, 0
	}
	if int(fragmentIdx) >= r.fecN {
		return nil, 0
	}

	item := &r.items[ringIdx]
	size := item.FragmentSizes[fragmentIdx]
	if size == 0 {
		return nil, 0
	}

	return item.Fragments[fragmentIdx][:size], size
}

// GetFragmentBuffer returns the pre-allocated buffer for a fragment.
// Used for zero-copy decryption directly into the ring buffer.
// Returns nil if fragment already exists or indices are invalid.
// IMPORTANT: The buffer is zeroed before returning to ensure correct FEC recovery.
// FEC requires all fragments to be the same size (max_packet_size). Data fragments
// may be smaller than parity fragments. Zeroing ensures padding bytes match TX side.
func (r *Ring) GetFragmentBuffer(ringIdx int, fragmentIdx uint8) []byte {
	if ringIdx < 0 || ringIdx >= RingSize {
		return nil
	}
	if int(fragmentIdx) >= r.fecN {
		return nil
	}

	item := &r.items[ringIdx]

	// Already have this fragment
	if item.FragmentSizes[fragmentIdx] > 0 {
		return nil
	}

	// Zero the buffer before returning (critical for FEC recovery)
	// wfb-ng does: memset(p->fragments[fragment_idx], '\0', MAX_FEC_PAYLOAD)
	buf := item.Fragments[fragmentIdx]
	clear(buf) // Uses efficient memory zeroing (Go 1.21+)

	return buf
}

// SetFragmentSize marks a fragment as filled after direct decryption.
// Returns true if block is now complete (has k fragments).
func (r *Ring) SetFragmentSize(ringIdx int, fragmentIdx uint8, size int) bool {
	if ringIdx < 0 || ringIdx >= RingSize {
		return false
	}
	if int(fragmentIdx) >= r.fecN {
		return false
	}

	item := &r.items[ringIdx]
	item.FragmentSizes[fragmentIdx] = size
	item.HasFragments++

	return item.HasFragments >= uint8(r.fecK)
}

// GetNextToSend returns the next fragment index to send and advances the pointer.
func (r *Ring) GetNextToSend(ringIdx int) (uint8, bool) {
	if ringIdx < 0 || ringIdx >= RingSize {
		return 0, false
	}

	item := &r.items[ringIdx]
	if item.FragmentToSend >= uint8(r.fecK) {
		return 0, false
	}

	idx := item.FragmentToSend
	item.FragmentToSend++
	return idx, true
}

// PeekNextToSend returns the next fragment index to send without advancing.
func (r *Ring) PeekNextToSend(ringIdx int) (uint8, bool) {
	if ringIdx < 0 || ringIdx >= RingSize {
		return 0, false
	}

	item := &r.items[ringIdx]
	if item.FragmentToSend >= uint8(r.fecK) {
		return 0, false
	}

	return item.FragmentToSend, true
}

// AdvanceFragmentToSend advances the fragment send pointer by one.
func (r *Ring) AdvanceFragmentToSend(ringIdx int) {
	if ringIdx < 0 || ringIdx >= RingSize {
		return
	}
	item := &r.items[ringIdx]
	if item.FragmentToSend < uint8(r.fecK) {
		item.FragmentToSend++
	}
}

// WasSent checks if a fragment has already been sent (index < FragmentToSend).
func (r *Ring) WasSent(ringIdx int, fragmentIdx uint8) bool {
	if ringIdx < 0 || ringIdx >= RingSize {
		return true
	}
	return fragmentIdx < r.items[ringIdx].FragmentToSend
}

// CountMissing counts the number of data fragments (0..k-1) that were never received.
func (r *Ring) CountMissing(ringIdx int) int {
	if ringIdx < 0 || ringIdx >= RingSize {
		return 0
	}
	item := &r.items[ringIdx]
	count := 0
	for i := 0; i < r.fecK; i++ {
		if item.FragmentSizes[i] == 0 {
			count++
		}
	}
	return count
}

// FecK returns the FEC k parameter.
func (r *Ring) FecK() int {
	return r.fecK
}

// IsComplete checks if all data fragments of a block have been sent.
func (r *Ring) IsComplete(ringIdx int) bool {
	if ringIdx < 0 || ringIdx >= RingSize {
		return true
	}
	return r.items[ringIdx].FragmentToSend >= uint8(r.fecK)
}

// Front returns the front ring index.
func (r *Ring) Front() int {
	return r.front
}

// FrontBlockIdx returns the block index at the front of the ring.
func (r *Ring) FrontBlockIdx() uint64 {
	if r.alloc == 0 {
		return 0
	}
	return r.items[r.front].BlockIdx
}

// GetBlockIdx returns the block index for a given ring index.
func (r *Ring) GetBlockIdx(ringIdx int) uint64 {
	if ringIdx < 0 || ringIdx >= RingSize {
		return 0
	}
	return r.items[ringIdx].BlockIdx
}

// Advance moves the front of the ring forward by one.
func (r *Ring) Advance() {
	if r.alloc > 0 {
		r.clearItem(r.front)
		r.front = modN(r.front+1, RingSize)
		r.alloc--
	}
}

// Stats returns ring statistics.
func (r *Ring) Stats() (alloc int, lastKnown uint64) {
	return r.alloc, r.lastKnown
}
