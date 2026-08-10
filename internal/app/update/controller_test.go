package update

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/damonto/sigmo/internal/app/buildinfo"
	"github.com/damonto/sigmo/internal/pkg/settings"
)

type fakeSource struct {
	mu      sync.Mutex
	release Release
	err     error
	started chan struct{}
	unblock chan struct{}
	calls   int
	channel string
	target  string
}

func (s *fakeSource) Latest(ctx context.Context, channel, target string) (Release, error) {
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
			return Release{}, ctx.Err()
		case <-s.unblock:
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.channel = channel
	s.target = target
	return s.release, s.err
}

func (s *fakeSource) Download(context.Context, Release) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("binary")), nil
}

func TestControllerCommunitySettingsAndAvailability(t *testing.T) {
	store := settings.NewMemoryStore(settings.Default())
	source := &fakeSource{release: Release{Manifest: Manifest{Channel: "stable", Version: "v1.2.0"}}}
	controller, err := NewController(ControllerConfig{
		Build:    buildinfo.Info{Edition: "community", Channel: "stable", Version: "v1.1.0", Distribution: "standalone", ReleasePublicKey: "key"},
		Settings: store, Source: source, Executable: "/tmp/sigmo-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.UpdateSettings(t.Context(), settings.Updates{Channel: "dev"}); err == nil {
		t.Fatal("UpdateSettings(dev) error = nil")
	}
	if err := controller.Check(t.Context()); err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !controller.Snapshot().UpdateAvailable {
		t.Fatal("UpdateAvailable = false")
	}
}

func TestControllerForcesStoredCommunityChannelToStable(t *testing.T) {
	current := settings.Default()
	current.Updates = settings.Updates{Automatic: true, Channel: settings.UpdateChannelDev}
	source := &fakeSource{release: Release{Manifest: Manifest{
		Channel: settings.UpdateChannelStable,
		Version: "v1.2.0",
	}}}
	controller, err := NewController(ControllerConfig{
		Build: buildinfo.Info{
			Edition:          buildinfo.EditionCommunity,
			Channel:          buildinfo.ChannelStable,
			Version:          "v1.1.0",
			Target:           "linux-amd64",
			Distribution:     buildinfo.DistributionStandalone,
			ReleasePublicKey: "key",
		},
		Settings:   settings.NewMemoryStore(current),
		Source:     source,
		Executable: "/tmp/sigmo-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := controller.Snapshot().Settings.Channel; got != settings.UpdateChannelStable {
		t.Fatalf("Snapshot().Settings.Channel = %q, want stable", got)
	}
	if err := controller.Check(t.Context()); err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.channel != settings.UpdateChannelStable || source.target != "linux-amd64" {
		t.Fatalf("Latest() channel = %q, target = %q", source.channel, source.target)
	}
}

func TestControllerDefaultsUpdateChannelToCurrentBuild(t *testing.T) {
	source := &fakeSource{release: Release{Manifest: Manifest{
		Channel: settings.UpdateChannelDev,
		Version: "dev-22222222",
		Commit:  "2222222222222222222222222222222222222222",
	}}}
	controller, err := NewController(ControllerConfig{
		Build: buildinfo.Info{
			Edition:          buildinfo.EditionPro,
			Channel:          buildinfo.ChannelStable,
			Version:          "dev-11111111",
			Commit:           "1111111111111111111111111111111111111111",
			Target:           "linux-amd64",
			Distribution:     buildinfo.DistributionStandalone,
			ReleasePublicKey: "key",
		},
		Settings:   settings.NewMemoryStore(&settings.Settings{}),
		Source:     source,
		Executable: "/tmp/sigmo-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := controller.Snapshot().Settings.Channel; got != settings.UpdateChannelDev {
		t.Fatalf("Snapshot().Settings.Channel = %q, want dev", got)
	}
	if err := controller.Check(t.Context()); err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if source.channel != settings.UpdateChannelDev {
		t.Fatalf("Latest() channel = %q, want dev", source.channel)
	}

	snapshot, err := controller.UpdateSettings(t.Context(), settings.Updates{Channel: settings.UpdateChannelStable})
	if err != nil {
		t.Fatalf("UpdateSettings() error = %v", err)
	}
	if snapshot.Settings.Channel != settings.UpdateChannelStable {
		t.Fatalf("Snapshot().Settings.Channel = %q, want explicit stable", snapshot.Settings.Channel)
	}
}

func TestControllerSerializesChecks(t *testing.T) {
	source := &fakeSource{started: make(chan struct{}), unblock: make(chan struct{})}
	controller, err := NewController(ControllerConfig{
		Build:    buildinfo.Info{Edition: "community", Distribution: "developer"},
		Settings: settings.NewMemoryStore(settings.Default()), Source: source, Executable: "/tmp/sigmo-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- controller.Check(t.Context()) }()
	select {
	case <-source.started:
	case <-time.After(time.Second):
		t.Fatal("first check did not start")
	}
	if err := controller.Check(t.Context()); !errors.Is(err, ErrBusy) {
		t.Fatalf("second Check() error = %v", err)
	}
	close(source.unblock)
	if err := <-done; err != nil {
		t.Fatalf("first Check() error = %v", err)
	}
}

func TestControllerDisablesAutomaticInstallForContainer(t *testing.T) {
	current := settings.Default()
	current.Updates.Automatic = true
	controller, err := NewController(ControllerConfig{
		Build:    buildinfo.Info{Edition: "community", Distribution: "container"},
		Settings: settings.NewMemoryStore(current), Source: &fakeSource{}, Executable: "/tmp/sigmo-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := controller.Snapshot()
	if snapshot.Settings.Automatic || snapshot.SelfUpdateSupported || snapshot.UnsupportedReason != "container" {
		t.Fatalf("Snapshot() = %+v", snapshot)
	}
	if err := controller.StartInstall(); !errors.Is(err, ErrSelfUpdateUnsupported) {
		t.Fatalf("StartInstall() error = %v", err)
	}
}

func TestControllerSerializesSettingsChangesWithChecks(t *testing.T) {
	source := &fakeSource{started: make(chan struct{}), unblock: make(chan struct{})}
	controller, err := NewController(ControllerConfig{
		Build: buildinfo.Info{
			Edition:          buildinfo.EditionPro,
			Distribution:     buildinfo.DistributionStandalone,
			ReleasePublicKey: "release-key",
		},
		Settings:   settings.NewMemoryStore(settings.Default()),
		Source:     source,
		Executable: "/tmp/sigmo-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- controller.Check(t.Context()) }()
	select {
	case <-source.started:
	case <-time.After(time.Second):
		t.Fatal("check did not start")
	}
	if _, err := controller.UpdateSettings(t.Context(), settings.Updates{Channel: settings.UpdateChannelDev}); !errors.Is(err, ErrBusy) {
		t.Fatalf("UpdateSettings() error = %v", err)
	}
	close(source.unblock)
	if err := <-done; err != nil {
		t.Fatalf("Check() error = %v", err)
	}
}

func TestControllerPersistsAutomaticOffWithoutReleaseKey(t *testing.T) {
	controller, err := NewController(ControllerConfig{
		Build: buildinfo.Info{
			Edition:      buildinfo.EditionCommunity,
			Distribution: buildinfo.DistributionStandalone,
		},
		Settings: settings.NewMemoryStore(settings.Default()),
		Source: &fakeSource{release: Release{Manifest: Manifest{
			Channel: settings.UpdateChannelStable,
			Version: "v1.0.0",
		}}},
		Executable: "/tmp/sigmo-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := controller.UpdateSettings(t.Context(), settings.Updates{
		Automatic: true,
		Channel:   settings.UpdateChannelStable,
	})
	if err != nil {
		t.Fatalf("UpdateSettings() error = %v", err)
	}
	stored, _ := controller.settings.UpdateSettings()
	if snapshot.Settings.Automatic || stored.Automatic {
		t.Fatalf("automatic setting was not forced off: %+v", snapshot.Settings)
	}
}

func TestControllerSavesChannelWithoutRemoteCheck(t *testing.T) {
	source := &fakeSource{release: Release{Manifest: Manifest{
		Channel: settings.UpdateChannelStable,
		Version: "v2.0.0",
	}}}
	controller, err := NewController(ControllerConfig{
		Build: buildinfo.Info{
			Edition:          buildinfo.EditionPro,
			Channel:          buildinfo.ChannelStable,
			Version:          "v1.0.0",
			Distribution:     buildinfo.DistributionStandalone,
			ReleasePublicKey: "release-key",
		},
		Settings:   settings.NewMemoryStore(settings.Default()),
		Source:     source,
		Executable: "/tmp/sigmo-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.Check(t.Context()); err != nil {
		t.Fatal(err)
	}

	snapshot, err := controller.UpdateSettings(t.Context(), settings.Updates{
		Channel: settings.UpdateChannelDev,
	})
	if err != nil {
		t.Fatalf("UpdateSettings() error = %v", err)
	}
	if source.calls != 1 {
		t.Fatalf("Latest() calls = %d, want 1", source.calls)
	}
	if snapshot.Settings.Channel != settings.UpdateChannelDev || snapshot.Latest != nil || snapshot.UpdateAvailable {
		t.Fatalf("Snapshot() = %+v", snapshot)
	}
}

func TestControllerClearsStaleReleaseAfterCheckFailure(t *testing.T) {
	source := &fakeSource{release: Release{Manifest: Manifest{
		Channel: settings.UpdateChannelStable,
		Version: "v2.0.0",
	}}}
	controller, err := NewController(ControllerConfig{
		Build: buildinfo.Info{
			Edition:          buildinfo.EditionCommunity,
			Channel:          buildinfo.ChannelStable,
			Version:          "v1.0.0",
			Distribution:     buildinfo.DistributionStandalone,
			ReleasePublicKey: "release-key",
		},
		Settings:   settings.NewMemoryStore(settings.Default()),
		Source:     source,
		Executable: "/tmp/sigmo-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	source.mu.Lock()
	source.err = errors.New("upstream unavailable")
	source.mu.Unlock()
	if err := controller.Check(t.Context()); err == nil {
		t.Fatal("Check() error = nil")
	}
	snapshot := controller.Snapshot()
	if snapshot.Latest != nil || snapshot.UpdateAvailable || snapshot.Error == "" {
		t.Fatalf("Snapshot() = %+v", snapshot)
	}
}

func TestControllerRejectsConcurrentInstallations(t *testing.T) {
	source := &fakeSource{release: Release{
		Verified: true,
		Manifest: Manifest{Channel: buildinfo.ChannelStable, Version: "v2.0.0"},
		Artifact: testRawArtifact([]byte("binary")),
	}}
	controller, err := NewController(ControllerConfig{
		Build: buildinfo.Info{
			Edition:          buildinfo.EditionCommunity,
			Channel:          buildinfo.ChannelStable,
			Version:          "v1.0.0",
			Distribution:     buildinfo.DistributionStandalone,
			ReleasePublicKey: "release-key",
		},
		Settings:   settings.NewMemoryStore(settings.Default()),
		Source:     source,
		Executable: "/tmp/sigmo-test-missing",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.Check(t.Context()); err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	source.started = make(chan struct{})
	source.unblock = make(chan struct{})
	if err := controller.StartInstall(); err != nil {
		t.Fatalf("first StartInstall() error = %v", err)
	}
	select {
	case <-source.started:
	case <-time.After(time.Second):
		t.Fatal("installation did not start")
	}
	if err := controller.StartInstall(); !errors.Is(err, ErrBusy) {
		t.Fatalf("second StartInstall() error = %v", err)
	}
	close(source.unblock)
	deadline := time.Now().Add(time.Second)
	for operationRunning(controller.Snapshot().State) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if operationRunning(controller.Snapshot().State) {
		t.Fatal("installation did not stop")
	}
}
