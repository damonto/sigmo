package call

type DialRequest struct {
	To    string `json:"to"`
	Route string `json:"route"`
}

type UpdateCallRequest struct {
	State  string `json:"state"`
	Reason string `json:"reason"`
}

type CallResponse struct {
	ID         string `json:"callID"`
	Route      string `json:"route"`
	Direction  string `json:"direction"`
	Number     string `json:"number"`
	State      string `json:"state"`
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

type MediaInfoResponse struct {
	Codec           string `json:"codec"`
	PayloadType     int    `json:"payloadType"`
	ClockRate       int    `json:"clockRate"`
	Channels        int    `json:"channels"`
	DTMFPayloadType int    `json:"dtmfPayloadType"`
	DTMFClockRate   int    `json:"dtmfClockRate"`
	PTimeMillis     int    `json:"ptimeMillis"`
}

type MediaMessage struct {
	Type  string            `json:"type"`
	Media MediaInfoResponse `json:"media"`
}
