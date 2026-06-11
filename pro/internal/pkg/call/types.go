//go:build wifi_calling

package call

type WebRTCICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

type WebRTCSessionDescription struct {
	Type string
	SDP  string
}

type WebRTCICECandidate struct {
	Candidate        string
	SDPMid           *string
	SDPMLineIndex    *uint16
	UsernameFragment *string
}
