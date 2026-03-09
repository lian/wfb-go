package server

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// CBAUD is the bitmask for baud rate in termios cflag (Linux-specific).
const cbaudMask = 0010017

// TUN constants
const (
	tunDevice = "/dev/net/tun"
	tunSetIff = 0x400454ca
	iffTun    = 0x0001
	iffNoPi   = 0x1000
	ifnamsiz  = 16
)

type tunIfreq struct {
	name  [ifnamsiz]byte
	flags uint16
	_     [22]byte
}

func openTunDevice(name string) (*os.File, error) {
	fd, err := os.OpenFile(tunDevice, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}

	var req tunIfreq
	copy(req.name[:], name)
	req.flags = iffTun | iffNoPi

	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd.Fd(), tunSetIff, uintptr(unsafe.Pointer(&req)))
	if errno != 0 {
		fd.Close()
		return nil, errno
	}

	// Set non-blocking mode for poll-based reading
	if err := syscall.SetNonblock(int(fd.Fd()), true); err != nil {
		fd.Close()
		return nil, err
	}

	return fd, nil
}

func configureTunDevice(name, addr string, mtu int, defaultRoute bool) error {
	cmd := exec.Command("ip", "link", "set", "up", "mtu", strconv.Itoa(mtu), "dev", name)
	if err := cmd.Run(); err != nil {
		return err
	}

	if addr != "" {
		cmd = exec.Command("ip", "addr", "add", addr, "dev", name)
		if err := cmd.Run(); err != nil {
			return err
		}
	}

	if defaultRoute && addr != "" {
		ip, _, _ := net.ParseCIDR(addr)
		if ip != nil {
			exec.Command("ip", "route", "add", "default", "via", ip.String(), "dev", name).Run()
		}
	}

	return nil
}

func openSerialPort(port string, baud int) (int, error) {
	fd, err := syscall.Open(port, syscall.O_RDWR|syscall.O_NOCTTY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return -1, err
	}

	var termios syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TCGETS, uintptr(unsafe.Pointer(&termios)))
	if errno != 0 {
		syscall.Close(fd)
		return -1, errno
	}

	speed := baudRate(baud)
	if speed == 0 {
		syscall.Close(fd)
		return -1, fmt.Errorf("unsupported baud: %d", baud)
	}

	// Raw mode
	termios.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP | syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	termios.Oflag &^= syscall.OPOST
	termios.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	termios.Cflag &^= syscall.CSIZE | syscall.PARENB
	termios.Cflag |= syscall.CS8 | syscall.CLOCAL | syscall.CREAD
	termios.Cflag &^= cbaudMask
	termios.Cflag |= speed
	termios.Ispeed = speed
	termios.Ospeed = speed

	_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TCSETS, uintptr(unsafe.Pointer(&termios)))
	if errno != 0 {
		syscall.Close(fd)
		return -1, errno
	}

	return fd, nil
}

func baudRate(baud int) uint32 {
	rates := map[int]uint32{
		9600: syscall.B9600, 19200: syscall.B19200, 38400: syscall.B38400,
		57600: syscall.B57600, 115200: syscall.B115200, 230400: syscall.B230400,
		460800: syscall.B460800, 500000: syscall.B500000, 576000: syscall.B576000,
		921600: syscall.B921600, 1000000: syscall.B1000000, 1500000: syscall.B1500000,
	}
	return rates[baud]
}

// readSerial reads from a serial port file descriptor with non-blocking handling.
func readSerial(fd int, buf []byte) (int, error) {
	n, err := syscall.Read(fd, buf)
	if err == syscall.EAGAIN {
		time.Sleep(10 * time.Millisecond)
		return 0, nil
	}
	return n, err
}

// tunReader wraps TUN device reading with efficient poll-based waiting.
type tunReader struct {
	fd      int // raw fd for syscall.Read (bypasses Go's netpoller)
	pollFds []unix.PollFd
}

func newTunReader(fd *os.File) *tunReader {
	return &tunReader{
		fd:      int(fd.Fd()),
		pollFds: []unix.PollFd{{Fd: int32(fd.Fd()), Events: unix.POLLIN | unix.POLLERR | unix.POLLHUP}},
	}
}

// Read reads from TUN with poll-based waiting (100ms timeout).
// Returns (0, nil) on timeout to allow checking stop conditions.
// Returns error on fd closed or other errors.
func (t *tunReader) Read(buf []byte) (int, error) {
	// Poll with 100ms timeout - efficient wait, allows quick shutdown
	n, err := unix.Poll(t.pollFds, 100)
	if err != nil {
		if err == unix.EINTR {
			return 0, nil // Interrupted, let caller check stop condition
		}
		return 0, err
	}
	if n == 0 {
		return 0, nil // Timeout, let caller check stop condition
	}

	// Check for poll errors
	revents := t.pollFds[0].Revents
	if revents&unix.POLLHUP != 0 || revents&unix.POLLERR != 0 || revents&unix.POLLNVAL != 0 {
		return 0, os.ErrClosed
	}

	// Data ready - read it using raw syscall (bypasses Go's netpoller)
	n, err = syscall.Read(t.fd, buf)
	if err != nil {
		// Handle EAGAIN/EWOULDBLOCK from non-blocking fd (spurious poll wakeup)
		if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
			return 0, nil
		}
		return 0, err
	}
	return n, nil
}
