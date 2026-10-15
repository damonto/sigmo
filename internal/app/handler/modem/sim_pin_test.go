package modem

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/damonto/sigmo/internal/app/httpapi"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	wwan "github.com/damonto/wwan-go/modem"
	"github.com/labstack/echo/v5"
)

func TestSIMPINProblemContract(t *testing.T) {
	tests := []struct {
		err    error
		status int
		code   string
	}{
		{mmodem.ErrSIMPINInvalid, 400, "sim_pin_invalid_request"},
		{mmodem.ErrSIMIncorrectPIN, 422, "sim_pin_incorrect"},
		{mmodem.ErrSIMIncorrectPUK, 422, "sim_puk_incorrect"},
		{mmodem.ErrSIMPUKRequired, 409, "sim_puk_required"},
		{mmodem.ErrSIMPermanentlyBlocked, 409, "sim_permanently_blocked"},
		{mmodem.ErrSIMChanged, 409, "sim_target_changed"},
		{mmodem.ErrSIMPINState, 409, "sim_pin_state_conflict"},
		{mmodem.ErrSIMPINUnsupported, 501, "sim_pin_unsupported"},
		{mmodem.ErrSIMPINUnavailable, 503, "sim_pin_unavailable"},
		{mmodem.ErrSIMPINResultUnknown, 504, "sim_pin_result_unknown"},
		{mmodem.ErrEnableAfterSIMUnlock, 500, "enable_modem_after_unlock_failed"},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c := echo.New().NewContext(httptest.NewRequest("POST", "/api/v1/modems/modem/sim-unlocks", nil), rec)
			err := errors.Join(tt.err, errors.New("private credential 00123456"))
			if err := simPINError(c, err); err != nil {
				t.Fatal(err)
			}
			var problem httpapi.ErrorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
				t.Fatal(err)
			}
			if rec.Code != tt.status || problem.ErrorCode != tt.code {
				t.Fatalf("%d %+v", rec.Code, problem)
			}
			if strings.Contains(rec.Body.String(), "00123456") {
				t.Fatal("credential leaked")
			}
		})
	}
}

func TestSIMPINUnknownFieldsAndLegacyRequest(t *testing.T) {
	data, err := json.Marshal(NewSIMPINResponse(mmodem.SIMPINStatus{Supported: true, Lock: wwan.SIMLockUnknown, SIMToken: "opaque"}))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"enabled":null`, `"pinRetries":null`, `"pukRetries":null`} {
		if !strings.Contains(string(data), field) {
			t.Fatalf("missing %s in %s", field, data)
		}
	}
	var legacy UnlockSIMRequest
	if err := json.Unmarshal([]byte(`{"pin":"0001"}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.PIN != "0001" || legacy.PUK != "" || legacy.SIMToken != "" {
		t.Fatal("legacy request changed")
	}
	var protection setSIMPINProtectionRequest
	if err := json.Unmarshal([]byte(`{"enabled":false,"pin":"0001","simToken":"opaque"}`), &protection); err != nil {
		t.Fatal(err)
	}
	if protection.Enabled == nil || *protection.Enabled {
		t.Fatal("explicit disable missing")
	}
}
