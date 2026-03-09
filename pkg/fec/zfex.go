// Package fec provides Reed-Solomon FEC encoding/decoding.
// This file implements a pure Go version of zfex.
package fec

import (
	"errors"
)

var (
	ErrInvalidParams     = errors.New("fec: invalid parameters (need 1 <= k <= n <= 255)")
	ErrTooFewShards      = errors.New("fec: too few shards for reconstruction")
	ErrShardSizeMismatch = errors.New("fec: shard sizes do not match")
)

// Encoder provides FEC encoding compatible with zfex.
type Encoder struct {
	k         int
	n         int
	encMatrix []byte // n*k encoding matrix
}

// NewEncoder creates an encoder for the given k (data shards) and n (total shards).
func NewEncoder(k, n int) (*Encoder, error) {
	if k < 1 || n < k || n > 255 {
		return nil, ErrInvalidParams
	}

	initGF()

	e := &Encoder{
		k:         k,
		n:         n,
		encMatrix: make([]byte, n*k),
	}

	// Build encoding matrix (same algorithm as zfex)
	e.buildEncMatrix()

	return e, nil
}

// buildEncMatrix constructs the systematic encoding matrix.
// This matches zfex's fec_new() exactly.
func (e *Encoder) buildEncMatrix() {
	k := e.k
	n := e.n

	// Temporary Vandermonde matrix
	tmpM := make([]byte, n*k)

	// First row is special: [1, 0, 0, ..., 0]
	tmpM[0] = 1
	for col := 1; col < k; col++ {
		tmpM[col] = 0
	}

	// Remaining rows: element at (row, col) = α^(row*col)
	// Note: row index starts from 0 for the second row in the matrix
	for row := 0; row < n-1; row++ {
		for col := 0; col < k; col++ {
			tmpM[(row+1)*k+col] = gfExp[modnn(row*col)]
		}
	}

	// Invert top k*k Vandermonde matrix
	invertVandermonde(tmpM, k)

	// Multiply bottom (n-k) rows by the inverted top k*k matrix
	// Result goes into encMatrix rows k to n-1
	matMul(tmpM[k*k:], tmpM[:k*k], e.encMatrix[k*k:], n-k, k, k)

	// Top k*k of encMatrix is identity matrix
	for i := 0; i < k*k; i++ {
		e.encMatrix[i] = 0
	}
	for i := 0; i < k; i++ {
		e.encMatrix[i*k+i] = 1
	}
}

// K returns the number of data shards.
func (e *Encoder) K() int { return e.k }

// N returns the total number of shards.
func (e *Encoder) N() int { return e.n }

// Close releases resources (no-op for pure Go implementation).
func (e *Encoder) Close() {}

// Encode generates parity shards from data shards.
// dataShards must have exactly k elements, all the same size.
// parityShards must have exactly n-k elements, pre-allocated to the same size.
func (e *Encoder) Encode(dataShards [][]byte, parityShards [][]byte) error {
	k := e.k
	n := e.n
	numParity := n - k

	if len(dataShards) != k {
		return ErrTooFewShards
	}
	if len(parityShards) != numParity {
		return errors.New("fec: wrong number of parity shards")
	}

	if k == 0 || numParity == 0 {
		return nil
	}

	// Get shard size from first data shard
	sz := len(dataShards[0])
	if sz == 0 {
		return nil
	}

	// Verify all data shards are the same size
	for i := 1; i < k; i++ {
		if len(dataShards[i]) != sz {
			return ErrShardSizeMismatch
		}
	}

	// Verify parity shards are allocated and same size
	for i := 0; i < numParity; i++ {
		if len(parityShards[i]) < sz {
			return errors.New("fec: parity shard too small")
		}
	}

	// Generate parity shards
	for i := 0; i < numParity; i++ {
		fecNum := i + k
		// Clear parity shard
		for j := 0; j < sz; j++ {
			parityShards[i][j] = 0
		}

		// Multiply by encoding matrix row
		matrixRow := e.encMatrix[fecNum*k : fecNum*k+k]
		for j := 0; j < k; j++ {
			gfAddMul(parityShards[i][:sz], dataShards[j][:sz], matrixRow[j])
		}
	}

	return nil
}

// EncodeInPlace takes a slice of n shards where the first k contain data
// and the remaining n-k will be filled with parity data.
// All shards must be pre-allocated and the same size.
func (e *Encoder) EncodeInPlace(shards [][]byte) error {
	if len(shards) != e.n {
		return ErrTooFewShards
	}
	return e.Encode(shards[:e.k], shards[e.k:])
}

// Decoder provides FEC decoding compatible with zfex.
// This is a pure Go implementation.
type Decoder struct {
	k         int
	n         int
	encMatrix []byte // n*k encoding matrix (same as encoder)
}

// NewDecoder creates a decoder using pure Go implementation.
func NewDecoder(k, n int) (*Decoder, error) {
	if k < 1 || n < k || n > 255 {
		return nil, ErrInvalidParams
	}

	initGF()

	d := &Decoder{
		k:         k,
		n:         n,
		encMatrix: make([]byte, n*k),
	}

	// Build encoding matrix (same as encoder)
	d.buildEncMatrix()

	return d, nil
}

// buildEncMatrix constructs the systematic encoding matrix.
func (d *Decoder) buildEncMatrix() {
	k := d.k
	n := d.n

	tmpM := make([]byte, n*k)

	tmpM[0] = 1
	for col := 1; col < k; col++ {
		tmpM[col] = 0
	}

	for row := 0; row < n-1; row++ {
		for col := 0; col < k; col++ {
			tmpM[(row+1)*k+col] = gfExp[modnn(row*col)]
		}
	}

	invertVandermonde(tmpM, k)
	matMul(tmpM[k*k:], tmpM[:k*k], d.encMatrix[k*k:], n-k, k, k)

	for i := 0; i < k*k; i++ {
		d.encMatrix[i] = 0
	}
	for i := 0; i < k; i++ {
		d.encMatrix[i*k+i] = 1
	}
}

// K returns the number of data shards.
func (d *Decoder) K() int { return d.k }

// N returns the total number of shards.
func (d *Decoder) N() int { return d.n }

// Close releases resources (no-op for pure Go implementation).
func (d *Decoder) Close() {}

// Reconstruct reconstructs missing data shards.
// Input shards should be a slice of n elements.
// Missing shards should be nil (or zero length).
// After return, all data shards (0 to k-1) will be filled.
func (d *Decoder) Reconstruct(shards [][]byte) error {
	k := d.k
	n := d.n

	if len(shards) != n {
		return ErrTooFewShards
	}

	// Count available shards and determine size
	available := 0
	sz := 0
	for i := 0; i < n; i++ {
		if shards[i] != nil && len(shards[i]) > 0 {
			available++
			if sz == 0 {
				sz = len(shards[i])
			}
		}
	}

	if available < k {
		return ErrTooFewShards
	}

	// Find missing data shards
	var missingDataIndices []int
	for i := 0; i < k; i++ {
		if shards[i] == nil || len(shards[i]) == 0 {
			missingDataIndices = append(missingDataIndices, i)
		}
	}

	if len(missingDataIndices) == 0 {
		// No reconstruction needed
		return nil
	}

	// Build input array of k shards for decoding
	// If data shard i is present, it must be at position i
	// Parity shards fill in for missing data shards
	inputShards := make([][]byte, k)
	indices := make([]int, k)

	// First pass: place available data shards in their positions
	for i := 0; i < k; i++ {
		if shards[i] != nil && len(shards[i]) > 0 {
			inputShards[i] = shards[i]
			indices[i] = i
		}
	}

	// Second pass: fill missing positions with parity shards
	parityIdx := k
	for i := 0; i < k; i++ {
		if inputShards[i] == nil {
			// Find next available parity shard
			for parityIdx < n {
				if shards[parityIdx] != nil && len(shards[parityIdx]) > 0 {
					inputShards[i] = shards[parityIdx]
					indices[i] = parityIdx
					parityIdx++
					break
				}
				parityIdx++
			}
		}
	}

	// Shuffle input shards so that data shards are in their natural positions
	shuffleShards(inputShards, indices, k)

	// Build decode matrix
	decMatrix := make([]byte, k*k)
	d.buildDecodeMatrix(indices, decMatrix)

	// Allocate output buffers for missing data shards
	outputShards := make([][]byte, len(missingDataIndices))
	for i, idx := range missingDataIndices {
		if shards[idx] == nil {
			shards[idx] = make([]byte, sz)
		}
		outputShards[i] = shards[idx]
	}

	// Decode missing shards
	outIdx := 0
	for row := 0; row < k; row++ {
		if indices[row] >= k {
			// This row needs reconstruction
			// Clear output
			for j := 0; j < sz; j++ {
				outputShards[outIdx][j] = 0
			}

			// Multiply by decode matrix row
			for col := 0; col < k; col++ {
				gfAddMul(outputShards[outIdx][:sz], inputShards[col][:sz], decMatrix[row*k+col])
			}
			outIdx++
		}
	}

	return nil
}

// buildDecodeMatrix builds the decode matrix for the given shard indices.
func (d *Decoder) buildDecodeMatrix(indices []int, matrix []byte) {
	k := d.k

	// Build matrix from encoding matrix rows
	for i := 0; i < k; i++ {
		if indices[i] < k {
			// Data shard - identity row
			for j := 0; j < k; j++ {
				matrix[i*k+j] = 0
			}
			matrix[i*k+i] = 1
		} else {
			// Parity shard - copy encoding matrix row
			copy(matrix[i*k:i*k+k], d.encMatrix[indices[i]*k:indices[i]*k+k])
		}
	}

	// Invert the matrix
	invertMatrix(matrix, k)
}

// shuffleShards reorders shards so data shards are in natural positions.
func shuffleShards(shards [][]byte, indices []int, k int) {
	for i := 0; i < k; {
		if indices[i] >= k || indices[i] == i {
			i++
		} else {
			// Swap with correct position
			c := indices[i]
			indices[i], indices[c] = indices[c], indices[i]
			shards[i], shards[c] = shards[c], shards[i]
		}
	}
}

// matMul computes C = A * B where A is n*k, B is k*m, C is n*m.
func matMul(a, b, c []byte, n, k, m int) {
	for row := 0; row < n; row++ {
		for col := 0; col < m; col++ {
			var acc byte
			for i := 0; i < k; i++ {
				acc ^= gfMul(a[row*k+i], b[i*m+col])
			}
			c[row*m+col] = acc
		}
	}
}

// invertVandermonde inverts a Vandermonde matrix in place.
// This is the fast algorithm from zfex (_invert_vdm).
func invertVandermonde(src []byte, k int) {
	if k == 1 {
		return
	}

	c := make([]byte, k)
	b := make([]byte, k)
	p := make([]byte, k)

	// Extract p values from second column
	for i := 0; i < k; i++ {
		p[i] = src[i*k+1]
	}

	// Construct coefficients of P(x) = Prod(x - p_i)
	c[k-1] = p[0]
	for i := 1; i < k; i++ {
		pi := p[i]
		for j := k - 1 - (i - 1); j < k-1; j++ {
			c[j] ^= gfMul(pi, c[j+1])
		}
		c[k-1] ^= pi
	}

	// Compute inverse using synthetic division
	for row := 0; row < k; row++ {
		xx := p[row]
		var t byte = 1
		b[k-1] = 1 // c[k] = 1 implicitly

		for i := k - 1; i > 0; i-- {
			b[i-1] = c[i] ^ gfMul(xx, b[i])
			t = gfMul(xx, t) ^ b[i-1]
		}

		for col := 0; col < k; col++ {
			src[col*k+row] = gfMul(gfInverse[t], b[col])
		}
	}
}

// invertMatrix inverts a general matrix using Gauss-Jordan elimination.
// This matches zfex's _invert_mat.
func invertMatrix(src []byte, k int) {
	indxc := make([]int, k)
	indxr := make([]int, k)
	ipiv := make([]int, k)
	idRow := make([]byte, k)

	var irow, icol int

	for col := 0; col < k; col++ {
		// Find pivot
		if ipiv[col] != 1 && src[col*k+col] != 0 {
			irow = col
			icol = col
		} else {
			found := false
			for row := 0; row < k && !found; row++ {
				if ipiv[row] != 1 {
					for ix := 0; ix < k; ix++ {
						if ipiv[ix] == 0 && src[row*k+ix] != 0 {
							irow = row
							icol = ix
							found = true
							break
						}
					}
				}
			}
		}

		ipiv[icol]++

		// Swap rows if needed
		if irow != icol {
			for ix := 0; ix < k; ix++ {
				src[irow*k+ix], src[icol*k+ix] = src[icol*k+ix], src[irow*k+ix]
			}
		}

		indxr[col] = irow
		indxc[col] = icol

		pivotRow := src[icol*k : icol*k+k]
		c := pivotRow[icol]

		if c != 1 {
			c = gfInverse[c]
			pivotRow[icol] = 1
			for ix := 0; ix < k; ix++ {
				pivotRow[ix] = gfMul(c, pivotRow[ix])
			}
		}

		// Eliminate other rows
		idRow[icol] = 1
		isIdentity := true
		for ix := 0; ix < k; ix++ {
			if pivotRow[ix] != idRow[ix] {
				isIdentity = false
				break
			}
		}

		if !isIdentity {
			for ix := 0; ix < k; ix++ {
				if ix != icol {
					rowPtr := src[ix*k : ix*k+k]
					c := rowPtr[icol]
					rowPtr[icol] = 0
					gfAddMul(rowPtr, pivotRow, c)
				}
			}
		}
		idRow[icol] = 0
	}

	// Undo column swaps
	for col := k - 1; col >= 0; col-- {
		if indxr[col] != indxc[col] {
			for row := 0; row < k; row++ {
				src[row*k+indxr[col]], src[row*k+indxc[col]] = src[row*k+indxc[col]], src[row*k+indxr[col]]
			}
		}
	}
}
