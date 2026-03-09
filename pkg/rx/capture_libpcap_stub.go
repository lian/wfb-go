//go:build !libpcap

package rx

import (
	"errors"
	"time"

	"github.com/gopacket/gopacket"
)

// ErrLibpcapNotEnabled is returned when libpcap mode is requested but not compiled in.
var ErrLibpcapNotEnabled = errors.New("libpcap capture mode not enabled (build with -tags libpcap)")

// LibpcapSource is a stub when libpcap is not compiled in.
type LibpcapSource struct{}

// NewLibpcapSource returns an error when libpcap is not enabled.
func NewLibpcapSource(iface string, channelID uint32, wlanIdx uint8, rcvBufSize int) (*LibpcapSource, error) {
	return nil, ErrLibpcapNotEnabled
}

// NewLibpcapSourceWithTimeout returns an error when libpcap is not enabled.
func NewLibpcapSourceWithTimeout(iface string, channelID uint32, wlanIdx uint8, rcvBufSize int, timeout time.Duration) (*LibpcapSource, error) {
	return nil, ErrLibpcapNotEnabled
}

// ReadPacket is a stub that should never be called.
func (s *LibpcapSource) ReadPacket() ([]byte, gopacket.CaptureInfo, error) {
	return nil, gopacket.CaptureInfo{}, ErrLibpcapNotEnabled
}

// Close is a stub.
func (s *LibpcapSource) Close() error {
	return nil
}
