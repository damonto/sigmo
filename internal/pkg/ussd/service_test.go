package ussd

import (
	"context"
	"errors"
	"testing"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

func TestExecuteRoutesUSSD(t *testing.T) {
	tests := []struct {
		name           string
		status         RouteStatus
		statusErr      error
		modemReply     string
		modemErr       error
		routeReply     string
		wantReply      string
		wantErr        string
		wantModemCalls int
		wantRouteCalls int
	}{
		{
			name:           "preferred route skips modem",
			status:         RouteStatus{Preferred: true, Connected: true},
			routeReply:     "route balance",
			wantReply:      "route balance",
			wantRouteCalls: 1,
		},
		{
			name:           "modem succeeds when route is not preferred",
			modemReply:     "modem balance",
			wantReply:      "modem balance",
			wantModemCalls: 1,
		},
		{
			name:           "connected route fallback after modem fails",
			status:         RouteStatus{Connected: true},
			modemErr:       errors.New("modem ussd rejected"),
			routeReply:     "route fallback",
			wantReply:      "route fallback",
			wantModemCalls: 1,
			wantRouteCalls: 1,
		},
		{
			name:           "route unavailable still uses modem",
			statusErr:      ErrRouteUnavailable,
			modemReply:     "modem balance",
			wantReply:      "modem balance",
			wantModemCalls: 1,
		},
		{
			name:      "status error stops execution",
			statusErr: errors.New("status read failed"),
			wantErr:   "status read failed",
		},
		{
			name:           "modem error wins when route fallback also fails",
			status:         RouteStatus{Connected: true},
			modemErr:       errors.New("modem ussd rejected"),
			wantErr:        "modem ussd rejected",
			wantModemCalls: 1,
			wantRouteCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route := &fakeRoute{
				status:    tt.status,
				statusErr: tt.statusErr,
				ussdReply: tt.routeReply,
			}
			device := &fakeModemDevice{
				reply: tt.modemReply,
				err:   tt.modemErr,
			}
			service := New(route)

			got, err := service.execute(context.Background(), device, "initialize", "*123#")
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("execute() error = %v, want %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("execute() error = %v", err)
			}
			if got != tt.wantReply {
				t.Fatalf("execute() = %q, want %q", got, tt.wantReply)
			}
			if device.calls != tt.wantModemCalls {
				t.Fatalf("modem calls = %d, want %d", device.calls, tt.wantModemCalls)
			}
			if route.ussdCalls != tt.wantRouteCalls {
				t.Fatalf("route calls = %d, want %d", route.ussdCalls, tt.wantRouteCalls)
			}
		})
	}
}

type fakeModemDevice struct {
	reply string
	err   error
	calls int
}

func (f *fakeModemDevice) modem() *mmodem.Modem { return nil }

func (f *fakeModemDevice) executeUSSD(context.Context, string, string) (string, error) {
	f.calls++
	return f.reply, f.err
}

type fakeRoute struct {
	status    RouteStatus
	statusErr error
	ussdReply string
	ussdCalls int
}

func (f fakeRoute) Status(context.Context, *mmodem.Modem) (RouteStatus, error) {
	return f.status, f.statusErr
}

func (f *fakeRoute) ExecuteUSSD(context.Context, *mmodem.Modem, string, string) (string, error) {
	f.ussdCalls++
	if f.ussdReply == "" {
		return "", errors.New("route ussd rejected")
	}
	return f.ussdReply, nil
}
