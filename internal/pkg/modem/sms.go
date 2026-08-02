package modem

import (
	"slices"
	"time"

	wwanmodem "github.com/damonto/wwan-go/modem"
)

type MessageRef = wwanmodem.MessageRef

type SMS struct {
	Generation        uint64
	Refs              []MessageRef
	MessageReferences []uint32
	State             wwanmodem.MessageState
	Storage           wwanmodem.MessageStorage
	Number            string
	Text              string
	Timestamp         time.Time
}

func smsFromWWAN(modem *Modem, message wwanmodem.Message) *SMS {
	generation := uint64(0)
	if modem != nil {
		generation = modem.Generation()
	}
	return &SMS{Generation: generation, Refs: slices.Clone(message.Refs), State: message.State, Storage: message.Storage, Number: message.Number, Text: message.Text, Timestamp: message.Timestamp}
}
