//go:build esim_transfer

package esimtransfer

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/damonto/ts43-go"
)

type transferTestDoer func(*http.Request) (*http.Response, error)

func (f transferTestDoer) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

type transferTestChannel struct {
	identity *ts43.Identity
	err      error
	akaCalls int
}

func (c *transferTestChannel) Identity(context.Context) (*ts43.Identity, error) {
	return c.identity, c.err
}

func (c *transferTestChannel) AuthenticateAKA(context.Context, *ts43.AKARequest) (*ts43.AKAResponse, error) {
	c.akaCalls++
	return &ts43.AKAResponse{Res: []byte{1}}, nil
}

func TestPendingTransferContinuesSameSession(t *testing.T) {
	for _, tt := range []struct {
		name                          string
		installed, queryError, cancel bool
	}{
		{name: "download preparation"},
		{name: "activation confirmation", installed: true},
		{name: "download query error", queryError: true},
		{name: "activation query error", installed: true, queryError: true},
		{name: "cancel download wait", cancel: true},
		{name: "cancel activation wait", installed: true, cancel: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			queries, manages := 0, 0
			client, err := ts43.New(&ts43.Config{
				Logger:      slog.New(slog.DiscardHandler),
				Entitlement: ts43.Entitlement{Server: "https://carrier.example", Channel: ts43.EntitlementChannelSamsung, TransferType: ts43.TransferTypeTS43V8},
				Source:      ts43.Endpoint{Channel: &transferTestChannel{identity: &ts43.Identity{MCC: "450", MNC: "05", IMSI: "450051234567890", ICCID: "source"}}, Device: ts43.Device{IMEI: "111111111111111"}},
				Target:      ts43.Endpoint{Device: ts43.Device{IMEI: "222222222222222", EID: "89049032000001000000000000000000"}},
				HTTPClient: transferTestDoer(func(req *http.Request) (*http.Response, error) {
					body := ""
					code := http.StatusOK
					switch ts43.Operation(req.URL.Query().Get("operation")) {
					case "":
						body = `{"TOKEN":{"token":"auth","Validity":3600}}`
					case ts43.OperationCheck:
						body = `{"APPLICATION":{"OperationResult":1,"PrimaryAppEligibility":1}}`
					case ts43.OperationManage:
						manages++
						body = `{"APPLICATION":{"OperationResult":1,"SubscriptionResult":1,"SubscriptionServiceURL":"https://carrier.example/websheet"}}`
						if tt.installed {
							body = `{"APPLICATION":{"OperationResult":1,"SubscriptionResult":2,"DownloadInfo":{"ProfileSmdpAddress":"smdp.example"}}}`
							if manages > 1 {
								body = `{"APPLICATION":{"OperationResult":1,"SubscriptionResult":3}}`
							}
						}
					case ts43.OperationAcquireConf:
						queries++
						body = `{"APPLICATION":{"PrimaryConfiguration":{"ServiceStatus":1,"DownloadInfo":{"ProfileSmdpAddress":"smdp.example"}}}}`
						if queries == 1 {
							body = `{"APPLICATION":{"PrimaryConfiguration":{"ServiceStatus":2}}}`
							if tt.queryError {
								code = http.StatusServiceUnavailable
							}
						}
					default:
						return nil, errors.New("unexpected operation")
					}
					return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(client.CloseIdleConnections)
			result, err := client.Transfer(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if tt.installed {
				result, err = client.CompleteActivation(t.Context(), result, ts43.ActivationResult{ICCID: "installed"})
			} else {
				result, err = client.Continue(t.Context(), result, ts43.ContinueRequest{Websheet: &ts43.WebsheetResult{Event: ts43.WebsheetEventFinishFlow}})
			}
			if (err != nil) != tt.queryError || !transferPending(result) {
				t.Fatalf("pending result = %+v, error = %v", result, err)
			}

			type outcome struct {
				result *ts43.Result
				done   bool
				err    error
			}
			finished := make(chan outcome, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := testWSUpgrader.Upgrade(w, r, nil)
				if err != nil {
					finished <- outcome{err: err}
					return
				}
				defer conn.Close()
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				session := newWSSession(conn, cancel)
				runner := &transferRunner{}
				active := &transferState{ts43Client: client, logger: slog.New(slog.DiscardHandler)}
				next, done, err := runner.handleEvent(ctx, session, active, startRequest{}, result)
				finished <- outcome{next, done, err}
			}))
			defer server.Close()
			conn := dialTestWebSocket(t, server.URL)
			defer conn.Close()
			if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			var prompt wsServerMessage
			if err := conn.ReadJSON(&prompt); err != nil {
				t.Fatal(err)
			}
			if prompt.Type != wsTypeUserInput || prompt.Input == nil {
				t.Fatalf("message = %+v, want retry prompt", prompt)
			}
			accept := !tt.cancel
			if err := conn.WriteJSON(wsClientMessage{Type: wsTypeUserInput, Accept: &accept}); err != nil {
				t.Fatal(err)
			}
			got := <-finished
			if got.done {
				t.Fatal("pending event was reported as completed")
			}
			if tt.cancel {
				if !errors.Is(got.err, context.Canceled) || queries != 1 {
					t.Fatalf("cancel result = %+v, queries = %d", got, queries)
				}
				return
			}
			wantState, wantManages := ts43.StatePendingDownload, 1
			if tt.installed {
				wantState, wantManages = ts43.StateDone, 2
			}
			if got.err != nil || got.result.State != wantState || manages != wantManages || queries != 2 {
				t.Fatalf("result = %+v, err = %v, manages = %d, queries = %d", got.result, got.err, manages, queries)
			}
		})
	}
}

func TestActivationChannelChecksInstalledProfile(t *testing.T) {
	for _, tt := range []struct {
		name                 string
		installed            string
		identity             *ts43.Identity
		err                  error
		wantAKA, wantRelease bool
	}{
		{name: "not installed"},
		{name: "installed profile", installed: "target", identity: &ts43.Identity{ICCID: "target"}, wantAKA: true},
		{name: "old SIM", installed: "target", identity: &ts43.Identity{ICCID: "old"}, wantRelease: true},
		{name: "missing identity", installed: "target", wantRelease: true},
		{name: "read failure", installed: "target", err: errors.New("SIM unavailable"), wantRelease: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			channel := &transferTestChannel{identity: tt.identity, err: tt.err}
			releases := 0
			c := &activationChannel{iccid: tt.installed, channel: channel, release: func() { releases++ }}
			_, err := c.AuthenticateAKA(t.Context(), &ts43.AKARequest{})
			if (err == nil) != tt.wantAKA || (channel.akaCalls == 1) != tt.wantAKA {
				t.Fatalf("AuthenticateAKA() error = %v, calls = %d", err, channel.akaCalls)
			}
			if tt.err != nil && !errors.Is(err, tt.err) {
				t.Fatalf("error = %v, want wrapped %v", err, tt.err)
			}
			if (releases == 1) != tt.wantRelease {
				t.Errorf("release calls = %d", releases)
			}
			c.Close()
			c.Close()
			if releases != 1 {
				t.Errorf("Close() release calls = %d, want 1", releases)
			}
		})
	}
}

func TestTransferSupportAndroidBackend(t *testing.T) {
	for _, tt := range []struct {
		name, mcc, mnc string
		want           bool
	}{
		{name: "Google takes precedence", mcc: "232", mnc: "05", want: true},
		{name: "Ice requires Android", mcc: "242", mnc: "14"},
		{name: "supplemental Verizon requires Android", mcc: "312", mnc: "012"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := transferSupport(ts43.Identity{MCC: tt.mcc, MNC: tt.mnc}, ts43.SIMTypeESIM, "eSIM")
			if got != tt.want {
				t.Fatalf("transferSupport() = %t, %q, want %t", got, reason, tt.want)
			}
		})
	}
}

func TestDelayedDownloadRequiresCarrierCode(t *testing.T) {
	for _, tt := range []struct {
		name, code        string
		cancel, wantError bool
	}{
		{name: "carrier activation code", code: " LPA:1$smdp.example$matching-id "},
		{name: "invalid code", code: "invalid", wantError: true},
		{name: "cancel", cancel: true, wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			type outcome struct {
				config ts43.DownloadConfig
				err    error
			}
			finished := make(chan outcome, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := testWSUpgrader.Upgrade(w, r, nil)
				if err != nil {
					finished <- outcome{err: err}
					return
				}
				defer conn.Close()
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				session := newWSSession(conn, cancel)
				config, err := session.delayedDownloadConfig(ctx, ts43.DelayedDownloadEvent{TargetIMEI: "222222222222222"})
				finished <- outcome{config, err}
			}))
			defer server.Close()
			conn := dialTestWebSocket(t, server.URL)
			defer conn.Close()
			if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			var prompt wsServerMessage
			if err := conn.ReadJSON(&prompt); err != nil {
				t.Fatal(err)
			}
			if prompt.Type != wsTypeUserInput || prompt.Input == nil || !prompt.Input.FreeText {
				t.Fatalf("message = %+v, want activation code prompt", prompt)
			}
			accept := !tt.cancel
			if err := conn.WriteJSON(wsClientMessage{Type: wsTypeUserInput, Accept: &accept, Response: tt.code}); err != nil {
				t.Fatal(err)
			}
			got := <-finished
			if (got.err != nil) != tt.wantError {
				t.Fatalf("delayedDownloadConfig() error = %v", got.err)
			}
			if tt.cancel && !errors.Is(got.err, context.Canceled) {
				t.Errorf("cancel error = %v", got.err)
			}
			if !tt.wantError && (got.config.ActivationCode != strings.TrimSpace(tt.code) || got.config.IMEI != "222222222222222") {
				t.Errorf("config = %+v", got.config)
			}
		})
	}
}
