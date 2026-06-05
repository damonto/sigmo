package call

import (
	"testing"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"

	"github.com/damonto/sigmo/internal/pkg/voicecodec"
)

func TestMediaBridgeCodec(t *testing.T) {
	tests := []struct {
		name     string
		info     MediaInfo
		wantAMR  voicecodec.AMRCodec
		wantPCMU bool
		wantErr  error
	}{
		{name: "amr", info: MediaInfo{Codec: "AMR"}, wantAMR: voicecodec.CodecAMR},
		{name: "amr wb", info: MediaInfo{Codec: "AMR-WB"}, wantAMR: voicecodec.CodecAMRWB},
		{name: "pcmu", info: MediaInfo{Codec: "PCMU"}, wantPCMU: true},
		{name: "unsupported", info: MediaInfo{Codec: "EVS"}, wantErr: ErrUnsupportedCodec},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mediaBridgeCodec(tt.info)
			if err != tt.wantErr {
				t.Fatalf("mediaBridgeCodec() error = %v, want %v", err, tt.wantErr)
			}
			if got.amr != tt.wantAMR || got.pcmu != tt.wantPCMU {
				t.Fatalf("mediaBridgeCodec() = %+v, want amr %q pcmu %v", got, tt.wantAMR, tt.wantPCMU)
			}
		})
	}
}

func TestRewriteRTPPacketPreservesTimestampDelta(t *testing.T) {
	tests := []struct {
		name           string
		inTimestamp    uint32
		firstTimestamp uint32
		timestampBase  uint32
		wantTimestamp  uint32
	}{
		{name: "first packet", inTimestamp: 1000, firstTimestamp: 1000, timestampBase: 90000, wantTimestamp: 90000},
		{name: "later packet", inTimestamp: 1480, firstTimestamp: 1000, timestampBase: 90000, wantTimestamp: 90480},
		{name: "wraparound", inTimestamp: 10, firstTimestamp: ^uint32(20), timestampBase: 90000, wantTimestamp: 90031},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := []byte{1, 2, 3}
			got := rewriteRTPPacket(
				rtp.Packet{
					Header: rtp.Header{
						Version:          2,
						Padding:          true,
						Extension:        true,
						Marker:           true,
						PayloadType:      104,
						SequenceNumber:   7,
						Timestamp:        tt.inTimestamp,
						SSRC:             1234,
						CSRC:             []uint32{11, 12},
						ExtensionProfile: 0xBEDE,
					},
					Payload: payload,
				},
				0,
				42,
				tt.timestampBase,
				tt.firstTimestamp,
				5678,
			)

			if got.PayloadType != 0 || got.SequenceNumber != 42 || got.Timestamp != tt.wantTimestamp || got.SSRC != 5678 {
				t.Fatalf("rewriteRTPPacket() header = %+v, want pt 0 seq 42 timestamp %d ssrc 5678", got.Header, tt.wantTimestamp)
			}
			if !got.Marker || !got.Padding || !got.Extension || got.ExtensionProfile != 0xBEDE || len(got.CSRC) != 2 {
				t.Fatalf("rewriteRTPPacket() dropped RTP header fields: %+v", got.Header)
			}
			if string(got.Payload) != string(payload) {
				t.Fatalf("rewriteRTPPacket() payload = %v, want %v", got.Payload, payload)
			}
		})
	}
}

func TestShouldCloseDisconnectedBridge(t *testing.T) {
	tests := []struct {
		name  string
		state webrtc.PeerConnectionState
		want  bool
	}{
		{name: "disconnected", state: webrtc.PeerConnectionStateDisconnected, want: true},
		{name: "connected", state: webrtc.PeerConnectionStateConnected, want: false},
		{name: "failed", state: webrtc.PeerConnectionStateFailed, want: false},
		{name: "closed", state: webrtc.PeerConnectionStateClosed, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldCloseDisconnectedBridge(tt.state); got != tt.want {
				t.Fatalf("shouldCloseDisconnectedBridge(%s) = %v, want %v", tt.state, got, tt.want)
			}
		})
	}
}

func TestBridgeActionForPeerState(t *testing.T) {
	tests := []struct {
		name  string
		state webrtc.PeerConnectionState
		want  webRTCBridgeAction
	}{
		{name: "new", state: webrtc.PeerConnectionStateNew, want: webRTCBridgeActionNone},
		{name: "checking", state: webrtc.PeerConnectionStateConnecting, want: webRTCBridgeActionNone},
		{name: "connected", state: webrtc.PeerConnectionStateConnected, want: webRTCBridgeActionReady},
		{name: "disconnected", state: webrtc.PeerConnectionStateDisconnected, want: webRTCBridgeActionGraceClose},
		{name: "failed", state: webrtc.PeerConnectionStateFailed, want: webRTCBridgeActionCloseNow},
		{name: "closed", state: webrtc.PeerConnectionStateClosed, want: webRTCBridgeActionCloseNow},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bridgeActionForPeerState(tt.state); got != tt.want {
				t.Fatalf("bridgeActionForPeerState(%s) = %v, want %v", tt.state, got, tt.want)
			}
		})
	}
}

func TestWebRTCSessionConnected(t *testing.T) {
	tests := []struct {
		name    string
		session *WebRTCSession
		connect bool
		want    bool
	}{
		{name: "nil session"},
		{name: "open session", session: &WebRTCSession{bridge: &webRTCBridge{}}},
		{name: "connected session", session: &WebRTCSession{bridge: &webRTCBridge{}}, connect: true, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.connect {
				tt.session.bridge.markConnected()
			}
			if got := tt.session.Connected(); got != tt.want {
				t.Fatalf("Connected() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWebRTCSessionCloseIfNotConnected(t *testing.T) {
	tests := []struct {
		name       string
		session    *WebRTCSession
		connect    bool
		wantClosed bool
	}{
		{name: "nil session"},
		{name: "missing bridge", session: &WebRTCSession{}},
		{name: "setup incomplete", session: &WebRTCSession{bridge: &webRTCBridge{localICE: make(chan WebRTCICECandidate)}}, wantClosed: true},
		{name: "already connected", session: &WebRTCSession{bridge: &webRTCBridge{localICE: make(chan WebRTCICECandidate)}}, connect: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.connect {
				tt.session.bridge.markConnected()
			}
			got := tt.session.CloseIfNotConnected()
			if got != tt.wantClosed {
				t.Fatalf("CloseIfNotConnected() = %v, want %v", got, tt.wantClosed)
			}
			if tt.session != nil && tt.session.bridge != nil && tt.session.bridge.closed != tt.wantClosed {
				t.Fatalf("bridge closed = %v, want %v", tt.session.bridge.closed, tt.wantClosed)
			}
		})
	}
}

func TestWebRTCBridgeSendsLocalICECandidates(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "candidate"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bridge := &webRTCBridge{
				localICE: make(chan WebRTCICECandidate, 1),
			}
			want := WebRTCICECandidate{Candidate: "candidate:1 1 udp 2130706431 192.0.2.10 40000 typ host"}
			bridge.sendLocalICECandidate(want)

			if got := <-bridge.localICE; got.Candidate != want.Candidate {
				t.Fatalf("local ICE candidate = %q, want %q", got.Candidate, want.Candidate)
			}
		})
	}
}
