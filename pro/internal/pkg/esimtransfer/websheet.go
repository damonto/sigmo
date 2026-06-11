//go:build esim_transfer

package esimtransfer

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/damonto/ts43-go"

	"github.com/damonto/sigmo/pro/internal/pkg/websheet"
)

func (s *Service) handleWebsheet(ctx context.Context, session *session, active *activeSession, result *ts43.Result, event ts43.WebsheetEvent) (*ts43.Result, error) {
	if s.websheets == nil {
		return result, errWebsheetUnavailable
	}
	websheetSession, err := s.websheets.Create(ctx, websheet.Request{
		URL:         event.Websheet.URL,
		UserData:    event.Websheet.UserData,
		ContentType: event.Websheet.ContentsType,
		Title:       "Carrier websheet",
	})
	if err != nil {
		return result, err
	}
	defer s.websheets.Delete(websheetSession.Info().ID)

	info := websheetSession.Info()
	session.sendIfConnected(serverMessage{Type: wsTypeProgress, Stage: stageWebsheet})
	session.sendIfConnected(serverMessage{Type: wsTypeWebsheet, Websheet: &info})

	callback, err := websheetSession.WaitCallback(ctx)
	if err != nil {
		return result, err
	}
	answer, err := ts43WebsheetResult(callback)
	if err != nil {
		return result, err
	}
	next, err := active.client.Continue(ctx, result, ts43.ContinueRequest{Websheet: answer})
	if err != nil {
		if errors.Is(err, ts43.ErrWebsheetDismissed) {
			return next, errCarrierDismissed
		}
		return next, err
	}
	return next, nil
}

func ts43WebsheetResult(callback websheet.Callback) (*ts43.WebsheetResult, error) {
	switch normalizeCallbackName(callback.Event) {
	case "profilereadywithactivationcode", "activation":
		return &ts43.WebsheetResult{
			Event:          ts43.WebsheetEventProfileReadyWithActivationCode,
			ActivationCode: callback.ActivationCode,
			ICCID:          callback.ICCID,
			IMEI:           callback.IMEI,
		}, nil
	case "profilereadywithdefaultsmdp", "defaultsmdp":
		return &ts43.WebsheetResult{
			Event:    ts43.WebsheetEventProfileReadyWithDefaultSMDP,
			SMDPFQDN: firstNonEmpty(callback.DefaultSMDPAddress, callback.SMDPFQDN),
			ICCID:    callback.ICCID,
			IMEI:     callback.IMEI,
		}, nil
	case "selectioncompleted", "select":
		return &ts43.WebsheetResult{
			Event: ts43.WebsheetEventSelectionCompleted,
			ICCID: callback.ICCID,
			IMEI:  callback.IMEI,
		}, nil
	case "finishflow", "done":
		return &ts43.WebsheetResult{
			Event:      ts43.WebsheetEventFinishFlow,
			NextAction: ts43Operation(callback.NextAction),
		}, nil
	case "dismissflow", "dismiss":
		return &ts43.WebsheetResult{Event: ts43.WebsheetEventDismissFlow}, nil
	case "deletetoken":
		return &ts43.WebsheetResult{Event: ts43.WebsheetEventDeleteToken}, nil
	case "checkprofileservicestatus", "status":
		return &ts43.WebsheetResult{Event: ts43.WebsheetEventCheckProfileServiceStatus}, nil
	case "deleteprofileinuse", "deleteprofile":
		return &ts43.WebsheetResult{
			Event: ts43.WebsheetEventDeleteProfileInUse,
			ICCID: callback.ICCID,
		}, nil
	default:
		return nil, fmt.Errorf("unknown websheet callback %q", callback.Event)
	}
}

func ts43Operation(value string) ts43.Operation {
	switch normalizeCallbackName(value) {
	case "":
		return ""
	case "acquireconfiguration", "acquireconf", "configuration":
		return ts43.OperationAcquireConf
	case "managesubscription", "manage":
		return ts43.OperationManage
	default:
		return ts43.Operation(value)
	}
}

func normalizeCallbackName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimSuffix(name, "()")
	var b strings.Builder
	for _, r := range name {
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
