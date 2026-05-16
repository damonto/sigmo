package at

import (
	"encoding/hex"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

type CRSMInstruction uint16

const (
	CRSMReadBinary   CRSMInstruction = 0xB0
	CRSMReadRecord   CRSMInstruction = 0xB2
	CRSMGetResponse  CRSMInstruction = 0xC0
	CRSMUpdateBinary CRSMInstruction = 0xD6
	CRSMUpdateRecord CRSMInstruction = 0xDC
	CRSMStatus       CRSMInstruction = 0xF2
)

type CRSM struct{ at *AT }

func NewCRSM(at *AT) *CRSM { return &CRSM{at: at} }

type CRSMCommand struct {
	Instruction CRSMInstruction
	FileID      uint16
	P1          byte
	P2          byte
	Data        []byte
}

func (c CRSMCommand) Bytes() []byte {
	return fmt.Appendf(nil, "%d,%d,%d,%d,%d,\"%X\"", c.Instruction, c.FileID, c.P1, c.P2, len(c.Data), c.Data)
}

func (c *CRSM) Run(command []byte) ([]byte, error) {
	cmd := fmt.Sprintf("AT+CRSM=%s", command)
	slog.Debug("[AT] CRSM Sending", "command", cmd)
	response, err := c.at.Run(cmd)
	slog.Debug("[AT] CRSM Received", "response", response, "error", err)
	if err != nil {
		return nil, err
	}
	return c.sw(response)
}

func (c *CRSM) sw(response string) ([]byte, error) {
	index := strings.Index(response, "+CRSM:")
	if index < 0 {
		return nil, fmt.Errorf("unexpected response: %s", response)
	}
	parts := strings.SplitN(strings.TrimSpace(response[index+len("+CRSM:"):]), ",", 3)
	if len(parts) < 3 {
		return nil, fmt.Errorf("unexpected response: %s", response)
	}
	sw1, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return nil, fmt.Errorf("parse CRSM sw1: %w", err)
	}
	sw2, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return nil, fmt.Errorf("parse CRSM sw2: %w", err)
	}
	if sw1 != 144 || sw2 != 0 {
		return nil, fmt.Errorf("unexpected response status: %d,%d", sw1, sw2)
	}
	data := strings.Trim(strings.TrimSpace(parts[2]), "\"")
	if data == "" {
		return nil, nil
	}
	decoded, err := hex.DecodeString(data)
	if err != nil {
		return nil, fmt.Errorf("decode CRSM data: %w", err)
	}
	return decoded, nil
}
