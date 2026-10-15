package modem

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

// SIMPINResponse describes PIN1 independently of the modem's radio state.
type SIMPINResponse struct {
	Supported  bool    `json:"supported" jsonschema:"whether the active SIM supports PIN1"`
	Enabled    *bool   `json:"enabled" jsonschema:"PIN protection; null when unknown"`
	Lock       string  `json:"lock" jsonschema:"none, pin, puk, permanently_blocked, other, or unknown"`
	PINRetries *uint32 `json:"pinRetries" jsonschema:"remaining PIN attempts; null when unknown"`
	PUKRetries *uint32 `json:"pukRetries" jsonschema:"remaining PUK attempts; null when unknown"`
	SIMToken   string  `json:"simToken" jsonschema:"opaque active SIM token required for writes"`
}

// NewSIMPINResponse converts a modem observation into the public contract.
func NewSIMPINResponse(s mmodem.SIMPINStatus) SIMPINResponse {
	return SIMPINResponse{Supported: s.Supported, Enabled: s.Enabled, Lock: string(s.Lock), PINRetries: s.PINRetries, PUKRetries: s.PUKRetries, SIMToken: s.SIMToken}
}

type setSIMPINProtectionRequest struct {
	Enabled  *bool  `json:"enabled"`
	PIN      string `json:"pin"`
	SIMToken string `json:"simToken"`
}

type changeSIMPINRequest struct {
	OldPIN   string `json:"oldPin"`
	NewPIN   string `json:"newPin"`
	SIMToken string `json:"simToken"`
}

// SIMPINStatus returns the active SIM's protection and verification state.
func (h *Handler) SIMPINStatus(c *echo.Context) error {
	device, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "sim_pin_unavailable")
	}
	status, err := device.SIMPINStatus(c.Request().Context())
	if err != nil {
		return simPINError(c, err)
	}
	return c.JSON(http.StatusOK, NewSIMPINResponse(status))
}

// SetSIMPINProtection enables or disables protection on the observed SIM.
func (h *Handler) SetSIMPINProtection(c *echo.Context) error {
	var req setSIMPINProtectionRequest
	if err := httpapi.BindJSON(c, &req, "sim_pin_invalid_request"); err != nil {
		return err
	}
	if req.Enabled == nil {
		return simPINError(c, mmodem.ErrSIMPINInvalid)
	}
	device, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "sim_pin_unavailable")
	}
	if err := device.SetSIMPINProtection(c.Request().Context(), mmodem.SIMPINProtection{Enabled: *req.Enabled, PIN: req.PIN, SIMToken: req.SIMToken}); err != nil {
		return simPINError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// ChangeSIMPIN replaces PIN1 on the observed SIM.
func (h *Handler) ChangeSIMPIN(c *echo.Context) error {
	var req changeSIMPINRequest
	if err := httpapi.BindJSON(c, &req, "sim_pin_invalid_request"); err != nil {
		return err
	}
	device, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "sim_pin_unavailable")
	}
	if err := device.ChangeSIMPIN(c.Request().Context(), mmodem.SIMPINChange{OldPIN: req.OldPIN, NewPIN: req.NewPIN, SIMToken: req.SIMToken}); err != nil {
		return simPINError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// SIMPINProblem returns only fixed, credential-free problem descriptions.
// HTTP and MCP share these codes; arbitrary protocol error text is never exposed.
func SIMPINProblem(err error) (status int, code, detail string) {
	problems := []struct {
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
	}
	for _, problem := range problems {
		if errors.Is(err, problem.err) {
			return problem.status, problem.code, problem.err.Error()
		}
	}
	if errors.Is(err, mmodem.ErrEnableAfterSIMUnlock) {
		return 500, "enable_modem_after_unlock_failed", "SIM unlocked, but the modem could not be enabled; turn off airplane mode without submitting credentials again"
	}
	return 0, "", ""
}

func simPINError(c *echo.Context, err error) error {
	status, code, detail := SIMPINProblem(err)
	if code == "" {
		status, code, detail = 503, "sim_pin_unavailable", mmodem.ErrSIMPINUnavailable.Error()
	}
	return httpapi.Error(c, status, code, detail)
}
