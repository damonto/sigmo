package wwan

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
	"testing"

	"github.com/damonto/wwan-go/cdcwdm"
	"github.com/damonto/wwan-go/qcom"
	"github.com/damonto/wwan-go/qcom/qmi"
)

func TestIsTerminalError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil"},
		{name: "disconnected", err: cdcwdm.ErrDisconnected, want: true},
		{name: "QMI transport", err: &qmi.TransportError{Err: errors.New("invalid QMUX frame")}, want: true},
		{name: "EOF", err: io.EOF, want: true},
		{name: "truncated frame", err: io.ErrUnexpectedEOF, want: true},
		{name: "closed socket", err: net.ErrClosed, want: true},
		{name: "reset socket", err: syscall.ECONNRESET, want: true},
		{name: "broken pipe", err: syscall.EPIPE, want: true},
		{name: "deadline", err: context.DeadlineExceeded},
		{name: "canceled", err: context.Canceled},
		{name: "service error", err: qcom.QMIErrorNoNetworkFound},
		{name: "CID exhaustion requires registry policy", err: qcom.QMIErrorClientIDsExhausted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.err
			if err != nil {
				err = fmt.Errorf("read modem response: %w", err)
			}
			if got := IsTerminalError(err); got != tt.want {
				t.Errorf("IsTerminalError(%v) = %t, want %t", err, got, tt.want)
			}
		})
	}
}
