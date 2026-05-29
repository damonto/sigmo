package ussd

import (
	"context"
	"errors"
	"testing"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
	"github.com/damonto/sigmo/internal/pkg/websheet"
	"github.com/damonto/sigmo/internal/pkg/wificalling"
)

func TestExecuteRoutesUSSD(t *testing.T) {
	tests := []struct {
		name           string
		status         wificalling.Status
		statusErr      error
		modemReply     string
		modemErr       error
		wifiReply      string
		wantReply      string
		wantErr        string
		wantModemCalls int
		wantWiFiCalls  int
	}{
		{
			name:          "preferred wifi calling skips modem",
			status:        wificalling.Status{Settings: wificalling.Settings{Preferred: true}, Connected: true},
			wifiReply:     "wifi balance",
			wantReply:     "wifi balance",
			wantWiFiCalls: 1,
		},
		{
			name:           "modem succeeds when wifi calling is not preferred",
			modemReply:     "modem balance",
			wantReply:      "modem balance",
			wantModemCalls: 1,
		},
		{
			name:           "connected wifi calling fallback after modem fails",
			status:         wificalling.Status{Connected: true},
			modemErr:       errors.New("modem ussd rejected"),
			wifiReply:      "wifi fallback",
			wantReply:      "wifi fallback",
			wantModemCalls: 1,
			wantWiFiCalls:  1,
		},
		{
			name:           "wifi unavailable still uses modem",
			statusErr:      wificalling.ErrUnavailable,
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
			name:           "modem error wins when wifi fallback also fails",
			status:         wificalling.Status{Connected: true},
			modemErr:       errors.New("modem ussd rejected"),
			wantErr:        "modem ussd rejected",
			wantModemCalls: 1,
			wantWiFiCalls:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wifiCalling := &fakeWiFiCalling{
				status:    tt.status,
				statusErr: tt.statusErr,
				ussdReply: tt.wifiReply,
			}
			device := &fakeModemDevice{
				reply: tt.modemReply,
				err:   tt.modemErr,
			}
			service := New(wifiCalling)

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
			if wifiCalling.ussdCalls != tt.wantWiFiCalls {
				t.Fatalf("Wi-Fi Calling calls = %d, want %d", wifiCalling.ussdCalls, tt.wantWiFiCalls)
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

type fakeWiFiCalling struct {
	status    wificalling.Status
	statusErr error
	ussdReply string
	ussdCalls int
}

func (fakeWiFiCalling) Run(context.Context, *mmodem.Registry) error { return nil }

func (fakeWiFiCalling) Settings(context.Context, *mmodem.Modem) (wificalling.Settings, error) {
	return wificalling.Settings{}, nil
}

func (fakeWiFiCalling) UpdateSettings(context.Context, *mmodem.Modem, wificalling.Settings) error {
	return nil
}

func (f fakeWiFiCalling) Status(context.Context, *mmodem.Modem) (wificalling.Status, error) {
	return f.status, f.statusErr
}

func (fakeWiFiCalling) StartWebsheet(context.Context, *mmodem.Modem) (websheet.Info, error) {
	return websheet.Info{}, nil
}

func (fakeWiFiCalling) SendSMS(context.Context, *mmodem.Modem, string, string) (storage.Message, error) {
	return storage.Message{}, nil
}

func (f *fakeWiFiCalling) ExecuteUSSD(context.Context, *mmodem.Modem, string, string) (string, error) {
	f.ussdCalls++
	if f.ussdReply == "" {
		return "", errors.New("wifi ussd rejected")
	}
	return f.ussdReply, nil
}

func (fakeWiFiCalling) DialCall(context.Context, *mmodem.Modem, string) (wificalling.VoiceCall, error) {
	return wificalling.VoiceCall{}, nil
}

func (fakeWiFiCalling) AnswerCall(context.Context, *mmodem.Modem, string) (wificalling.VoiceCall, error) {
	return wificalling.VoiceCall{}, nil
}

func (fakeWiFiCalling) RejectCall(context.Context, *mmodem.Modem, string) (wificalling.VoiceCall, error) {
	return wificalling.VoiceCall{}, nil
}

func (fakeWiFiCalling) HangupCall(context.Context, *mmodem.Modem, string) (wificalling.VoiceCall, error) {
	return wificalling.VoiceCall{}, nil
}

func (fakeWiFiCalling) OpenCallMedia(context.Context, *mmodem.Modem, string) (wificalling.MediaSession, error) {
	return nil, nil
}

func (fakeWiFiCalling) SubscribeVoiceEvents(wificalling.VoiceEventFunc) func() {
	return func() {}
}
