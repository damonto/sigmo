package appinfo

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/buildinfo"
)

func TestHandlerGet(t *testing.T) {
	tests := []struct {
		name  string
		build buildinfo.Info
		want  string
	}{
		{
			name: "release metadata",
			build: buildinfo.Info{
				Version:      "v1.2.3",
				Commit:       "0123456789abcdef",
				Channel:      buildinfo.ChannelStable,
				Edition:      buildinfo.EditionCommunity,
				Target:       "linux-amd64",
				Distribution: buildinfo.DistributionStandalone,
			},
			want: `{"version":"v1.2.3","commit":"0123456789abcdef","channel":"stable","edition":"community","target":"linux-amd64","distribution":"standalone"}` + "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/app", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			if err := New(tt.build).Get(c); err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			if rec.Code != http.StatusOK {
				t.Fatalf("Get() status = %d, want %d", rec.Code, http.StatusOK)
			}
			if rec.Body.String() != tt.want {
				t.Fatalf("Get() body = %q, want %q", rec.Body.String(), tt.want)
			}
		})
	}
}
