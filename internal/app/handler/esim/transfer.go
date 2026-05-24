//go:build esim_transfer

package esim

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	sgp22 "github.com/damonto/euicc-go/v2"
	"github.com/labstack/echo/v5"

	"github.com/damonto/sigmo/internal/app/esimtransfer"
	"github.com/damonto/sigmo/internal/app/httpapi"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

const (
	errorCodeListTransferSourcesFailed  = "list_transfer_sources_failed"
	errorCodeListTransferProfilesFailed = "list_transfer_profiles_failed"
	errorCodeTransferInvalidRequest     = "transfer_invalid_request"
	errorCodeTransferSourceIMEIRequired = "transfer_source_imei_required"
	errorCodeTransferSourceNotFound     = "transfer_source_not_found"
	errorCodeTransferSourceUnsupported  = "transfer_source_unsupported"
	errorCodeTransferESIMFailed         = "transfer_esim_failed"
)

func (h *Handler) transferService() *esimtransfer.Service {
	return esimtransfer.New(esimtransfer.Config{
		Store:         h.provisioning.store,
		Registry:      h.registry,
		EnableProfile: h.enableTransferProfile,
		DeleteProfile: h.deleteTransferProfile,
	})
}

func (h *Handler) TransferSources(c *echo.Context) error {
	ctx := c.Request().Context()
	target, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeListTransferSourcesFailed)
	}
	response, err := h.transferService().Sources(ctx, target)
	if err != nil {
		return httpapi.Internal(c, errorCodeListTransferSourcesFailed, err)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) TransferProfiles(c *echo.Context) error {
	ctx := c.Request().Context()
	target, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeListTransferProfilesFailed)
	}
	var req esimtransfer.ProfilesRequest
	if err := c.Bind(&req); err != nil {
		return httpapi.BadRequest(c, errorCodeTransferInvalidRequest, err)
	}
	profiles, err := h.transferService().Profiles(ctx, target, req)
	if err != nil {
		return transferProfileError(c, err)
	}
	return c.JSON(http.StatusOK, profiles)
}

func (h *Handler) Transfer(c *echo.Context) error {
	ctx := c.Request().Context()
	target, err := h.registry.Find(ctx, c.Param("id"))
	if err != nil {
		return httpapi.ModemLookupError(c, err, errorCodeTransferESIMFailed)
	}
	conn, err := wsUpgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	return h.transferService().Serve(ctx, conn, target)
}

func transferProfileError(c *echo.Context, err error) error {
	if errors.Is(err, esimtransfer.ErrSourceIMEIRequired) {
		return httpapi.BadRequest(c, errorCodeTransferSourceIMEIRequired, err)
	}
	if errors.Is(err, mmodem.ErrNotFound) {
		return httpapi.NotFound(c, errorCodeTransferSourceNotFound, err)
	}
	if errors.Is(err, esimtransfer.ErrSourceUnsupported) {
		return httpapi.BadRequest(c, errorCodeTransferSourceUnsupported, err)
	}
	if errors.Is(err, esimtransfer.ErrSourceIsTarget) {
		return httpapi.BadRequest(c, errorCodeTransferInvalidRequest, err)
	}
	return httpapi.Internal(c, errorCodeListTransferProfilesFailed, err)
}

func (h *Handler) enableTransferProfile(ctx context.Context, modem *mmodem.Modem, iccid sgp22.ICCID) error {
	session, err := h.lifecycle.PrepareEnable(modem, iccid)
	if err != nil {
		if errors.Is(err, errProfileAlreadyActive) {
			return nil
		}
		return err
	}
	defer session.Close()
	sessionCtx, cancel := context.WithTimeout(ctx, enableTimeout)
	defer cancel()
	if err := h.internet.Restore(sessionCtx, modem); err != nil {
		return fmt.Errorf("restore internet connection: %w", err)
	}
	return session.Enable(sessionCtx)
}

func (h *Handler) deleteTransferProfile(ctx context.Context, modem *mmodem.Modem, iccid sgp22.ICCID) error {
	return h.lifecycle.Delete(modem, iccid)
}
