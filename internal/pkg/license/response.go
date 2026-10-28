package license

import (
	"errors"
	"github.com/damonto/sigmo/internal/pkg/apierror"
	"io"
	"mime"
	"net/http"
	"time"
)

func readServiceJSON(resp *http.Response) ([]byte, error) {
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return nil, invalidServiceResponse(resp, errors.New("unexpected JSON media type"))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	if err != nil {
		return nil, invalidServiceResponse(resp, err)
	}
	if len(data) > maxResponseSize {
		return nil, invalidServiceResponse(resp, errors.New("response exceeds size limit"))
	}
	return data, nil
}

func invalidServiceResponse(resp *http.Response, cause error) *serviceError {
	return &serviceError{
		StatusCode:   resp.StatusCode,
		Message:      "authorization service returned an invalid response",
		RequestID:    resp.Header.Get("X-Request-ID"),
		Allow:        resp.Header.Get("Allow"),
		Authenticate: resp.Header.Get("WWW-Authenticate"),
		RetryAfter:   resp.Header.Get("Retry-After"),
		RetryAt:      apierror.ParseRetryAfter(resp.Header.Get("Retry-After"), time.Now()),
		ParseErr:     cause,
	}
}
