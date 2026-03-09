package web

// VideoSink receives video data directly from RX services.
type VideoSink interface {
	// WriteVideo receives raw video NAL unit data.
	WriteVideo(data []byte)
}

// Ensure Server implements VideoSink
var _ VideoSink = (*Server)(nil)

// WriteVideo receives video data and broadcasts to WebSocket clients.
// This allows direct integration without UDP.
// Supports both RTP-wrapped HEVC and raw Annex B streams.
func (s *Server) WriteVideo(data []byte) {
	// Parse NAL units (auto-detects RTP vs Annex B)
	nalUnits := s.parser.ParseRTP(data)
	for _, nalu := range nalUnits {
		s.broadcastNALU(nalu)
	}
}
