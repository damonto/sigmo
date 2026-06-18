//go:build wifi_calling

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
