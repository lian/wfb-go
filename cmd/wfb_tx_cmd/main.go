// wfb_tx_cmd sends commands to a running wfb_tx instance.
//
// Usage:
//
//	wfb_tx_cmd <port> <command> [options]
//
// Commands:
//
//	set_fec   - Set FEC parameters (-k, -n)
//	get_fec   - Get current FEC parameters
//	set_radio - Set radio parameters (-B, -G, -S, -L, -M, -N, -V)
//	get_radio - Get current radio parameters
//
// Examples:
//
//	wfb_tx_cmd 8000 set_fec -k 4 -n 8
//	wfb_tx_cmd 8000 get_fec
//	wfb_tx_cmd 8000 set_radio -M 3 -B 40
//	wfb_tx_cmd 8000 get_radio
package main

import (
	"flag"
	"fmt"
	"math/rand"
	"net"
	"os"
	"strings"
	"time"

	"github.com/lian/wfb-go/pkg/protocol"
	"github.com/lian/wfb-go/pkg/version"
)

func main() {
	if len(os.Args) < 3 {
		usage()
		os.Exit(1)
	}

	// Parse port
	var port int
	if _, err := fmt.Sscanf(os.Args[1], "%d", &port); err != nil {
		fmt.Fprintf(os.Stderr, "Invalid port: %s\n", os.Args[1])
		os.Exit(1)
	}

	command := os.Args[2]

	// Seed random for request ID
	rand.Seed(time.Now().UnixNano())

	var err error
	switch command {
	case "set_fec":
		err = cmdSetFEC(port, os.Args[3:])
	case "get_fec":
		err = cmdGetFEC(port)
	case "set_radio":
		err = cmdSetRadio(port, os.Args[3:])
	case "get_radio":
		err = cmdGetRadio(port)
	case "-version", "--version", "version":
		fmt.Printf("wfb_tx_cmd %s\n", version.String())
		os.Exit(0)
	case "-h", "--help", "help":
		usage()
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		usage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "wfb_tx_cmd - Control a running wfb_tx instance\n\n")
	fmt.Fprintf(os.Stderr, "Usage: %s <port> <command> [options]\n\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "Commands:\n")
	fmt.Fprintf(os.Stderr, "  set_fec   Set FEC parameters\n")
	fmt.Fprintf(os.Stderr, "  get_fec   Get current FEC parameters\n")
	fmt.Fprintf(os.Stderr, "  set_radio Set radio parameters\n")
	fmt.Fprintf(os.Stderr, "  get_radio Get current radio parameters\n")
	fmt.Fprintf(os.Stderr, "\nExamples:\n")
	fmt.Fprintf(os.Stderr, "  %s 8000 set_fec -k 4 -n 8\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s 8000 get_fec\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s 8000 set_radio -M 3 -B 40\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s 8000 get_radio\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "\nVersion: %s\n", version.String())
}

func cmdSetFEC(port int, args []string) error {
	fs := flag.NewFlagSet("set_fec", flag.ExitOnError)
	k := fs.Int("k", 8, "FEC data shards")
	n := fs.Int("n", 12, "FEC total shards")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: wfb_tx_cmd <port> set_fec [-k RS_K] [-n RS_N]\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	req := &protocol.CmdRequest{
		ReqID: rand.Uint32(),
		CmdID: protocol.CMD_SET_FEC,
		SetFEC: protocol.CmdSetFEC{
			K: uint8(*k),
			N: uint8(*n),
		},
	}

	_, err := sendCommand(port, req)
	return err
}

func cmdGetFEC(port int) error {
	req := &protocol.CmdRequest{
		ReqID: rand.Uint32(),
		CmdID: protocol.CMD_GET_FEC,
	}

	resp, err := sendCommand(port, req)
	if err != nil {
		return err
	}

	fmt.Printf("k=%d\n", resp.GetFEC.K)
	fmt.Printf("n=%d\n", resp.GetFEC.N)
	return nil
}

func cmdSetRadio(port int, args []string) error {
	fs := flag.NewFlagSet("set_radio", flag.ExitOnError)
	bandwidth := fs.Int("B", 20, "Bandwidth (20/40/80 MHz)")
	gi := fs.String("G", "long", "Guard interval (short/long)")
	stbc := fs.Int("S", 0, "STBC streams (0-2)")
	ldpc := fs.Int("L", 0, "LDPC (0=off, 1=on)")
	mcs := fs.Int("M", 1, "MCS index")
	vhtNSS := fs.Int("N", 1, "VHT spatial streams")
	vhtMode := fs.Bool("V", false, "VHT mode (802.11ac)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: wfb_tx_cmd <port> set_radio [options]\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	shortGI := strings.ToLower(*gi) == "short" || *gi == "s" || *gi == "S"

	// Force VHT for bandwidth >= 80
	vht := *vhtMode || *bandwidth >= 80

	req := &protocol.CmdRequest{
		ReqID: rand.Uint32(),
		CmdID: protocol.CMD_SET_RADIO,
		SetRadio: protocol.CmdSetRadio{
			STBC:      uint8(*stbc),
			LDPC:      *ldpc != 0,
			ShortGI:   shortGI,
			Bandwidth: uint8(*bandwidth),
			MCSIndex:  uint8(*mcs),
			VHTMode:   vht,
			VHTNSS:    uint8(*vhtNSS),
		},
	}

	_, err := sendCommand(port, req)
	return err
}

func cmdGetRadio(port int) error {
	req := &protocol.CmdRequest{
		ReqID: rand.Uint32(),
		CmdID: protocol.CMD_GET_RADIO,
	}

	resp, err := sendCommand(port, req)
	if err != nil {
		return err
	}

	fmt.Printf("stbc=%d\n", resp.GetRadio.STBC)
	fmt.Printf("ldpc=%d\n", boolToInt(resp.GetRadio.LDPC))
	fmt.Printf("short_gi=%d\n", boolToInt(resp.GetRadio.ShortGI))
	fmt.Printf("bandwidth=%d\n", resp.GetRadio.Bandwidth)
	fmt.Printf("mcs_index=%d\n", resp.GetRadio.MCSIndex)
	fmt.Printf("vht_mode=%d\n", boolToInt(resp.GetRadio.VHTMode))
	fmt.Printf("vht_nss=%d\n", resp.GetRadio.VHTNSS)
	return nil
}

func sendCommand(port int, req *protocol.CmdRequest) (*protocol.CmdResponse, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	conn, err := net.Dial("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}
	defer conn.Close()

	// Set timeout
	conn.SetDeadline(time.Now().Add(3 * time.Second))

	// Send request
	reqData := protocol.MarshalCmdRequest(req)
	if _, err := conn.Write(reqData); err != nil {
		return nil, fmt.Errorf("failed to send command: %w", err)
	}

	// Read response
	respBuf := make([]byte, 64)
	n, err := conn.Read(respBuf)
	if err != nil {
		return nil, fmt.Errorf("command timed out")
	}

	// Parse response
	resp, err := protocol.UnmarshalCmdResponse(respBuf[:n], req.CmdID)
	if err != nil {
		return nil, fmt.Errorf("invalid response: %w", err)
	}

	// Check request ID
	if resp.ReqID != req.ReqID {
		return nil, fmt.Errorf("response ID mismatch")
	}

	// Check return code
	if resp.RC != 0 {
		return nil, fmt.Errorf("command failed: %s", errnoToString(resp.RC))
	}

	return resp, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func errnoToString(rc uint32) string {
	switch rc {
	case 22:
		return "EINVAL (invalid argument)"
	default:
		return fmt.Sprintf("errno %d", rc)
	}
}

