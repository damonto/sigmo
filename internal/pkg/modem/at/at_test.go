package at

import (
	"bufio"
	"errors"
	"strings"
	"testing"
)

func TestReadResponse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		command string
		want    string
		wantErr bool
	}{
		{
			name:    "skip echo and return body before ok",
			command: "AT+CSIM=?",
			input:   "\r\nAT+CSIM=?\r\n+CSIM: (10-512)\r\n\r\nOK\r\n",
			want:    "+CSIM: (10-512)",
		},
		{
			name:    "ok inside payload is not terminal",
			command: "AT+TEST",
			input:   "+TEST: LOOKUP\r\nOK\r\n",
			want:    "+TEST: LOOKUP",
		},
		{
			name:    "plain error",
			command: "AT+TEST",
			input:   "\r\nERROR\r\n",
			wantErr: true,
		},
		{
			name:    "cme error",
			command: "AT+TEST",
			input:   "\r\n+CME ERROR: 10\r\n",
			wantErr: true,
		},
		{
			name:    "eof before terminal response",
			command: "AT+TEST",
			input:   "\r\n+TEST: partial\r\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := readResponse(bufio.NewReader(strings.NewReader(tt.input)), tt.command)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("readResponse() error is nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("readResponse() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("readResponse() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTerminalError(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		{name: "ok", line: "OK"},
		{name: "plain error", line: "ERROR", want: true},
		{name: "cme error", line: "+CME ERROR: 10", want: true},
		{name: "cms error", line: "+CMS ERROR: 500", want: true},
		{name: "payload containing err", line: "+TEST: TERRAIN"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := terminalError(tt.line); got != tt.want {
				t.Fatalf("terminalError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReadResponseReturnsReaderError(t *testing.T) {
	_, err := readResponse(bufio.NewReader(errReader{}), "AT+TEST")
	if !errors.Is(err, errRead) {
		t.Fatalf("readResponse() error = %v, want %v", err, errRead)
	}
}

var errRead = errors.New("read")

type errReader struct{}

func (errReader) Read([]byte) (int, error) {
	return 0, errRead
}
