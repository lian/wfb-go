package tx

import (
	"net"

	"github.com/lian/wfb-go/pkg/protocol"
)

// UDPInjector sends packets via UDP (for testing/forwarding).
type UDPInjector struct {
	conn     *net.UDPConn
	destAddr *net.UDPAddr
}

// NewUDPInjector creates a new UDP injector.
func NewUDPInjector(destAddr string) (*UDPInjector, error) {
	addr, err := net.ResolveUDPAddr("udp", destAddr)
	if err != nil {
		return nil, err
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, err
	}

	return &UDPInjector{
		conn:     conn,
		destAddr: addr,
	}, nil
}

// Inject sends the packet via UDP.
func (u *UDPInjector) Inject(data []byte) error {
	_, err := u.conn.Write(data)
	return err
}

// Close closes the UDP connection.
func (u *UDPInjector) Close() error {
	return u.conn.Close()
}

// ForwarderInjector sends packets via UDP with the wrxfwd_t header.
// This is used for distributed TX where packets are forwarded to remote injectors.
type ForwarderInjector struct {
	conn     *net.UDPConn
	destAddr *net.UDPAddr
}

// NewForwarderInjector creates a new forwarder injector.
func NewForwarderInjector(destAddr string) (*ForwarderInjector, error) {
	addr, err := net.ResolveUDPAddr("udp", destAddr)
	if err != nil {
		return nil, err
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, err
	}

	return &ForwarderInjector{
		conn:     conn,
		destAddr: addr,
	}, nil
}

// Inject sends the packet via UDP with a fake RX forward header.
func (f *ForwarderInjector) Inject(data []byte) error {
	// Build wrxfwd_t header (for compatibility with wfb-ng aggregator)
	fwdHdr := protocol.RXForwardHeader{
		WlanIdx:   0,
		Antenna:   [4]uint8{0, 0xFF, 0xFF, 0xFF},
		RSSI:      [4]int8{-42, -128, -128, -128},
		Noise:     [4]int8{-70, 127, 127, 127},
		Freq:      5180, // Example 5GHz channel
		MCSIndex:  1,
		Bandwidth: 20,
	}

	// Combine header + payload
	pkt := make([]byte, protocol.RXForwardHeaderSize+len(data))
	copy(pkt[:protocol.RXForwardHeaderSize], fwdHdr.Marshal())
	copy(pkt[protocol.RXForwardHeaderSize:], data)

	_, err := f.conn.Write(pkt)
	return err
}

// Close closes the UDP connection.
func (f *ForwarderInjector) Close() error {
	return f.conn.Close()
}

// BufferInjector collects packets in memory (for testing).
type BufferInjector struct {
	Packets [][]byte
}

// NewBufferInjector creates a new buffer injector for testing.
func NewBufferInjector() *BufferInjector {
	return &BufferInjector{
		Packets: make([][]byte, 0),
	}
}

// Inject stores the packet in the buffer.
func (b *BufferInjector) Inject(data []byte) error {
	pkt := make([]byte, len(data))
	copy(pkt, data)
	b.Packets = append(b.Packets, pkt)
	return nil
}

// Clear clears all stored packets.
func (b *BufferInjector) Clear() {
	b.Packets = b.Packets[:0]
}

// Count returns the number of stored packets.
func (b *BufferInjector) Count() int {
	return len(b.Packets)
}
