// wfb_tun creates a TUN tunnel for IP networking over WFB.
//
// Usage:
//
//	wfb_tun [options]
//
// Example (drone side):
//
//	wfb_tun -t wfb-tun -a 10.5.0.1/24 -c 127.0.0.1 -u 5801 -l 5800
//
// Example (ground station side):
//
//	wfb_tun -t wfb-tun -a 10.5.0.2/24 -c 127.0.0.1 -u 5800 -l 5801
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
	"unsafe"

	"github.com/lian/wfb-go/pkg/version"
	"golang.org/x/sys/unix"
)

const (
	// MTU for radio packets
	MTU = 1445

	// Ping interval for keepalive
	PingIntervalMS = 500

	// Default aggregation timeout
	DefaultAggTimeoutMS = 5
)

// TUN packet header (2 bytes for packet size)
const tunPacketHdrSize = 2

// Config holds command-line configuration.
type Config struct {
	TunName      string
	TunAddr      string
	PeerAddr     string
	PeerPort     int
	ListenPort   int
	AggTimeoutMS int
}

func main() {
	cfg := parseFlags()

	if err := run(cfg); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func parseFlags() *Config {
	cfg := &Config{}

	flag.StringVar(&cfg.TunName, "t", "wfb-tun", "TUN interface name")
	flag.StringVar(&cfg.TunAddr, "a", "10.5.0.2/24", "TUN interface address (CIDR)")
	flag.StringVar(&cfg.PeerAddr, "c", "127.0.0.1", "Peer UDP address")
	flag.IntVar(&cfg.PeerPort, "u", 5801, "Peer UDP port")
	flag.IntVar(&cfg.ListenPort, "l", 5800, "Local UDP listen port")
	flag.IntVar(&cfg.AggTimeoutMS, "T", DefaultAggTimeoutMS, "Aggregation timeout in ms (0=disabled)")

	var showVersion bool
	flag.BoolVar(&showVersion, "version", false, "Show version")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "wfb_tun - TUN tunnel for WFB\n\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExample (drone):\n")
		fmt.Fprintf(os.Stderr, "  %s -t wfb-tun -a 10.5.0.1/24 -u 5801 -l 5800\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nExample (ground station):\n")
		fmt.Fprintf(os.Stderr, "  %s -t wfb-tun -a 10.5.0.2/24 -u 5800 -l 5801\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nVersion: %s\n", version.String())
	}

	flag.Parse()

	if showVersion {
		fmt.Printf("wfb_tun %s\n", version.String())
		os.Exit(0)
	}

	return cfg
}

func run(cfg *Config) error {
	log.Printf("wfb_tun starting...")
	log.Printf("  TUN: %s (%s)", cfg.TunName, cfg.TunAddr)
	log.Printf("  Peer: %s:%d", cfg.PeerAddr, cfg.PeerPort)
	log.Printf("  Listen: %d", cfg.ListenPort)
	log.Printf("  Aggregation timeout: %dms", cfg.AggTimeoutMS)

	// Open TUN device
	tunFd, err := openTun(cfg.TunName, cfg.TunAddr)
	if err != nil {
		return fmt.Errorf("failed to open TUN: %w", err)
	}
	defer syscall.Close(tunFd)

	log.Printf("TUN device %s opened", cfg.TunName)

	// Create UDP socket
	listenAddr := fmt.Sprintf(":%d", cfg.ListenPort)
	conn, err := net.ListenPacket("udp", listenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen UDP: %w", err)
	}
	defer conn.Close()

	// Resolve peer address
	peerAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", cfg.PeerAddr, cfg.PeerPort))
	if err != nil {
		return fmt.Errorf("failed to resolve peer: %w", err)
	}

	log.Printf("UDP socket listening on %s, peer %s", listenAddr, peerAddr)

	// Set up signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Channels for communication
	tunToUDP := make(chan []byte, 64)
	udpToTun := make(chan []byte, 64)
	done := make(chan struct{})

	// Start TUN reader
	go tunReader(tunFd, tunToUDP, done)

	// Start UDP reader
	go udpReader(conn, udpToTun, done)

	// Start TUN writer
	go tunWriter(tunFd, udpToTun, done)

	// Start UDP writer with aggregation
	go udpWriter(conn, peerAddr, tunToUDP, cfg.AggTimeoutMS, done)

	// Start ping sender
	go pingSender(conn, peerAddr, done)

	// Wait for signal
	sig := <-sigCh
	log.Printf("Received signal %v, shutting down...", sig)

	close(done)

	return nil
}

// Linux TUN/TAP ioctl constants
const (
	TUNSETIFF   = 0x400454ca
	IFF_TUN     = 0x0001
	IFF_NO_PI   = 0x1000
	IFNAMSIZ    = 16
)

type ifReq struct {
	Name  [IFNAMSIZ]byte
	Flags uint16
	_     [22]byte // padding
}

func openTun(name, addr string) (int, error) {
	// Open /dev/net/tun
	fd, err := syscall.Open("/dev/net/tun", syscall.O_RDWR|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return -1, fmt.Errorf("open /dev/net/tun: %w", err)
	}

	// Set up interface
	var ifr ifReq
	copy(ifr.Name[:], name)
	ifr.Flags = IFF_TUN | IFF_NO_PI

	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), TUNSETIFF, uintptr(unsafe.Pointer(&ifr)))
	if errno != 0 {
		syscall.Close(fd)
		return -1, fmt.Errorf("ioctl TUNSETIFF: %v", errno)
	}

	// Get actual interface name (may differ if we used pattern like "tun%d")
	actualName := string(ifr.Name[:])
	for i, b := range ifr.Name {
		if b == 0 {
			actualName = string(ifr.Name[:i])
			break
		}
	}

	// Configure interface MTU and address using ip command
	mtu := MTU - tunPacketHdrSize
	if err := exec.Command("ip", "link", "set", "up", "mtu", fmt.Sprintf("%d", mtu), "dev", actualName).Run(); err != nil {
		syscall.Close(fd)
		return -1, fmt.Errorf("ip link set: %w", err)
	}

	if err := exec.Command("ip", "addr", "add", addr, "dev", actualName).Run(); err != nil {
		syscall.Close(fd)
		return -1, fmt.Errorf("ip addr add: %w", err)
	}

	return fd, nil
}

func tunReader(fd int, out chan<- []byte, done <-chan struct{}) {
	buf := make([]byte, MTU)

	for {
		select {
		case <-done:
			return
		default:
		}

		// Use poll for non-blocking read with timeout
		pfd := unix.PollFd{
			Fd:     int32(fd),
			Events: unix.POLLIN,
		}

		n, err := unix.Poll([]unix.PollFd{pfd}, 100)
		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			log.Printf("TUN poll error: %v", err)
			continue
		}

		if n == 0 {
			continue // timeout
		}

		nr, err := syscall.Read(fd, buf)
		if err != nil {
			if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
				continue
			}
			log.Printf("TUN read error: %v", err)
			continue
		}

		if nr <= 0 {
			continue
		}

		// Copy packet data
		pkt := make([]byte, nr)
		copy(pkt, buf[:nr])

		select {
		case out <- pkt:
		case <-done:
			return
		}
	}
}

func udpReader(conn net.PacketConn, out chan<- []byte, done <-chan struct{}) {
	buf := make([]byte, MTU)

	for {
		select {
		case <-done:
			return
		default:
		}

		conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		nr, _, err := conn.ReadFrom(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			select {
			case <-done:
				return
			default:
				log.Printf("UDP read error: %v", err)
				continue
			}
		}

		if nr == 0 {
			// Ping packet, ignore
			continue
		}

		// Copy packet data
		pkt := make([]byte, nr)
		copy(pkt, buf[:nr])

		select {
		case out <- pkt:
		case <-done:
			return
		}
	}
}

func tunWriter(fd int, in <-chan []byte, done <-chan struct{}) {
	for {
		select {
		case <-done:
			return
		case data := <-in:
			// Data contains aggregated packets with headers
			offset := 0
			for offset+tunPacketHdrSize <= len(data) {
				pktSize := int(binary.BigEndian.Uint16(data[offset : offset+tunPacketHdrSize]))
				if offset+tunPacketHdrSize+pktSize > len(data) {
					log.Printf("TUN write: invalid packet size %d at offset %d (data len %d)", pktSize, offset, len(data))
					break
				}

				pkt := data[offset+tunPacketHdrSize : offset+tunPacketHdrSize+pktSize]

				// Write to TUN
				_, err := syscall.Write(fd, pkt)
				if err != nil {
					log.Printf("TUN write error: %v", err)
				}

				offset += tunPacketHdrSize + pktSize
			}
		}
	}
}

func udpWriter(conn net.PacketConn, peer *net.UDPAddr, in <-chan []byte, aggTimeoutMS int, done <-chan struct{}) {
	buf := make([]byte, MTU*2)
	bufSize := 0
	batchSize := 0

	var aggTimer <-chan time.Time
	if aggTimeoutMS > 0 {
		aggTimer = nil // Will be set when we start aggregating
	}

	flush := func() {
		if batchSize > 0 {
			conn.WriteTo(buf[:batchSize], peer)
			// Move remaining data to front
			if bufSize > batchSize {
				copy(buf, buf[batchSize:bufSize])
				bufSize -= batchSize
				batchSize = bufSize
			} else {
				bufSize = 0
				batchSize = 0
			}
		}
		aggTimer = nil
	}

	for {
		select {
		case <-done:
			flush()
			return

		case <-aggTimer:
			flush()

		case pkt := <-in:
			isNewBuffer := (bufSize == 0)

			// Add packet with header
			pktWithHdr := tunPacketHdrSize + len(pkt)
			if bufSize+pktWithHdr > MTU*2 {
				// Buffer full, flush first
				flush()
				isNewBuffer = true
			}

			// Write packet header (size)
			binary.BigEndian.PutUint16(buf[bufSize:], uint16(len(pkt)))
			copy(buf[bufSize+tunPacketHdrSize:], pkt)
			bufSize += pktWithHdr

			// Update batch size if still within MTU
			if bufSize <= MTU {
				batchSize = bufSize
			}

			// Decide whether to flush or continue aggregating
			if bufSize >= MTU || aggTimeoutMS == 0 {
				flush()
			} else if isNewBuffer && aggTimeoutMS > 0 {
				// Start aggregation timer
				aggTimer = time.After(time.Duration(aggTimeoutMS) * time.Millisecond)
			}
		}
	}
}

func pingSender(conn net.PacketConn, peer *net.UDPAddr, done <-chan struct{}) {
	ticker := time.NewTicker(PingIntervalMS * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			// Send empty ping packet
			conn.WriteTo([]byte{}, peer)
		}
	}
}
