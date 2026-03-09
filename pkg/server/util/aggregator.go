package util

import (
	"sync"
	"time"
)

// PacketAggregator buffers small packets and flushes them based on size or timeout.
// This improves link efficiency by reducing per-packet overhead.
//
// Used by:
//   - Mavlink: aggregates telemetry messages (timeout ~100ms)
//   - Tunnel: aggregates IP packets with length framing (timeout ~5ms)
type PacketAggregator struct {
	mu sync.Mutex

	maxSize int           // Maximum aggregated size before flush
	timeout time.Duration // Timeout to flush incomplete buffer

	buffer    []byte   // Current aggregated data (non-framed mode)
	packets   [][]byte // Individual packets (framed mode)
	size      int      // Current buffer size
	useFrames bool     // Whether to use 2-byte length framing

	flushFn func([]byte) // Callback to send aggregated data
	timer   *time.Timer  // Flush timer
	stopped bool
}

// PacketAggregatorConfig holds configuration for the aggregator.
type PacketAggregatorConfig struct {
	MaxSize   int           // Maximum aggregated size (usually MTU - headers)
	Timeout   time.Duration // Flush timeout (e.g., 100ms for mavlink, 5ms for tunnel)
	UseFrames bool          // Use 2-byte big-endian length framing for each packet
	FlushFn   func([]byte)  // Callback when flushing
}

// NewPacketAggregator creates a new packet aggregator.
func NewPacketAggregator(cfg PacketAggregatorConfig) *PacketAggregator {
	return &PacketAggregator{
		maxSize:   cfg.MaxSize,
		timeout:   cfg.Timeout,
		useFrames: cfg.UseFrames,
		flushFn:   cfg.FlushFn,
		packets:   make([][]byte, 0, 16),
	}
}

// Add adds a packet to the aggregation buffer.
// Returns true if the packet was added, false if it was too large.
func (a *PacketAggregator) Add(data []byte) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.stopped {
		return false
	}

	// Calculate the size this packet will take
	packetSize := len(data)
	if a.useFrames {
		packetSize += 2 // 2-byte length header
	}

	// Check if packet itself is too large
	if packetSize > a.maxSize {
		return false
	}

	// Check if adding this packet would exceed max size
	if a.size+packetSize > a.maxSize {
		// Flush current buffer first
		a.flushLocked()
	}

	// Add packet to buffer
	if a.useFrames {
		// Store packet for later framing
		pktCopy := make([]byte, len(data))
		copy(pktCopy, data)
		a.packets = append(a.packets, pktCopy)
		a.size += packetSize
	} else {
		// No framing, just append raw data
		a.buffer = append(a.buffer, data...)
		a.size += len(data)
	}

	// Start timer if not already running
	if a.timer == nil && a.timeout > 0 {
		a.timer = time.AfterFunc(a.timeout, func() {
			a.mu.Lock()
			defer a.mu.Unlock()
			if !a.stopped {
				a.flushLocked()
			}
		})
	}

	return true
}

// Flush forces a flush of the current buffer.
func (a *PacketAggregator) Flush() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.flushLocked()
}

func (a *PacketAggregator) flushLocked() {
	if a.size == 0 {
		return
	}

	// Cancel timer
	if a.timer != nil {
		a.timer.Stop()
		a.timer = nil
	}

	// Build output data
	var data []byte
	if a.useFrames {
		// Build framed output: [len1][pkt1][len2][pkt2]...
		data = make([]byte, 0, a.size)
		for _, pkt := range a.packets {
			// Big-endian 16-bit length
			data = append(data, byte(len(pkt)>>8), byte(len(pkt)))
			data = append(data, pkt...)
		}
		a.packets = a.packets[:0]
	} else {
		data = a.buffer
		a.buffer = nil
	}
	a.size = 0

	// Send via callback
	if a.flushFn != nil && len(data) > 0 {
		a.flushFn(data)
	}
}

// Stop stops the aggregator and flushes any remaining data.
func (a *PacketAggregator) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.stopped = true
	a.flushLocked()
}

// Size returns the current buffered size.
func (a *PacketAggregator) Size() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.size
}
