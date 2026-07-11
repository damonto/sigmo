//go:build ims

package call

import (
	"bytes"
	"testing"

	imsvoice "github.com/damonto/ims-go/ims/voice"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"

	"github.com/damonto/sigmo/pro/voicecodec"
)

func TestEVSPayloadRoundTrip(t *testing.T) {
	tests := []struct {
		name           string
		headerFullOnly bool
		wantHeaderFull bool
		frame          voicecodec.EVSFrame
	}{
		{name: "compact native", frame: voicecodec.EVSFrame{FrameType: 4, Quality: true, Data: make([]byte, 33)}},
		{name: "header full native", headerFullOnly: true, wantHeaderFull: true, frame: voicecodec.EVSFrame{FrameType: 4, Quality: true, Data: make([]byte, 33)}},
		{name: "compact amr wb io", frame: voicecodec.EVSFrame{FrameType: 2, Quality: true, AMRWBIO: true, Data: make([]byte, 32)}},
		{name: "header full amr wb io", headerFullOnly: true, wantHeaderFull: true, frame: voicecodec.EVSFrame{FrameType: 2, Quality: true, AMRWBIO: true, Data: make([]byte, 32)}},
		{name: "native sid", wantHeaderFull: false, frame: voicecodec.EVSFrame{FrameType: 12, Quality: true, Data: make([]byte, 6)}},
		{name: "amr wb io sid", wantHeaderFull: true, frame: voicecodec.EVSFrame{FrameType: 9, Quality: true, AMRWBIO: true, Data: make([]byte, 5)}},
		{name: "native no data", wantHeaderFull: true, frame: voicecodec.EVSFrame{FrameType: 15, Quality: true}},
		{name: "native speech lost", wantHeaderFull: true, frame: voicecodec.EVSFrame{FrameType: 14}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, err := encodeEVSPayload(tt.frame, tt.headerFullOnly)
			if err != nil {
				t.Fatalf("encodeEVSPayload() error = %v", err)
			}
			decodedPayload, err := (imsvoice.EVSPayloadCodec{HeaderFullOnly: tt.headerFullOnly}).Unmarshal(payload)
			if err != nil {
				t.Fatalf("EVSPayloadCodec.Unmarshal() error = %v", err)
			}
			if decodedPayload.HeaderFull != tt.wantHeaderFull {
				t.Fatalf("HeaderFull = %v, want %v", decodedPayload.HeaderFull, tt.wantHeaderFull)
			}
			frames, err := decodeEVSPayload(payload, tt.headerFullOnly)
			if err != nil {
				t.Fatalf("decodeEVSPayload() error = %v", err)
			}
			if len(frames) != 1 {
				t.Fatalf("frames = %d, want 1", len(frames))
			}
			got := frames[0]
			if got.FrameType != tt.frame.FrameType || got.Quality != tt.frame.Quality || got.AMRWBIO != tt.frame.AMRWBIO || !bytes.Equal(got.Data, tt.frame.Data) {
				t.Fatalf("frame = %+v, want %+v", got, tt.frame)
			}
		})
	}
}

func TestEVSRTPWriterAdvancesFrameTiming(t *testing.T) {
	tests := []struct {
		name           string
		headerFullOnly bool
	}{
		{name: "compact"},
		{name: "header full", headerFullOnly: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writer := newEVSRTPWriter(127, tt.headerFullOnly, 42, 90000, 5678)
			frame := voicecodec.EVSFrame{FrameType: 4, Quality: true, Data: make([]byte, 33)}
			for i := range 2 {
				data, err := writer.packet(frame)
				if err != nil {
					t.Fatalf("packet() error = %v", err)
				}
				var packet rtp.Packet
				if err := packet.Unmarshal(data); err != nil {
					t.Fatalf("RTP Unmarshal() error = %v", err)
				}
				wantSequence := uint16(42 + i)
				wantTimestamp := uint32(90000 + i*voicecodec.EVSSamplesPerFrame)
				if packet.PayloadType != 127 || packet.SequenceNumber != wantSequence || packet.Timestamp != wantTimestamp || packet.SSRC != 5678 {
					t.Fatalf("RTP header = %+v, want pt 127 seq %d timestamp %d ssrc 5678", packet.Header, wantSequence, wantTimestamp)
				}
				writer.commit()
			}
		})
	}
}

func TestEVSRTPWriterDoesNotAdvanceBeforeWrite(t *testing.T) {
	tests := []struct {
		name           string
		headerFullOnly bool
	}{
		{name: "compact"},
		{name: "header full", headerFullOnly: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writer := newEVSRTPWriter(127, tt.headerFullOnly, 42, 90000, 5678)
			frame := voicecodec.EVSFrame{FrameType: 4, Quality: true, Data: make([]byte, 33)}
			for range 2 {
				data, err := writer.packet(frame)
				if err != nil {
					t.Fatalf("packet() error = %v", err)
				}
				var packet rtp.Packet
				if err := packet.Unmarshal(data); err != nil {
					t.Fatalf("RTP Unmarshal() error = %v", err)
				}
				if packet.SequenceNumber != 42 || packet.Timestamp != 90000 {
					t.Fatalf("RTP timing = %d/%d, want unchanged 42/90000", packet.SequenceNumber, packet.Timestamp)
				}
			}
		})
	}
}

func TestMediaBridgeCodec(t *testing.T) {
	tests := []struct {
		name     string
		info     MediaInfo
		wantAMR  voicecodec.AMRCodec
		wantEVS  bool
		wantPCMU bool
		wantErr  error
	}{
		{name: "amr", info: MediaInfo{Codec: "AMR"}, wantAMR: voicecodec.CodecAMR},
		{name: "amr wb", info: MediaInfo{Codec: "AMR-WB"}, wantAMR: voicecodec.CodecAMRWB},
		{name: "evs", info: MediaInfo{Codec: "EVS"}, wantEVS: true},
		{name: "pcmu", info: MediaInfo{Codec: "PCMU"}, wantPCMU: true},
		{name: "unsupported", info: MediaInfo{Codec: "OPUS"}, wantErr: ErrUnsupportedCodec},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mediaBridgeCodec(tt.info)
			if err != tt.wantErr {
				t.Fatalf("mediaBridgeCodec() error = %v, want %v", err, tt.wantErr)
			}
			if got.amr != tt.wantAMR || got.evs != tt.wantEVS || got.pcmu != tt.wantPCMU {
				t.Fatalf("mediaBridgeCodec() = %+v, want amr %q evs %v pcmu %v", got, tt.wantAMR, tt.wantEVS, tt.wantPCMU)
			}
		})
	}
}

func TestRewriteRTPPacketWithSourceTimingPreservesTimestampDelta(t *testing.T) {
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
			got := rewriteRTPPacketWithSourceTiming(
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
				t.Fatalf("rewriteRTPPacketWithSourceTiming() header = %+v, want pt 0 seq 42 timestamp %d ssrc 5678", got.Header, tt.wantTimestamp)
			}
			if !got.Marker || !got.Padding || !got.Extension || got.ExtensionProfile != 0xBEDE || len(got.CSRC) != 2 {
				t.Fatalf("rewriteRTPPacketWithSourceTiming() dropped RTP header fields: %+v", got.Header)
			}
			if string(got.Payload) != string(payload) {
				t.Fatalf("rewriteRTPPacketWithSourceTiming() payload = %v, want %v", got.Payload, payload)
			}
		})
	}
}

func TestPCMUDownlinkRewriterRepairsTimestampGaps(t *testing.T) {
	const (
		seqBase = 42
		tsBase  = 90000
		ssrc    = 5678
	)
	payload := func(value byte, samples int) []byte {
		out := make([]byte, samples)
		for i := range out {
			out[i] = value
		}
		return out
	}

	tests := []struct {
		name         string
		inTimestamps []uint32
		inSequences  []uint16
		inSamples    []int
		wantPayloads [][]byte
		wantTS       []uint32
	}{
		{
			name:         "continuous",
			inTimestamps: []uint32{1000, 1160},
			wantPayloads: [][]byte{payload(1, 160), payload(2, 160)},
			wantTS:       []uint32{tsBase, tsBase + 160},
		},
		{
			name:         "packet duration changes",
			inTimestamps: []uint32{1000, 1320},
			inSamples:    []int{320, 160},
			wantPayloads: [][]byte{payload(1, 320), payload(2, 160)},
			wantTS:       []uint32{tsBase, tsBase + 320},
		},
		{
			name:         "single missing frame",
			inTimestamps: []uint32{1000, 1320},
			wantPayloads: [][]byte{payload(1, 160), payload(pcmuSilenceByte, 160), payload(2, 160)},
			wantTS:       []uint32{tsBase, tsBase + 160, tsBase + 320},
		},
		{
			name:         "multiple missing frames",
			inTimestamps: []uint32{1000, 1480},
			wantPayloads: [][]byte{payload(1, 160), payload(pcmuSilenceByte, 160), payload(pcmuSilenceByte, 160), payload(2, 160)},
			wantTS:       []uint32{tsBase, tsBase + 160, tsBase + 320, tsBase + 480},
		},
		{
			name:         "timestamp wraparound",
			inTimestamps: []uint32{^uint32(79), 80},
			inSequences:  []uint16{^uint16(0), 0},
			wantPayloads: [][]byte{payload(1, 160), payload(2, 160)},
			wantTS:       []uint32{tsBase, tsBase + 160},
		},
		{
			name:         "duplicate packet",
			inTimestamps: []uint32{1000, 1000, 1160},
			inSequences:  []uint16{100, 100, 101},
			wantPayloads: [][]byte{payload(1, 160), payload(3, 160)},
			wantTS:       []uint32{tsBase, tsBase + 160},
		},
		{
			name:         "late packet",
			inTimestamps: []uint32{1000, 1160, 1000},
			inSequences:  []uint16{100, 101, 100},
			wantPayloads: [][]byte{payload(1, 160), payload(2, 160)},
			wantTS:       []uint32{tsBase, tsBase + 160},
		},
		{
			name:         "huge gap resyncs without padding",
			inTimestamps: []uint32{1000, 1000 + maxPCMUSilenceGapSamples + 161},
			wantPayloads: [][]byte{payload(1, 160), payload(2, 160)},
			wantTS:       []uint32{tsBase, tsBase + 160},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rewriter := newPCMUDownlinkRewriter(seqBase, tsBase, ssrc)
			got := []rtp.Packet{}
			for i, timestamp := range tt.inTimestamps {
				samples := 160
				if i < len(tt.inSamples) {
					samples = tt.inSamples[i]
				}
				sequenceNumber := uint16(100 + i)
				if i < len(tt.inSequences) {
					sequenceNumber = tt.inSequences[i]
				}
				in := rtp.Packet{
					Header: rtp.Header{
						Version:        2,
						PayloadType:    104,
						SequenceNumber: sequenceNumber,
						Timestamp:      timestamp,
						SSRC:           1234,
						Extension:      true,
					},
					Payload: payload(byte(i+1), samples),
				}
				err := rewriter.rewrite(in, func(out rtp.Packet) error {
					got = append(got, out)
					return nil
				})
				if err != nil {
					t.Fatalf("rewrite() error = %v", err)
				}
			}

			if len(got) != len(tt.wantPayloads) {
				t.Fatalf("rewrite() packets = %d, want %d", len(got), len(tt.wantPayloads))
			}
			for i, packet := range got {
				if packet.PayloadType != pcmuPayloadType || packet.SequenceNumber != seqBase+uint16(i) || packet.Timestamp != tt.wantTS[i] || packet.SSRC != ssrc {
					t.Fatalf("packet %d header = %+v, want pt %d seq %d timestamp %d ssrc %d", i, packet.Header, pcmuPayloadType, seqBase+uint16(i), tt.wantTS[i], ssrc)
				}
				if packet.Extension || packet.Padding || len(packet.CSRC) != 0 {
					t.Fatalf("packet %d kept source RTP header fields: %+v", i, packet.Header)
				}
				if string(packet.Payload) != string(tt.wantPayloads[i]) {
					t.Fatalf("packet %d payload = %v, want %v", i, packet.Payload, tt.wantPayloads[i])
				}
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
