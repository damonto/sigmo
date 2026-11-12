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

func TestTransferRetryable(t *testing.T) {
	for _, tt := range []struct {
		name   string
		result *ts43.Result
		err    error
		want   bool
	}{
		{name: "no transfer", err: errors.New("network unavailable")},
		{name: "completed transfer", result: &ts43.Result{State: ts43.StateDone}, err: errors.New("network unavailable")},
		{name: "download network failure", result: &ts43.Result{State: ts43.StatePendingConfiguration}, err: errors.New("network unavailable"), want: true},
		{name: "activation network failure", result: &ts43.Result{State: ts43.StatePendingActivation}, err: errors.New("network unavailable"), want: true},
		{name: "download deactivated", result: &ts43.Result{State: ts43.StatePendingConfiguration}, err: &ts43.CarrierDeniedError{Operation: ts43.OperationAcquireConf, Result: &ts43.ODSAResponse{ServiceStatus: ts43.ServiceStatusDeactivated}}},
		{name: "activation deactivated no reuse", result: &ts43.Result{State: ts43.StatePendingActivation}, err: &ts43.CarrierDeniedError{Operation: ts43.OperationAcquireConf, Result: &ts43.ODSAResponse{ServiceStatus: ts43.ServiceStatusDeactivatedNoReuse}}},
		{name: "forbidden", result: &ts43.Result{State: ts43.StatePendingConfiguration}, err: &ts43.HTTPError{StatusCode: http.StatusForbidden}},
		{name: "credential retries exhausted", result: &ts43.Result{State: ts43.StatePendingConfiguration}, err: &ts43.HTTPError{StatusCode: http.StatusNetworkAuthenticationRequired}},
		{name: "server unavailable", result: &ts43.Result{State: ts43.StatePendingConfiguration}, err: &ts43.HTTPError{StatusCode: http.StatusServiceUnavailable}, want: true},
		{name: "rate limited", result: &ts43.Result{State: ts43.StatePendingConfiguration}, err: &ts43.HTTPError{StatusCode: http.StatusTooManyRequests}, want: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := transferRetryable(tt.result, tt.err); got != tt.want {
				t.Errorf("transferRetryable() = %t, want %t", got, tt.want)
			}
		})
	}
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
				Entitlement: ts43.Entitlement{Server: "https://carrier.example", TransferType: ts43.TransferTypeTS43V8},
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
							body = `{"APPLICATION":{"PrimaryConfiguration":{"ServiceStatus":2,"PollingInterval":0}}}`
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
				session := newWSSession(ctx, conn, cancel)
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

func TestTransferSupport(t *testing.T) {
	carriers := testCarrierConfigs(t)
	ambiguous := new(ts43.EntitlementConfigs)
	if err := ambiguous.UnmarshalJSON([]byte(`[
 {"mccmnc":"00101","carrierName":"first","entitlementServer":"https://first.example","supportEsimToEsimTransfer":true,"transferTypeEsim":7},
 {"mccmnc":"00101","carrierName":"second","entitlementServer":"https://second.example","supportEsimToEsimTransfer":true,"transferTypeEsim":7}
]`)); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name     string
		identity ts43.Identity
		simType  ts43.SIMType
		configs  *ts43.EntitlementConfigs
		want     bool
		reason   string
	}{
		{
			name: "supported eSIM", identity: ts43.Identity{MCC: "232", MNC: "05"},
			configs: carriers, want: true,
		},
		{
			name: "supported pSIM", identity: ts43.Identity{MCC: "204", MNC: "08"},
			simType: ts43.SIMTypePSIM, configs: carriers, want: true,
		},
		{
			name: "missing configuration", identity: ts43.Identity{MCC: "232", MNC: "05"},
			reason: "carrier does not support eSIM transfer",
		},
		{
			name: "missing carrier", identity: ts43.Identity{MCC: "242", MNC: "14"},
			configs: carriers, reason: "carrier does not support eSIM transfer",
		},
		{
			name: "unsupported direction", identity: ts43.Identity{MCC: "232", MNC: "05"},
			simType: ts43.SIMTypePSIM, configs: carriers, reason: "carrier does not support pSIM transfer",
		},
		{
			name: "ambiguous carrier", identity: ts43.Identity{MCC: "001", MNC: "01"},
			configs: ambiguous, reason: "carrier entitlement config is ambiguous",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			label := "eSIM"
			if tt.simType == ts43.SIMTypePSIM {
				label = "pSIM"
			}
			got, reason := transferSupport(tt.identity, tt.simType, label, tt.configs)
			if got != tt.want || reason != tt.reason {
				t.Fatalf("transferSupport() = %t, %q, want %t, %q", got, reason, tt.want, tt.reason)
			}
		})
	}
}
