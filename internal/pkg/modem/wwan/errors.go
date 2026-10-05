package wwan

import (
	"errors"
	"io"
	"net"
	"syscall"

	"github.com/damonto/wwan-go/cdcwdm"
	"github.com/damonto/wwan-go/qcom/qmi"
)

// IsTerminalError reports transport failures that require a new QMI or MBIM
// client. Request cancellation, timeouts, and service errors do not invalidate
// the transport.
func IsTerminalError(err error) bool {
	if _, ok := errors.AsType[*qmi.TransportError](err); ok {
		return true
	}
	// MBIM preserves socket read errors without a protocol-specific wrapper.
	return errors.Is(err, cdcwdm.ErrDisconnected) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, net.ErrClosed) || errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE)
}
