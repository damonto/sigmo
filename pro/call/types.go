//go:build ims

package call

type WebRTCICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

type WebRTCSessionDescription struct {
	Type string `json:"type" validate:"required"`
	SDP  string `json:"sdp" validate:"required"`
}

type WebRTCICECandidate struct {
	Candidate        string  `json:"candidate" validate:"required"`
	SDPMid           *string `json:"sdpMid"`
	SDPMLineIndex    *uint16 `json:"sdpMLineIndex"`
	UsernameFragment *string `json:"usernameFragment"`
}

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
	ID         string `json:"callID" jsonschema:"Sigmo call-record identifier; use this exact value as callId when deleting the record"`
	Route      string `json:"route" jsonschema:"call transport route, such as VoLTE or Wi-Fi Calling"`
	Direction  string `json:"direction" jsonschema:"call direction: incoming or outgoing"`
	Number     string `json:"number" jsonschema:"remote party phone number"`
	State      string `json:"state" jsonschema:"last recorded call state"`
	Hold       string `json:"hold" jsonschema:"last recorded hold state; none when the call was not held"`
	Reason     string `json:"reason" jsonschema:"termination or state-change reason; empty when unavailable"`
	StartedAt  string `json:"startedAt" jsonschema:"call start time as an RFC 3339 timestamp"`
	AnsweredAt string `json:"answeredAt" jsonschema:"answer time as an RFC 3339 timestamp; empty when unanswered"`
	EndedAt    string `json:"endedAt" jsonschema:"end time as an RFC 3339 timestamp; empty while not ended"`
	UpdatedAt  string `json:"updatedAt" jsonschema:"last record update time as an RFC 3339 timestamp"`
}

type EventMessage struct {
	Type string       `json:"type"`
	Call CallResponse `json:"call"`
}

type WebRTCICEServersResponse struct {
	ICEServers []WebRTCICEServerResponse `json:"iceServers"`
}

type WebRTCICEServerResponse struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

type WebRTCSignalMessage struct {
	Type      string                    `json:"type"`
	Offer     *WebRTCSessionDescription `json:"offer,omitempty"`
	Answer    *WebRTCSessionDescription `json:"answer,omitempty"`
	Candidate *WebRTCICECandidate       `json:"candidate,omitempty"`
	Message   string                    `json:"message,omitempty"`
}
