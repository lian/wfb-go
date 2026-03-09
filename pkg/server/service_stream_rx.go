package server

import (
	"context"
	"log"
	"net"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/lian/wfb-go/pkg/rx"
	"github.com/lian/wfb-go/pkg/server/mavlink"
)

func (s *StreamService) startRX(ctx context.Context, channelID uint32) error {
	outputFn := func(data []byte) error {
		// Reset RX activity semaphore (for keepalive logic)
		atomic.StoreInt32(&s.pktInSem, 2)

		// Send to video callback for web UI (raw video data)
		if s.cfg.VideoCallback != nil {
			s.cfg.VideoCallback(data)
		}

		// Mirror to OSD if configured (raw packets before any splitting)
		if s.osdConn != nil {
			s.osdConn.Write(data)
		}

		// Log mavlink messages if configured
		if s.mavlinkLogger != nil {
			s.mavlinkLogger.Log(data)
		}

		// Send to mavlink TCP clients (raw packets before any splitting)
		if len(s.mavlinkTCPClients) > 0 {
			s.writeToMavlinkTCP(data)
		}

		// Unpack framed packets (tunnel mode)
		if s.cfg.AggFramed && len(data) >= 2 {
			return s.unpackFramedPackets(data)
		}

		// Split aggregated mavlink packets for mavlink-router compatibility
		// Mavlink-router has issues with multiple messages in one UDP packet
		if s.cfg.ServiceType == ServiceMavlink && s.cfg.AggTimeout > 0 {
			return s.unpackMavlinkPackets(data)
		}

		return s.writeToPeer(data)
	}

	fwdCfg := rx.ForwarderConfig{
		Interfaces:     s.cfg.Wlans,
		ChannelID:      channelID,
		KeyData:        s.cfg.KeyData,
		Epoch:          s.cfg.Epoch,
		OutputFn:       outputFn,
		CaptureManager: s.cfg.CaptureManager,
	}

	forwarder, err := rx.NewForwarder(fwdCfg)
	if err != nil {
		return err
	}
	s.forwarder = forwarder

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.forwarder.Run()
	}()

	return nil
}

// writeToPeer sends data to the configured peer.
// Fast path for UDP connect mode (no lock needed - socket set once at startup).
func (s *StreamService) writeToPeer(data []byte) error {
	// Fast path: connected UDP socket (most common case)
	if s.udpConn != nil {
		_, err := s.udpConn.Write(data)
		return err
	}

	// Fast path: TUN device (set once at startup)
	if s.tunFd != nil {
		_, err := s.tunFd.Write(data)
		return err
	}

	// Fast path: serial port (set once at startup)
	if s.serialFd > 0 {
		_, err := syscall.Write(s.serialFd, data)
		return err
	}

	// Slow path: needs lock for lastSender or tcpClients
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.udpListen != nil && s.lastSender != nil {
		_, err := s.udpListen.WriteTo(data, s.lastSender)
		return err
	}

	if len(s.tcpClients) > 0 {
		// Send to all TCP clients
		for i := len(s.tcpClients) - 1; i >= 0; i-- {
			client := s.tcpClients[i]
			client.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
			if _, err := client.Write(data); err != nil {
				client.Close()
				s.tcpClients = append(s.tcpClients[:i], s.tcpClients[i+1:]...)
			}
		}
		return nil
	}

	return nil
}

// unpackFramedPackets unpacks length-prefixed packets from aggregated data.
// Format: [2-byte big-endian length][packet data]...
func (s *StreamService) unpackFramedPackets(data []byte) error {
	i := 0
	for i < len(data) {
		// Need at least 2 bytes for length header
		if len(data)-i < 2 {
			log.Printf("[%s] Truncated frame header at offset %d", s.name, i)
			break
		}

		// Read big-endian 16-bit length
		pktLen := int(data[i])<<8 | int(data[i+1])
		i += 2

		// Validate length
		if pktLen == 0 {
			// Empty packet (keepalive), skip
			continue
		}
		if len(data)-i < pktLen {
			log.Printf("[%s] Truncated packet body: want %d, have %d", s.name, pktLen, len(data)-i)
			break
		}

		// Write individual packet to peer
		if err := s.writeToPeer(data[i : i+pktLen]); err != nil {
			return err
		}
		i += pktLen
	}
	return nil
}

// unpackMavlinkPackets splits aggregated mavlink messages for mavlink-router compatibility.
// Mavlink-router has issues when multiple mavlink messages arrive in a single UDP packet.
// This parses the aggregated data and writes each message individually.
func (s *StreamService) unpackMavlinkPackets(data []byte) error {
	parser := mavlink.NewParser()
	messages := parser.Parse(data)

	// If no valid messages found, write raw data as fallback
	if len(messages) == 0 {
		return s.writeToPeer(data)
	}

	// Write each message individually
	for _, msg := range messages {
		// Check arm state for HEARTBEAT messages
		s.checkMavlinkArmed(msg.Raw)

		if err := s.writeToPeer(msg.Raw); err != nil {
			return err
		}
	}

	return nil
}

// tcpAcceptLoop handles TCP peer connections.
func (s *StreamService) tcpAcceptLoop(ctx context.Context) {
	defer s.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		default:
		}

		if l, ok := s.tcpListener.(*net.TCPListener); ok {
			l.SetDeadline(time.Now().Add(100 * time.Millisecond))
		}

		conn, err := s.tcpListener.Accept()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			if s.stopped.Load() {
				return
			}
			continue
		}

		log.Printf("[%s] TCP client: %s", s.name, conn.RemoteAddr())

		s.mu.Lock()
		s.tcpClients = append(s.tcpClients, conn)
		s.mu.Unlock()

		s.wg.Add(1)
		go s.tcpClientLoop(ctx, conn)
	}
}

// tcpClientLoop handles data from a connected TCP client.
func (s *StreamService) tcpClientLoop(ctx context.Context, conn net.Conn) {
	defer s.wg.Done()
	defer func() {
		s.mu.Lock()
		for i, c := range s.tcpClients {
			if c == conn {
				s.tcpClients = append(s.tcpClients[:i], s.tcpClients[i+1:]...)
				break
			}
		}
		s.mu.Unlock()
		conn.Close()
	}()

	// Use mavlink parser for mavlink service to properly frame messages
	var parser *mavlink.Parser
	if s.cfg.ServiceType == ServiceMavlink {
		parser = mavlink.NewParser()
	}

	buf := make([]byte, 1500)
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		default:
		}

		conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, err := conn.Read(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return
		}
		if n > 0 {
			if parser != nil {
				// Parse mavlink messages and send each one
				for _, msg := range parser.Parse(buf[:n]) {
					s.sendToTX(msg.Raw)
				}
			} else {
				s.sendToTX(buf[:n])
			}
		}
	}
}
