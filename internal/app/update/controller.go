package update

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/damonto/sigmo/internal/app/buildinfo"
	"github.com/damonto/sigmo/internal/pkg/settings"
)

const (
	StateIdle        = "idle"
	StateChecking    = "checking"
	StateDownloading = "downloading"
	StateVerifying   = "verifying"
	StateRestarting  = "restarting"
	StateFailed      = "failed"
)

var (
	ErrBusy                  = errors.New("update operation is already running")
	ErrNoUpdate              = errors.New("no update is available")
	ErrSelfUpdateUnsupported = errors.New("self-update is unavailable for this distribution")
)

type Snapshot struct {
	Settings            settings.Updates `json:"settings"`
	Current             buildinfo.Info   `json:"current"`
	Latest              *Manifest        `json:"latest,omitempty"`
	License             *Licensee        `json:"license,omitempty"`
	State               string           `json:"state"`
	CheckedAt           *time.Time       `json:"checkedAt,omitempty"`
	UpdateAvailable     bool             `json:"updateAvailable"`
	SelfUpdateSupported bool             `json:"selfUpdateSupported"`
	UnsupportedReason   string           `json:"unsupportedReason,omitempty"`
	ErrorCode           string           `json:"errorCode,omitempty"`
	Error               string           `json:"error,omitempty"`
}

type ControllerConfig struct {
	Build      buildinfo.Info
	Settings   *settings.Store
	Source     Source
	License    LicenseProvider
	Executable string
	Restart    func()
}

type Controller struct {
	build      buildinfo.Info
	settings   *settings.Store
	source     Source
	license    LicenseProvider
	executable string
	restart    func()

	mu            sync.RWMutex
	state         string
	checkedAt     *time.Time
	latest        *Release
	available     bool
	lastErrorCode string
	lastError     string
	stopping      bool
	installCancel context.CancelFunc
	operations    sync.WaitGroup
}

func NewController(cfg ControllerConfig) (*Controller, error) {
	if cfg.Settings == nil {
		return nil, errors.New("update settings store is required")
	}
	if cfg.Source == nil {
		return nil, errors.New("update source is required")
	}
	if cfg.Executable == "" {
		executable, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("resolve executable: %w", err)
		}
		cfg.Executable = executable
	}
	return &Controller{
		build:      cfg.Build,
		settings:   cfg.Settings,
		source:     cfg.Source,
		license:    cfg.License,
		executable: cfg.Executable,
		restart:    cfg.Restart,
		state:      StateIdle,
	}, nil
}

func (c *Controller) Snapshot() Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var latest *Manifest
	if c.latest != nil {
		copy := c.latest.Manifest
		copy.Artifacts = slices.Clone(copy.Artifacts)
		latest = &copy
	}
	var checkedAt *time.Time
	if c.checkedAt != nil {
		copy := *c.checkedAt
		checkedAt = &copy
	}
	supported := c.selfUpdateSupported()
	reason := ""
	if !supported {
		switch c.build.Distribution {
		case buildinfo.DistributionContainer:
			reason = "container"
		case buildinfo.DistributionDeveloper:
			reason = "developer_build"
		default:
			reason = "release_key_missing"
		}
	}
	var licensee *Licensee
	if c.license != nil {
		licensee = c.license.Licensee()
	}
	currentSettings := c.effectiveSettings()
	return Snapshot{
		Settings:            currentSettings,
		Current:             c.build,
		Latest:              latest,
		License:             licensee,
		State:               c.state,
		CheckedAt:           checkedAt,
		UpdateAvailable:     c.available,
		SelfUpdateSupported: supported,
		UnsupportedReason:   reason,
		ErrorCode:           c.lastErrorCode,
		Error:               c.lastError,
	}
}

func (c *Controller) MarkHealthy() error {
	return MarkHealthy(c.executable)
}

func (c *Controller) UpdateSettings(ctx context.Context, next settings.Updates) (Snapshot, error) {
	if next.Channel != settings.UpdateChannelStable && next.Channel != settings.UpdateChannelDev {
		return Snapshot{}, errors.New("update channel must be stable or dev")
	}
	if c.build.Edition == buildinfo.EditionCommunity && next.Channel != settings.UpdateChannelStable {
		return Snapshot{}, errors.New("community edition only supports stable updates")
	}
	if !c.selfUpdateSupported() {
		next.Automatic = false
	}
	if !c.begin(StateChecking) {
		return Snapshot{}, ErrBusy
	}
	previous := c.effectiveSettings()
	_, err := c.settings.Update(ctx, func(current *settings.Settings) error {
		current.Updates = next
		return nil
	})
	if err != nil {
		err = fmt.Errorf("save update settings: %w", err)
		c.fail(err)
		c.endOperation()
		return c.Snapshot(), err
	}
	if previous.Channel != next.Channel {
		c.mu.Lock()
		c.checkedAt = nil
		c.latest = nil
		c.available = false
		c.lastErrorCode = ""
		c.lastError = ""
		c.mu.Unlock()
	}
	c.endOperation()
	return c.Snapshot(), nil
}

func (c *Controller) Check(ctx context.Context) error {
	if !c.begin(StateChecking) {
		return ErrBusy
	}
	defer c.endOperation()
	return c.checkLatest(ctx)
}

func (c *Controller) checkLatest(ctx context.Context) error {
	currentSettings := c.effectiveSettings()
	release, err := c.source.Latest(ctx, currentSettings.Channel, c.build.Target)
	now := time.Now().UTC()
	c.mu.Lock()
	c.checkedAt = &now
	if err != nil {
		c.state = StateFailed
		c.latest = nil
		c.available = false
		c.lastErrorCode = ErrorCode(err)
		c.lastError = err.Error()
		c.mu.Unlock()
		return err
	}
	c.latest = &release
	c.available = updateAvailable(c.build.Channel, c.build.Version, c.build.Commit, release.Manifest)
	c.lastErrorCode = ""
	c.lastError = ""
	c.mu.Unlock()
	return nil
}

func (c *Controller) StartInstall() error {
	c.mu.Lock()
	if !c.selfUpdateSupported() {
		c.mu.Unlock()
		return ErrSelfUpdateUnsupported
	}
	if !c.available {
		c.mu.Unlock()
		return ErrNoUpdate
	}
	if c.stopping || operationRunning(c.state) {
		c.mu.Unlock()
		return ErrBusy
	}
	installCtx, cancel := context.WithCancel(context.Background())
	c.installCancel = cancel
	c.state = StateDownloading
	c.lastError = ""
	c.operations.Add(1)
	c.mu.Unlock()
	go func() {
		defer c.operations.Done()
		defer cancel()
		c.install(installCtx)
		c.mu.Lock()
		c.installCancel = nil
		c.mu.Unlock()
	}()
	return nil
}

func (c *Controller) Run(ctx context.Context) error {
	defer func() {
		c.mu.Lock()
		c.stopping = true
		cancel := c.installCancel
		c.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		c.operations.Wait()
	}()

	initial := time.NewTimer(2 * time.Minute)
	defer initial.Stop()
	select {
	case <-ctx.Done():
		return nil
	case <-initial.C:
	}
	for {
		if c.effectiveSettings().Automatic {
			if err := c.Check(ctx); err != nil && !errors.Is(err, ErrBusy) {
				slog.Warn("check for update", "error", err)
			} else if c.Snapshot().UpdateAvailable {
				switch err := c.StartInstall(); {
				case err == nil, errors.Is(err, ErrBusy), errors.Is(err, ErrNoUpdate):
					// A manual request or settings change may win the race after
					// the check. Both outcomes are expected and need no retry here.
				default:
					slog.Warn("start automatic update", "error", err)
				}
			}
		}
		jitter := time.Duration(rand.Int64N(int64(30 * time.Minute)))
		timer := time.NewTimer(6*time.Hour + jitter)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func (c *Controller) install(ctx context.Context) {
	defer c.endOperation()
	currentSettings := c.effectiveSettings()
	release, err := c.source.Latest(ctx, currentSettings.Channel, c.build.Target)
	if err != nil {
		c.fail(err)
		return
	}
	if !release.Verified {
		c.fail(ErrInvalidSignature)
		return
	}
	if !updateAvailable(c.build.Channel, c.build.Version, c.build.Commit, release.Manifest) {
		c.fail(ErrNoUpdate)
		return
	}
	body, err := c.source.Download(ctx, release)
	if err != nil {
		c.fail(err)
		return
	}
	defer func() {
		if err := body.Close(); err != nil {
			slog.Debug("close update download", "error", err)
		}
	}()
	c.setState(StateVerifying)
	if err := Apply(ctx, c.executable, release, body); err != nil {
		c.fail(err)
		return
	}
	c.mu.Lock()
	c.latest = &release
	c.available = false
	c.state = StateRestarting
	c.lastError = ""
	c.mu.Unlock()
	if c.restart != nil {
		c.restart()
	}
}

func (c *Controller) begin(state string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopping || operationRunning(c.state) {
		return false
	}
	c.state = state
	c.lastErrorCode = ""
	c.lastError = ""
	return true
}

func (c *Controller) selfUpdateSupported() bool {
	return c.build.SelfUpdateSupported() && c.build.ReleasePublicKey != ""
}

func (c *Controller) effectiveSettings() settings.Updates {
	current := c.settings.UpdateSettings()
	if c.build.Edition == buildinfo.EditionCommunity {
		current.Channel = settings.UpdateChannelStable
	}
	if !c.selfUpdateSupported() {
		current.Automatic = false
	}
	return current
}

func operationRunning(state string) bool {
	return state == StateChecking || state == StateDownloading || state == StateVerifying || state == StateRestarting
}

func (c *Controller) endOperation() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state != StateFailed && c.state != StateRestarting {
		c.state = StateIdle
	}
}

func (c *Controller) setState(state string) {
	c.mu.Lock()
	c.state = state
	c.mu.Unlock()
}

func (c *Controller) fail(err error) {
	c.mu.Lock()
	c.state = StateFailed
	c.lastErrorCode = ErrorCode(err)
	c.lastError = err.Error()
	c.mu.Unlock()
}
