package modem

import (
	"context"
	"errors"
	"os"
	"time"

	wwanmodem "github.com/damonto/wwan-go/modem"
)

func isTransientRestartError(err error) bool {
	return errors.Is(err, wwanmodem.ErrClosed) || errors.Is(err, os.ErrNotExist) ||
		errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func IsTransientRestartError(err error) bool { return isTransientRestartError(err) }

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
