package at

import (
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

type CSIM struct {
	at *AT
}

func NewCSIM(at *AT) *CSIM { return &CSIM{at: at} }

func (c *CSIM) Run(command []byte) ([]byte, error) {
	sw, err := c.runRaw(command)
	if err != nil {
		return nil, err
	}
	if len(sw) < 2 {
		return nil, fmt.Errorf("unexpected response: %X", sw)
	}
	switch sw[len(sw)-2] {
	case 0x61:
		return c.read(sw[len(sw)-1:])
	case 0x90:
		return sw, nil
	default:
		return sw, fmt.Errorf("unexpected response: %X", sw)
	}
}

func (c *CSIM) runRaw(command []byte) ([]byte, error) {
	cmd := fmt.Sprintf("%X", command)
	cmd = fmt.Sprintf("AT+CSIM=%d,%q", len(cmd), cmd)
	slog.Debug("[AT] CSIM Sending", "command", cmd)
	response, err := c.at.Run(cmd)
	slog.Debug("[AT] CSIM Received", "response", response, "error", err)
	if err != nil {
		return nil, err
	}
	sw, err := c.sw(response)
	if err != nil {
		return nil, err
	}
	return sw, nil
}

func (c *CSIM) read(length []byte) ([]byte, error) {
	var data []byte
	for {
		b, err := c.runRaw(append([]byte{0x00, 0xC0, 0x00, 0x00}, length...))
		if err != nil {
			return nil, err
		}
		if len(b) < 2 {
			return nil, fmt.Errorf("unexpected response: %X", b)
		}
		data = append(data, b[:len(b)-2]...)
		if b[len(b)-2] == 0x90 {
			break
		}
		if b[len(b)-2] != 0x61 {
			return nil, fmt.Errorf("unexpected response: %X", b)
		}
		length = b[len(b)-1:]
	}
	return data, nil
}

func (c *CSIM) sw(response string) ([]byte, error) {
	index := strings.Index(response, "+CSIM:")
	if index < 0 {
		return nil, fmt.Errorf("unexpected response: %s", response)
	}
	parts := strings.SplitN(strings.TrimSpace(response[index+len("+CSIM:"):]), ",", 2)
	if len(parts) < 2 {
		return nil, fmt.Errorf("unexpected response: %s", response)
	}
	size, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return nil, fmt.Errorf("parse CSIM response length: %w", err)
	}
	data := strings.Trim(strings.TrimSpace(parts[1]), "\"")
	if data == "" {
		return nil, errors.New("response data is empty")
	}
	if len(data) != size {
		return nil, fmt.Errorf("CSIM response length = %d, want %d", len(data), size)
	}
	decoded, err := hex.DecodeString(data)
	if err != nil {
		return nil, fmt.Errorf("decode CSIM data: %w", err)
	}
	return decoded, nil
}
