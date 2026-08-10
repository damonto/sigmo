package update

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/buildinfo"
	appupdate "github.com/damonto/sigmo/internal/app/update"
	"github.com/damonto/sigmo/internal/pkg/settings"
)

type source struct {
	release appupdate.Release
	started chan struct{}
	unblock chan struct{}
}

func (s *source) Latest(ctx context.Context, _, _ string) (appupdate.Release, error) {
	if s.started != nil {
		select {
		case <-s.started:
		default:
			close(s.started)
		}
	}
	if s.unblock != nil {
		select {
		case <-ctx.Done():
			return appupdate.Release{}, ctx.Err()
		case <-s.unblock:
		}
	}
	return s.release, nil
}

func (*source) Download(context.Context, appupdate.Release) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("binary")), nil
}

func TestCreateCheckReturnsConflictWhileBusy(t *testing.T) {
	releaseSource := &source{started: make(chan struct{}), unblock: make(chan struct{})}
	controller := newController(t, buildinfo.Info{
		Edition:      buildinfo.EditionCommunity,
		Distribution: buildinfo.DistributionDeveloper,
	}, releaseSource)
	done := make(chan error, 1)
	go func() { done <- controller.Check(t.Context()) }()
	select {
	case <-releaseSource.started:
	case <-time.After(time.Second):
		t.Fatal("check did not start")
	}

	recorder := httptest.NewRecorder()
	ctx := echo.New().NewContext(httptest.NewRequest(http.MethodPost, "/api/v1/update-checks", nil), recorder)
	if err := New(controller).CreateCheck(ctx); err != nil {
		t.Fatalf("CreateCheck() error = %v", err)
	}
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "update_busy") {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
	close(releaseSource.unblock)
	if err := <-done; err != nil {
		t.Fatalf("background Check() error = %v", err)
	}
}

func TestCreateInstallationRejectsUnsupportedDistribution(t *testing.T) {
	releaseSource := &source{release: appupdate.Release{Manifest: appupdate.Manifest{
		Channel: settings.UpdateChannelStable,
		Version: "v2.0.0",
	}}}
	controller := newController(t, buildinfo.Info{
		Edition:          buildinfo.EditionCommunity,
		Channel:          buildinfo.ChannelStable,
		Version:          "v1.0.0",
		Distribution:     buildinfo.DistributionContainer,
		ReleasePublicKey: "release-key",
	}, releaseSource)
	if err := controller.Check(t.Context()); err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	ctx := echo.New().NewContext(httptest.NewRequest(http.MethodPost, "/api/v1/update-installations", nil), recorder)
	if err := New(controller).CreateInstallation(ctx); err != nil {
		t.Fatalf("CreateInstallation() error = %v", err)
	}
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "self_update_unsupported") {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func newController(t *testing.T, build buildinfo.Info, releaseSource appupdate.Source) *appupdate.Controller {
	t.Helper()
	controller, err := appupdate.NewController(appupdate.ControllerConfig{
		Build:      build,
		Settings:   settings.NewMemoryStore(settings.Default()),
		Source:     releaseSource,
		Executable: "/tmp/sigmo-handler-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	return controller
}
