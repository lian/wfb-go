package server

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lian/wfb-go/pkg/adaptive"
	"github.com/lian/wfb-go/pkg/config"
	"github.com/lian/wfb-go/pkg/rx"
	"github.com/lian/wfb-go/pkg/server/util"
	"github.com/lian/wfb-go/pkg/server/web"
	"github.com/lian/wfb-go/pkg/wifi/adapter"
)

// Server orchestrates WFB services.
type Server struct {
	config      *config.Config
	configPath  string
	wlans       []string
	captureMode rx.CaptureMode
	services    []Service

	wlanMgr        *adapter.Manager
	aggregator     *StatsAggregator
	txSelector     *TXAntennaSelector
	apiServer      *APIServer
	rfTempMeter    *util.RFTempMeter
	captureManager *rx.CaptureManager     // Shared capture manager for all RX services
	linkMonitor    *adaptive.LinkMonitor  // Adaptive link monitor (sends stats to drone)
	linkReceiver   *adaptive.LinkReceiver // Adaptive link receiver (receives stats from GS, adjusts TX)
	webServer      *web.Server            // Web UI server (optional)
	videoChan      chan []byte            // Buffered channel for video data to web UI

	mu      sync.Mutex
	running bool
	ctx     context.Context
	cancel  context.CancelFunc

	// Stats collection goroutine
	statsStopCh chan struct{}
	statsWg     sync.WaitGroup
}

// ServerConfig holds server configuration.
type ServerConfig struct {
	Config       *config.Config // YAML configuration
	ConfigPath   string         // Path to config file (for saving changes)
	Wlans        []string       // WiFi interfaces (overrides config if set)
	SkipWlanInit bool           // Skip WLAN initialization
	JSONPort     int            // JSON API port (0 to use config)
	MsgPackPort  int            // MsgPack API port (0 to use config)
	CaptureMode  rx.CaptureMode // Capture mode for RX services
}

// NewServer creates a new server instance.
func NewServer(cfg ServerConfig) (*Server, error) {
	if cfg.Config == nil {
		return nil, fmt.Errorf("config is required")
	}

	// Use wlans from config if not overridden
	wlans := cfg.Wlans
	if len(wlans) == 0 {
		wlans = cfg.Config.Hardware.WLANs
	}

	ctx, cancel := context.WithCancel(context.Background())

	s := &Server{
		config:      cfg.Config,
		configPath:  cfg.ConfigPath,
		wlans:       wlans,
		captureMode: cfg.CaptureMode,
		ctx:         ctx,
		cancel:      cancel,
		statsStopCh: make(chan struct{}),
	}

	// Create WLAN manager
	if !cfg.SkipWlanInit {
		s.wlanMgr = adapter.NewManager(cfg.Config, wlans)
	}

	return s, nil
}

// Start initializes and starts all services.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("server already running")
	}

	// Initialize WLANs
	if s.wlanMgr != nil {
		log.Printf("Initializing WiFi interfaces...")
		if err := s.wlanMgr.InitWlans(); err != nil {
			return fmt.Errorf("init wlans: %w", err)
		}
		log.Printf("WiFi interfaces initialized: %v", s.wlans)
	}

	// Get link ID - use numeric ID if set, otherwise hash domain
	var linkID uint32
	if s.config.Link.ID != 0 {
		linkID = s.config.Link.ID & 0xFFFFFF // Mask to 24 bits
		log.Printf("LinkID: 0x%06x (from config)", linkID)
	} else {
		linkID = HashLinkDomain(s.config.Link.Domain)
		log.Printf("LinkDomain: %s, LinkID: 0x%06x", s.config.Link.Domain, linkID)
	}

	// Get common config settings with defaults
	rssiDelta := 3
	logInterval := 1000
	if s.config.Common != nil {
		if s.config.Common.TxSelRssiDelta > 0 {
			rssiDelta = s.config.Common.TxSelRssiDelta
		}
		if s.config.Common.LogInterval > 0 {
			logInterval = s.config.Common.LogInterval
		}
	}

	// Create TX antenna selector only if we have multiple wlans
	// With a single adapter, there's no selection to make
	if len(s.wlans) > 1 {
		s.txSelector = NewTXAntennaSelector(TXAntennaSelectorConfig{
			RssiDelta: rssiDelta,
		})
	}

	// Create stats aggregator
	logIntervalDur := time.Duration(logInterval) * time.Millisecond
	linkDomainStr := s.config.Link.Domain
	if s.config.Link.ID != 0 {
		linkDomainStr = fmt.Sprintf("0x%06x", linkID)
	}
	s.aggregator = NewStatsAggregator(StatsAggregatorConfig{
		Profile:     "default",
		LinkDomain:  linkDomainStr,
		LogInterval: logIntervalDur,
		TXSelector:  s.txSelector,
	})

	// Create shared capture manager for all RX services
	s.captureManager = rx.NewCaptureManager(s.captureMode)
	log.Printf("Capture mode: %v", s.captureMode)

	// Create web server if enabled (before services so we can wire up video callback)
	var videoStreamName string
	if s.config.Web != nil && s.config.Web.Enabled {
		videoStreamName = s.config.Web.VideoStream
		if videoStreamName == "" {
			videoStreamName = "video" // Default stream name
		}

		port := s.config.Web.Port
		if port == 0 {
			port = 8080
		}

		// Create buffered channel for video data (non-blocking sends from RX)
		s.videoChan = make(chan []byte, 100)

		// Create web server without UDP source (direct sink mode)
		s.webServer = web.NewServer(web.Config{
			Addr: fmt.Sprintf(":%d", port),
		})

		// Set config callbacks for web API
		s.webServer.SetConfigCallbacks(s.getConfigForWeb, s.setConfigFromWeb)

		// Set stream callbacks for web API
		s.webServer.SetStreamCallbacks(s.getStreamsForWeb, s.startStreamByName, s.stopStreamByName)

		// Start goroutine to drain video channel and send to web server
		s.statsWg.Add(1)
		go s.videoChannelDrainer()

		log.Printf("Web UI enabled on port %d (video from stream '%s')", port, videoStreamName)
	}

	// Create and start services
	for name, streamCfg := range s.config.Streams {
		svcCfg, err := NewServiceConfig(name, &streamCfg, s.config, s.wlans, linkID)
		if err != nil {
			// Stop already started services
			for _, started := range s.services {
				started.Stop()
			}
			return fmt.Errorf("configure service %s: %w", name, err)
		}
		svcCfg.CaptureManager = s.captureManager

		// Wire up video callback for the designated video stream
		if s.videoChan != nil && name == videoStreamName {
			svcCfg.VideoCallback = s.makeVideoCallback()
		}

		svc, err := CreateService(svcCfg)
		if err != nil {
			// Stop already started services
			for _, started := range s.services {
				started.Stop()
			}
			return fmt.Errorf("create service %s: %w", name, err)
		}

		log.Printf("Starting service: %s (type=%s)", svc.Name(), svc.Type())
		if err := svc.Start(s.ctx); err != nil {
			// Stop already started services
			for _, started := range s.services {
				started.Stop()
			}
			return fmt.Errorf("start service %s: %w", name, err)
		}

		s.services = append(s.services, svc)
	}

	// Wire up TX antenna selection callback
	// When the selector determines a better antenna, switch all TX services to it
	if s.txSelector != nil {
		s.txSelector.AddCallback(func(wlanIdx uint8) {
			s.mu.Lock()
			defer s.mu.Unlock()
			for _, svc := range s.services {
				if as, ok := svc.(AntennaRoutable); ok {
					as.SetAntenna(wlanIdx)
				}
			}
		})
	}

	// Start capture manager (starts shared capture loops in shared mode, no-op in dedicated)
	s.captureManager.Start()

	// Create RF temperature meter
	s.rfTempMeter = util.NewRFTempMeter(s.wlans, 10*time.Second)

	// Start stats collection
	s.statsWg.Add(1)
	go s.collectStats()

	// Start stats aggregator
	s.aggregator.Start()

	// Start API server if enabled and ports are configured
	if s.config.API != nil && s.config.API.Enabled {
		jsonPort := s.config.API.JSONPort
		msgpackPort := s.config.API.StatsPort

		if jsonPort > 0 || msgpackPort > 0 {
			s.apiServer = NewAPIServer(APIServerConfig{
				Profile:     "default",
				IsCluster:   false,
				Wlans:       s.wlans,
				LogInterval: logIntervalDur,
				Aggregator:  s.aggregator,
			})

			if err := s.apiServer.Start(jsonPort, msgpackPort); err != nil {
				log.Printf("Warning: failed to start API server: %v", err)
			}
		}
	}

	// Start adaptive link based on config
	if s.config.Adaptive != nil && s.config.Adaptive.Enabled {
		s.startAdaptiveLink()
	}

	// Start web server if configured
	if s.webServer != nil {
		go func() {
			if err := s.webServer.Run(s.ctx); err != nil && err.Error() != "http: Server closed" {
				log.Printf("Web server error: %v", err)
			}
		}()
	}

	s.running = true
	log.Printf("Server started with %d service(s)", len(s.services))

	return nil
}

// startAdaptiveLink starts the adaptive link monitor or receiver based on mode.
func (s *Server) startAdaptiveLink() {
	if s.config.Adaptive.Mode == "gs" && s.config.Adaptive.SendAddr != "" {
		// GS mode - sends stats to drone
		linkCfg := adaptive.DefaultLinkConfig()
		linkCfg.DroneAddr = s.config.Adaptive.SendAddr

		getStats := func() *adaptive.LinkStats {
			aggStats := s.aggregator.GetStats()
			if aggStats == nil || len(aggStats.Antennas) == 0 {
				return nil
			}

			stats := &adaptive.LinkStats{
				NumAntennas: len(aggStats.Antennas),
			}

			for _, ant := range aggStats.Antennas {
				stats.AntennaRSSI = append(stats.AntennaRSSI, ant.RSSIAvg)
				stats.AntennaSNR = append(stats.AntennaSNR, ant.SNRAvg)
			}

			for name, svc := range aggStats.Services {
				if strings.Contains(strings.ToLower(name), "video") {
					stats.FECRecovered = uint32(svc.PacketsFECRec)
					stats.LostPackets = uint32(svc.PacketsLost)
					stats.AllPackets = uint32(svc.PacketsReceived)
					stats.FecK = svc.SessionFecK
					stats.FecN = svc.SessionFecN
					break
				}
			}

			return stats
		}

		var err error
		s.linkMonitor, err = adaptive.NewLinkMonitor(linkCfg, getStats)
		if err != nil {
			log.Printf("Warning: failed to create adaptive link monitor: %v", err)
		} else {
			s.linkMonitor.Start()
			log.Printf("Adaptive link started (GS mode), sending to %s", s.config.Adaptive.SendAddr)
		}
	} else if s.config.Adaptive.Mode == "drone" && s.config.Adaptive.ListenPort > 0 {
		// Drone mode - receives stats from GS, adjusts TX
		recvCfg := adaptive.LinkReceiverConfig{
			Port: s.config.Adaptive.ListenPort,
			OnProfileChange: func(profile *adaptive.TXProfile, msg *adaptive.LinkMessage) {
				s.applyTXProfile(profile)
			},
		}

		var err error
		s.linkReceiver, err = adaptive.NewLinkReceiver(recvCfg)
		if err != nil {
			log.Printf("Warning: failed to create adaptive link receiver: %v", err)
		} else {
			s.linkReceiver.Start()
			log.Printf("Adaptive link started (Drone mode), listening on port %d", s.config.Adaptive.ListenPort)
		}
	}
}

// Stop stops all services.
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	log.Printf("Stopping server...")
	s.cancel()

	// Stop stats collection
	close(s.statsStopCh)
	s.statsWg.Wait()

	// Stop adaptive link
	if s.linkMonitor != nil {
		s.linkMonitor.Stop()
	}
	if s.linkReceiver != nil {
		s.linkReceiver.Stop()
	}

	// Stop API server
	if s.apiServer != nil {
		s.apiServer.Stop()
	}

	// Stop aggregator
	if s.aggregator != nil {
		s.aggregator.Stop()
	}

	// Stop RF temperature meter
	if s.rfTempMeter != nil {
		s.rfTempMeter.Stop()
	}

	// Stop services in reverse order
	for i := len(s.services) - 1; i >= 0; i-- {
		svc := s.services[i]
		log.Printf("Stopping service: %s", svc.Name())
		if err := svc.Stop(); err != nil {
			log.Printf("Error stopping service %s: %v", svc.Name(), err)
		}
	}

	// Close capture manager after all services stopped
	if s.captureManager != nil {
		s.captureManager.Close()
	}

	s.services = nil
	s.running = false
	log.Printf("Server stopped")

	return nil
}

// collectStats periodically collects stats from services and updates the aggregator.
func (s *Server) collectStats() {
	defer s.statsWg.Done()

	ticker := time.NewTicker(100 * time.Millisecond) // Collect more frequently than log interval
	defer ticker.Stop()

	for {
		select {
		case <-s.statsStopCh:
			return
		case <-ticker.C:
			s.mu.Lock()

			// Collect service stats and latency
			allLatency := make(map[uint32]LatencyStatsData)
			for _, svc := range s.services {
				stats := svc.Stats()
				s.aggregator.UpdateStats(svc.Name(), stats)

				// Collect latency stats if service provides them
				if lp, ok := svc.(LatencyProvider); ok {
					for k, v := range lp.GetLatencyStats() {
						allLatency[k] = v
					}
				}
			}

			// Update latency stats
			if len(allLatency) > 0 {
				s.aggregator.UpdateLatencyStats(allLatency)
			}

			// Update RF temperature
			if s.rfTempMeter != nil {
				s.aggregator.UpdateRFTemperature(s.rfTempMeter.GetTemperatures())
			}

			// Push stats to web server if enabled
			if s.webServer != nil {
				s.pushWebStats()
			}

			s.mu.Unlock()
		}
	}
}

// Stats returns aggregated statistics from all services.
func (s *Server) Stats() map[string]*ServiceStats {
	s.mu.Lock()
	defer s.mu.Unlock()

	stats := make(map[string]*ServiceStats)
	for _, svc := range s.services {
		stats[svc.Name()] = svc.Stats()
	}
	return stats
}

// GetAggregatedStats returns the current aggregated stats.
func (s *Server) GetAggregatedStats() *AggregatedStats {
	if s.aggregator != nil {
		return s.aggregator.GetStats()
	}
	return nil
}

// Services returns the list of running services.
func (s *Server) Services() []Service {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]Service, len(s.services))
	copy(result, s.services)
	return result
}

// Config returns the loaded configuration.
func (s *Server) Config() *config.Config {
	return s.config
}

// IsRunning returns true if the server is running.
func (s *Server) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// GetTXAntennaSelector returns the TX antenna selector.
func (s *Server) GetTXAntennaSelector() *TXAntennaSelector {
	return s.txSelector
}

// GetTXPower returns current TX power in dBm (0 if unknown).
func (s *Server) GetTXPower() int {
	if s.wlanMgr != nil {
		return s.wlanMgr.GetTXPower()
	}
	return 0
}

// applyTXProfile applies TX parameters from an adaptive link profile to all TX services.
func (s *Server) applyTXProfile(profile *adaptive.TXProfile) {
	s.mu.Lock()
	defer s.mu.Unlock()

	params := TXParams{
		MCS:       profile.MCS,
		ShortGI:   profile.ShortGI,
		Bandwidth: profile.Bandwidth,
		FecK:      profile.FecK,
		FecN:      profile.FecN,
		TXPower:   profile.TXPower,
	}

	log.Printf("Adaptive link: applying profile MCS=%d GI=%v BW=%d FEC=%d/%d TXPower=%d",
		params.MCS, params.ShortGI, params.Bandwidth, params.FecK, params.FecN, params.TXPower)

	// Set TX power first (ensure adequate signal before modulation change)
	if params.TXPower > 0 && s.wlanMgr != nil {
		if err := s.wlanMgr.SetTXPower(params.TXPower); err != nil {
			log.Printf("Adaptive link: failed to set TX power: %v", err)
		}
	}

	// Update radiotap/FEC on video TX service only (matches alink_drone targeting port 8000)
	for _, svc := range s.services {
		if strings.Contains(strings.ToLower(svc.Name()), "video") {
			if tc, ok := svc.(TXConfigurable); ok {
				tc.SetTXParams(params)
			}
			break
		}
	}
}

// makeVideoCallback creates a non-blocking callback that sends video data to the channel.
func (s *Server) makeVideoCallback() func(data []byte) {
	return func(data []byte) {
		// Make a copy since data buffer may be reused
		dataCopy := make([]byte, len(data))
		copy(dataCopy, data)

		// Non-blocking send - drop if channel is full
		select {
		case s.videoChan <- dataCopy:
		default:
			// Channel full, drop packet (web UI can't keep up)
		}
	}
}

// videoChannelDrainer reads video data from the channel and sends to web server.
func (s *Server) videoChannelDrainer() {
	defer s.statsWg.Done()

	for {
		select {
		case <-s.statsStopCh:
			return
		case data := <-s.videoChan:
			if s.webServer != nil {
				s.webServer.WriteVideo(data)
			}
		}
	}
}

// pushWebStats converts aggregated stats to web format and pushes to web server.
func (s *Server) pushWebStats() {
	aggStats := s.aggregator.GetStats()
	if aggStats == nil {
		return
	}

	webStats := &web.Stats{
		Timestamp: time.Now().UnixMilli(),
	}

	// Get best RSSI/SNR from antennas and build per-antenna stats
	var bestRSSI, bestSNR int8 = -128, -128
	var totalPackets uint64
	for _, ant := range aggStats.Antennas {
		if ant.RSSIAvg > bestRSSI {
			bestRSSI = ant.RSSIAvg
		}
		if ant.SNRAvg > bestSNR {
			bestSNR = ant.SNRAvg
		}
		totalPackets += ant.PacketsTotal

		// Add per-antenna stats
		var wlanName string
		if int(ant.WlanIdx) < len(s.wlans) {
			wlanName = s.wlans[ant.WlanIdx]
		}
		webStats.Antennas = append(webStats.Antennas, web.AntennaStats{
			WlanIdx:   ant.WlanIdx,
			WlanName:  wlanName,
			Antenna:   ant.Antenna,
			Freq:      ant.Freq,
			MCS:       ant.MCSIndex,
			Bandwidth: ant.Bandwidth,
			Packets:   ant.PacketsTotal,
			RSSIMin:   ant.RSSIMin,
			RSSIAvg:   ant.RSSIAvg,
			RSSIMax:   ant.RSSIMax,
			SNRMin:    ant.SNRMin,
			SNRAvg:    ant.SNRAvg,
			SNRMax:    ant.SNRMax,
		})
	}
	webStats.RSSI = int(bestRSSI)
	webStats.SNR = int(bestSNR)
	webStats.Packets = totalPackets

	// Set TX antenna selection
	webStats.TXWlan = aggStats.TXWlanIdx

	// Build per-stream stats and aggregate totals
	// Show all streams from config, even if no packets yet
	var fecRecovered, fecLost, decErrors uint64
	var txInjected, txDropped uint64
	var rxBytesRate, rxPacketRate float64

	// Sort stream names for consistent ordering
	streamNames := make([]string, 0, len(aggStats.Services))
	for name := range aggStats.Services {
		streamNames = append(streamNames, name)
	}
	sort.Strings(streamNames)

	for _, name := range streamNames {
		svc := aggStats.Services[name]
		// Determine stream type based on which direction has activity or config
		hasRX := svc.PacketsReceived > 0
		hasTX := svc.PacketsInjected > 0

		if hasRX || !hasTX {
			// Show as RX stream (or default if no activity)
			mcs := svc.SessionMCS
			webStats.Streams = append(webStats.Streams, web.StreamStats{
				Name:        name,
				Type:        "rx",
				PacketRate:  svc.RxRate,
				ByteRate:    svc.RxBytesRate,
				Packets:     svc.PacketsReceived,
				FECRecovery: svc.PacketsFECRec,
				FECLost:     svc.PacketsLost,
				DecErrors:   svc.PacketsDecErr,
				FecK:        svc.SessionFecK,
				FecN:        svc.SessionFecN,
				MCS:         &mcs,
			})
		}
		if hasTX {
			// Show as TX stream
			mcs := svc.SessionMCS
			webStats.Streams = append(webStats.Streams, web.StreamStats{
				Name:       name,
				Type:       "tx",
				PacketRate: svc.TxRate,
				ByteRate:   svc.TxBytesRate,
				Packets:    svc.PacketsInjected,
				Dropped:    svc.PacketsDropped,
				FecK:       svc.SessionFecK,
				FecN:       svc.SessionFecN,
				MCS:        &mcs,
			})
		}

		// Aggregate totals
		fecRecovered += svc.PacketsFECRec
		fecLost += svc.PacketsLost
		decErrors += svc.PacketsDecErr
		rxBytesRate += svc.RxBytesRate
		rxPacketRate += svc.RxRate
		txInjected += svc.PacketsInjected
		txDropped += svc.PacketsDropped
	}

	webStats.FECRecovery = int(fecRecovered)
	webStats.FECLost = int(fecLost)
	webStats.DecErrors = int(decErrors)
	webStats.Bitrate = rxBytesRate * 8 / 1_000_000 // Convert bytes/sec to Mbps
	webStats.PacketRate = rxPacketRate
	webStats.TXInjected = txInjected
	webStats.TXDropped = txDropped

	// Add adaptive link stats if enabled
	if s.config.Adaptive != nil && s.config.Adaptive.Enabled {
		alStats := &web.AdaptiveLinkStats{
			Mode: s.config.Adaptive.Mode,
		}

		if s.config.Adaptive.Mode == "gs" && s.linkMonitor != nil {
			// GS mode - we calculate and send stats to drone
			monStats := s.linkMonitor.GetStats()
			alStats.Score = monStats.Score
			alStats.BestRSSI = monStats.BestRSSI
			alStats.BestSNR = monStats.BestSNR
			alStats.Noise = monStats.Noise
		} else if s.config.Adaptive.Mode == "drone" && s.linkReceiver != nil {
			// Drone mode - we receive stats and adjust TX
			alStats.Score = s.linkReceiver.GetSmoothedScore()
			alStats.InFallback = s.linkReceiver.IsInFallbackMode()
			alStats.Paused = s.linkReceiver.IsPaused()

			if profile := s.linkReceiver.GetCurrentProfile(); profile != nil {
				alStats.ProfileMCS = profile.MCS
				alStats.ProfileFecK = profile.FecK
				alStats.ProfileFecN = profile.FecN
				alStats.ProfileBitrate = profile.Bitrate
				alStats.ProfileTXPower = profile.TXPower
				alStats.ProfileRangeMin = profile.RangeMin
				alStats.ProfileRangeMax = profile.RangeMax
			}
		}

		webStats.AdaptiveLink = alStats
	}

	// Build streams running status (caller holds s.mu)
	webStats.StreamsRunning = s.getStreamsRunningLocked()

	s.webServer.UpdateStats(webStats)
}

// getConfigForWeb returns the current config for the web API.
func (s *Server) getConfigForWeb() *config.Config {
	return s.config
}

// setConfigFromWeb saves a new config from the web API.
func (s *Server) setConfigFromWeb(newCfg *config.Config) error {
	if s.configPath == "" {
		return fmt.Errorf("config path not set, cannot save")
	}

	// Save to file
	if err := newCfg.Save(s.configPath); err != nil {
		return err
	}

	// Apply to running server
	s.applyConfig(newCfg)

	log.Printf("Config saved to %s", s.configPath)
	return nil
}

// applyConfig applies config changes to the running server without restart.
func (s *Server) applyConfig(newCfg *config.Config) {
	s.mu.Lock()
	oldCfg := s.config
	s.config = newCfg
	s.mu.Unlock()

	// Apply hardware changes
	s.applyHardwareChanges(oldCfg, newCfg)

	// Apply stream changes (restart affected streams)
	s.applyStreamChanges(oldCfg, newCfg)

	// Apply adaptive link changes
	s.applyAdaptiveChanges(oldCfg, newCfg)
}

// applyHardwareChanges applies hardware config changes.
// Only handles true hardware settings (channel, tx_power).
// Radio params (MCS, STBC, etc.) are per-stream and applied on stream restart.
func (s *Server) applyHardwareChanges(oldCfg, newCfg *config.Config) {
	if s.wlanMgr == nil {
		return
	}

	old := oldCfg.Hardware
	new := newCfg.Hardware

	// Channel/bandwidth change (requires iw command)
	if old.Channel != new.Channel || old.Bandwidth != new.Bandwidth {
		if err := s.wlanMgr.SetChannel(new.Channel, new.Bandwidth); err != nil {
			log.Printf("Failed to set channel: %v", err)
		} else {
			log.Printf("Channel changed to %d (BW=%d)", new.Channel, new.Bandwidth)
		}
	}

	// TX power change (requires iw command)
	oldPower := 0
	newPower := 0
	if old.TXPower != nil {
		oldPower = *old.TXPower
	}
	if new.TXPower != nil {
		newPower = *new.TXPower
	}
	if oldPower != newPower && newPower > 0 {
		if err := s.wlanMgr.SetTXPower(newPower); err != nil {
			log.Printf("Failed to set TX power: %v", err)
		} else {
			log.Printf("TX power changed to %d dBm", newPower)
		}
	}
}

// applyStreamChanges detects and applies stream config changes.
func (s *Server) applyStreamChanges(oldCfg, newCfg *config.Config) {
	// Find streams to stop (removed or changed)
	for name, oldStream := range oldCfg.Streams {
		newStream, exists := newCfg.Streams[name]
		if !exists {
			// Stream removed - stop it
			log.Printf("Stream %s removed, stopping", name)
			s.stopStreamByName(name)
		} else if !streamsEqual(&oldStream, &newStream) {
			// Stream changed - restart it
			log.Printf("Stream %s changed, restarting", name)
			s.stopStreamByName(name)
			if err := s.startStreamByName(name); err != nil {
				log.Printf("Failed to restart stream %s: %v", name, err)
			}
		}
	}

	// Find new streams to start
	for name := range newCfg.Streams {
		if _, exists := oldCfg.Streams[name]; !exists {
			// New stream - start it
			log.Printf("Stream %s added, starting", name)
			if err := s.startStreamByName(name); err != nil {
				log.Printf("Failed to start new stream %s: %v", name, err)
			}
		}
	}
}

// streamsEqual compares two stream configs for equality.
func streamsEqual(a, b *config.StreamConfig) bool {
	if a.ServiceType != b.ServiceType {
		return false
	}
	if a.Peer != b.Peer {
		return false
	}
	if !uint8PtrEqual(a.StreamRX, b.StreamRX) || !uint8PtrEqual(a.StreamTX, b.StreamTX) {
		return false
	}
	if a.FEC != b.FEC {
		return false
	}
	// Radio params
	if !intPtrEqual(a.MCS, b.MCS) || a.ShortGI != b.ShortGI {
		return false
	}
	if !intPtrEqual(a.STBC, b.STBC) || !intPtrEqual(a.LDPC, b.LDPC) {
		return false
	}
	if a.Bandwidth != b.Bandwidth {
		return false
	}
	if a.FECTimeout != b.FECTimeout || a.FECDelay != b.FECDelay {
		return false
	}
	if a.Mirror != b.Mirror || a.UseQdisc != b.UseQdisc || a.FWMark != b.FWMark {
		return false
	}
	// Compare tunnel config
	if (a.Tunnel == nil) != (b.Tunnel == nil) {
		return false
	}
	if a.Tunnel != nil && b.Tunnel != nil {
		if a.Tunnel.Ifname != b.Tunnel.Ifname || a.Tunnel.Ifaddr != b.Tunnel.Ifaddr {
			return false
		}
	}
	// Compare mavlink config
	if (a.Mavlink == nil) != (b.Mavlink == nil) {
		return false
	}
	if a.Mavlink != nil && b.Mavlink != nil {
		if a.Mavlink.InjectRSSI != b.Mavlink.InjectRSSI ||
			a.Mavlink.SysID != b.Mavlink.SysID ||
			a.Mavlink.CompID != b.Mavlink.CompID {
			return false
		}
	}
	return true
}

func uint8PtrEqual(a, b *uint8) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func intPtrEqual(a, b *int) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// applyAdaptiveChanges applies adaptive link config changes.
func (s *Server) applyAdaptiveChanges(oldCfg, newCfg *config.Config) {
	oldEnabled := oldCfg.Adaptive != nil && oldCfg.Adaptive.Enabled
	newEnabled := newCfg.Adaptive != nil && newCfg.Adaptive.Enabled

	// If disabled -> enabled or config changed, restart
	needsRestart := false
	if !oldEnabled && newEnabled {
		needsRestart = true
	} else if oldEnabled && newEnabled {
		// Check if config changed
		needsRestart = !adaptiveConfigsEqual(oldCfg.Adaptive, newCfg.Adaptive)
	}

	// Stop existing
	if oldEnabled && (needsRestart || !newEnabled) {
		if s.linkMonitor != nil {
			s.linkMonitor.Stop()
			s.linkMonitor = nil
			log.Printf("Adaptive link monitor stopped")
		}
		if s.linkReceiver != nil {
			s.linkReceiver.Stop()
			s.linkReceiver = nil
			log.Printf("Adaptive link receiver stopped")
		}
	}

	// Start new
	if newEnabled && needsRestart {
		s.startAdaptiveLink()
	}
}

// adaptiveConfigsEqual compares adaptive configs (simplified).
func adaptiveConfigsEqual(a, b *config.AdaptiveConfig) bool {
	if a.Mode != b.Mode {
		return false
	}
	if a.SendAddr != b.SendAddr {
		return false
	}
	if a.ListenPort != b.ListenPort {
		return false
	}
	// Could add more detailed comparison but mode/addr/port are the key ones
	return true
}

// getStreamsForWeb returns stream info for the web API.
func (s *Server) getStreamsForWeb() []web.StreamInfo {
	s.mu.Lock()
	defer s.mu.Unlock()

	var streams []web.StreamInfo

	// Build a map of running services by name
	runningServices := make(map[string]Service)
	for _, svc := range s.services {
		runningServices[svc.Name()] = svc
	}

	// Report all configured streams
	for name, streamCfg := range s.config.Streams {
		_, running := runningServices[name]
		fecK, fecN := streamCfg.GetFEC()

		info := web.StreamInfo{
			Name:        name,
			Running:     running,
			ServiceType: streamCfg.ServiceType,
			StreamRX:    streamCfg.StreamRX,
			StreamTX:    streamCfg.StreamTX,
			Peer:        streamCfg.Peer,
			FEC:         [2]int{fecK, fecN},
			Config:      &streamCfg,
		}
		streams = append(streams, info)
	}

	return streams
}

// getStreamsRunningLocked returns a map of stream name to running status.
// Caller must hold s.mu.
func (s *Server) getStreamsRunningLocked() map[string]bool {
	running := make(map[string]bool, len(s.config.Streams))
	for _, svc := range s.services {
		running[svc.Name()] = true
	}
	for name := range s.config.Streams {
		if _, ok := running[name]; !ok {
			running[name] = false
		}
	}
	return running
}

// startStreamByName starts a stream by name.
func (s *Server) startStreamByName(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if already running
	for _, svc := range s.services {
		if svc.Name() == name {
			return fmt.Errorf("stream %s is already running", name)
		}
	}

	// Get stream config
	streamCfg, ok := s.config.Streams[name]
	if !ok {
		return fmt.Errorf("stream %s not found in config", name)
	}

	// Get link ID
	var linkID uint32
	if s.config.Link.ID != 0 {
		linkID = s.config.Link.ID & 0xFFFFFF
	} else {
		linkID = HashLinkDomain(s.config.Link.Domain)
	}

	// Create service config
	svcCfg, err := NewServiceConfig(name, &streamCfg, s.config, s.wlans, linkID)
	if err != nil {
		return fmt.Errorf("configure service: %w", err)
	}
	svcCfg.CaptureManager = s.captureManager

	// Wire up video callback if this is the video stream
	if s.videoChan != nil && s.config.Web != nil {
		videoStreamName := s.config.Web.VideoStream
		if videoStreamName == "" {
			videoStreamName = "video"
		}
		if name == videoStreamName {
			svcCfg.VideoCallback = s.makeVideoCallback()
		}
	}

	// Create and start service
	svc, err := CreateService(svcCfg)
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}

	if err := svc.Start(s.ctx); err != nil {
		return fmt.Errorf("start service: %w", err)
	}

	s.services = append(s.services, svc)
	log.Printf("Started stream: %s", name)

	return nil
}

// stopStreamByName stops a stream by name.
func (s *Server) stopStreamByName(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Find the service
	var idx int = -1
	var svc Service
	for i, srv := range s.services {
		if srv.Name() == name {
			idx = i
			svc = srv
			break
		}
	}

	if idx < 0 {
		return fmt.Errorf("stream %s is not running", name)
	}

	// Stop the service
	if err := svc.Stop(); err != nil {
		return fmt.Errorf("stop service: %w", err)
	}

	// Remove from list
	s.services = append(s.services[:idx], s.services[idx+1:]...)
	log.Printf("Stopped stream: %s", name)

	return nil
}
