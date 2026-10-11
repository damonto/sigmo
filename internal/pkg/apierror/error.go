// Package apierror implements Sigmo's HTTP Problem Details contract. Callers
// decide whether an error warrants retrying or changing local credentials.
package apierror

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// MediaType is the only supported error representation. Success bodies keep
// their resource-specific media types.
const MediaType = "application/problem+json"

// MaxBodySize bounds an untrusted error body independently of success payloads.
const MaxBodySize = 64 * 1024

// TypePrefix is independent of the host serving an API or its deployment alias.
const TypePrefix = "https://sigmo-pro.952007.xyz/problems/"

// Problem is the wire representation shared by the Worker and local backend.
type Problem struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Detail    string `json:"detail"`
	ErrorCode string `json:"error_code"`
	RequestID string `json:"request_id"`
}

// Error retains HTTP metadata even when a proxy returns an invalid error body.
// ErrorCode is populated only from a registered, internally consistent problem.
type Error struct {
	StatusCode   int
	ProblemType  string
	ErrorCode    string
	Message      string
	RequestID    string
	RetryAfter   string
	RetryAt      time.Time
	Allow        string
	Authenticate string
	ParseErr     error
}

// Error describes the failure without exposing a raw response body.
func (e *Error) Error() string { return e.Message }

// Code preserves the application's error-code interface used by update jobs.
func (e *Error) Code() string { return e.ErrorCode }

// Unwrap exposes representation failures to diagnostics without losing status.
func (e *Error) Unwrap() error { return e.ParseErr }

// Read reads at most MaxBodySize+1 bytes and does not close resp.Body. The extra
// byte distinguishes a body exactly at the limit from a truncated representation.
func Read(resp *http.Response) *Error {
	remote := &Error{
		StatusCode:   resp.StatusCode,
		Message:      http.StatusText(resp.StatusCode),
		RequestID:    resp.Header.Get("X-Request-ID"),
		RetryAfter:   resp.Header.Get("Retry-After"),
		Allow:        resp.Header.Get("Allow"),
		Authenticate: resp.Header.Get("WWW-Authenticate"),
	}
	remote.RetryAt = ParseRetryAfter(remote.RetryAfter, time.Now())
	if remote.Message == "" {
		remote.Message = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || mediaType != MediaType {
		remote.ParseErr = errors.New("unsupported error media type")
		return remote
	}
	if resp.Body == nil {
		remote.ParseErr = errors.New("empty error response")
		return remote
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxBodySize+1))
	if err != nil {
		remote.ParseErr = fmt.Errorf("read problem response: %w", err)
		return remote
	}
	if len(data) > MaxBodySize {
		remote.ParseErr = errors.New("error response exceeds size limit")
		return remote
	}
	// A missing/null extension is optional; a present empty or conflicting code
	// must not gain the authority of the registered type.
	var problem struct {
		Type      string  `json:"type"`
		Title     string  `json:"title"`
		Status    int     `json:"status"`
		Detail    string  `json:"detail"`
		ErrorCode *string `json:"error_code"`
		RequestID string  `json:"request_id"`
	}
	if err := json.Unmarshal(data, &problem); err != nil {
		remote.ParseErr = errors.New("invalid problem JSON")
		return remote
	}
	remote.ProblemType = problem.Type
	code, known := CodeFromType(problem.Type)
	if !known || problem.Title == "" || problem.Status != resp.StatusCode ||
		(problem.ErrorCode != nil && *problem.ErrorCode != code) ||
		(remote.RequestID != "" && problem.RequestID != "" && remote.RequestID != problem.RequestID) {
		remote.ParseErr = errors.New("inconsistent or unknown problem response")
		return remote
	}
	remote.ErrorCode = code
	if problem.Detail != "" {
		remote.Message = problem.Detail
	}
	if remote.RequestID == "" {
		remote.RequestID = problem.RequestID
	}
	return remote
}

// ParseRetryAfter accepts delay-seconds and HTTP-date. Past dates impose no wait.
func ParseRetryAfter(value string, now time.Time) time.Time {
	value = strings.TrimSpace(value)
	if value != "" && strings.IndexFunc(value, func(r rune) bool { return r < '0' || r > '9' }) == -1 {
		if seconds, err := strconv.ParseInt(value, 10, 32); err == nil {
			return now.Add(time.Duration(seconds) * time.Second)
		}
		return time.Time{}
	}
	if retryAt, err := http.ParseTime(value); err == nil && retryAt.After(now) {
		return retryAt
	}
	return time.Time{}
}
