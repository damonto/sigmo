package message

import "time"

type MessageResponse struct {
	ID        int64     `json:"id"`
	Sender    string    `json:"sender"`
	Recipient string    `json:"recipient"`
	Text      string    `json:"text"`
	Timestamp time.Time `json:"timestamp"`
	Status    string    `json:"status"`
	Incoming  bool      `json:"incoming"`
	Routed    bool      `json:"routed"`
}

type SendMessageRequest struct {
	To   string `json:"to"`
	Text string `json:"text"`
}

type SendMessageResponse struct {
	To string `json:"to"`
}
