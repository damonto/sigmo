package esim

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	elpa "github.com/damonto/euicc-go/lpa"
	sgp22 "github.com/damonto/euicc-go/v2"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v5"

	appesim "github.com/damonto/sigmo/internal/app/esim"
	"github.com/damonto/sigmo/internal/app/httpapi"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

// DownloadHandler owns the interactive WebSocket download protocol.
type DownloadHandler struct {
	registry *mmodem.Registry
	service  *appesim.Service
}

// NewDownloadHandler creates interactive download endpoints.
func NewDownloadHandler(registry *mmodem.Registry, service *appesim.Service) *DownloadHandler {
	return &DownloadHandler{registry: registry, service: service}
}

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

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

func (h *DownloadHandler) Download(c *echo.Context) error {
	modem, err := h.registry.Find(c.Request().Context(), c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, "download_esim_failed")
	}

	conn, err := wsUpgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	stopClose := context.AfterFunc(c.Request().Context(), func() { _ = conn.Close() }) // Unblock reads when authorization expires.
	defer stopClose()

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

	session := newDownloadSession(downloadCtx, conn, cancel)
	defer session.close()

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

	if err := h.service.Download(downloadCtx, modem, start.SEID, activationCode, opts); err != nil {
		_ = session.send(downloadServerMessage{Type: wsTypeError, Message: err.Error()})
		return nil
	}

	_ = session.send(downloadServerMessage{Type: wsTypeCompleted})
	return nil
}

func readStartMessage(conn *websocket.Conn) (downloadClientMessage, error) {
	conn.SetReadLimit(downloadMessageLimit)
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
