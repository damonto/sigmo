//go:build ims

package ims

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	imsgo "github.com/damonto/ims-go"
	imsvoice "github.com/damonto/ims-go/ims/voice"
	"github.com/damonto/ims-go/lte"
	pinternet "github.com/damonto/sigmo/internal/pkg/internet"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	wwan "github.com/damonto/sigmo/internal/pkg/modem/wwan"
	"github.com/damonto/sigmo/pro/websheet"
	"github.com/godbus/dbus/v5"
)

func TestRetryDelays(t *testing.T) {
	tests := []struct {
		name string
		want []time.Duration
	}{
		{
			name: "Wi-Fi Calling connect backoff",
			want: []time.Duration{
				30 * time.Second,
				60 * time.Second,
				120 * time.Second,
				240 * time.Second,
				300 * time.Second,
				600 * time.Second,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !slices.Equal(retryDelays, tt.want) {
				t.Fatalf("retryDelays = %v, want %v", retryDelays, tt.want)
			}
		})
	}
}

func TestVoLTEDelays(t *testing.T) {
	tests := []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{name: "airplane mode reset", got: voLTEResetDelay, want: time.Second},
		{name: "packet service poll", got: packetServicePollInterval, want: time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("delay = %v, want %v", tt.got, tt.want)
			}
		})
	}
}

func TestTerminalInfo(t *testing.T) {
	tests := []struct {
		name string
		imei string
		want imsgo.TerminalInfo
	}{
		{
			name: "uses real device imei and transfer device shape",
			imei: "123456789012345",
			want: imsgo.TerminalInfo{
				ID:              "123456789012345",
				Vendor:          "Google",
				Model:           "Pixel 8 Pro",
				SoftwareVersion: "15/AP3A.240905.015",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := terminalInfo(tt.imei); got != tt.want {
				t.Fatalf("terminalInfo() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestModemClientConfigForIMEI(t *testing.T) {
	tests := []struct {
		name         string
		imei         string
		access       Access
		profileIndex uint8
	}{
		{name: "uses IMEI for Wi-Fi Calling", imei: "123456789012345", access: AccessWiFiCalling},
		{name: "uses selected IMS profile for VoLTE", imei: "123456789012345", access: AccessVoLTE, profileIndex: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			defer slog.SetDefault(previous)

			cfg := modemClientConfigForIMEI(tt.imei, tt.access, tt.profileIndex)
			if cfg.Logger == nil {
				t.Fatal("Logger = nil, want configured logger")
			}
			if cfg.Terminal.ID != tt.imei {
				t.Fatalf("Terminal.ID = %q, want %q", cfg.Terminal.ID, tt.imei)
			}
			if tt.access == AccessWiFiCalling && cfg.Access.VoWiFi == nil {
				t.Fatal("Access.VoWiFi = nil, want Wi-Fi Calling access")
			}
			if tt.access == AccessVoLTE {
				if cfg.Access.VoLTE == nil {
					t.Fatal("Access.VoLTE = nil, want VoLTE access")
				}
				if cfg.Access.VoLTE.APN != lte.DefaultAPN || cfg.Access.VoLTE.ProfileIndex != tt.profileIndex {
					t.Fatalf("Access.VoLTE APN/profile = %q/%d, want %q/%d", cfg.Access.VoLTE.APN, cfg.Access.VoLTE.ProfileIndex, lte.DefaultAPN, tt.profileIndex)
				}
			}
			cfg.Logger.Info("config log")
			if !strings.Contains(logs.String(), "imei="+tt.imei) {
				t.Fatalf("logs = %s, want IMEI field", logs.String())
			}
		})
	}
}

type fakeManagedVoLTEDevice struct {
	status           wwan.VoLTEStatus
	statusErr        error
	testMode         bool
	testModeErr      error
	testModeCtxErr   error
	setTestModeErr   error
	enableFlightErr  error
	disableFlightErr error
	cancel           context.CancelFunc
	calls            []string
	restoreCtxErr    error
	packetStatuses   []wwan.PacketServiceStatus
	packetErrors     []error
	profileIndex     uint8
	profileErr       error
	closed           bool
}

func (d *fakeManagedVoLTEDevice) Close() error {
	d.closed = true
	return nil
}

func (d *fakeManagedVoLTEDevice) VoLTEStatus(context.Context) (wwan.VoLTEStatus, error) {
	d.calls = append(d.calls, "status")
	return d.status, d.statusErr
}

func (d *fakeManagedVoLTEDevice) PacketServiceStatus(context.Context) (wwan.PacketServiceStatus, error) {
	d.calls = append(d.calls, "packet-service")
	status := wwan.PacketServiceStatus{Registered: true, PSAttached: true, LTE: true}
	if len(d.packetStatuses) > 0 {
		status = d.packetStatuses[0]
		d.packetStatuses = d.packetStatuses[1:]
	}
	var err error
	if len(d.packetErrors) > 0 {
		err = d.packetErrors[0]
		d.packetErrors = d.packetErrors[1:]
	}
	return status, err
}

func (d *fakeManagedVoLTEDevice) IMSProfileIndex(context.Context) (uint8, error) {
	d.calls = append(d.calls, "ims-profile")
	if d.profileIndex == 0 {
		d.profileIndex = 2
	}
	return d.profileIndex, d.profileErr
}

func (d *fakeManagedVoLTEDevice) IMSSTestMode(ctx context.Context) (bool, error) {
	d.calls = append(d.calls, "test-mode")
	d.testModeCtxErr = ctx.Err()
	return d.testMode, d.testModeErr
}

func (d *fakeManagedVoLTEDevice) SetIMSSTestMode(_ context.Context, enabled bool) error {
	d.calls = append(d.calls, fmt.Sprintf("set-test-mode:%t", enabled))
	return d.setTestModeErr
}

func (d *fakeManagedVoLTEDevice) SetAirplaneMode(ctx context.Context, enabled bool) error {
	d.calls = append(d.calls, fmt.Sprintf("set-airplane-mode:%t", enabled))
	if enabled {
		if d.cancel != nil {
			d.cancel()
		}
		return d.enableFlightErr
	}
	d.restoreCtxErr = ctx.Err()
	return d.disableFlightErr
}

func TestValidateManagedVoLTE(t *testing.T) {
	errOpen := errors.New("open rejected")
	errStatus := errors.New("status rejected")
	errProfile := errors.New("profile rejected")
	errTestMode := errors.New("test mode rejected")
	errPacket := errors.New("packet service rejected")
	tests := []struct {
		name       string
		device     *fakeManagedVoLTEDevice
		openErr    error
		wantCalls  []string
		wantErr    error
		wantClosed bool
	}{
		{
			name:    "device unavailable",
			openErr: wwan.ErrUnsupported,
			wantErr: ErrUnavailable,
		},
		{
			name:    "device open rejected",
			openErr: errOpen,
			wantErr: errOpen,
		},
		{
			name:       "IMSA unavailable continues with WDS",
			device:     &fakeManagedVoLTEDevice{},
			wantCalls:  []string{"status", "ims-profile", "packet-service"},
			wantClosed: true,
		},
		{
			name:       "occupied IMS checks test mode",
			device:     &fakeManagedVoLTEDevice{status: wwan.VoLTEStatus{Occupied: true}},
			wantCalls:  []string{"status", "ims-profile", "test-mode", "packet-service"},
			wantClosed: true,
		},
		{
			name:       "status rejected",
			device:     &fakeManagedVoLTEDevice{statusErr: errStatus},
			wantCalls:  []string{"status"},
			wantErr:    errStatus,
			wantClosed: true,
		},
		{
			name:       "IMS profile unavailable",
			device:     &fakeManagedVoLTEDevice{profileErr: errProfile},
			wantCalls:  []string{"status", "ims-profile"},
			wantErr:    errProfile,
			wantClosed: true,
		},
		{
			name:       "test mode rejected",
			device:     &fakeManagedVoLTEDevice{status: wwan.VoLTEStatus{Occupied: true}, testModeErr: errTestMode},
			wantCalls:  []string{"status", "ims-profile", "test-mode"},
			wantErr:    errTestMode,
			wantClosed: true,
		},
		{
			name:       "packet service rejected",
			device:     &fakeManagedVoLTEDevice{packetErrors: []error{errPacket}},
			wantCalls:  []string{"status", "ims-profile", "packet-service"},
			wantErr:    errPacket,
			wantClosed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previousOpen := openManagedVoLTEDevice
			openManagedVoLTEDevice = func(*mmodem.Modem) (managedVoLTEDevice, error) {
				return tt.device, tt.openErr
			}
			t.Cleanup(func() {
				openManagedVoLTEDevice = previousOpen
			})

			err := validateManagedVoLTE(context.Background(), &mmodem.Modem{})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("validateManagedVoLTE() error = %v, want %v", err, tt.wantErr)
			}
			var calls []string
			closed := false
			if tt.device != nil {
				calls = tt.device.calls
				closed = tt.device.closed
			}
			if !slices.Equal(calls, tt.wantCalls) {
				t.Fatalf("calls = %v, want %v", calls, tt.wantCalls)
			}
			if closed != tt.wantClosed {
				t.Fatalf("device closed = %v, want %v", closed, tt.wantClosed)
			}
		})
	}
}

func TestPrepareManagedVoLTE(t *testing.T) {
	errStatus := errors.New("status rejected")
	errTestMode := errors.New("test mode rejected")
	errSetTestMode := errors.New("set test mode rejected")
	errEnableFlight := errors.New("enable flight rejected")
	errDisableFlight := errors.New("disable flight rejected")
	errProfile := errors.New("profile rejected")
	tests := []struct {
		name        string
		device      *fakeManagedVoLTEDevice
		cancel      bool
		wantCalls   []string
		wantProfile uint8
		wantErr     error
	}{
		{
			name:        "IMSA unavailable continues with WDS",
			device:      &fakeManagedVoLTEDevice{},
			wantCalls:   []string{"status", "ims-profile", "packet-service"},
			wantProfile: 2,
		},
		{
			name:      "status rejected",
			device:    &fakeManagedVoLTEDevice{statusErr: errStatus},
			wantCalls: []string{"status"},
			wantErr:   errStatus,
		},
		{
			name:        "IMS available",
			device:      &fakeManagedVoLTEDevice{},
			wantCalls:   []string{"status", "ims-profile", "packet-service"},
			wantProfile: 2,
		},
		{
			name: "waits for packet service before IMS",
			device: &fakeManagedVoLTEDevice{
				status: wwan.VoLTEStatus{},
				packetStatuses: []wwan.PacketServiceStatus{
					{},
					{Registered: true, PSAttached: true, LTE: true},
				},
			},
			wantCalls:   []string{"status", "ims-profile", "packet-service", "packet-service"},
			wantProfile: 2,
		},
		{
			name:        "test mode already enabled",
			device:      &fakeManagedVoLTEDevice{status: wwan.VoLTEStatus{Occupied: true}, testMode: true},
			wantCalls:   []string{"status", "ims-profile", "test-mode", "packet-service"},
			wantProfile: 2,
		},
		{
			name:      "test mode query rejected",
			device:    &fakeManagedVoLTEDevice{status: wwan.VoLTEStatus{Occupied: true}, testModeErr: errTestMode},
			wantCalls: []string{"status", "ims-profile", "test-mode"},
			wantErr:   errTestMode,
		},
		{
			name:      "test mode update rejected",
			device:    &fakeManagedVoLTEDevice{status: wwan.VoLTEStatus{Occupied: true}, setTestModeErr: errSetTestMode},
			wantCalls: []string{"status", "ims-profile", "test-mode", "set-test-mode:true"},
			wantErr:   errSetTestMode,
		},
		{
			name:      "airplane mode enable rejected",
			device:    &fakeManagedVoLTEDevice{status: wwan.VoLTEStatus{Occupied: true}, enableFlightErr: errEnableFlight},
			wantCalls: []string{"status", "ims-profile", "test-mode", "set-test-mode:true", "set-airplane-mode:true"},
			wantErr:   errEnableFlight,
		},
		{
			name:        "takes over occupied IMS",
			device:      &fakeManagedVoLTEDevice{status: wwan.VoLTEStatus{Occupied: true}},
			wantCalls:   []string{"status", "ims-profile", "test-mode", "set-test-mode:true", "set-airplane-mode:true", "set-airplane-mode:false", "packet-service"},
			wantProfile: 2,
		},
		{
			name:      "IMS profile unavailable",
			device:    &fakeManagedVoLTEDevice{profileErr: errProfile},
			wantCalls: []string{"status", "ims-profile"},
			wantErr:   errProfile,
		},
		{
			name:      "airplane mode restore rejected",
			device:    &fakeManagedVoLTEDevice{status: wwan.VoLTEStatus{Occupied: true}, disableFlightErr: errDisableFlight},
			wantCalls: []string{"status", "ims-profile", "test-mode", "set-test-mode:true", "set-airplane-mode:true", "set-airplane-mode:false"},
			wantErr:   errDisableFlight,
		},
		{
			name:      "cancellation still restores online",
			device:    &fakeManagedVoLTEDevice{status: wwan.VoLTEStatus{Occupied: true}},
			cancel:    true,
			wantCalls: []string{"status", "ims-profile", "test-mode", "set-test-mode:true", "set-airplane-mode:true", "set-airplane-mode:false"},
			wantErr:   context.Canceled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previousOpen := openManagedVoLTEDevice
			previousResetDelay := voLTEResetDelay
			previousPollInterval := packetServicePollInterval
			previousPacketWaitTimeout := packetServiceWaitTimeout
			openManagedVoLTEDevice = func(*mmodem.Modem) (managedVoLTEDevice, error) {
				return tt.device, nil
			}
			voLTEResetDelay = time.Nanosecond
			packetServicePollInterval = time.Nanosecond
			packetServiceWaitTimeout = time.Hour
			t.Cleanup(func() {
				openManagedVoLTEDevice = previousOpen
				voLTEResetDelay = previousResetDelay
				packetServicePollInterval = previousPollInterval
				packetServiceWaitTimeout = previousPacketWaitTimeout
			})

			ctx := context.Background()
			if tt.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				tt.device.cancel = cancel
				voLTEResetDelay = time.Hour
			}
			profileIndex, err := prepareManagedVoLTE(ctx, &mmodem.Modem{}, nil)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("prepareManagedVoLTE() error = %v, want %v", err, tt.wantErr)
			}
			if profileIndex != tt.wantProfile {
				t.Fatalf("prepareManagedVoLTE() profile = %d, want %d", profileIndex, tt.wantProfile)
			}
			if !slices.Equal(tt.device.calls, tt.wantCalls) {
				t.Fatalf("calls = %v, want %v", tt.device.calls, tt.wantCalls)
			}
			if !tt.device.closed {
				t.Fatal("device was not closed")
			}
			if tt.cancel && tt.device.restoreCtxErr != nil {
				t.Fatalf("restore context error = %v, want nil", tt.device.restoreCtxErr)
			}
		})
	}
}

func TestReleaseManagedVoLTE(t *testing.T) {
	errTestMode := errors.New("test mode rejected")
	errSetTestMode := errors.New("set test mode rejected")
	errEnableFlight := errors.New("enable flight rejected")
	errDisableFlight := errors.New("disable flight rejected")
	tests := []struct {
		name      string
		device    *fakeManagedVoLTEDevice
		openErr   error
		cancel    bool
		wantCalls []string
		wantErr   error
	}{
		{
			name:    "unsupported device requires no restore",
			openErr: wwan.ErrUnsupported,
		},
		{
			name:      "test mode already disabled",
			device:    &fakeManagedVoLTEDevice{},
			wantCalls: []string{"test-mode"},
		},
		{
			name:      "MBIM does not require test mode restore",
			device:    &fakeManagedVoLTEDevice{testModeErr: wwan.ErrUnsupported},
			wantCalls: []string{"test-mode"},
		},
		{
			name:      "test mode query rejected",
			device:    &fakeManagedVoLTEDevice{testModeErr: errTestMode},
			wantCalls: []string{"test-mode"},
			wantErr:   errTestMode,
		},
		{
			name:      "test mode restore rejected",
			device:    &fakeManagedVoLTEDevice{testMode: true, setTestModeErr: errSetTestMode},
			wantCalls: []string{"test-mode", "set-test-mode:false"},
			wantErr:   errSetTestMode,
		},
		{
			name:      "airplane mode enable rejected",
			device:    &fakeManagedVoLTEDevice{testMode: true, enableFlightErr: errEnableFlight},
			wantCalls: []string{"test-mode", "set-test-mode:false", "set-airplane-mode:true"},
			wantErr:   errEnableFlight,
		},
		{
			name:      "restores modem IMS",
			device:    &fakeManagedVoLTEDevice{testMode: true},
			wantCalls: []string{"test-mode", "set-test-mode:false", "set-airplane-mode:true", "set-airplane-mode:false", "packet-service"},
		},
		{
			name:      "airplane mode restore rejected",
			device:    &fakeManagedVoLTEDevice{testMode: true, disableFlightErr: errDisableFlight},
			wantCalls: []string{"test-mode", "set-test-mode:false", "set-airplane-mode:true", "set-airplane-mode:false"},
			wantErr:   errDisableFlight,
		},
		{
			name:      "cancellation still restores online",
			device:    &fakeManagedVoLTEDevice{testMode: true},
			cancel:    true,
			wantCalls: []string{"test-mode", "set-test-mode:false", "set-airplane-mode:true", "set-airplane-mode:false"},
			wantErr:   context.Canceled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previousOpen := openManagedVoLTEDevice
			previousResetDelay := voLTEResetDelay
			previousPollInterval := packetServicePollInterval
			openManagedVoLTEDevice = func(*mmodem.Modem) (managedVoLTEDevice, error) {
				return tt.device, tt.openErr
			}
			voLTEResetDelay = time.Nanosecond
			packetServicePollInterval = time.Nanosecond
			t.Cleanup(func() {
				openManagedVoLTEDevice = previousOpen
				voLTEResetDelay = previousResetDelay
				packetServicePollInterval = previousPollInterval
			})

			ctx := context.Background()
			if tt.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				tt.device.cancel = cancel
				voLTEResetDelay = time.Hour
			}
			err := releaseManagedVoLTE(ctx, &mmodem.Modem{}, nil)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("releaseManagedVoLTE() error = %v, want %v", err, tt.wantErr)
			}
			if tt.device != nil {
				if !slices.Equal(tt.device.calls, tt.wantCalls) {
					t.Fatalf("calls = %v, want %v", tt.device.calls, tt.wantCalls)
				}
				if !tt.device.closed {
					t.Fatal("device was not closed")
				}
				if tt.cancel && tt.device.restoreCtxErr != nil {
					t.Fatalf("restore context error = %v, want nil", tt.device.restoreCtxErr)
				}
			}
		})
	}
}

func TestWaitForPacketService(t *testing.T) {
	errNAS := errors.New("NAS unavailable")
	tests := []struct {
		name      string
		statuses  []wwan.PacketServiceStatus
		errors    []error
		cancel    bool
		wantCalls int
		wantErr   error
	}{
		{
			name:      "ready immediately",
			statuses:  []wwan.PacketServiceStatus{{Registered: true, PSAttached: true, LTE: true}},
			wantCalls: 1,
		},
		{
			name: "waits through errors and incomplete states",
			statuses: []wwan.PacketServiceStatus{
				{},
				{Registered: true, LTE: true},
				{Registered: true, PSAttached: true, LTE: true},
			},
			errors:    []error{errNAS, nil, nil},
			wantCalls: 3,
		},
		{
			name:      "context cancelled",
			statuses:  []wwan.PacketServiceStatus{{}},
			cancel:    true,
			wantCalls: 1,
			wantErr:   context.Canceled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previousInterval := packetServicePollInterval
			packetServicePollInterval = time.Nanosecond
			if tt.cancel {
				packetServicePollInterval = time.Hour
			}
			t.Cleanup(func() {
				packetServicePollInterval = previousInterval
			})

			device := &fakeManagedVoLTEDevice{
				packetStatuses: slices.Clone(tt.statuses),
				packetErrors:   slices.Clone(tt.errors),
			}
			ctx := context.Background()
			if tt.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			err := waitForPacketService(ctx, device)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("waitForPacketService() error = %v, want %v", err, tt.wantErr)
			}
			if got := len(device.calls); got != tt.wantCalls {
				t.Fatalf("packet service calls = %d, want %d", got, tt.wantCalls)
			}
		})
	}
}

type fakeInternetRestorer struct {
	connection    *pinternet.Connection
	currentErr    error
	connectErr    error
	connectErrors []error
	cancel        context.CancelFunc
	calls         []string
	prefs         pinternet.Preferences
	qmapErr       error
}

func (r *fakeInternetRestorer) SetQMAPEnabled(_ context.Context, _ *mmodem.Modem, enabled bool) error {
	r.calls = append(r.calls, fmt.Sprintf("qmap:%t", enabled))
	return r.qmapErr
}

func (r *fakeInternetRestorer) Current(context.Context, *mmodem.Modem) (*pinternet.Connection, error) {
	r.calls = append(r.calls, "current")
	return r.connection, r.currentErr
}

func (r *fakeInternetRestorer) Connect(_ context.Context, _ *mmodem.Modem, prefs pinternet.Preferences) (*pinternet.Connection, error) {
	r.calls = append(r.calls, "connect")
	r.prefs = prefs
	if r.cancel != nil {
		r.cancel()
	}
	if len(r.connectErrors) > 0 {
		err := r.connectErrors[0]
		r.connectErrors = r.connectErrors[1:]
		return r.connection, err
	}
	return r.connection, r.connectErr
}

func TestConnectOnceEnablesQMAPBeforeVoLTE(t *testing.T) {
	errQMAP := errors.New("qmap rejected")
	tests := []struct {
		name      string
		internet  *fakeInternetRestorer
		wantCalls []string
		wantErr   error
	}{
		{
			name:      "returns QMAP migration error before opening IMS",
			internet:  &fakeInternetRestorer{qmapErr: errQMAP},
			wantCalls: []string{"qmap:true"},
			wantErr:   errQMAP,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			coordinator := &coordinator{access: AccessVoLTE, internet: tt.internet}
			modem := &mmodem.Modem{
				EquipmentIdentifier: "modem-1",
				PrimaryPort:         "cdc-wdm3",
				Ports: []mmodem.ModemPort{{
					Device:   "cdc-wdm3",
					PortType: mmodem.ModemPortTypeQmi,
				}},
			}
			_, err := coordinator.connectOnce(context.Background(), modem, 1)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("connectOnce() error = %v, want %v", err, tt.wantErr)
			}
			if !slices.Equal(tt.internet.calls, tt.wantCalls) {
				t.Fatalf("Internet calls = %v, want %v", tt.internet.calls, tt.wantCalls)
			}
		})
	}
}

func TestResetManagedVoLTERestoresInternet(t *testing.T) {
	errCurrent := errors.New("current rejected")
	errConnect := errors.New("connect rejected")
	connected := &pinternet.Connection{
		Status:       pinternet.StatusConnected,
		APN:          "ereseller",
		IPType:       "ipv4v6",
		APNUsername:  "user",
		APNPassword:  "pass",
		APNAuth:      "pap",
		DefaultRoute: true,
		ProxyEnabled: true,
		AlwaysOn:     true,
	}
	wantPrefs := pinternet.Preferences{
		APN:          "ereseller",
		IPType:       "ipv4v6",
		APNUsername:  "user",
		APNPassword:  "pass",
		APNAuth:      "pap",
		DefaultRoute: true,
		ProxyEnabled: true,
		AlwaysOn:     true,
	}
	tests := []struct {
		name          string
		internet      *fakeInternetRestorer
		wantCalls     []string
		wantDevice    []string
		wantPrefs     pinternet.Preferences
		wantErr       error
		cancelRestore bool
	}{
		{
			name:       "reconnects internet after packet service returns",
			internet:   &fakeInternetRestorer{connection: connected, connectErrors: []error{errConnect, nil}},
			wantCalls:  []string{"current", "connect", "connect"},
			wantDevice: []string{"set-airplane-mode:true", "set-airplane-mode:false", "packet-service"},
			wantPrefs:  wantPrefs,
		},
		{
			name:       "leaves disconnected internet alone",
			internet:   &fakeInternetRestorer{connection: &pinternet.Connection{Status: pinternet.StatusDisconnected}},
			wantCalls:  []string{"current"},
			wantDevice: []string{"set-airplane-mode:true", "set-airplane-mode:false", "packet-service"},
		},
		{
			name:      "returns current connection error before airplane mode",
			internet:  &fakeInternetRestorer{currentErr: errCurrent},
			wantCalls: []string{"current"},
			wantErr:   errCurrent,
		},
		{
			name:          "returns reconnect error after modem recovery",
			internet:      &fakeInternetRestorer{connection: connected, connectErr: errConnect},
			wantCalls:     []string{"current", "connect"},
			wantDevice:    []string{"set-airplane-mode:true", "set-airplane-mode:false", "packet-service"},
			wantPrefs:     wantPrefs,
			wantErr:       errConnect,
			cancelRestore: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previousResetDelay := voLTEResetDelay
			previousPollInterval := packetServicePollInterval
			previousRestoreInterval := internetRestoreInterval
			previousRestoreTimeout := internetRestoreTimeout
			voLTEResetDelay = time.Nanosecond
			packetServicePollInterval = time.Nanosecond
			internetRestoreInterval = time.Nanosecond
			internetRestoreTimeout = time.Hour
			if tt.cancelRestore {
				internetRestoreInterval = time.Hour
				internetRestoreTimeout = time.Millisecond
			}
			t.Cleanup(func() {
				voLTEResetDelay = previousResetDelay
				packetServicePollInterval = previousPollInterval
				internetRestoreInterval = previousRestoreInterval
				internetRestoreTimeout = previousRestoreTimeout
			})

			device := &fakeManagedVoLTEDevice{}
			ctx := context.Background()
			if tt.cancelRestore {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				tt.internet.cancel = cancel
			}
			err := resetManagedVoLTE(ctx, &mmodem.Modem{}, device, tt.internet)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("resetManagedVoLTE() error = %v, want %v", err, tt.wantErr)
			}
			if !slices.Equal(tt.internet.calls, tt.wantCalls) {
				t.Fatalf("internet calls = %v, want %v", tt.internet.calls, tt.wantCalls)
			}
			if !slices.Equal(device.calls, tt.wantDevice) {
				t.Fatalf("device calls = %v, want %v", device.calls, tt.wantDevice)
			}
			if tt.internet.prefs != tt.wantPrefs {
				t.Fatalf("internet preferences = %+v, want %+v", tt.internet.prefs, tt.wantPrefs)
			}
		})
	}
}

func TestConnectedClientRequiresSameProfile(t *testing.T) {
	tests := []struct {
		name      string
		session   *sessionState
		profileID string
		wantErr   error
	}{
		{
			name:      "same profile",
			session:   &sessionState{client: &imsgo.Client{}, profileID: "profile-a", connected: true},
			profileID: "profile-a",
		},
		{
			name:      "different profile",
			session:   &sessionState{client: &imsgo.Client{}, profileID: "profile-a", connected: true},
			profileID: "profile-b",
			wantErr:   ErrNotConnected,
		},
		{
			name:      "disconnected",
			session:   &sessionState{client: &imsgo.Client{}, profileID: "profile-a"},
			profileID: "profile-a",
			wantErr:   ErrNotConnected,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &coordinator{sessions: map[string]*sessionState{"modem-1": tt.session}}
			_, err := c.connectedClient("modem-1", tt.profileID)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("connectedClient() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestConnectingSessionLifecycle(t *testing.T) {
	now := time.Date(2026, 5, 29, 10, 0, 0, 0, time.UTC)
	c := &coordinator{
		sessions: map[string]*sessionState{
			"modem-1": {
				client:      &imsgo.Client{},
				connected:   true,
				connectedAt: now,
				phase:       sessionPhaseDisconnected,
				profileID:   "profile-1",
			},
		},
	}

	c.markConnecting("modem-1", 0)

	session := c.sessions["modem-1"]
	if session.client != nil {
		t.Fatal("client is set, want nil")
	}
	if session.connected {
		t.Fatal("connected = true, want false")
	}
	if !session.connectedAt.IsZero() {
		t.Fatalf("connectedAt = %v, want zero", session.connectedAt)
	}
	if session.phase != sessionPhaseConnecting {
		t.Fatalf("phase = %q, want %q", session.phase, sessionPhaseConnecting)
	}
	got := statusFromSession(Settings{Enabled: true}, session, "profile-1", now)
	if got.State != StateConnecting {
		t.Fatalf("State = %q, want %q", got.State, StateConnecting)
	}

	c.markDisconnected("modem-1", 0, nil)

	if session.phase != sessionPhaseDisconnected {
		t.Fatalf("phase after failed connect = %q, want %q", session.phase, sessionPhaseDisconnected)
	}
	got = statusFromSession(Settings{Enabled: true}, session, "profile-1", now)
	if got.State != StateDisconnected {
		t.Fatalf("State after failed connect = %q, want %q", got.State, StateDisconnected)
	}
}

func TestSessionStateIgnoresStaleSessionID(t *testing.T) {
	tests := []struct {
		name          string
		apply         func(*coordinator, *imsgo.Client, *imsgo.Client)
		wantConnected bool
		wantPhase     sessionPhase
	}{
		{
			name: "mark connected",
			apply: func(c *coordinator, oldClient *imsgo.Client, currentClient *imsgo.Client) {
				c.markConnected("modem-1", 1, oldClient)
			},
			wantConnected: true,
			wantPhase:     sessionPhaseConnected,
		},
		{
			name: "mark connecting",
			apply: func(c *coordinator, oldClient *imsgo.Client, currentClient *imsgo.Client) {
				c.markConnecting("modem-1", 1)
			},
			wantConnected: true,
			wantPhase:     sessionPhaseConnected,
		},
		{
			name: "mark disconnected",
			apply: func(c *coordinator, oldClient *imsgo.Client, currentClient *imsgo.Client) {
				c.markDisconnected("modem-1", 1, currentClient)
			},
			wantConnected: true,
			wantPhase:     sessionPhaseConnected,
		},
		{
			name: "stop async",
			apply: func(c *coordinator, oldClient *imsgo.Client, currentClient *imsgo.Client) {
				c.stopAsyncSession("modem-1", 1)
			},
			wantConnected: true,
			wantPhase:     sessionPhaseConnected,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldClient := &imsgo.Client{}
			currentClient := &imsgo.Client{}
			c := &coordinator{
				sessions: map[string]*sessionState{
					"modem-1": {
						id:        2,
						client:    currentClient,
						connected: true,
						phase:     sessionPhaseConnected,
					},
				},
				voiceSubscribers: make(map[uint64]VoiceEventFunc),
			}

			tt.apply(c, oldClient, currentClient)

			session := c.sessions["modem-1"]
			if session.client != currentClient {
				t.Fatal("stale session changed current client")
			}
			if session.connected != tt.wantConnected {
				t.Fatalf("connected = %v, want %v", session.connected, tt.wantConnected)
			}
			if session.phase != tt.wantPhase {
				t.Fatalf("phase = %q, want %q", session.phase, tt.wantPhase)
			}
		})
	}
}

func TestStopDeletesPendingWebsheet(t *testing.T) {
	tests := []struct {
		name string
		stop func(*coordinator)
	}{
		{
			name: "stop",
			stop: func(c *coordinator) {
				c.stop("modem-1")
			},
		},
		{
			name: "stop async",
			stop: func(c *coordinator) {
				c.stopAsync("modem-1")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			broker := websheet.New(websheet.Config{AllowPrivateHosts: true})
			sheet, err := broker.Create(context.Background(), websheet.Request{URL: "http://127.0.0.1/setup"})
			if err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			info := sheet.Info()
			c := &coordinator{
				websheets: broker,
				sessions: map[string]*sessionState{
					"modem-1": {
						websheet: sheet,
					},
				},
				voiceSubscribers: make(map[uint64]VoiceEventFunc),
			}

			tt.stop(c)

			if _, err := broker.Get(info.ID); !errors.Is(err, websheet.ErrNotFound) {
				t.Fatalf("Get() error = %v, want %v", err, websheet.ErrNotFound)
			}
		})
	}
}

func TestAttachWebsheetDeletesStaleSession(t *testing.T) {
	tests := []struct {
		name       string
		sessions   map[string]*sessionState
		sessionID  uint64
		wantStored bool
	}{
		{
			name: "current session",
			sessions: map[string]*sessionState{
				"modem-1": {id: 2},
			},
			sessionID:  2,
			wantStored: true,
		},
		{
			name: "stale session",
			sessions: map[string]*sessionState{
				"modem-1": {id: 2},
			},
			sessionID: 1,
		},
		{
			name:      "detached session",
			sessions:  make(map[string]*sessionState),
			sessionID: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			broker := websheet.New(websheet.Config{AllowPrivateHosts: true})
			sheet, err := broker.Create(context.Background(), websheet.Request{URL: "http://127.0.0.1/setup"})
			if err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			info := sheet.Info()
			c := &coordinator{
				websheets: broker,
				sessions:  tt.sessions,
			}

			c.attachWebsheet("modem-1", tt.sessionID, sheet)

			_, err = broker.Get(info.ID)
			if tt.wantStored {
				if err != nil {
					t.Fatalf("Get() error = %v, want stored websheet", err)
				}
				if c.sessions["modem-1"].websheet != sheet {
					t.Fatal("current session websheet was not stored")
				}
				return
			}
			if !errors.Is(err, websheet.ErrNotFound) {
				t.Fatalf("Get() error = %v, want %v", err, websheet.ErrNotFound)
			}
			if session := c.sessions["modem-1"]; session != nil && session.websheet != nil {
				t.Fatal("stale websheet was stored on current session")
			}
		})
	}
}

func TestMarkDisconnectedFailsOpenCalls(t *testing.T) {
	client := &imsgo.Client{}
	c := &coordinator{
		sessions: map[string]*sessionState{
			"modem-1": {
				client:    client,
				connected: true,
				calls: map[string]*voiceCallState{
					"ringing": {
						info: VoiceCall{ID: "ringing", State: string(imsvoice.CallStateRinging)},
					},
					"answering": {
						info: VoiceCall{ID: "answering", State: string(imsvoice.CallStateAnswering)},
					},
					"active": {
						info: VoiceCall{ID: "active", State: string(imsvoice.CallStateActive)},
					},
					"ended": {
						info: VoiceCall{ID: "ended", State: string(imsvoice.CallStateEnded)},
					},
				},
			},
		},
		voiceSubscribers: make(map[uint64]VoiceEventFunc),
	}
	var events []VoiceEvent
	unsubscribe := c.SubscribeVoiceEvents(func(event VoiceEvent) {
		events = append(events, event)
	})
	defer unsubscribe()

	c.markDisconnected("modem-1", 0, client)

	session := c.sessions["modem-1"]
	if session.connected || session.client != nil {
		t.Fatalf("session connected = %v client nil = %v, want disconnected", session.connected, session.client == nil)
	}
	if session.phase != sessionPhaseDisconnected {
		t.Fatalf("session phase = %q, want %q", session.phase, sessionPhaseDisconnected)
	}
	for _, id := range []string{"ringing", "answering", "active"} {
		call := session.calls[id].info
		if call.State != string(imsvoice.CallStateFailed) {
			t.Fatalf("call %s state = %q, want failed", id, call.State)
		}
		if call.Reason != "wifi calling disconnected" {
			t.Fatalf("call %s reason = %q, want wifi calling disconnected", id, call.Reason)
		}
		if call.EndedAt.IsZero() || call.UpdatedAt.IsZero() {
			t.Fatalf("call %s times = ended %v updated %v, want set", id, call.EndedAt, call.UpdatedAt)
		}
	}
	if got := session.calls["ended"].info.State; got != string(imsvoice.CallStateEnded) {
		t.Fatalf("ended call state = %q, want ended", got)
	}

	gotIDs := make([]string, 0, len(events))
	for _, event := range events {
		gotIDs = append(gotIDs, event.Call.ID)
	}
	sort.Strings(gotIDs)
	if want := []string{"active", "answering", "ringing"}; !slices.Equal(gotIDs, want) {
		t.Fatalf("event ids = %v, want %v", gotIDs, want)
	}
}

func TestMarkDisconnectedIgnoresStaleClient(t *testing.T) {
	c := &coordinator{
		sessions: map[string]*sessionState{
			"modem-1": {
				client:    &imsgo.Client{},
				connected: true,
				calls: map[string]*voiceCallState{
					"active": {
						info: VoiceCall{ID: "active", State: string(imsvoice.CallStateActive)},
					},
				},
			},
		},
		voiceSubscribers: make(map[uint64]VoiceEventFunc),
	}
	c.markDisconnected("modem-1", 0, &imsgo.Client{})

	session := c.sessions["modem-1"]
	if !session.connected {
		t.Fatal("stale client disconnected the active session")
	}
	if got := session.calls["active"].info.State; got != string(imsvoice.CallStateActive) {
		t.Fatalf("active call state = %q, want active", got)
	}
}

func TestMapClientConnectionErrorMarksSessionDisconnected(t *testing.T) {
	client := &imsgo.Client{}
	c := &coordinator{
		sessions: map[string]*sessionState{
			"modem-1": {
				client:    client,
				connected: true,
				phase:     sessionPhaseConnected,
			},
		},
		voiceSubscribers: make(map[uint64]VoiceEventFunc),
	}

	err := c.handleClientDisconnected("modem-1", client, errors.Join(errors.New("sending SMS"), imsgo.ErrClientNotConnected))

	if !errors.Is(err, ErrNotConnected) {
		t.Fatalf("handleClientDisconnected() error = %v, want %v", err, ErrNotConnected)
	}
	session := c.sessions["modem-1"]
	if session.connected || session.client != nil {
		t.Fatalf("session connected = %v client nil = %v, want disconnected", session.connected, session.client == nil)
	}
	if session.phase != sessionPhaseDisconnected {
		t.Fatalf("phase = %q, want %q", session.phase, sessionPhaseDisconnected)
	}
}

func TestStopFailsOpenCallsBeforeRemovingSession(t *testing.T) {
	cancelled := false
	c := &coordinator{
		sessions: map[string]*sessionState{
			"modem-1": {
				cancel: func() {
					cancelled = true
				},
				connected: true,
				calls: map[string]*voiceCallState{
					"active": {
						info: VoiceCall{ID: "active", State: string(imsvoice.CallStateActive)},
					},
				},
			},
		},
		voiceSubscribers: make(map[uint64]VoiceEventFunc),
	}
	var events []VoiceEvent
	unsubscribe := c.SubscribeVoiceEvents(func(event VoiceEvent) {
		events = append(events, event)
	})
	defer unsubscribe()

	c.stop("modem-1")

	if !cancelled {
		t.Fatal("session was not cancelled")
	}
	if _, ok := c.sessions["modem-1"]; ok {
		t.Fatal("session was not removed")
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].Call.ID != "active" || events[0].Call.State != string(imsvoice.CallStateFailed) {
		t.Fatalf("event = %+v, want failed active call", events[0])
	}
}

func TestDisconnectRemovesSessionAndFailsOpenCalls(t *testing.T) {
	cancelled := false
	c := &coordinator{
		sessions: map[string]*sessionState{
			"modem-1": {
				cancel: func() {
					cancelled = true
				},
				connected: true,
				calls: map[string]*voiceCallState{
					"active": {
						info: VoiceCall{ID: "active", State: string(imsvoice.CallStateActive)},
					},
				},
			},
		},
		voiceSubscribers: make(map[uint64]VoiceEventFunc),
	}
	var events []VoiceEvent
	unsubscribe := c.SubscribeVoiceEvents(func(event VoiceEvent) {
		events = append(events, event)
	})
	defer unsubscribe()

	err := c.Disconnect(context.Background(), &mmodem.Modem{EquipmentIdentifier: "modem-1"})

	if err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if !cancelled {
		t.Fatal("session was not cancelled")
	}
	if _, ok := c.sessions["modem-1"]; ok {
		t.Fatal("session was not removed")
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].Call.ID != "active" || events[0].Call.State != string(imsvoice.CallStateFailed) {
		t.Fatalf("event = %+v, want failed active call", events[0])
	}
}

func TestDisconnectIsIdempotent(t *testing.T) {
	c := &coordinator{sessions: make(map[string]*sessionState)}

	if err := c.Disconnect(context.Background(), &mmodem.Modem{EquipmentIdentifier: "modem-1"}); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if err := c.Disconnect(context.Background(), nil); err != nil {
		t.Fatalf("Disconnect(nil) error = %v", err)
	}
}

func TestStopByPathStopsMatchingSession(t *testing.T) {
	tests := []struct {
		name          string
		removedPath   dbus.ObjectPath
		wantRemaining string
	}{
		{
			name:          "removes matching path",
			removedPath:   "/modem/1",
			wantRemaining: "modem-2",
		},
		{
			name:          "ignores unknown path",
			removedPath:   "/modem/3",
			wantRemaining: "modem-1,modem-2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cancelled := make(map[string]bool)
			session := func(modemID string, path dbus.ObjectPath) *sessionState {
				return &sessionState{
					cancel: func() {
						cancelled[modemID] = true
					},
					modemPath: path,
					profileID: modemID + "-profile",
					client:    nil,
					connected: true,
				}
			}
			c := &coordinator{sessions: map[string]*sessionState{
				"modem-1": session("modem-1", "/modem/1"),
				"modem-2": session("modem-2", "/modem/2"),
			}}

			c.stopByPath(tt.removedPath)

			gotRemaining := sessionKeys(c.sessions)
			if gotRemaining != tt.wantRemaining {
				t.Fatalf("remaining sessions = %q, want %q", gotRemaining, tt.wantRemaining)
			}
			if tt.removedPath == "/modem/1" && !cancelled["modem-1"] {
				t.Fatal("matching session was not cancelled")
			}
		})
	}
}

func sessionKeys(sessions map[string]*sessionState) string {
	return strings.Join(slices.Sorted(maps.Keys(sessions)), ",")
}
