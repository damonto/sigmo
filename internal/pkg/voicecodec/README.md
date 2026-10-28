# Call audio codecs

The WebRTC bridge selects the first supported codec in the client's SDP offer,
honoring its preference between Opus and PCMU. The iOS client offers Opus first
and retains PCMU for compatibility. IMS continues using its negotiated AMR,
AMR-WB, EVS or PCMU format. HTTP routes and WebSocket signaling messages are
unchanged; codec selection uses SDP.

`opus.go` wraps the pure Go `github.com/pion/opus` encoder and decoder. The
dependency is pinned to a specific commit because v0.1.0 does not expose the
encoder. No system libopus, CGo or additional WASM module is needed.

The bridge uses 16 kHz mono PCM and emits 20 ms Opus packets at up to 32 kbit/s,
respecting the receiver's bitrate and playback-rate limits. The pinned encoder's
normal entry point emits CELT and requires 48 kHz PCM, so input is interpolated
from 16 kHz before encoding. Its incomplete SILK encoder is not used in the
production sending path. Incoming packets may use SILK, hybrid or CELT, including
stereo and packet durations up to 120 ms; decoding produces mono 16 kHz PCM.

Opus RTP uses a 48 kHz clock and `opus/48000/2` even for mono audio. The answer
disables DTX and in-band FEC. A bounded packet reorder buffer precedes decoding,
and gaps up to 120 ms receive packet loss concealment. Larger gaps resynchronize
the incoming decoder timeline. This does not provide adaptive bitrate or FEC
recovery; these require separate feedback and buffering work.

Opus preserves the current wideband PCM path. It does not add fullband EVS:
`EVSSampleRate` remains 16 kHz. Carrier-side PCMU and AMR narrowband sources also
remain narrowband after Opus encoding.

Verification:

```sh
go test ./internal/pkg/voicecodec ./internal/app/call
go test ./internal/pkg/voicecodec -run Opus -bench OpusVoiceFrame
```

Reference decoding fixtures are documented in `testdata/README.md`. WebRTC tests
use local peers and simulated IMS packets, including both client codec orders,
single-codec offers, negotiated payload types, AMR/AMR-WB/EVS transcoding, and
receive-only offers. They require local UDP sockets but do not contact STUN/TURN,
carrier services or hardware.
