//go:build ims

package ims

import (
	"context"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

// WiFiCalling is the concrete Wi-Fi Calling access used by call routing.
type WiFiCalling struct {
	*voiceAccess
}

// VoLTE is the concrete LTE IMS access used by call routing.
type VoLTE struct {
	*voiceAccess
}

type voiceAccess struct {
	access *coordinator
}

type VoiceStatus struct {
	Connected bool
}

func (c *Connectivity) WiFiCalling() *WiFiCalling {
	if c == nil {
		return nil
	}
	return c.wifiCallingVoice
}

func (c *Connectivity) VoLTE() *VoLTE {
	if c == nil {
		return nil
	}
	return c.volteVoice
}

func (a *voiceAccess) Status(ctx context.Context, modem *mmodem.Modem) (VoiceStatus, error) {
	if a == nil || a.access == nil {
		return VoiceStatus{}, ErrUnavailable
	}
	connected, err := a.access.connected(ctx, modem)
	if err != nil {
		return VoiceStatus{}, err
	}
	return VoiceStatus{Connected: connected}, nil
}

func (a *voiceAccess) DialCall(ctx context.Context, modem *mmodem.Modem, number string) (VoiceCall, error) {
	if a == nil || a.access == nil {
		return VoiceCall{}, ErrUnavailable
	}
	return a.access.DialCall(ctx, modem, number)
}

func (a *voiceAccess) AnswerCall(ctx context.Context, modem *mmodem.Modem, callID string) (VoiceCall, error) {
	if a == nil || a.access == nil {
		return VoiceCall{}, ErrUnavailable
	}
	return a.access.AnswerCall(ctx, modem, callID)
}

func (a *voiceAccess) RejectCall(ctx context.Context, modem *mmodem.Modem, callID string) (VoiceCall, error) {
	if a == nil || a.access == nil {
		return VoiceCall{}, ErrUnavailable
	}
	return a.access.RejectCall(ctx, modem, callID)
}

func (a *voiceAccess) HangupCall(ctx context.Context, modem *mmodem.Modem, callID string) (VoiceCall, error) {
	if a == nil || a.access == nil {
		return VoiceCall{}, ErrUnavailable
	}
	return a.access.HangupCall(ctx, modem, callID)
}

func (a *voiceAccess) HoldCall(ctx context.Context, modem *mmodem.Modem, callID string) (VoiceCall, error) {
	if a == nil || a.access == nil {
		return VoiceCall{}, ErrUnavailable
	}
	return a.access.HoldCall(ctx, modem, callID)
}

func (a *voiceAccess) ResumeCall(ctx context.Context, modem *mmodem.Modem, callID string) (VoiceCall, error) {
	if a == nil || a.access == nil {
		return VoiceCall{}, ErrUnavailable
	}
	return a.access.ResumeCall(ctx, modem, callID)
}

func (a *voiceAccess) SendCallDTMF(ctx context.Context, modem *mmodem.Modem, callID, digits string) error {
	if a == nil || a.access == nil {
		return ErrUnavailable
	}
	return a.access.SendCallDTMF(ctx, modem, callID, digits)
}

func (a *voiceAccess) OpenCallMedia(ctx context.Context, modem *mmodem.Modem, callID string) (MediaSession, error) {
	if a == nil || a.access == nil {
		return nil, ErrUnavailable
	}
	return a.access.OpenCallMedia(ctx, modem, callID)
}

func (a *voiceAccess) SubscribeVoiceEvents(subscriber VoiceEventFunc) func() {
	if a == nil || a.access == nil {
		return func() {}
	}
	return a.access.SubscribeVoiceEvents(subscriber)
}
