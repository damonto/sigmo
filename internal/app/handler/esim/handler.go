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
	"github.com/labstack/echo/v4"

	"github.com/damonto/sigmo/internal/app/httpapi"
	"github.com/damonto/sigmo/internal/pkg/carrier"
	"github.com/damonto/sigmo/internal/pkg/config"
	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type Handler struct {
	cfg     *config.Config
	manager *mmodem.Manager
	service *Service
}

const (
	errorCodeModemNotFound                    = "modem_not_found"
	errorCodeEuiccNotSupported                = "euicc_not_supported"
	errorCodeListESIMsFailed                  = "list_esims_failed"
	errorCodeDiscoverESIMsFailed              = "discover_esims_failed"
	errorCodeICCIDRequired                    = "iccid_required"
	errorCodeInvalidICCID                     = "invalid_iccid"
	errorCodeEnableESIMTimeout                = "esim_enable_timeout"
	errorCodeEnableESIMFailed                 = "enable_esim_failed"
	errorCodeDeleteESIMFailed                 = "delete_esim_failed"
	errorCodeDownloadESIMFailed               = "download_esim_failed"
	errorCodeUpdateESIMNicknameInvalidRequest = "update_esim_nickname_invalid_request"
	errorCodeInvalidNickname                  = "invalid_nickname"
	errorCodeUpdateESIMNicknameFailed         = "update_esim_nickname_failed"
)

var (
	errModemNotFound = errors.New("modem not found")
	errICCIDRequired = errors.New("iccid is required")
	errInvalidICCID  = errors.New("invalid iccid")
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

const enableTimeout = time.Minute

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

func New(cfg *config.Config, manager *mmodem.Manager) *Handler {
	return &Handler{
		cfg:     cfg,
		manager: manager,
		service: NewService(cfg, manager),
	}
}

func (h *Handler) List(c echo.Context) error {
	modem, err := h.findModem(c.Param("id"))
	if err != nil {
		return h.modemLookupError(c, err, errorCodeListESIMsFailed)
	}
	response, err := h.service.List(modem)
	if err != nil {
		if errors.Is(err, lpa.ErrNoSupportedAID) {
			return httpapi.NotFound(c, errorCodeEuiccNotSupported, err)
		}
		return httpapi.Internal(c, errorCodeListESIMsFailed)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) Discover(c echo.Context) error {
	modem, err := h.findModem(c.Param("id"))
	if err != nil {
		return h.modemLookupError(c, err, errorCodeDiscoverESIMsFailed)
	}
	response, err := h.service.Discover(modem)
	if err != nil {
		if errors.Is(err, lpa.ErrNoSupportedAID) {
			return httpapi.NotFound(c, errorCodeEuiccNotSupported, err)
		}
		return httpapi.Internal(c, errorCodeDiscoverESIMsFailed)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) Enable(c echo.Context) error {
	modem, err := h.findModem(c.Param("id"))
	if err != nil {
		return h.modemLookupError(c, err, errorCodeEnableESIMFailed)
	}
	iccid, err := iccidFromParam(c)
	if err != nil {
		if errors.Is(err, errICCIDRequired) {
			return httpapi.BadRequest(c, errorCodeICCIDRequired, err)
		}
		return httpapi.BadRequest(c, errorCodeInvalidICCID, err)
	}
	ctx, cancel := context.WithTimeout(c.Request().Context(), enableTimeout)
	defer cancel()
	if err := h.service.Enable(ctx, modem, iccid); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return httpapi.RequestTimeout(c, errorCodeEnableESIMTimeout, errEnableTimeout)
		}
		if errors.Is(err, context.Canceled) {
			return nil
		}
		if errors.Is(err, lpa.ErrNoSupportedAID) {
			return httpapi.NotFound(c, errorCodeEuiccNotSupported, err)
		}
		return httpapi.Internal(c, errorCodeEnableESIMFailed)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) Delete(c echo.Context) error {
	modem, err := h.findModem(c.Param("id"))
	if err != nil {
		return h.modemLookupError(c, err, errorCodeDeleteESIMFailed)
	}
	iccid, err := iccidFromParam(c)
	if err != nil {
		if errors.Is(err, errICCIDRequired) {
			return httpapi.BadRequest(c, errorCodeICCIDRequired, err)
		}
		return httpapi.BadRequest(c, errorCodeInvalidICCID, err)
	}
	if err := h.service.Delete(modem, iccid); err != nil {
		if errors.Is(err, lpa.ErrNoSupportedAID) {
			return httpapi.NotFound(c, errorCodeEuiccNotSupported, err)
		}
		return httpapi.Internal(c, errorCodeDeleteESIMFailed)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) Download(c echo.Context) error {
	modem, err := h.findModem(c.Param("id"))
	if err != nil {
		return h.modemLookupError(c, err, errorCodeDownloadESIMFailed)
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

	activationCode, err := buildActivationCode(modem, start)
	if err != nil {
		_ = conn.WriteJSON(downloadServerMessage{Type: wsTypeError, Message: err.Error()})
		return nil
	}

	downloadCtx, cancel := context.WithCancel(context.Background())
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

	if err := h.service.Download(downloadCtx, modem, activationCode, opts); err != nil {
		_ = session.send(downloadServerMessage{Type: wsTypeError, Message: err.Error()})
		return nil
	}

	_ = session.send(downloadServerMessage{Type: wsTypeCompleted})
	return nil
}

func (h *Handler) UpdateNickname(c echo.Context) error {
	modem, err := h.findModem(c.Param("id"))
	if err != nil {
		return h.modemLookupError(c, err, errorCodeUpdateESIMNicknameFailed)
	}
	iccid, err := iccidFromParam(c)
	if err != nil {
		if errors.Is(err, errICCIDRequired) {
			return httpapi.BadRequest(c, errorCodeICCIDRequired, err)
		}
		return httpapi.BadRequest(c, errorCodeInvalidICCID, err)
	}
	var req UpdateNicknameRequest
	if err := httpapi.BindAndValidate(c, &req, errorCodeUpdateESIMNicknameInvalidRequest); err != nil {
		return err
	}
	if err := h.service.UpdateNickname(modem, iccid, req.Nickname); err != nil {
		if errors.Is(err, errInvalidNickname) {
			return httpapi.BadRequest(c, errorCodeInvalidNickname, err)
		}
		if errors.Is(err, lpa.ErrNoSupportedAID) {
			return httpapi.NotFound(c, errorCodeEuiccNotSupported, err)
		}
		return httpapi.Internal(c, errorCodeUpdateESIMNicknameFailed)
	}
	return c.NoContent(http.StatusNoContent)
}

func iccidFromParam(c echo.Context) (sgp22.ICCID, error) {
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
	return start, nil
}

func (h *Handler) modemLookupError(c echo.Context, err error, internalErrorCode string) error {
	if errors.Is(err, errModemNotFound) {
		return httpapi.NotFound(c, errorCodeModemNotFound, err)
	}
	return httpapi.Internal(c, internalErrorCode)
}

func (h *Handler) findModem(id string) (*mmodem.Modem, error) {
	modems, err := h.manager.Modems()
	if err != nil {
		return nil, fmt.Errorf("listing modems: %w", err)
	}
	for _, modem := range modems {
		if modem.EquipmentIdentifier == id {
			return modem, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", errModemNotFound, id)
}

func profilePreviewFrom(info *sgp22.ProfileInfo) downloadProfilePreview {
	carrierInfo := carrier.Lookup(info.ProfileOwner.MCC() + info.ProfileOwner.MNC())
	preview := downloadProfilePreview{
		ICCID:               info.ICCID.String(),
		ServiceProviderName: info.ServiceProviderName,
		ProfileName:         info.ProfileName,
		ProfileNickname:     info.ProfileNickname,
		ProfileState:        info.ProfileState.String(),
		RegionCode:          carrierInfo.Region,
	}
	if info.Icon.Valid() {
		preview.Icon = fmt.Sprintf("data:%s;base64,%s", info.Icon.FileType(), info.Icon.String())
	}
	return preview
}
