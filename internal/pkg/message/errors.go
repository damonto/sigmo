package message

import "errors"

var (
	// ErrProfileChanged prevents a reply from being sent through another SIM.
	ErrProfileChanged = errors.New("message SIM profile changed")
	// ErrSendUnconfirmed means a transport stopped before confirming the send result.
	ErrSendUnconfirmed     = errors.New("message send result is unconfirmed")
	ErrParticipantRequired = errors.New("participant is required")
	ErrRecipientRequired   = errors.New("recipient is required")
	ErrRecipientInvalid    = errors.New("invalid recipient")
	ErrTextRequired        = errors.New("text is required")
	ErrRouteUnavailable    = errors.New("message route is unavailable")
	ErrRouteNotConnected   = errors.New("message route is not connected")
)
