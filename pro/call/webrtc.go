//go:build ims

package call

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	pims "github.com/damonto/sigmo/pro/ims"
	"github.com/damonto/sigmo/pro/voicecodec"
)

const (
	pcmuPayloadType             = 0
	pcmuClockRate               = 8000
	maxPCMUSilenceGapSamples    = pcmuClockRate
	pcmuSilenceByte             = 0xff
	webRTCUDPPortMin            = 40000
	webRTCUDPPortMax            = 40100
	webRTCDisconnectedGraceTime = 5 * time.Second
	mediaCleanupTimeout         = 5 * time.Second
)

type Media struct {
	calls *Calls

	ice webRTCICEProvider

	bridgeMu sync.Mutex
	bridges  map[*webRTCBridge]struct{}
	closing  bool
}

type WebRTCSession struct {
	bridge *webRTCBridge
}

func NewMedia(calls *Calls) *Media {
	return &Media{
		calls:   calls,
		ice:     newWebRTCICEProvider(),
		bridges: make(map[*webRTCBridge]struct{}),
	}
}

func (m *Media) Run(ctx context.Context) error {
	<-ctx.Done()
	cleanupCtx, cancel := mediaCleanupContext(ctx)
	defer cancel()
	return m.Close(cleanupCtx)
}

func (m *Media) OpenWebRTCSession(ctx context.Context, modem *mmodem.Modem, callID string) (*WebRTCSession, error) {
	iceServers, err := m.webRTCICEServers(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch WebRTC ICE servers: %w", err)
	}
	media, err := m.calls.OpenMedia(ctx, modem, callID)
	if err != nil {
		return nil, err
	}
	codec, err := mediaBridgeCodec(media.Info())
	if err != nil {
		return nil, err
	}
	bridge, err := newWebRTCBridge(media, codec, iceServers)
	if err != nil {
		return nil, err
	}
	if !m.registerBridge(bridge) {
		bridge.close()
		return nil, ErrMediaUnavailable
	}
	bridge.onClose = func() {
		m.unregisterBridge(bridge)
	}
	return &WebRTCSession{bridge: bridge}, nil
}

func (s *WebRTCSession) Answer(ctx context.Context, offer WebRTCSessionDescription) (WebRTCSessionDescription, error) {
	if s == nil || s.bridge == nil {
		return WebRTCSessionDescription{}, ErrMediaUnavailable
	}
	offer.Type = strings.TrimSpace(strings.ToLower(offer.Type))
	if offer.Type != "offer" || strings.TrimSpace(offer.SDP) == "" {
		return WebRTCSessionDescription{}, ErrMediaUnavailable
	}
	return s.bridge.answer(ctx, offer)
}

func (s *WebRTCSession) AddICECandidate(candidate WebRTCICECandidate) error {
	if s == nil || s.bridge == nil {
		return ErrMediaUnavailable
	}
	return s.bridge.addRemoteICECandidate(candidate)
}

func (s *WebRTCSession) ICECandidates() <-chan WebRTCICECandidate {
	if s == nil || s.bridge == nil {
		return nil
	}
	return s.bridge.localICE
}

func (s *WebRTCSession) Close() {
	if s == nil || s.bridge == nil {
		return
	}
	s.bridge.close()
}

func (s *WebRTCSession) CloseIfNotConnected() bool {
	if s == nil || s.bridge == nil || s.Connected() {
		return false
	}
	s.Close()
	return true
}

func (s *WebRTCSession) Connected() bool {
	if s == nil || s.bridge == nil {
		return false
	}
	return s.bridge.connected()
}

func mediaCleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), mediaCleanupTimeout)
}

func (m *Media) registerBridge(bridge *webRTCBridge) bool {
	m.bridgeMu.Lock()
	defer m.bridgeMu.Unlock()
	if m.closing {
		return false
	}
	m.bridges[bridge] = struct{}{}
	return true
}

func (m *Media) unregisterBridge(bridge *webRTCBridge) {
	m.bridgeMu.Lock()
	delete(m.bridges, bridge)
	m.bridgeMu.Unlock()
}

func (m *Media) Close(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.bridgeMu.Lock()
	m.closing = true
	bridges := slices.Collect(maps.Keys(m.bridges))
	m.bridgeMu.Unlock()

	for _, bridge := range bridges {
		bridge.close()
	}
	return nil
}

type webRTCBridge struct {
	media MediaSession
	info  MediaInfo
	pc    *webrtc.PeerConnection
	track *webrtc.TrackLocalStaticRTP
	codec bridgeCodec

	cancel       context.CancelFunc
	once         sync.Once
	wg           sync.WaitGroup
	downlinkOnce sync.Once

	stateMu       sync.Mutex
	closed        bool
	connectedOnce bool

	doneOnce sync.Once
	onClose  func()

	disconnectMu    sync.Mutex
	disconnectTimer *time.Timer

	iceMu     sync.Mutex
	localICE  chan WebRTCICECandidate
	iceClosed bool
}

type bridgeCodec struct {
	amr  voicecodec.AMRCodec
	pcmu bool
}

type webRTCBridgeAction int

const (
	webRTCBridgeActionNone webRTCBridgeAction = iota
	webRTCBridgeActionReady
	webRTCBridgeActionGraceClose
	webRTCBridgeActionCloseNow
)

func newWebRTCBridge(media MediaSession, codec bridgeCodec, iceServers []webrtc.ICEServer) (*webRTCBridge, error) {
	info := media.Info()
	mediaEngine := &webrtc.MediaEngine{}
	if err := mediaEngine.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypePCMU,
			ClockRate: pcmuClockRate,
			Channels:  1,
		},
		PayloadType: pcmuPayloadType,
	}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, fmt.Errorf("register PCMU codec: %w", err)
	}
	interceptors := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(mediaEngine, interceptors); err != nil {
		return nil, fmt.Errorf("register WebRTC interceptors: %w", err)
	}
	settingEngine := webrtc.SettingEngine{}
	if err := settingEngine.SetEphemeralUDPPortRange(webRTCUDPPortMin, webRTCUDPPortMax); err != nil {
		return nil, fmt.Errorf("set WebRTC UDP port range: %w", err)
	}
	interfaceNames, err := defaultRouteInterfaceNames()
	if err != nil {
		slog.Warn("detect WebRTC ICE default interface", "error", err)
	} else if len(interfaceNames) > 0 {
		allowedInterfaces := interfaceNameSet(interfaceNames)
		settingEngine.SetInterfaceFilter(func(interfaceName string) bool {
			_, ok := allowedInterfaces[interfaceName]
			return ok
		})
		slog.Debug("filter WebRTC ICE interfaces", "interfaces", interfaceNames)
	}
	api := webrtc.NewAPI(
		webrtc.WithMediaEngine(mediaEngine),
		webrtc.WithInterceptorRegistry(interceptors),
		webrtc.WithSettingEngine(settingEngine),
	)
	pc, err := api.NewPeerConnection(webrtc.Configuration{
		ICEServers: iceServers,
	})
	if err != nil {
		return nil, fmt.Errorf("create WebRTC peer connection: %w", err)
	}
	track, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypePCMU, ClockRate: pcmuClockRate, Channels: 1},
		"audio",
		"sigmo-call",
	)
	if err != nil {
		_ = pc.Close()
		return nil, fmt.Errorf("create WebRTC audio track: %w", err)
	}
	sender, err := pc.AddTrack(track)
	if err != nil {
		_ = pc.Close()
		return nil, fmt.Errorf("add WebRTC audio track: %w", err)
	}
	go drainRTCP(sender)

	bridgeCtx, cancel := context.WithCancel(context.Background())
	bridge := &webRTCBridge{
		media:    media,
		info:     info,
		pc:       pc,
		track:    track,
		codec:    codec,
		cancel:   cancel,
		localICE: make(chan WebRTCICECandidate, 32),
	}
	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}
		bridge.sendLocalICECandidate(webRTCICECandidateFromPion(candidate.ToJSON()))
	})
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch bridgeActionForPeerState(state) {
		case webRTCBridgeActionReady:
			bridge.markConnected()
			bridge.cancelDisconnectTimer()
			bridge.startDownlink(bridgeCtx)
		case webRTCBridgeActionGraceClose:
			bridge.closeAfterDisconnectGrace()
		case webRTCBridgeActionCloseNow:
			go bridge.close()
		}
	})
	pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		if !strings.EqualFold(track.Codec().MimeType, webrtc.MimeTypePCMU) {
			return
		}
		bridge.startUplink(bridgeCtx, track, codec)
	})
	return bridge, nil
}

func (b *webRTCBridge) answer(ctx context.Context, offer WebRTCSessionDescription) (WebRTCSessionDescription, error) {
	if err := b.pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  offer.SDP,
	}); err != nil {
		return WebRTCSessionDescription{}, fmt.Errorf("set WebRTC offer: %w", err)
	}
	answer, err := b.pc.CreateAnswer(nil)
	if err != nil {
		return WebRTCSessionDescription{}, fmt.Errorf("create WebRTC answer: %w", err)
	}
	if err := b.pc.SetLocalDescription(answer); err != nil {
		return WebRTCSessionDescription{}, fmt.Errorf("set WebRTC answer: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return WebRTCSessionDescription{}, err
	}
	local := b.pc.LocalDescription()
	if local == nil {
		return WebRTCSessionDescription{}, fmt.Errorf("read WebRTC local description: %w", ErrMediaUnavailable)
	}
	return WebRTCSessionDescription{Type: "answer", SDP: local.SDP}, nil
}

func (b *webRTCBridge) close() {
	b.stop()
	b.wg.Wait()
	b.doneOnce.Do(func() {
		if b.onClose != nil {
			b.onClose()
		}
	})
}

func (b *webRTCBridge) stop() {
	b.once.Do(func() {
		b.cancelDisconnectTimer()
		b.stateMu.Lock()
		b.closed = true
		b.stateMu.Unlock()
		if b.cancel != nil {
			b.cancel()
		}
		if b.pc != nil {
			_ = b.pc.Close()
		}
		b.closeLocalICECandidates()
	})
}

func (b *webRTCBridge) markConnected() {
	b.stateMu.Lock()
	b.connectedOnce = true
	b.stateMu.Unlock()
}

func (b *webRTCBridge) connected() bool {
	b.stateMu.Lock()
	defer b.stateMu.Unlock()
	return b.connectedOnce
}

func (b *webRTCBridge) addRemoteICECandidate(candidate WebRTCICECandidate) error {
	candidate.Candidate = strings.TrimSpace(candidate.Candidate)
	if candidate.Candidate == "" {
		return ErrMediaUnavailable
	}
	if err := b.pc.AddICECandidate(webrtc.ICECandidateInit{
		Candidate:        candidate.Candidate,
		SDPMid:           candidate.SDPMid,
		SDPMLineIndex:    candidate.SDPMLineIndex,
		UsernameFragment: candidate.UsernameFragment,
	}); err != nil {
		return fmt.Errorf("add WebRTC ICE candidate: %w", err)
	}
	return nil
}

func (b *webRTCBridge) sendLocalICECandidate(candidate WebRTCICECandidate) {
	b.iceMu.Lock()
	defer b.iceMu.Unlock()
	if b.iceClosed {
		return
	}
	select {
	case b.localICE <- candidate:
	default:
		slog.Warn("drop WebRTC ICE candidate")
	}
}

func (b *webRTCBridge) closeLocalICECandidates() {
	b.iceMu.Lock()
	defer b.iceMu.Unlock()
	if b.iceClosed {
		return
	}
	b.iceClosed = true
	if b.localICE != nil {
		close(b.localICE)
	}
}

func (b *webRTCBridge) closeAfterDisconnectGrace() {
	b.disconnectMu.Lock()
	defer b.disconnectMu.Unlock()
	if b.disconnectTimer != nil {
		return
	}
	b.disconnectTimer = time.AfterFunc(webRTCDisconnectedGraceTime, func() {
		if !shouldCloseDisconnectedBridge(b.pc.ConnectionState()) {
			b.cancelDisconnectTimer()
			return
		}
		b.close()
	})
}

func (b *webRTCBridge) cancelDisconnectTimer() {
	b.disconnectMu.Lock()
	defer b.disconnectMu.Unlock()
	if b.disconnectTimer == nil {
		return
	}
	b.disconnectTimer.Stop()
	b.disconnectTimer = nil
}

func (b *webRTCBridge) startDownlink(ctx context.Context) {
	b.downlinkOnce.Do(func() {
		if !b.addWorker() {
			return
		}
		go func() {
			defer b.wg.Done()
			b.runDownlink(ctx, b.codec)
		}()
	})
}

func (b *webRTCBridge) startUplink(ctx context.Context, track *webrtc.TrackRemote, codec bridgeCodec) {
	if !b.addWorker() {
		return
	}
	go func() {
		defer b.wg.Done()
		b.runUplink(ctx, track, codec)
	}()
}

func (b *webRTCBridge) addWorker() bool {
	b.stateMu.Lock()
	defer b.stateMu.Unlock()
	if b.closed {
		return false
	}
	b.wg.Add(1)
	return true
}

func (b *webRTCBridge) runDownlink(ctx context.Context, codec bridgeCodec) {
	if codec.pcmu {
		b.runPCMUPassthroughDownlink(ctx)
		return
	}
	amr, err := voicecodec.NewAMRTranscoder(codec.amr)
	if err != nil {
		b.stop()
		return
	}

	sequenceNumber := random16()
	timestamp := random32()
	ssrc := random32()
	for {
		packet, err := b.media.ReadPacket(ctx)
		if err != nil {
			b.stop()
			return
		}
		var inbound rtp.Packet
		if err := inbound.Unmarshal(packet); err != nil || int(inbound.PayloadType) != b.info.PayloadType {
			continue
		}
		payload := voicecodec.AMRPayload{Codec: codec.amr, OctetAligned: b.info.OctetAlign}
		if err := payload.UnmarshalBinary(inbound.Payload); err != nil {
			continue
		}
		for _, frame := range payload.Frames {
			if frame.FrameType == 15 || !frame.Quality {
				continue
			}
			pcm, err := amr.Decode(frame)
			if err != nil {
				b.stop()
				return
			}
			pcm8, err := voicecodec.ResampleLinear(pcm, voicecodec.AMRSampleRate(codec.amr), pcmuClockRate)
			if err != nil {
				b.stop()
				return
			}
			out := &rtp.Packet{
				Header: rtp.Header{
					Version:        2,
					PayloadType:    pcmuPayloadType,
					SequenceNumber: sequenceNumber,
					Timestamp:      timestamp,
					SSRC:           ssrc,
				},
				Payload: voicecodec.EncodePCMU(pcm8),
			}
			if err := b.track.WriteRTP(out); err != nil {
				b.stop()
				return
			}
			sequenceNumber++
			timestamp += uint32(len(pcm8))
		}
	}
}

func (b *webRTCBridge) runPCMUPassthroughDownlink(ctx context.Context) {
	rewriter := newPCMUDownlinkRewriter(random16(), random32(), random32())
	for {
		packet, err := b.media.ReadPacket(ctx)
		if err != nil {
			b.stop()
			return
		}
		var inbound rtp.Packet
		if err := inbound.Unmarshal(packet); err != nil || int(inbound.PayloadType) != b.info.PayloadType {
			continue
		}
		if err := rewriter.rewrite(inbound, func(out rtp.Packet) error {
			return b.track.WriteRTP(&out)
		}); err != nil {
			b.stop()
			return
		}
	}
}

func (b *webRTCBridge) runUplink(ctx context.Context, track *webrtc.TrackRemote, codec bridgeCodec) {
	if codec.pcmu {
		b.runPCMUPassthroughUplink(ctx, track)
		return
	}
	amr, err := voicecodec.NewAMRTranscoder(codec.amr)
	if err != nil {
		b.stop()
		return
	}

	sequenceNumber := random16()
	timestamp := random32()
	ssrc := random32()
	buffer := []int16{}
	frameSamples := voicecodec.AMRSamplesPerFrame(codec.amr)
	for {
		packet, _, err := track.ReadRTP()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				b.stop()
			}
			return
		}
		if int(packet.PayloadType) != pcmuPayloadType {
			continue
		}
		pcm8 := voicecodec.DecodePCMU(packet.Payload)
		pcm, err := voicecodec.ResampleLinear(pcm8, pcmuClockRate, voicecodec.AMRSampleRate(codec.amr))
		if err != nil {
			b.stop()
			return
		}
		buffer = append(buffer, pcm...)
		for len(buffer) >= frameSamples {
			chunk := make([]int16, frameSamples)
			copy(chunk, buffer[:frameSamples])
			buffer = buffer[frameSamples:]
			frames, err := amr.Encode(chunk)
			if err != nil {
				b.stop()
				return
			}
			payload, err := (voicecodec.AMRPayload{
				Codec:        codec.amr,
				OctetAligned: b.info.OctetAlign,
				Frames:       frames,
			}).MarshalBinary()
			if err != nil {
				b.stop()
				return
			}
			out := rtp.Packet{
				Header: rtp.Header{
					Version:        2,
					PayloadType:    uint8(b.info.PayloadType),
					SequenceNumber: sequenceNumber,
					Timestamp:      timestamp,
					SSRC:           ssrc,
				},
				Payload: payload,
			}
			data, err := out.Marshal()
			if err != nil {
				b.stop()
				return
			}
			if err := b.media.WritePacket(ctx, data); errors.Is(err, pims.ErrCallOnHold) {
				continue
			} else if err != nil {
				b.stop()
				return
			}
			sequenceNumber++
			timestamp += uint32(frameSamples)
		}
	}
}

func (b *webRTCBridge) runPCMUPassthroughUplink(ctx context.Context, track *webrtc.TrackRemote) {
	sequenceNumber := random16()
	timestamp := random32()
	ssrc := random32()
	var firstTimestamp uint32
	firstPacket := true
	for {
		packet, _, err := track.ReadRTP()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				b.stop()
			}
			return
		}
		if int(packet.PayloadType) != pcmuPayloadType {
			continue
		}
		if firstPacket {
			firstTimestamp = packet.Timestamp
			firstPacket = false
		}
		out := rewriteRTPPacketWithSourceTiming(*packet, uint8(b.info.PayloadType), sequenceNumber, timestamp, firstTimestamp, ssrc)
		data, err := out.Marshal()
		if err != nil {
			b.stop()
			return
		}
		if err := b.media.WritePacket(ctx, data); errors.Is(err, pims.ErrCallOnHold) {
			continue
		} else if err != nil {
			b.stop()
			return
		}
		sequenceNumber++
	}
}

func rewriteRTPPacketWithSourceTiming(in rtp.Packet, payloadType uint8, sequenceNumber uint16, timestampBase uint32, firstTimestamp uint32, ssrc uint32) *rtp.Packet {
	header := in.Header
	header.PayloadType = payloadType
	header.SequenceNumber = sequenceNumber
	header.Timestamp = timestampBase + in.Timestamp - firstTimestamp
	header.SSRC = ssrc
	return &rtp.Packet{
		Header:  header,
		Payload: in.Payload,
	}
}

type pcmuDownlinkRewriter struct {
	sequenceNumber uint16
	timestamp      uint32
	ssrc           uint32

	seen                     bool
	lastSourceSequenceNumber uint16
	lastSourceTimestamp      uint32
	lastSourceSamples        uint32
}

func newPCMUDownlinkRewriter(sequenceNumber uint16, timestamp uint32, ssrc uint32) pcmuDownlinkRewriter {
	return pcmuDownlinkRewriter{
		sequenceNumber: sequenceNumber,
		timestamp:      timestamp,
		ssrc:           ssrc,
	}
}

func (r *pcmuDownlinkRewriter) rewrite(in rtp.Packet, write func(rtp.Packet) error) error {
	samples := uint32(len(in.Payload))
	if samples == 0 {
		return nil
	}

	if r.seen {
		if !rtpSequenceNumberAhead(in.SequenceNumber, r.lastSourceSequenceNumber) {
			return nil
		}
		sourceDelta := in.Timestamp - r.lastSourceTimestamp
		if sourceDelta > r.lastSourceSamples {
			gap := sourceDelta - r.lastSourceSamples
			if gap <= maxPCMUSilenceGapSamples {
				if err := r.writeSilence(gap, r.lastSourceSamples, write); err != nil {
					return err
				}
			} else {
				slog.Warn("resync PCMU downlink RTP timestamp",
					"gap_samples", gap,
					"packet_samples", samples,
				)
			}
		}
	} else {
		r.seen = true
	}
	r.lastSourceSequenceNumber = in.SequenceNumber
	r.lastSourceTimestamp = in.Timestamp
	r.lastSourceSamples = samples

	return write(r.packet(in.Payload))
}

func rtpSequenceNumberAhead(current uint16, previous uint16) bool {
	delta := current - previous
	return delta != 0 && delta < 1<<15
}

func (r *pcmuDownlinkRewriter) writeSilence(samples uint32, packetSamples uint32, write func(rtp.Packet) error) error {
	for samples > 0 {
		n := min(samples, packetSamples)
		payload := make([]byte, n)
		for i := range payload {
			payload[i] = pcmuSilenceByte
		}
		if err := write(r.packet(payload)); err != nil {
			return err
		}
		samples -= n
	}
	return nil
}

func (r *pcmuDownlinkRewriter) packet(payload []byte) rtp.Packet {
	out := rtp.Packet{
		Header: rtp.Header{
			Version:        2,
			PayloadType:    pcmuPayloadType,
			SequenceNumber: r.sequenceNumber,
			Timestamp:      r.timestamp,
			SSRC:           r.ssrc,
		},
		Payload: payload,
	}
	r.sequenceNumber++
	r.timestamp += uint32(len(payload))
	return out
}

func shouldCloseDisconnectedBridge(state webrtc.PeerConnectionState) bool {
	return state == webrtc.PeerConnectionStateDisconnected
}

func bridgeActionForPeerState(state webrtc.PeerConnectionState) webRTCBridgeAction {
	switch state {
	case webrtc.PeerConnectionStateConnected:
		return webRTCBridgeActionReady
	case webrtc.PeerConnectionStateDisconnected:
		return webRTCBridgeActionGraceClose
	case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
		return webRTCBridgeActionCloseNow
	default:
		return webRTCBridgeActionNone
	}
}

func mediaBridgeCodec(info MediaInfo) (bridgeCodec, error) {
	switch strings.ToUpper(strings.TrimSpace(info.Codec)) {
	case string(voicecodec.CodecAMR):
		return bridgeCodec{amr: voicecodec.CodecAMR}, nil
	case string(voicecodec.CodecAMRWB):
		return bridgeCodec{amr: voicecodec.CodecAMRWB}, nil
	case "PCMU":
		return bridgeCodec{pcmu: true}, nil
	default:
		return bridgeCodec{}, ErrUnsupportedCodec
	}
}

func drainRTCP(sender *webrtc.RTPSender) {
	buf := make([]byte, 1500)
	for {
		if _, _, err := sender.Read(buf); err != nil {
			return
		}
	}
}

func random16() uint16 {
	var data [2]byte
	if _, err := rand.Read(data[:]); err != nil {
		return uint16(time.Now().UnixNano())
	}
	return binary.BigEndian.Uint16(data[:])
}

func random32() uint32 {
	var data [4]byte
	if _, err := rand.Read(data[:]); err != nil {
		return uint32(time.Now().UnixNano())
	}
	return binary.BigEndian.Uint32(data[:])
}

func webRTCICECandidateFromPion(candidate webrtc.ICECandidateInit) WebRTCICECandidate {
	return WebRTCICECandidate{
		Candidate:        candidate.Candidate,
		SDPMid:           candidate.SDPMid,
		SDPMLineIndex:    candidate.SDPMLineIndex,
		UsernameFragment: candidate.UsernameFragment,
	}
}
