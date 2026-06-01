//go:build wifi_calling

package wificalling

import (
	"context"
	"errors"
	"strings"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/websheet"
	"github.com/damonto/vowifi-go/wfcsetup"
)

func (c *coordinator) StartWebsheet(ctx context.Context, modem *mmodem.Modem) (websheet.Info, error) {
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return websheet.Info{}, err
	}
	c.mu.Lock()
	session := c.sessions[modem.EquipmentIdentifier]
	if session == nil || session.profileID != profileID || session.websheet == nil {
		c.mu.Unlock()
		return websheet.Info{}, ErrWebsheetNotPending
	}
	info := session.websheet.Info()
	c.mu.Unlock()
	return info, nil
}

func (c *coordinator) wfcWebsheetRequest(err error) (websheet.Request, bool) {
	if c.websheets == nil || !errors.Is(err, wfcsetup.ErrUserActionRequired) {
		return websheet.Request{}, false
	}
	var setupErr *wfcsetup.Error
	if !errors.As(err, &setupErr) {
		return websheet.Request{}, false
	}
	return wfcWebsheetRequestFromResult(setupErr.Result)
}

func (c *coordinator) createWFCWebsheet(ctx context.Context, result wfcsetup.Result) (websheet.Info, error) {
	switch result.Action {
	case wfcsetup.ActionOpenWebsheet:
		req, ok := wfcWebsheetRequestFromResult(result)
		if !ok {
			return websheet.Info{}, ErrWebsheetUnavailable
		}
		session, err := c.websheets.Create(ctx, req)
		if err != nil {
			return websheet.Info{}, err
		}
		return session.Info(), nil
	case wfcsetup.ActionWait:
		return websheet.Info{}, ErrWFCSetupPending
	case wfcsetup.ActionDenied, wfcsetup.ActionDisableWFC:
		return websheet.Info{}, ErrWFCSetupDenied
	default:
		return websheet.Info{}, ErrWebsheetUnavailable
	}
}

func wfcWebsheetRequestFromResult(result wfcsetup.Result) (websheet.Request, bool) {
	sheet := result.Websheet
	if sheet == nil || strings.TrimSpace(sheet.URL) == "" {
		return websheet.Request{}, false
	}
	title := firstNonEmpty(sheet.Title, result.Carrier, "Wi-Fi Calling")
	if result.Scheme == wfcsetup.SchemeNSDS {
		return websheet.Request{
			URL:         strings.TrimSpace(sheet.URL),
			UserData:    wfcUserActionData(sheet.Data),
			ContentType: "application/x-www-form-urlencoded",
			Title:       title,
		}, true
	}
	return websheet.Request{
		URL:   wfcUserActionURL(sheet.URL, sheet.Data),
		Title: title,
	}, true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (c *coordinator) setWebsheet(modemID string, websheetSession *websheet.Session) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if session := c.sessions[modemID]; session != nil {
		session.websheet = websheetSession
		session.phase = sessionPhaseWebsheetRequired
	}
}

func (c *coordinator) waitForWebsheet(ctx context.Context, modemID string) error {
	c.mu.Lock()
	session := c.sessions[modemID]
	var websheetSession *websheet.Session
	if session != nil {
		websheetSession = session.websheet
	}
	c.mu.Unlock()
	if websheetSession == nil {
		return ErrWebsheetNotPending
	}
	for {
		callback, err := websheetSession.WaitCallback(ctx)
		if err != nil {
			return err
		}
		switch wfcWebsheetCallbackResult(callback) {
		case wfcWebsheetCallbackRetry:
			c.clearWebsheet(modemID, websheetSession)
			return nil
		case wfcWebsheetCallbackDismiss:
			c.clearWebsheet(modemID, websheetSession)
			return ErrWebsheetDismissed
		}
	}
}

func (c *coordinator) clearWebsheet(modemID string, websheetSession *websheet.Session) {
	c.mu.Lock()
	if session := c.sessions[modemID]; session != nil && session.websheet == websheetSession {
		session.websheet = nil
		session.phase = sessionPhaseConnecting
	}
	c.mu.Unlock()
	if c.websheets != nil {
		c.websheets.Delete(websheetSession.Info().ID)
	}
}

type wfcWebsheetCallbackAction int

const (
	wfcWebsheetCallbackWait wfcWebsheetCallbackAction = iota
	wfcWebsheetCallbackRetry
	wfcWebsheetCallbackDismiss
)

func wfcWebsheetCallbackResult(callback websheet.Callback) wfcWebsheetCallbackAction {
	event := normalizeWebsheetCallbackKey(firstNonEmpty(callback.Event, callback.Method, callback.ResultCode))
	method := normalizeWebsheetCallbackKey(callback.Method)
	result := normalizeWebsheetCallbackKey(callback.ResultCode)
	switch {
	case event == "dismissflow" || event == "cancel" || result == "cancel":
		return wfcWebsheetCallbackDismiss
	case strings.Contains(method, "cancel") || strings.Contains(method, "closewebview"):
		return wfcWebsheetCallbackDismiss
	case event == "entitlementchanged" || event == "finishflow" || event == "done" || event == "phoneservicesaccountstatuschanged" || result == "success":
		return wfcWebsheetCallbackRetry
	default:
		return wfcWebsheetCallbackWait
	}
}

func normalizeWebsheetCallbackKey(value string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	for _, r := range value {
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}
