package call

type DialRequest struct {
	To    string `json:"to"`
	Route string `json:"route"`
}

type UpdateCallRequest struct {
	State  string `json:"state"`
	Reason string `json:"reason"`
	Hold   string `json:"hold"`
}

type SendDTMFRequest struct {
	Digits string `json:"digits"`
}

type CallResponse struct {
	ID         string `json:"callID"`
	Route      string `json:"route"`
	Direction  string `json:"direction"`
	Number     string `json:"number"`
	State      string `json:"state"`
	Hold       string `json:"hold"`
	Reason     string `json:"reason"`
	StartedAt  string `json:"startedAt"`
	AnsweredAt string `json:"answeredAt"`
	EndedAt    string `json:"endedAt"`
	UpdatedAt  string `json:"updatedAt"`
}

type EventMessage struct {
	Type string       `json:"type"`
	Call CallResponse `json:"call"`
}

type WebRTCSessionDescriptionRequest struct {
	Type string `json:"type" validate:"required"`
	SDP  string `json:"sdp" validate:"required"`
}

type WebRTCSessionDescriptionResponse struct {
	Type string `json:"type"`
	SDP  string `json:"sdp"`
}
