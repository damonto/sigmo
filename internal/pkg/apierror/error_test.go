package apierror

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSharedContract(t *testing.T) {
	data, err := os.ReadFile("testdata/problems.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name    string            `json:"name"`
		Status  int               `json:"status"`
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body"`
		Repeat  int               `json:"repeat"`
		Code    string            `json:"code"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			body := fixture.Body
			if fixture.Repeat > 0 {
				body = strings.Repeat(body, fixture.Repeat)
			}
			headers := make(http.Header)
			for key, value := range fixture.Headers {
				headers.Set(key, value)
			}
			remote := Read(&http.Response{StatusCode: fixture.Status, Header: headers, Body: io.NopCloser(strings.NewReader(body))})
			if remote.StatusCode != fixture.Status || remote.ErrorCode != fixture.Code {
				t.Fatalf("error = %+v, want status=%d code=%q", remote, fixture.Status, fixture.Code)
			}
			if (remote.ParseErr != nil) != (fixture.Code == "") {
				t.Fatalf("parse error = %v", remote.ParseErr)
			}
			if remote.RequestID != headers.Get("X-Request-ID") || remote.Allow != headers.Get("Allow") ||
				remote.Authenticate != headers.Get("WWW-Authenticate") || remote.RetryAfter != headers.Get("Retry-After") {
				t.Fatalf("lost response metadata: %+v", remote)
			}
			if remote.ParseErr != nil && !errors.Is(remote, remote.ParseErr) {
				t.Fatal("lost parse error chain")
			}
		})
	}
}

type countingBody struct {
	remaining, read int
	err             error
}

func (b *countingBody) Read(p []byte) (int, error) {
	if b.err != nil {
		return 0, b.err
	}
	if b.remaining == 0 {
		return 0, io.EOF
	}
	n := min(len(p), b.remaining)
	for i := range n {
		p[i] = 'x'
	}
	b.read += n
	b.remaining -= n
	return n, nil
}
func (b *countingBody) Close() error { return nil }

func TestReadBoundsBodyAndPreservesReadFailure(t *testing.T) {
	for _, test := range []struct {
		name     string
		body     *countingBody
		wantRead int
	}{
		{"oversize", &countingBody{remaining: MaxBodySize * 4}, MaxBodySize + 1},
		{"read error", &countingBody{err: io.ErrUnexpectedEOF}, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			headers := http.Header{"Content-Type": {MediaType}, "Retry-After": {"90"}, "X-Request-Id": {"upstream"}}
			remote := Read(&http.Response{StatusCode: 503, Header: headers, Body: test.body})
			if remote.StatusCode != 503 || remote.ErrorCode != "" || remote.ParseErr == nil || remote.RetryAt.IsZero() || remote.RequestID != "upstream" {
				t.Fatalf("error = %+v", remote)
			}
			if test.body.read != test.wantRead {
				t.Fatalf("read %d bytes, want %d", test.body.read, test.wantRead)
			}
		})
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		value string
		want  time.Time
	}{
		{"90", now.Add(90 * time.Second)}, {" 90 ", now.Add(90 * time.Second)}, {"0", now},
		{now.Add(time.Minute).Format(http.TimeFormat), now.Add(time.Minute)},
		{now.Add(-time.Minute).Format(http.TimeFormat), time.Time{}},
		{"", time.Time{}}, {"-1", time.Time{}}, {"+1", time.Time{}}, {"1.5", time.Time{}},
		{"2147483648", time.Time{}}, {"2026-10-10", time.Time{}}, {"later", time.Time{}},
	} {
		t.Run(test.value, func(t *testing.T) {
			if got := ParseRetryAfter(test.value, now); !got.Equal(test.want) {
				t.Fatalf("deadline = %v, want %v", got, test.want)
			}
		})
	}
}
