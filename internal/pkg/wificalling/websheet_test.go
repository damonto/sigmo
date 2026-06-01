//go:build wifi_calling

package wificalling

import (
	"context"
	"errors"
	"testing"

	"github.com/damonto/sigmo/internal/pkg/websheet"
	"github.com/damonto/vowifi-go/wfcsetup"
)

func TestWFCWebsheetRequestFromSetupErrors(t *testing.T) {
	tests := []struct {
		name            string
		err             error
		wantURL         string
		wantUserData    string
		wantContentType string
	}{
		{
			name: "nsds",
			err: &wfcsetup.Error{
				Err: wfcsetup.ErrUserActionRequired,
				Result: wfcsetup.Result{
					Scheme:  wfcsetup.SchemeNSDS,
					Carrier: "Carrier",
					Websheet: &wfcsetup.Websheet{
						Kind:  wfcsetup.WebsheetKindEmergencyAddress,
						URL:   "https://example.com/nsds",
						Data:  "token=abc",
						Title: "Wi-Fi Calling",
					},
				},
			},
			wantURL:         "https://example.com/nsds",
			wantUserData:    "token=abc",
			wantContentType: "application/x-www-form-urlencoded",
		},
		{
			name: "ts43",
			err: &wfcsetup.Error{
				Err: wfcsetup.ErrUserActionRequired,
				Result: wfcsetup.Result{
					Scheme:  wfcsetup.SchemeTS43,
					Carrier: "Carrier",
					Websheet: &wfcsetup.Websheet{
						Kind:  wfcsetup.WebsheetKindEmergencyAddress,
						URL:   "https://example.com/ts43?existing=1",
						Data:  "token=abc",
						Title: "Wi-Fi Calling",
					},
				},
			},
			wantURL: "https://example.com/ts43?existing=1&token=abc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &coordinator{websheets: websheet.New(websheet.Config{AllowPrivateHosts: true})}
			req, ok := c.wfcWebsheetRequest(tt.err)
			if !ok {
				t.Fatal("wfcWebsheetRequest() ok = false")
			}
			if req.URL != tt.wantURL {
				t.Fatalf("URL = %q, want %q", req.URL, tt.wantURL)
			}
			if req.UserData != tt.wantUserData {
				t.Fatalf("UserData = %q, want %q", req.UserData, tt.wantUserData)
			}
			if req.ContentType != tt.wantContentType {
				t.Fatalf("ContentType = %q, want %q", req.ContentType, tt.wantContentType)
			}
		})
	}
}

func TestCreateWFCWebsheetFromSetupResult(t *testing.T) {
	tests := []struct {
		name       string
		result     wfcsetup.Result
		wantMethod string
		wantErr    error
	}{
		{
			name: "opens emergency address websheet",
			result: wfcsetup.Result{
				Scheme:  wfcsetup.SchemeNSDS,
				Carrier: "Carrier",
				Action:  wfcsetup.ActionOpenWebsheet,
				Websheet: &wfcsetup.Websheet{
					Kind:  wfcsetup.WebsheetKindEmergencyAddress,
					URL:   "http://127.0.0.1/e911",
					Data:  "token=abc",
					Title: "E911",
				},
			},
			wantMethod: "POST",
		},
		{
			name: "pending",
			result: wfcsetup.Result{
				Action:  wfcsetup.ActionWait,
				Pending: true,
			},
			wantErr: ErrWFCSetupPending,
		},
		{
			name: "denied",
			result: wfcsetup.Result{
				Action: wfcsetup.ActionDenied,
			},
			wantErr: ErrWFCSetupDenied,
		},
		{
			name: "missing websheet",
			result: wfcsetup.Result{
				Action: wfcsetup.ActionOpenWebsheet,
			},
			wantErr: ErrWebsheetUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &coordinator{websheets: websheet.New(websheet.Config{AllowPrivateHosts: true})}
			info, err := c.createWFCWebsheet(context.Background(), tt.result)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("createWFCWebsheet() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("createWFCWebsheet() error = %v", err)
			}
			if info.Method != tt.wantMethod {
				t.Fatalf("Method = %q, want %q", info.Method, tt.wantMethod)
			}
		})
	}
}

func TestWFCWebsheetCallbackResult(t *testing.T) {
	tests := []struct {
		name     string
		callback websheet.Callback
		want     wfcWebsheetCallbackAction
	}{
		{
			name:     "carrier setup changed retries connection",
			callback: websheet.Callback{Source: "vowifi", Controller: "VoWiFiWebServiceFlow", Method: "entitlementChanged", Event: "entitlementChanged", ResultCode: "success"},
			want:     wfcWebsheetCallbackRetry,
		},
		{
			name:     "manual done retries connection",
			callback: websheet.Callback{Event: "finishFlow"},
			want:     wfcWebsheetCallbackRetry,
		},
		{
			name:     "dismiss cancels pending connection",
			callback: websheet.Callback{Source: "vowifi", Controller: "VoWiFiWebServiceFlow", Method: "dismissFlow", Event: "dismissFlow", ResultCode: "cancel"},
			want:     wfcWebsheetCallbackDismiss,
		},
		{
			name:     "close webview cancels pending connection",
			callback: websheet.Callback{Source: "vowifi", Controller: "WiFiCallingWebViewController", Method: "CloseWebView"},
			want:     wfcWebsheetCallbackDismiss,
		},
		{
			name:     "status update retries connection",
			callback: websheet.Callback{Source: "vowifi", Controller: "WiFiCallingWebViewController", Method: "phoneServicesAccountStatusChanged", Event: "phoneServicesAccountStatusChanged"},
			want:     wfcWebsheetCallbackRetry,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := wfcWebsheetCallbackResult(tt.callback); got != tt.want {
				t.Fatalf("wfcWebsheetCallbackResult() = %v, want %v", got, tt.want)
			}
		})
	}
}
