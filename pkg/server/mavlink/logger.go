package mavlink

import (
	"time"

	"github.com/lian/wfb-go/pkg/server/util"
)

// Logger logs mavlink messages to a binary logger.
// Matches wfb-ng's MavlinkLoggerProtocol format.
type Logger struct {
	logger *util.BinaryLogger
	parser *Parser
}

// NewLogger creates a new mavlink message logger.
func NewLogger(logger *util.BinaryLogger) *Logger {
	return &Logger{
		logger: logger,
		parser: NewParser(),
	}
}

// Log parses and logs mavlink messages from raw data.
func (m *Logger) Log(data []byte) {
	messages := m.parser.Parse(data)
	for _, msg := range messages {
		m.logMessage(&msg)
	}
}

// logMessage logs a single mavlink message.
func (m *Logger) logMessage(msg *Message) {
	// Match wfb-ng format: type, timestamp, hdr (seq, sys_id, comp_id, msg_id), msg
	record := map[string]interface{}{
		"type":      "mavlink",
		"timestamp": float64(time.Now().UnixNano()) / 1e9,
		"hdr": []interface{}{
			msg.Header.Seq,
			msg.Header.SysID,
			msg.Header.CompID,
			msg.Header.MsgID,
		},
		"msg": msg.Raw,
	}

	m.logger.Write(record)
}
