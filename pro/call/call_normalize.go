//go:build ims

package call

import (
	"strings"
	"time"

	"github.com/damonto/sigmo/internal/pkg/storage"
	pims "github.com/damonto/sigmo/pro/ims"
)

func callFromIMS(call pims.VoiceCall) storage.Call {
	state := strings.TrimSpace(call.State)
	route := strings.TrimSpace(call.Route)
	if route == "" {
		route = RouteWiFiCalling
	}
	now := time.Now()
	updatedAt := call.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = now
	}
	startedAt := call.StartedAt
	if startedAt.IsZero() {
		startedAt = updatedAt
	}
	return storage.Call{
		ID:         call.ID,
		ProfileID:  call.ProfileID,
		ModemID:    call.ModemID,
		Route:      route,
		Direction:  call.Direction,
		Number:     call.Number,
		State:      state,
		Hold:       normalizeHold(call.Hold),
		Reason:     call.Reason,
		StartedAt:  startedAt,
		AnsweredAt: call.AnsweredAt,
		EndedAt:    call.EndedAt,
		UpdatedAt:  updatedAt,
	}
}

func isUSSDDialString(number string) bool {
	return strings.HasPrefix(number, "*") || strings.HasPrefix(number, "#")
}

func normalizeDialString(number string) (string, error) {
	normalized, ok := compactDialString(number)
	if !ok {
		return "", ErrInvalidNumber
	}
	if normalized == "" {
		return "", ErrNumberRequired
	}
	return normalized, nil
}

func compactDialString(value string) (string, bool) {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '+' && b.Len() == 0:
			b.WriteRune(r)
		case r == ' ', r == '-', r == '.', r == '(', r == ')':
		default:
			return "", false
		}
	}
	number := b.String()
	if number == "+" {
		return "", false
	}
	return number, true
}
