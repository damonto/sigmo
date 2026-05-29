package message

import "errors"

var (
	ErrParticipantRequired = errors.New("participant is required")
	ErrRecipientRequired   = errors.New("recipient is required")
	ErrRecipientInvalid    = errors.New("invalid recipient")
	ErrTextRequired        = errors.New("text is required")
)
