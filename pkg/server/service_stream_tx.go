package server

import (
	"context"
	"log"
	"sync/atomic"
	"time"

	"github.com/lian/wfb-go/pkg/wifi/radiotap"
	"github.com/lian/wfb-go/pkg/server/mavlink"
	"github.com/lian/wfb-go/pkg/server/util"
	"github.com/lian/wfb-go/pkg/tx"
)

func (s *StreamService) startTX(ctx context.Context, channelID uint32) error {
	for i, wlan := range s.cfg.Wlans {
		rtHdr := &radiotap.TXHeader{
			MCSIndex:  uint8(s.cfg.MCSIndex),
			Bandwidth: uint8(s.cfg.Bandwidth),
			ShortGI:   s.cfg.ShortGI,
			STBC:      uint8(s.cfg.STBC),
			LDPC:      s.cfg.LDPC != 0,
			VHTMode:   s.cfg.Bandwidth > 20 || s.cfg.ForceVHT,
		}

		rawCfg := tx.RawSocketConfig{
			Interfaces: []string{wlan},
			ChannelID:  channelID,
			Radiotap:   rtHdr,
			UseQdisc:   s.cfg.UseQdisc,
			Fwmark:     s.cfg.Fwmark,
		}

		injector, err := tx.NewRawSocketInjector(rawCfg)
		if err != nil {
			return err
		}
		s.injectors = append(s.injectors, injector)

		txCfg := tx.Config{
			FecK:       s.cfg.FecK,
			FecN:       s.cfg.FecN,
			Epoch:      s.cfg.Epoch,
			ChannelID:  channelID,
			FecDelay:   time.Duration(s.cfg.FecDelay) * time.Microsecond,
			FecTimeout: time.Duration(s.cfg.FecTimeout) * time.Millisecond,
			KeyData:    s.cfg.KeyData,
		}

		transmitter, err := tx.New(txCfg, injector)
		if err != nil {
			injector.Close()
			return err
		}
		s.transmitters = append(s.transmitters, transmitter)
		log.Printf("[%s] TX[%d] on %s", s.name, i, wlan)
	}

	// Log mirror mode status
	if s.cfg.Mirror {
		log.Printf("[%s] Mirror mode enabled - packets sent to ALL %d antennas", s.name, len(s.transmitters))
	}

	// Create packet aggregator if aggregation is enabled
	if s.cfg.AggTimeout > 0 && s.cfg.AggMaxSize > 0 {
		s.aggregator = util.NewPacketAggregator(util.PacketAggregatorConfig{
			MaxSize:   s.cfg.AggMaxSize,
			Timeout:   time.Duration(s.cfg.AggTimeout * float64(time.Second)),
			UseFrames: s.cfg.AggFramed,
			FlushFn:   s.sendToTX,
		})
		log.Printf("[%s] Packet aggregation enabled (max=%d, timeout=%.3fs, framed=%v)",
			s.name, s.cfg.AggMaxSize, s.cfg.AggTimeout, s.cfg.AggFramed)
	}

	// Create mavlink parser for serial port (byte stream needs message framing)
	if s.serialFd > 0 && s.cfg.ServiceType == ServiceMavlink {
		s.mavlinkParser = mavlink.NewParser()
		log.Printf("[%s] Mavlink parser enabled for serial port", s.name)
	}

	// Start input reader (reads from peer, sends to WFB)
	s.wg.Add(1)
	go s.inputLoop(ctx)

	// Start session announcer
	s.wg.Add(1)
	go s.sessionLoop(ctx)

	// Start keepalive loop for tunnel mode
	if s.cfg.AggFramed && s.cfg.KeepaliveMS > 0 {
		s.wg.Add(1)
		go s.keepaliveLoop(ctx)
	}

	return nil
}

// inputLoop reads from peer and sends to WFB TX.
func (s *StreamService) inputLoop(ctx context.Context) {
	defer s.wg.Done()

	buf := make([]byte, 65536) // Large buffer for jumbo packets

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		default:
		}

		n, err := s.readFromPeer(buf)
		if err != nil {
			if s.stopped.Load() {
				return
			}
			continue
		}
		if n == 0 {
			continue
		}

		// Parse mavlink messages from serial byte stream
		if s.mavlinkParser != nil {
			for _, msg := range s.mavlinkParser.Parse(buf[:n]) {
				s.sendPacket(msg.Raw)
			}
			continue
		}

		s.sendPacket(buf[:n])
	}
}

// sendPacket sends a packet through aggregator or directly to TX.
func (s *StreamService) sendPacket(data []byte) {
	// Log mavlink messages if configured (TX direction)
	if s.mavlinkLogger != nil {
		s.mavlinkLogger.Log(data)
	}

	if s.aggregator != nil {
		if !s.aggregator.Add(data) {
			// Packet too large for aggregation, send directly
			s.sendToTX(data)
		}
	} else {
		s.sendToTX(data)
	}
}

// readFromPeer reads data from the configured peer.
func (s *StreamService) readFromPeer(buf []byte) (int, error) {
	switch {
	case s.udpConn != nil:
		s.udpConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		return s.udpConn.Read(buf)

	case s.udpListen != nil:
		s.udpListen.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, addr, err := s.udpListen.ReadFrom(buf)
		if addr != nil {
			s.mu.Lock()
			s.lastSender = addr
			s.mu.Unlock()
		}
		return n, err

	case s.tunReader != nil:
		return s.tunReader.Read(buf)

	case s.serialFd > 0:
		n, err := readSerial(s.serialFd, buf)
		return n, err

	default:
		// No input peer (TCP clients handled separately)
		time.Sleep(100 * time.Millisecond)
		return 0, nil
	}
}

// sendToTX sends a packet to the appropriate TX antenna(s).
// In mirror mode, sends to ALL antennas for redundancy.
// Otherwise, sends to the currently selected antenna.
func (s *StreamService) sendToTX(data []byte) {
	// Reset TX activity semaphore (for keepalive logic)
	atomic.StoreInt32(&s.pktOutSem, 1)

	s.stats.mu.Lock()
	s.stats.PacketsIncoming++
	s.stats.BytesIncoming += uint64(len(data))
	s.stats.mu.Unlock()

	if s.cfg.Mirror {
		// Mirror mode: send to ALL antennas
		for _, t := range s.transmitters {
			if _, err := t.SendPacket(data); err != nil {
				s.stats.mu.Lock()
				s.stats.PacketsDropped++
				s.stats.mu.Unlock()
			}
		}
	} else {
		// Normal mode: send to selected antenna
		txIdx := atomic.LoadInt32(&s.currentTX)
		if int(txIdx) < len(s.transmitters) {
			if _, err := s.transmitters[txIdx].SendPacket(data); err != nil {
				s.stats.mu.Lock()
				s.stats.PacketsDropped++
				s.stats.mu.Unlock()
			}
		}
	}
}

// sendToAllTX sends a packet to ALL TX antennas (for keepalive broadcast).
func (s *StreamService) sendToAllTX(data []byte) {
	for _, t := range s.transmitters {
		t.SendPacket(data)
	}
}

func (s *StreamService) sessionLoop(ctx context.Context) {
	defer s.wg.Done()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			for _, t := range s.transmitters {
				t.SendSessionKey()
			}
		}
	}
}

// keepaliveLoop sends keepalive packets for tunnel mode.
// Logic:
//   - If no RX for 2 intervals: broadcast empty packet to ALL antennas
//   - If no TX for 1 interval: send empty packet to current antenna
//
// This helps maintain connections with multiple directional antennas.
func (s *StreamService) keepaliveLoop(ctx context.Context) {
	defer s.wg.Done()

	interval := time.Duration(s.cfg.KeepaliveMS) * time.Millisecond
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("[%s] Keepalive broadcast enabled (interval=%v)", s.name, interval)

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			inSem := atomic.LoadInt32(&s.pktInSem)
			outSem := atomic.LoadInt32(&s.pktOutSem)

			if inSem == 0 {
				// No RX for 2 intervals - broadcast to ALL antennas
				s.sendToAllTX([]byte{})
			} else if outSem == 0 {
				// No TX for 1 interval - send to current antenna only
				s.sendToTX([]byte{})
			}

			// Decrement semaphores
			if inSem > 0 {
				atomic.AddInt32(&s.pktInSem, -1)
			}
			if outSem > 0 {
				atomic.AddInt32(&s.pktOutSem, -1)
			}
		}
	}
}
