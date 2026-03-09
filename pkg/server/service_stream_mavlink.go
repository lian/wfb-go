package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lian/wfb-go/pkg/server/mavlink"
	"github.com/lian/wfb-go/pkg/server/util"
)

// setupMavlinkLogger initializes the mavlink message logger.
func (s *StreamService) setupMavlinkLogger() error {
	// Format the log file path with service name (like wfb-ng does with profile)
	logPath := fmt.Sprintf(s.cfg.BinaryLogFile, s.name)

	logger, err := util.NewBinaryLogger(logPath)
	if err != nil {
		return fmt.Errorf("create binary logger %s: %w", logPath, err)
	}
	s.binLogger = logger
	s.mavlinkLogger = mavlink.NewLogger(logger)
	log.Printf("[%s] Mavlink message logging to %s", s.name, logPath)
	return nil
}

// setupOSD initializes the OSD mirror connection.
// OSD address format: "connect://host:port"
func (s *StreamService) setupOSD() error {
	osd := s.cfg.OSD
	if !strings.HasPrefix(osd, "connect://") {
		return fmt.Errorf("OSD must use connect:// format, got: %s", osd)
	}

	addr := strings.TrimPrefix(osd, "connect://")
	conn, err := net.Dial("udp", addr)
	if err != nil {
		return fmt.Errorf("dial osd %s: %w", addr, err)
	}
	s.osdConn = conn
	log.Printf("[%s] OSD mirror connected to %s", s.name, addr)
	return nil
}

// setupMavlinkTCP initializes the additional mavlink TCP server.
func (s *StreamService) setupMavlinkTCP() error {
	addr := fmt.Sprintf(":%d", s.cfg.MavlinkTCPPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen mavlink tcp %s: %w", addr, err)
	}
	s.mavlinkTCPListener = listener
	log.Printf("[%s] Mavlink TCP listening on port %d", s.name, s.cfg.MavlinkTCPPort)
	return nil
}

// mavlinkTCPAcceptLoop accepts connections on the mavlink TCP port.
func (s *StreamService) mavlinkTCPAcceptLoop(ctx context.Context) {
	defer s.wg.Done()

	for {
		conn, err := s.mavlinkTCPListener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-s.stopCh:
				return
			default:
				if !s.stopped.Load() {
					log.Printf("[%s] Mavlink TCP accept error: %v", s.name, err)
				}
				return
			}
		}

		log.Printf("[%s] Mavlink TCP client connected: %s", s.name, conn.RemoteAddr())

		s.mu.Lock()
		s.mavlinkTCPClients = append(s.mavlinkTCPClients, conn)
		s.mu.Unlock()

		// Handle client in goroutine
		s.wg.Add(1)
		go s.mavlinkTCPClientHandler(ctx, conn)
	}
}

// mavlinkTCPClientHandler handles data from a mavlink TCP client.
func (s *StreamService) mavlinkTCPClientHandler(ctx context.Context, conn net.Conn) {
	defer s.wg.Done()
	defer func() {
		conn.Close()
		s.mu.Lock()
		for i, c := range s.mavlinkTCPClients {
			if c == conn {
				s.mavlinkTCPClients = append(s.mavlinkTCPClients[:i], s.mavlinkTCPClients[i+1:]...)
				break
			}
		}
		s.mu.Unlock()
		log.Printf("[%s] Mavlink TCP client disconnected: %s", s.name, conn.RemoteAddr())
	}()

	// Use mavlink parser for proper message framing
	parser := mavlink.NewParser()
	buf := make([]byte, 4096)

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
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			return
		}

		// Parse mavlink messages and send to TX
		for _, msg := range parser.Parse(buf[:n]) {
			s.sendPacket(msg.Raw)
		}
	}
}

// writeToMavlinkTCP sends data to all connected mavlink TCP clients.
func (s *StreamService) writeToMavlinkTCP(data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := len(s.mavlinkTCPClients) - 1; i >= 0; i-- {
		client := s.mavlinkTCPClients[i]
		client.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
		if _, err := client.Write(data); err != nil {
			client.Close()
			s.mavlinkTCPClients = append(s.mavlinkTCPClients[:i], s.mavlinkTCPClients[i+1:]...)
		}
	}
}

// checkMavlinkArmed parses mavlink HEARTBEAT messages to detect arm state changes.
// Executes CallOnArm/CallOnDisarm callbacks when state changes.
func (s *StreamService) checkMavlinkArmed(data []byte) {
	// Only check if callbacks are configured
	if s.cfg.CallOnArm == "" && s.cfg.CallOnDisarm == "" {
		return
	}

	// Parse the mavlink message
	msg := mavlink.ParseMessage(data)
	if msg == nil {
		return
	}

	// Only process HEARTBEAT from autopilot (sys_id=1, comp_id=1)
	if msg.Header.MsgID != mavlink.MsgIDHeartbeat || msg.Header.SysID != 1 || msg.Header.CompID != 1 {
		return
	}

	// Extract armed state from heartbeat
	baseMode, _, _, ok := mavlink.ParseHeartbeat(msg)
	if !ok {
		return
	}

	isArmed := mavlink.IsArmed(baseMode)

	// Update state and trigger callbacks
	const (
		stateUnknown  = 0
		stateDisarmed = 1
		stateArmed    = 2
	)

	var newState int32
	if isArmed {
		newState = stateArmed
	} else {
		newState = stateDisarmed
	}

	oldState := atomic.SwapInt32(&s.mavlinkArmed, newState)
	if oldState == newState || oldState == stateUnknown {
		return // No change or first message
	}

	// State changed - execute callback
	if isArmed && s.cfg.CallOnArm != "" {
		log.Printf("[%s] Armed - executing: %s", s.name, s.cfg.CallOnArm)
		s.executeCallback(s.cfg.CallOnArm)
	} else if !isArmed && s.cfg.CallOnDisarm != "" {
		log.Printf("[%s] Disarmed - executing: %s", s.name, s.cfg.CallOnDisarm)
		s.executeCallback(s.cfg.CallOnDisarm)
	}
}

// executeCallback runs a shell command asynchronously.
func (s *StreamService) executeCallback(command string) {
	go func() {
		cmd := exec.Command("sh", "-c", command)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			log.Printf("[%s] Callback failed: %v", s.name, err)
		}
	}()
}
