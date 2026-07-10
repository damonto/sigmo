//go:build ims

package call

import (
	"context"
	"log/slog"

	pims "github.com/damonto/sigmo/pro/ims"
)

func runVoiceEvents(ctx context.Context, voices []VoiceRoute, records *callRecords) error {
	if len(voices) == 0 {
		<-ctx.Done()
		return nil
	}
	var unsubscribers []func()
	for _, route := range voices {
		if route.Voice == nil {
			continue
		}
		unsubscribe := route.Voice.SubscribeVoiceEvents(func(event pims.VoiceEvent) {
			if event.Call.ID == "" {
				return
			}
			call := callFromIMS(event.Call)
			if _, err := records.saveAndPublish(ctx, call); err != nil {
				slog.Warn("save IMS voice event",
					"call_id", call.ID,
					"route", call.Route,
					"modem_id", call.ModemID,
					"profile_id", call.ProfileID,
					"state", call.State,
					"error", err,
				)
				records.events.publish(Event{Call: call})
				return
			}
		})
		unsubscribers = append(unsubscribers, unsubscribe)
	}
	defer func() {
		for _, unsubscribe := range unsubscribers {
			unsubscribe()
		}
	}()
	<-ctx.Done()
	return nil
}
