package protocol

import (
	"encoding/binary"
	"errors"
)

// Command IDs
const (
	CMD_SET_FEC   = 1
	CMD_SET_RADIO = 2
	CMD_GET_FEC   = 3
	CMD_GET_RADIO = 4
)

// Command request/response sizes
const (
	CMD_REQ_HEADER_SIZE  = 5 // req_id (4) + cmd_id (1)
	CMD_RESP_HEADER_SIZE = 8 // req_id (4) + rc (4)

	CMD_FEC_SIZE   = 2 // k (1) + n (1)
	CMD_RADIO_SIZE = 7 // stbc, ldpc, short_gi, bandwidth, mcs_index, vht_mode, vht_nss
)

var (
	ErrInvalidCommand = errors.New("protocol: invalid command")
)

// CmdSetFEC holds FEC parameters for set_fec command.
type CmdSetFEC struct {
	K uint8
	N uint8
}

// CmdSetRadio holds radio parameters for set_radio command.
type CmdSetRadio struct {
	STBC      uint8
	LDPC      bool
	ShortGI   bool
	Bandwidth uint8
	MCSIndex  uint8
	VHTMode   bool
	VHTNSS    uint8
}

// CmdRequest represents a command request.
type CmdRequest struct {
	ReqID uint32
	CmdID uint8

	// Union payload (only one is valid based on CmdID)
	SetFEC   CmdSetFEC
	SetRadio CmdSetRadio
}

// CmdResponse represents a command response.
type CmdResponse struct {
	ReqID uint32
	RC    uint32 // Return code (0 = success, errno on error)

	// Union payload for GET commands
	GetFEC   CmdSetFEC
	GetRadio CmdSetRadio
}

// MarshalCmdRequest serializes a command request.
func MarshalCmdRequest(req *CmdRequest) []byte {
	var buf []byte

	switch req.CmdID {
	case CMD_SET_FEC:
		buf = make([]byte, CMD_REQ_HEADER_SIZE+CMD_FEC_SIZE)
		binary.BigEndian.PutUint32(buf[0:4], req.ReqID)
		buf[4] = req.CmdID
		buf[5] = req.SetFEC.K
		buf[6] = req.SetFEC.N

	case CMD_SET_RADIO:
		buf = make([]byte, CMD_REQ_HEADER_SIZE+CMD_RADIO_SIZE)
		binary.BigEndian.PutUint32(buf[0:4], req.ReqID)
		buf[4] = req.CmdID
		buf[5] = req.SetRadio.STBC
		buf[6] = boolToByte(req.SetRadio.LDPC)
		buf[7] = boolToByte(req.SetRadio.ShortGI)
		buf[8] = req.SetRadio.Bandwidth
		buf[9] = req.SetRadio.MCSIndex
		buf[10] = boolToByte(req.SetRadio.VHTMode)
		buf[11] = req.SetRadio.VHTNSS

	case CMD_GET_FEC, CMD_GET_RADIO:
		buf = make([]byte, CMD_REQ_HEADER_SIZE)
		binary.BigEndian.PutUint32(buf[0:4], req.ReqID)
		buf[4] = req.CmdID

	default:
		return nil
	}

	return buf
}

// UnmarshalCmdRequest parses a command request.
func UnmarshalCmdRequest(data []byte) (*CmdRequest, error) {
	if len(data) < CMD_REQ_HEADER_SIZE {
		return nil, ErrInvalidCommand
	}

	req := &CmdRequest{
		ReqID: binary.BigEndian.Uint32(data[0:4]),
		CmdID: data[4],
	}

	switch req.CmdID {
	case CMD_SET_FEC:
		if len(data) < CMD_REQ_HEADER_SIZE+CMD_FEC_SIZE {
			return nil, ErrInvalidCommand
		}
		req.SetFEC.K = data[5]
		req.SetFEC.N = data[6]

	case CMD_SET_RADIO:
		if len(data) < CMD_REQ_HEADER_SIZE+CMD_RADIO_SIZE {
			return nil, ErrInvalidCommand
		}
		req.SetRadio.STBC = data[5]
		req.SetRadio.LDPC = data[6] != 0
		req.SetRadio.ShortGI = data[7] != 0
		req.SetRadio.Bandwidth = data[8]
		req.SetRadio.MCSIndex = data[9]
		req.SetRadio.VHTMode = data[10] != 0
		req.SetRadio.VHTNSS = data[11]

	case CMD_GET_FEC, CMD_GET_RADIO:
		// No additional payload

	default:
		return nil, ErrInvalidCommand
	}

	return req, nil
}

// MarshalCmdResponse serializes a command response.
func MarshalCmdResponse(resp *CmdResponse, cmdID uint8) []byte {
	var buf []byte

	switch cmdID {
	case CMD_SET_FEC, CMD_SET_RADIO:
		buf = make([]byte, CMD_RESP_HEADER_SIZE)
		binary.BigEndian.PutUint32(buf[0:4], resp.ReqID)
		binary.BigEndian.PutUint32(buf[4:8], resp.RC)

	case CMD_GET_FEC:
		buf = make([]byte, CMD_RESP_HEADER_SIZE+CMD_FEC_SIZE)
		binary.BigEndian.PutUint32(buf[0:4], resp.ReqID)
		binary.BigEndian.PutUint32(buf[4:8], resp.RC)
		buf[8] = resp.GetFEC.K
		buf[9] = resp.GetFEC.N

	case CMD_GET_RADIO:
		buf = make([]byte, CMD_RESP_HEADER_SIZE+CMD_RADIO_SIZE)
		binary.BigEndian.PutUint32(buf[0:4], resp.ReqID)
		binary.BigEndian.PutUint32(buf[4:8], resp.RC)
		buf[8] = resp.GetRadio.STBC
		buf[9] = boolToByte(resp.GetRadio.LDPC)
		buf[10] = boolToByte(resp.GetRadio.ShortGI)
		buf[11] = resp.GetRadio.Bandwidth
		buf[12] = resp.GetRadio.MCSIndex
		buf[13] = boolToByte(resp.GetRadio.VHTMode)
		buf[14] = resp.GetRadio.VHTNSS

	default:
		return nil
	}

	return buf
}

// UnmarshalCmdResponse parses a command response.
func UnmarshalCmdResponse(data []byte, cmdID uint8) (*CmdResponse, error) {
	if len(data) < CMD_RESP_HEADER_SIZE {
		return nil, ErrInvalidCommand
	}

	resp := &CmdResponse{
		ReqID: binary.BigEndian.Uint32(data[0:4]),
		RC:    binary.BigEndian.Uint32(data[4:8]),
	}

	switch cmdID {
	case CMD_SET_FEC, CMD_SET_RADIO:
		// No payload

	case CMD_GET_FEC:
		if len(data) < CMD_RESP_HEADER_SIZE+CMD_FEC_SIZE {
			return nil, ErrInvalidCommand
		}
		resp.GetFEC.K = data[8]
		resp.GetFEC.N = data[9]

	case CMD_GET_RADIO:
		if len(data) < CMD_RESP_HEADER_SIZE+CMD_RADIO_SIZE {
			return nil, ErrInvalidCommand
		}
		resp.GetRadio.STBC = data[8]
		resp.GetRadio.LDPC = data[9] != 0
		resp.GetRadio.ShortGI = data[10] != 0
		resp.GetRadio.Bandwidth = data[11]
		resp.GetRadio.MCSIndex = data[12]
		resp.GetRadio.VHTMode = data[13] != 0
		resp.GetRadio.VHTNSS = data[14]

	default:
		return nil, ErrInvalidCommand
	}

	return resp, nil
}

func boolToByte(b bool) byte {
	if b {
		return 1
	}
	return 0
}
