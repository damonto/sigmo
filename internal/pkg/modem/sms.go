package modem

import (
	"slices"
	"time"

	wwanmodem "github.com/damonto/wwan-go/modem"
)

type MessageRef = wwanmodem.MessageRef
type MessageStorage = wwanmodem.MessageStorage

type SMS struct {
	Generation        uint64
	Refs              []MessageRef
	MessageReferences []uint32
	State             SMSState
	Storage           SMSStorage
	Number            string
	Text              string
	Timestamp         time.Time
}

func smsFromWWAN(modem *Modem, message wwanmodem.Message) *SMS {
	generation := uint64(0)
	if modem != nil {
		generation = modem.Generation()
	}
	return &SMS{Generation: generation, Refs: slices.Clone(message.Refs), State: legacySMSState(message.State), Storage: legacySMSStorage(message.Storage), Number: message.Number, Text: message.Text, Timestamp: message.Timestamp}
}

func legacySMSState(state wwanmodem.MessageState) SMSState {
	switch state {
	case wwanmodem.MessageStateReceivedUnread, wwanmodem.MessageStateReceivedRead:
		return SMSStateReceived
	case wwanmodem.MessageStateStoredSent:
		return SMSStateSent
	case wwanmodem.MessageStateStoredUnsent:
		return SMSStateStored
	default:
		return SMSStateUnknown
	}
}
