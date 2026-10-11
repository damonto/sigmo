package message

import "time"

// MessageResponse preserves the legacy SMS representation and adds stable history identity.
type MessageResponse struct {
	ID             int64     `json:"id" jsonschema:"Sigmo database identifier for this message"`
	ConversationID string    `json:"conversationId" jsonschema:"stable conversation identifier within this backend instance"`
	ProfileID      string    `json:"profileId" jsonschema:"SIM profile identifier owning the message"`
	Sender         string    `json:"sender" jsonschema:"message sender address or phone number"`
	Recipient      string    `json:"recipient" jsonschema:"message recipient address or phone number"`
	Text           string    `json:"text" jsonschema:"SMS message body"`
	Timestamp      time.Time `json:"timestamp" jsonschema:"message timestamp in UTC"`
	Status         string    `json:"status" jsonschema:"delivery or storage status reported for this message"`
	Incoming       bool      `json:"incoming" jsonschema:"whether the message was received by the modem"`
	Routed         bool      `json:"routed" jsonschema:"whether Sigmo has already processed this message through its configured route"`
}

// SendMessageRequest optionally binds a submission to the SIM selected by the client.
type SendMessageRequest struct {
	To                string `json:"to"`
	Text              string `json:"text"`
	ExpectedProfileID string `json:"expectedProfileId,omitempty"`
}

// SendMessageResponse reports the canonical recipient and the submitted conversation.
type SendMessageResponse struct {
	To             string `json:"to" jsonschema:"normalized recipient address used to send the SMS"`
	ConversationID string `json:"conversationId,omitempty" jsonschema:"stable conversation identifier for the expected SIM profile and canonical recipient"`
}
