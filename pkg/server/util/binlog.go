package util

import (
	"encoding/binary"
	"os"
	"sync"

	"github.com/vmihailenco/msgpack/v5"
)

// BinaryLogger writes msgpack-encoded records to a binary log file.
// This matches wfb-ng's BinLogger format for compatibility.
type BinaryLogger struct {
	mu   sync.Mutex
	file *os.File
}

// NewBinaryLogger creates a new binary logger writing to the specified file.
func NewBinaryLogger(path string) (*BinaryLogger, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	return &BinaryLogger{file: file}, nil
}

// Write writes a record to the log file.
// Format: [4-byte big-endian length][msgpack data]
func (l *BinaryLogger) Write(data map[string]interface{}) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	encoded, err := msgpack.Marshal(data)
	if err != nil {
		return err
	}

	// Write length prefix (big-endian)
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(encoded)))

	if _, err := l.file.Write(lenBuf); err != nil {
		return err
	}
	if _, err := l.file.Write(encoded); err != nil {
		return err
	}

	return nil
}

// Close closes the log file.
func (l *BinaryLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.file.Close()
}
