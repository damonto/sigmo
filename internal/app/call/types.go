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

// DialRequest optionally binds an outgoing call to the client's selected SIM.
type DialRequest struct {
	ClientID          string `json:"clientId,omitempty"`
	ExpectedProfileID string `json:"expectedProfileId,omitempty"`
	To                string `json:"to"`
	Route             string `json:"route"`
}

// UpdateCallRequest associates a state or hold change with its controlling client.
type UpdateCallRequest struct {
	ClientID string `json:"clientId,omitempty"`
	State    string `json:"state"`
	Reason   string `json:"reason"`
	Hold     string `json:"hold"`
}

// SendDTMFRequest keeps ordered keypad input on the owning endpoint.
type SendDTMFRequest struct {
	ClientID string `json:"clientId,omitempty"`
	Digits   string `json:"digits"`
}

// CallResponse shares stable call identity and ownership with web and native clients.
type CallResponse struct {
	ModemID          string `json:"modemId" jsonschema:"original modem identifier; retained when history is assigned to another SIM"`
	LineLabel        string `json:"lineLabel" jsonschema:"display label of the original SIM"`
	HistoryProfileID string `json:"historyProfileId" jsonschema:"SIM currently owning the archived record"`
	BackendID        string `json:"backendId" jsonschema:"persistent backend instance identifier"`
	CallUUID         string `json:"callUUID" jsonschema:"stable UUID shared by call snapshots and native incoming-call invitations"`
	ProfileID        string `json:"profileId" jsonschema:"SIM profile identifier for the original call line"`
	OwnerClientID    string `json:"ownerClientId" jsonschema:"exclusive controlling client identifier; empty until the call is claimed"`
	ID               string `json:"callID" jsonschema:"Sigmo call-record identifier; use this exact value as callId when deleting the record"`
	Route            string `json:"route" jsonschema:"call transport route, such as VoLTE or Wi-Fi Calling"`
	Direction        string `json:"direction" jsonschema:"call direction: incoming or outgoing"`
	Number           string `json:"number" jsonschema:"remote party phone number"`
	State            string `json:"state" jsonschema:"last recorded call state"`
	Hold             string `json:"hold" jsonschema:"last recorded hold state; none when the call was not held"`
	Reason           string `json:"reason" jsonschema:"termination or state-change reason; empty when unavailable"`
	StartedAt        string `json:"startedAt" jsonschema:"call start time as an RFC 3339 timestamp"`
	AnsweredAt       string `json:"answeredAt" jsonschema:"answer time as an RFC 3339 timestamp; empty when unanswered"`
	EndedAt          string `json:"endedAt" jsonschema:"end time as an RFC 3339 timestamp; empty while not ended"`
	UpdatedAt        string `json:"updatedAt" jsonschema:"last record update time as an RFC 3339 timestamp"`
}

type EventMessage struct {
	Type string       `json:"type"`
	Call CallResponse `json:"call"`
}

// DTMFMessage exposes a remote key on the call WebSocket without changing call state.
type DTMFMessage struct {
	Type   string `json:"type"`
	CallID string `json:"callID"`
	Digit  string `json:"digit"`
	At     string `json:"at"`
}

type WebRTCICEServersResponse struct {
	ICEServers []WebRTCICEServerResponse `json:"iceServers"`
}

type WebRTCICEServerResponse struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// WebRTCSignalMessage carries signaling and identifies the client when opening media.
type WebRTCSignalMessage struct {
	ClientID  string                    `json:"clientId,omitempty"`
	Code      string                    `json:"code,omitempty"`
	Type      string                    `json:"type"`
	Offer     *WebRTCSessionDescription `json:"offer,omitempty"`
	Answer    *WebRTCSessionDescription `json:"answer,omitempty"`
	Candidate *WebRTCICECandidate       `json:"candidate,omitempty"`
	Message   string                    `json:"message,omitempty"`
}
