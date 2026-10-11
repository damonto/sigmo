package license

import (
	"crypto/tls"
	"errors"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"time"
)

func authorizationRetryDelay(err error, backoff time.Duration) (time.Duration, bool) {
	if !retryableAuthorizationError(err) {
		return 0, false
	}
	// Retain half the delay so jitter cannot turn an outage into a busy loop.
	delay := backoff/2 + time.Duration(rand.Int64N(int64(backoff-backoff/2)))
	var remote *serviceError
	if errors.As(err, &remote) {
		delay = max(delay, time.Until(remote.RetryAt))
	}
	return delay, true
}

func retryableAuthorizationError(err error) bool {
	var remote *serviceError
	if errors.As(err, &remote) {
		if explicitAuthorizationError(remote) {
			return false
		}
		switch {
		case remote.ErrorCode == "license_challenge_invalid" && remote.StatusCode == http.StatusForbidden,
			remote.ErrorCode == "license_challenge_expired" && remote.StatusCode == http.StatusGone:
			// A new transport challenge can retry the same pending rotation.
			return true
		}
		switch remote.StatusCode {
		case http.StatusRequestTimeout, http.StatusTooManyRequests,
			http.StatusInternalServerError, http.StatusBadGateway,
			http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true
		default:
			return false
		}
	}
	if _, ok := errors.AsType[*tls.CertificateVerificationError](err); ok {
		return false
	}
	// url.Error implements net.Error even when its cause is a permanent error.
	if requestErr, ok := errors.AsType[*url.Error](err); ok {
		err = requestErr.Err
	}
	var networkErr net.Error
	return errors.As(err, &networkErr) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}
