package esim

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	elpa "github.com/damonto/euicc-go/lpa"
	sgp22 "github.com/damonto/euicc-go/v2"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/internet"
	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/reminder"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

type Config struct {
	Store     *settings.Store
	Registry  *mmodem.Registry
	Internet  *internet.Connector
	LPA       *lpa.Pool
	Reminders *reminder.Scheduler
	Storage   *storage.Store
}

type Handler struct {
	registry     *mmodem.Registry
	profile      *profile
	provisioning *provisioning
	lifecycle    *lifecycle
	internet     *internet.Connector
	reminders    *reminder.Scheduler
}

var (
	errICCIDRequired = errors.New("iccid is required")
	errInvalidICCID  = errors.New("invalid iccid")
	errEuiccBusy     = errors.New("eUICC is busy, retry later")
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// Some Qualcomm firmware needs nearly five minutes between accepting
// EnableProfile and exposing the replacement profile through UIM.
const enableTimeout = 5 * time.Minute

var errEnableTimeout = errors.New("enabling timed out, please refresh to confirm whether the profile is active")

const (
	wsTypeStart                    = "start"
	wsTypeProgress                 = "progress"
	wsTypePreview                  = "preview"
	wsTypeConfirm                  = "confirm"
	wsTypeConfirmationCode         = "confirmation_code"
	wsTypeConfirmationCodeRequired = "confirmation_code_required"
	wsTypeCancel                   = "cancel"
	wsTypeCompleted                = "completed"
	wsTypeError                    = "error"
)

func New(cfg Config) *Handler {
	profiles := newProfile(cfg.Store, cfg.Reminders, cfg.LPA)
	profiles.history = cfg.Storage
	return &Handler{
		registry:     cfg.Registry,
		profile:      profiles,
		provisioning: newProvisioning(cfg.LPA),
		lifecycle:    newLifecycle(cfg.Store, cfg.Registry, cfg.LPA),
		internet:     cfg.Internet,
		reminders:    cfg.Reminders,
	}
}

func (h *Handler) List(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "list_esims_failed")
	}
	response, err := h.profile.List(c.Request().Context(), modem)
	if err != nil {
		if errors.Is(err, lpa.ErrNoSupportedAID) {
			return httpapi.NotFound(c, "euicc_not_supported", err)
		}
		return httpapi.Internal(c, "list_esims_failed", err)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) Discovery(c *echo.Context) error {
	ctx := c.Request().Context()
	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "discover_esims_failed")
	}
	response, err := h.provisioning.Discovery(ctx, modem, c.Param("seId"))
	if err != nil {
		if seErr := seRequestError(c, err); seErr != nil {
			return seErr
		}
		if errors.Is(err, lpa.ErrNoSupportedAID) {
			return httpapi.NotFound(c, "euicc_not_supported", err)
		}
		return httpapi.Internal(c, "discover_esims_failed", err)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) Enable(c *echo.Context) error {
	ctx, cancel := context.WithTimeout(c.Request().Context(), enableTimeout)
	defer cancel()

	modem, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "enable_esim_failed")
	}
	iccid, err := iccidFromParam(c)
	if err != nil {
		if errors.Is(err, errICCIDRequired) {
			return httpapi.BadRequest(c, "iccid_required", err)
		}
		return httpapi.BadRequest(c, "invalid_iccid", err)
	}
	session, err := h.lifecycle.PrepareEnable(ctx, modem, c.Param("seId"), iccid)
	if err != nil {
		return enablePrepareError(c, err)
	}
	defer session.Close()

	if err := h.restoreInternetBeforeProfileEnable(ctx, modem); err != nil {
		return httpapi.Internal(c, "enable_esim_failed", err)
	}
	if err := session.Enable(ctx); err != nil {
		return enableError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) restoreInternetBeforeProfileEnable(ctx context.Context, modem *mmodem.Modem) error {
	if modem != nil && modem.Snapshot().Locked() {
		return nil
	}
	if h.internet == nil {
		return errors.New("internet connector is required")
	}
	return h.internet.Restore(ctx, modem)
}

func enablePrepareError(c *echo.Context, err error) error {
	if errors.Is(err, errProfileAlreadyActive) {
		return c.NoContent(http.StatusNoContent)
	}
	return enableError(c, err)
}

func enableError(c *echo.Context, err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return httpapi.RequestTimeout(c, "esim_enable_timeout", errEnableTimeout)
	}
	if errors.Is(err, context.Canceled) {
		return nil
	}
	if seErr := seRequestError(c, err); seErr != nil {
		return seErr
	}
	if errors.Is(err, lpa.ErrNoSupportedAID) {
		return httpapi.NotFound(c, "euicc_not_supported", err)
	}
	if errors.Is(err, sgp22.ErrCatBusy) {
		return httpapi.Error(c, http.StatusConflict, "esim_enable_busy", errEuiccBusy.Error())
	}
	if errors.Is(err, errProfileNotFound) {
		return httpapi.BadRequest(c, "esim_profile_not_found", err)
	}
	return httpapi.Internal(c, "enable_esim_failed", err)
}

func (h *Handler) Delete(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "delete_esim_failed")
	}
	iccid, err := iccidFromParam(c)
	if err != nil {
		if errors.Is(err, errICCIDRequired) {
			return httpapi.BadRequest(c, "iccid_required", err)
		}
		return httpapi.BadRequest(c, "invalid_iccid", err)
	}
	if err := h.DeleteProfile(c.Request().Context(), modem, c.Param("seId"), iccid); err != nil {
		if seErr := seRequestError(c, err); seErr != nil {
			return seErr
		}
		if errors.Is(err, lpa.ErrNoSupportedAID) {
			return httpapi.NotFound(c, "euicc_not_supported", err)
		}
		if errors.Is(err, errActiveProfileCannotDelete) {
			return httpapi.BadRequest(c, "active_esim_profile", err)
		}
		if errors.Is(err, errProfileNotFound) {
			return httpapi.BadRequest(c, "esim_profile_not_found", err)
		}
		return httpapi.Internal(c, "delete_esim_failed", err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) Download(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "download_esim_failed")
	}

	conn, err := wsUpgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	start, err := readStartMessage(conn)
	if err != nil {
		_ = conn.WriteJSON(downloadServerMessage{Type: wsTypeError, Message: err.Error()})
		return nil
	}

	activationCode, err := buildActivationCode(c.Request().Context(), modem, start)
	if err != nil {
		_ = conn.WriteJSON(downloadServerMessage{Type: wsTypeError, Message: err.Error()})
		return nil
	}

	downloadCtx, cancel := context.WithCancel(c.Request().Context())
	defer cancel()

	session := newDownloadSession(conn, cancel)

	opts := &elpa.DownloadOptions{
		OnProgress: func(stage elpa.DownloadStage) {
			session.sendIfConnected(downloadServerMessage{
				Type:  wsTypeProgress,
				Stage: stage.String(),
			})
		},
		OnConfirm: func(info *sgp22.ProfileInfo) bool {
			preview := profilePreviewFrom(info)
			if err := session.send(downloadServerMessage{
				Type:    wsTypePreview,
				Profile: &preview,
			}); err != nil {
				return false
			}
			return session.waitForConfirm(downloadCtx)
		},
		OnEnterConfirmationCode: func() string {
			session.sendIfConnected(downloadServerMessage{
				Type: wsTypeConfirmationCodeRequired,
			})
			code := session.waitForConfirmationCode(downloadCtx)
			return strings.TrimSpace(code)
		},
	}

	if err := h.provisioning.Download(downloadCtx, modem, start.SEID, activationCode, opts); err != nil {
		_ = session.send(downloadServerMessage{Type: wsTypeError, Message: err.Error()})
		return nil
	}

	_ = session.send(downloadServerMessage{Type: wsTypeCompleted})
	return nil
}

func (h *Handler) UpdateNickname(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "update_esim_nickname_failed")
	}
	iccid, err := iccidFromParam(c)
	if err != nil {
		if errors.Is(err, errICCIDRequired) {
			return httpapi.BadRequest(c, "iccid_required", err)
		}
		return httpapi.BadRequest(c, "invalid_iccid", err)
	}
	var req UpdateNicknameRequest
	if err := httpapi.BindAndValidate(c, &req, "update_esim_nickname_invalid_request"); err != nil {
		return err
	}
	if err := h.profile.UpdateNickname(c.Request().Context(), modem, c.Param("seId"), iccid, req.Nickname); err != nil {
		if seErr := seRequestError(c, err); seErr != nil {
			return seErr
		}
		if errors.Is(err, errInvalidNickname) {
			return httpapi.BadRequest(c, "invalid_nickname", err)
		}
		if errors.Is(err, lpa.ErrNoSupportedAID) {
			return httpapi.NotFound(c, "euicc_not_supported", err)
		}
		return httpapi.Internal(c, "update_esim_nickname_failed", err)
	}
	return c.NoContent(http.StatusNoContent)
}

func seRequestError(c *echo.Context, err error) error {
	if errors.Is(err, lpa.ErrSERequired) {
		return httpapi.BadRequest(c, "se_required", err)
	}
	if errors.Is(err, lpa.ErrSENotFound) {
		return httpapi.NotFound(c, "se_not_found", err)
	}
	return nil
}

func iccidFromParam(c *echo.Context) (sgp22.ICCID, error) {
	iccidParam := c.Param("iccid")
	if iccidParam == "" {
		return nil, errICCIDRequired
	}
	iccid, err := sgp22.NewICCID(iccidParam)
	if err != nil {
		return nil, fmt.Errorf("%w %q: %w", errInvalidICCID, iccidParam, err)
	}
	return iccid, nil
}

func readStartMessage(conn *websocket.Conn) (downloadClientMessage, error) {
	var start downloadClientMessage
	if err := conn.ReadJSON(&start); err != nil {
		return downloadClientMessage{}, err
	}
	if start.Type != "" && start.Type != wsTypeStart {
		return downloadClientMessage{}, fmt.Errorf("unexpected message type %q", start.Type)
	}
	if start.SMDP == "" {
		return downloadClientMessage{}, errors.New("smdp is required")
	}
	start.SEID = strings.TrimSpace(start.SEID)
	if start.SEID == "" {
		return downloadClientMessage{}, errors.New("seId is required")
	}
	return start, nil
}
