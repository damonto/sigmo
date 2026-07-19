package message

import "time"

type MessageResponse struct {
	ID        int64     `json:"id" jsonschema:"Sigmo database identifier for this message"`
	Sender    string    `json:"sender" jsonschema:"message sender address or phone number"`
	Recipient string    `json:"recipient" jsonschema:"message recipient address or phone number"`
	Text      string    `json:"text" jsonschema:"SMS message body"`
	Timestamp time.Time `json:"timestamp" jsonschema:"message timestamp in UTC"`
	Status    string    `json:"status" jsonschema:"delivery or storage status reported for this message"`
	Incoming  bool      `json:"incoming" jsonschema:"whether the message was received by the modem"`
	Routed    bool      `json:"routed" jsonschema:"whether Sigmo has already processed this message through its configured route"`
}

type SendMessageRequest struct {
	To   string `json:"to"`
	Text string `json:"text"`
}

type SendMessageResponse struct {
	To string `json:"to" jsonschema:"normalized recipient address used to send the SMS"`
}
