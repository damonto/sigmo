//go:build esim_transfer

package esimtransfer

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	sgp22 "github.com/damonto/euicc-go/v2"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/pro/internal/pkg/websheet"
	"github.com/damonto/ts43-go"
)

var testWSUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func TestCandidateSupport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		candidate profileCandidate
		wantOK    bool
	}{
		{
			name:      "enabled esim with entitlement",
			candidate: esimCandidate(testProfile(sgp22.ProfileEnabled)),
			wantOK:    true,
		},
		{
			name:      "disabled esim with entitlement is transferable",
			candidate: esimCandidate(testProfile(sgp22.ProfileDisabled)),
			wantOK:    true,
		},
		{
			name: "physical sim with entitlement",
			candidate: physicalCandidate(ts43.Identity{
				ICCID: "8900000000000000000",
				MCC:   "204",
				MNC:   "08",
			}),
			wantOK: true,
		},
		{
			name:      "esim without entitlement is unsupported",
			candidate: esimCandidate(testUnsupportedProfile()),
		},
		{
			name: "physical sim without entitlement is unsupported",
			candidate: physicalCandidate(ts43.Identity{
				ICCID: "8900000000000000000",
				MCC:   "999",
				MNC:   "99",
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.candidate.response.Supported != tt.wantOK {
				t.Fatalf("Supported = %v, want %v", tt.candidate.response.Supported, tt.wantOK)
			}
		})
	}
}

func TestValidateStartRequiresCCIDIMEI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		start   Start
		wantErr error
	}{
		{
			name: "ccid requires source imei when transfer starts",
			start: Start{
				SourceType: SourceCCID,
				SourceID:   "reader-1",
				ProfileID:  "profile-1",
			},
			wantErr: ErrSourceIMEIRequired,
		},
		{
			name: "ccid with source imei",
			start: Start{
				SourceType: SourceCCID,
				SourceID:   "reader-1",
				ProfileID:  "profile-1",
				SourceIMEI: "123456789012345",
			},
		},
		{
			name: "modem source does not require extra imei input",
			start: Start{
				SourceType: SourceModem,
				SourceID:   "modem-1",
				ProfileID:  "profile-1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateStart(tt.start)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("validateStart() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateTargetRejectsSourceTargetModem(t *testing.T) {
	t.Parallel()

	target := &mmodem.Modem{EquipmentIdentifier: "target-imei"}
	tests := []struct {
		name    string
		start   Start
		wantErr error
	}{
		{
			name:    "same modem source and target",
			start:   Start{SourceType: SourceModem, SourceID: "target-imei"},
			wantErr: ErrSourceIsTarget,
		},
		{
			name:  "different modem source",
			start: Start{SourceType: SourceModem, SourceID: "source-imei"},
		},
		{
			name:  "ccid source can target modem",
			start: Start{SourceType: SourceCCID, SourceID: "target-imei"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTarget(target, tt.start)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("validateTarget() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestSelectModemSourcePort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		modem    *mmodem.Modem
		wantType mmodem.ModemPortType
		wantDev  string
		wantSlot int
	}{
		{
			name: "qmi primary uses qmi port",
			modem: &mmodem.Modem{
				PrimaryPort:    "/dev/cdc-wdm1",
				PrimarySimSlot: 2,
				Ports: []mmodem.ModemPort{
					{PortType: mmodem.ModemPortTypeQmi, Device: "/dev/cdc-wdm1"},
					{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB6"},
				},
			},
			wantType: mmodem.ModemPortTypeQmi,
			wantDev:  "/dev/cdc-wdm1",
			wantSlot: 2,
		},
		{
			name: "mbim primary uses at port",
			modem: &mmodem.Modem{
				PrimaryPort: "/dev/cdc-wdm0",
				Ports: []mmodem.ModemPort{
					{PortType: mmodem.ModemPortTypeMbim, Device: "/dev/cdc-wdm0"},
					{PortType: mmodem.ModemPortTypeAt, Device: "/dev/ttyUSB2"},
				},
			},
			wantType: mmodem.ModemPortTypeAt,
			wantDev:  "/dev/ttyUSB2",
			wantSlot: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectModemSourcePort(tt.modem)
			if err != nil {
				t.Fatalf("selectModemSourcePort() error = %v", err)
			}
			if got.portType != tt.wantType || got.device != tt.wantDev || got.slot != tt.wantSlot {
				t.Fatalf("selectModemSourcePort() = %+v, want type %v device %q slot %d", got, tt.wantType, tt.wantDev, tt.wantSlot)
			}
		})
	}
}

func TestModemName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		currentSettings settings.Settings
		modem           *mmodem.Modem
		want            string
	}{
		{
			name: "configured alias wins",
			currentSettings: settings.Settings{Modems: map[string]settings.Modem{
				"imei-1": {Alias: "Office"},
			}},
			modem: &mmodem.Modem{
				EquipmentIdentifier: "imei-1",
				Model:               "RM520N-GL",
			},
			want: "Office",
		},
		{
			name:            "model fallback matches modem overview",
			currentSettings: settings.Settings{},
			modem: &mmodem.Modem{
				EquipmentIdentifier: "imei-2",
				Manufacturer:        "Quectel",
				Model:               "RM520N-GL",
			},
			want: "RM520N-GL",
		},
		{
			name:            "empty model stays empty",
			currentSettings: settings.Settings{},
			modem: &mmodem.Modem{
				EquipmentIdentifier: "imei-3",
				Manufacturer:        "Quectel",
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := modemName(&tt.currentSettings, tt.modem); got != tt.want {
				t.Fatalf("modemName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCCIDServiceUnavailable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "pcsc no service code",
			err:  errors.New("scardEstablishContext() returned 0x8010001D"),
			want: true,
		},
		{
			name: "pcsc no service name",
			err:  errors.New("scard failure: SCARD_E_NO_SERVICE (Service not available.)"),
			want: true,
		},
		{
			name: "other ccid error",
			err:  errors.New("scard failure: SCARD_E_NO_READERS_AVAILABLE"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ccidServiceUnavailable(tt.err); got != tt.want {
				t.Fatalf("ccidServiceUnavailable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSourceEndpointCloseOnce(t *testing.T) {
	t.Parallel()

	calls := 0
	source := &sourceEndpoint{
		release: func() {
			calls++
		},
	}

	source.Close()
	source.Close()

	if calls != 1 {
		t.Fatalf("release calls = %d, want 1", calls)
	}
}

func TestActiveSessionCloseAllowsMissingTargetClient(t *testing.T) {
	t.Parallel()

	calls := 0
	active := &activeSession{
		source: &sourceEndpoint{
			release: func() {
				calls++
			},
		},
	}

	active.Close()
	active.Close()

	if calls != 1 {
		t.Fatalf("source release calls = %d, want 1", calls)
	}
}

func TestSessionCancelCancelsContext(t *testing.T) {
	t.Parallel()

	done := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := testWSUpgrader.Upgrade(w, r, nil)
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		_ = newSession(conn, cancel)

		select {
		case <-ctx.Done():
			done <- nil
		case <-time.After(time.Second):
			done <- errors.New("transfer session did not cancel")
		}
	}))
	defer server.Close()

	conn := dialTestWebSocket(t, server.URL)
	if err := conn.WriteJSON(clientMessage{Type: wsTypeCancel}); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestSessionWaitForStartStopsOnDisconnect(t *testing.T) {
	t.Parallel()

	done := make(chan bool, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := testWSUpgrader.Upgrade(w, r, nil)
		if err != nil {
			done <- true
			return
		}
		defer conn.Close()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		session := newSession(conn, cancel)
		_, ok := session.waitForStart(ctx)
		done <- ok
	}))
	defer server.Close()

	conn := dialTestWebSocket(t, server.URL)
	if err := conn.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if ok := <-done; ok {
		t.Fatal("waitForStart() ok = true, want false")
	}
}

func dialTestWebSocket(t *testing.T, rawURL string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(rawURL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	return conn
}

func TestTS43SourceSIMType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		profileType ProfileType
		want        ts43.SIMType
	}{
		{name: "eSIM source", profileType: ProfileESIM, want: ts43.SIMTypeESIM},
		{name: "pSIM source", profileType: ProfilePhysical, want: ts43.SIMTypePSIM},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ts43SourceSIMType(tt.profileType); got != tt.want {
				t.Fatalf("ts43SourceSIMType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSMDSDiscoveryEventFromDelayedDownload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		event ts43.DelayedDownloadEvent
		want  ts43.SMDSDiscoveryEvent
	}{
		{
			name: "preserves delayed download fields",
			event: ts43.DelayedDownloadEvent{
				SourceICCID:        "8910000000000000000",
				TargetEID:          "89049032000001000000000000000000",
				TargetIMEI:         "222222222222222",
				SubscriptionResult: ts43.SubscriptionResultDelayedDownload,
			},
			want: ts43.SMDSDiscoveryEvent{
				SourceICCID:        "8910000000000000000",
				TargetEID:          "89049032000001000000000000000000",
				TargetIMEI:         "222222222222222",
				SubscriptionResult: ts43.SubscriptionResultDelayedDownload,
			},
		},
		{
			name: "keeps empty optional fields empty",
			event: ts43.DelayedDownloadEvent{
				SubscriptionResult: ts43.SubscriptionResultDelayedDownload,
			},
			want: ts43.SMDSDiscoveryEvent{
				SubscriptionResult: ts43.SubscriptionResultDelayedDownload,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := smdsDiscoveryEventFromDelayedDownload(tt.event); got != tt.want {
				t.Fatalf("smdsDiscoveryEventFromDelayedDownload() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestTS43WebsheetResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		callback websheet.Callback
		want     ts43.WebsheetCallbackEvent
		wantNext ts43.Operation
	}{
		{
			name: "activation code",
			callback: websheet.Callback{
				Event:          "profileReadyWithActivationCode",
				ActivationCode: "1$example.com$matching-id",
				ICCID:          "8901",
				IMEI:           "123456789012345",
			},
			want: ts43.WebsheetEventProfileReadyWithActivationCode,
		},
		{
			name: "finish flow acquire configuration",
			callback: websheet.Callback{
				Event:      "finishFlow",
				NextAction: "AcquireConfiguration",
			},
			want:     ts43.WebsheetEventFinishFlow,
			wantNext: ts43.OperationAcquireConf,
		},
		{
			name:     "delete token",
			callback: websheet.Callback{Event: "deleteToken"},
			want:     ts43.WebsheetEventDeleteToken,
		},
		{
			name:     "status",
			callback: websheet.Callback{Event: "checkProfileServiceStatus"},
			want:     ts43.WebsheetEventCheckProfileServiceStatus,
		},
		{
			name:     "delete profile",
			callback: websheet.Callback{Event: "deleteProfileInUse", ICCID: "8901"},
			want:     ts43.WebsheetEventDeleteProfileInUse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ts43WebsheetResult(tt.callback)
			if err != nil {
				t.Fatalf("ts43WebsheetResult() error = %v", err)
			}
			if got.Event != tt.want {
				t.Fatalf("Event = %v, want %v", got.Event, tt.want)
			}
			if got.NextAction != tt.wantNext {
				t.Fatalf("NextAction = %v, want %v", got.NextAction, tt.wantNext)
			}
		})
	}
}

func testProfile(state sgp22.ProfileState) *sgp22.ProfileInfo {
	return &sgp22.ProfileInfo{
		ICCID:               sgp22.ICCID{0x98, 0x10},
		ProfileState:        state,
		ProfileNickname:     "Boost",
		ServiceProviderName: "Boost",
		ProfileOwner: sgp22.OperatorId{
			PLMN: []byte{0x02, 0xf8, 0x90},
			GID1: []byte{0x63, 0x32},
		},
	}
}

func testUnsupportedProfile() *sgp22.ProfileInfo {
	profile := testProfile(sgp22.ProfileEnabled)
	profile.ProfileOwner = sgp22.OperatorId{
		PLMN: []byte{0x99, 0xf9, 0x99},
	}
	return profile
}
