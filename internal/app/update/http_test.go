package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/damonto/sigmo/internal/app/buildinfo"
	"github.com/damonto/sigmo/internal/pkg/apierror"
	"github.com/damonto/sigmo/internal/pkg/settings"
)

func TestUpdateSnapshotHidesUpstreamDetails(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code string
	}{
		{
			name: "registered problem",
			err:  &apierror.Error{StatusCode: http.StatusServiceUnavailable, ErrorCode: "database_maintenance", Message: "private database details"},
			code: "database_maintenance",
		},
		{
			name: "invalid problem",
			err:  &apierror.Error{StatusCode: http.StatusBadGateway, Message: "private response", ParseErr: errors.New("invalid JSON")},
			code: "upstream_response_invalid",
		},
		{
			name: "download transport error",
			err:  &url.Error{Op: "Get", URL: "https://worker.example/v1/downloads/secret-ticket", Err: errors.New("private transport details")},
			code: "upstream_unavailable",
		},
		{
			name: "download timeout",
			err:  &url.Error{Op: "Get", URL: "https://worker.example/v1/downloads/secret-ticket", Err: context.DeadlineExceeded},
			code: "upstream_timeout",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := &fakeSource{err: fmt.Errorf("get release: %w", tt.err)}
			controller, err := NewController(ControllerConfig{
				Build:    buildinfo.Info{Edition: buildinfo.EditionPro, Channel: buildinfo.ChannelStable},
				Settings: settings.NewMemoryStore(settings.Default()),
				Source:   source,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := controller.Check(t.Context()); !errors.Is(err, tt.err) {
				t.Fatalf("Check() error = %v, want original cause", err)
			}
			snapshot := controller.Snapshot()
			title, known := apierror.Title(tt.code)
			if !known || snapshot.ErrorCode != tt.code || snapshot.Error != title {
				t.Fatalf("snapshot error = %q / %q, want %q / %q", snapshot.ErrorCode, snapshot.Error, tt.code, title)
			}
			body, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(body), "secret-ticket") || strings.Contains(string(body), "private") {
				t.Fatalf("snapshot exposes upstream details: %s", body)
			}
			source.err = nil
			if err := controller.Check(t.Context()); err != nil {
				t.Fatal(err)
			}
			if snapshot := controller.Snapshot(); snapshot.ErrorCode != "" || snapshot.Error != "" {
				t.Fatalf("successful check retained error: %q / %q", snapshot.ErrorCode, snapshot.Error)
			}
		})
	}
}
