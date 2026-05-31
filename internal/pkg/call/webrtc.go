package call

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/voicecodec"
	"github.com/damonto/sigmo/internal/pkg/wificalling"
)

const (
	pcmuPayloadType             = 0
	pcmuClockRate               = 8000
	webRTCUDPPortMin            = 40000
	webRTCUDPPortMax            = 40100
	webRTCICEGatheringTimeout   = 20 * time.Second
	webRTCDisconnectedGraceTime = 5 * time.Second
)

type WebRTCSessionDescription struct {
	Type string
	SDP  string
}

var defaultWebRTCICEServers = []webrtc.ICEServer{
	{URLs: []string{"stun:stun.l.google.com:19302"}},
	{URLs: []string{"stun:stun.cloudflare.com:3478"}},
}

func (s *Service) WebRTCOffer(ctx context.Context, modem *mmodem.Modem, callID string, offer WebRTCSessionDescription) (WebRTCSessionDescription, error) {
	offer.Type = strings.TrimSpace(strings.ToLower(offer.Type))
	if offer.Type != "offer" || strings.TrimSpace(offer.SDP) == "" {
		return WebRTCSessionDescription{}, ErrMediaUnavailable
	}
	media, err := s.OpenMedia(ctx, modem, callID)
	if err != nil {
		return WebRTCSessionDescription{}, err
	}
	codec, err := mediaBridgeCodec(media.Info())
	if err != nil {
		return WebRTCSessionDescription{}, err
	}
	var factory *voicecodec.AMRCodecFactory
	if codec.amr != "" {
		var source string
		factory, source, err = s.amrCodecFactory(ctx)
		if err != nil {
			slog.Warn("open AMR codec",
				"call_id", callID,
				"modem", modem.EquipmentIdentifier,
				"source", source,
				"error", err,
			)
			return WebRTCSessionDescription{}, ErrMediaUnavailable
		}
	}
	bridge, err := newWebRTCBridge(media, factory, codec)
	if err != nil {
		return WebRTCSessionDescription{}, err
	}
	if !s.registerBridge(bridge) {
		bridge.close()
		return WebRTCSessionDescription{}, ErrMediaUnavailable
	}
	bridge.onClose = func() {
		s.unregisterBridge(bridge)
	}
	answer, err := bridge.answer(ctx, offer)
	if err != nil {
		bridge.close()
		return WebRTCSessionDescription{}, err
	}
	return answer, nil
}

func amrWASMPath() string {
	return strings.TrimSpace(os.Getenv("SIGMO_AMR_WASM"))
}

func (s *Service) amrCodecFactory(ctx context.Context) (*voicecodec.AMRCodecFactory, string, error) {
	s.amrMu.Lock()
	defer s.amrMu.Unlock()
	if s.amrFactory != nil {
		return s.amrFactory, s.amrSource, nil
	}

	source := "embedded"
	var (
		factory *voicecodec.AMRCodecFactory
		err     error
	)
	if path := amrWASMPath(); path != "" {
		source = path
		factory, err = voicecodec.NewAMRCodecFactoryFromFile(ctx, path)
	} else {
		factory, err = voicecodec.NewDefaultAMRCodecFactory(ctx)
	}
	if err != nil {
		return nil, source, err
	}
	s.amrFactory = factory
	s.amrSource = source
	return factory, source, nil
}

func (s *Service) closeAMRCodecFactory(ctx context.Context) error {
	s.amrMu.Lock()
	factory := s.amrFactory
	s.amrFactory = nil
	s.amrSource = ""
	s.amrMu.Unlock()
	if factory == nil {
		return nil
	}
	return factory.Close(ctx)
}

func (s *Service) registerBridge(bridge *webRTCBridge) bool {
	s.bridgeMu.Lock()
	defer s.bridgeMu.Unlock()
	if s.closing {
		return false
	}
	s.bridges[bridge] = struct{}{}
	return true
}

func (s *Service) unregisterBridge(bridge *webRTCBridge) {
	s.bridgeMu.Lock()
	delete(s.bridges, bridge)
	s.bridgeMu.Unlock()
}

func (s *Service) closeMedia(ctx context.Context) error {
	s.bridgeMu.Lock()
	s.closing = true
	bridges := make([]*webRTCBridge, 0, len(s.bridges))
	for bridge := range s.bridges {
		bridges = append(bridges, bridge)
	}
	s.bridgeMu.Unlock()

	for _, bridge := range bridges {
		bridge.close()
	}
	return s.closeAMRCodecFactory(ctx)
}

type webRTCBridge struct {
	media   MediaSession
	info    MediaInfo
	factory *voicecodec.AMRCodecFactory
	pc      *webrtc.PeerConnection
	track   *webrtc.TrackLocalStaticRTP
	codec   bridgeCodec

	ctx          context.Context
	cancel       context.CancelFunc
	once         sync.Once
	wg           sync.WaitGroup
	downlinkOnce sync.Once

	stateMu sync.Mutex
	closed  bool

	doneOnce sync.Once
	onClose  func()

	disconnectMu    sync.Mutex
	disconnectTimer *time.Timer
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

func newWebRTCBridge(media MediaSession, factory *voicecodec.AMRCodecFactory, codec bridgeCodec) (*webRTCBridge, error) {
	info := media.Info()
	if codec.amr != "" && factory == nil {
		return nil, ErrMediaUnavailable
	}
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
	api := webrtc.NewAPI(
		webrtc.WithMediaEngine(mediaEngine),
		webrtc.WithInterceptorRegistry(interceptors),
		webrtc.WithSettingEngine(settingEngine),
	)
	pc, err := api.NewPeerConnection(webrtc.Configuration{
		ICEServers: defaultWebRTCICEServers,
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

	// The HTTP request ends once the SDP answer is returned; media lives with the peer connection.
	bridgeCtx, cancel := context.WithCancel(context.Background())
	bridge := &webRTCBridge{
		media:   media,
		info:    info,
		factory: factory,
		pc:      pc,
		track:   track,
		codec:   codec,
		ctx:     bridgeCtx,
		cancel:  cancel,
	}
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch bridgeActionForPeerState(state) {
		case webRTCBridgeActionReady:
			bridge.cancelDisconnectTimer()
			bridge.startDownlink()
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
		bridge.startUplink(track, codec)
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
	gatherComplete := webrtc.GatheringCompletePromise(b.pc)
	if err := b.pc.SetLocalDescription(answer); err != nil {
		return WebRTCSessionDescription{}, fmt.Errorf("set WebRTC answer: %w", err)
	}
	gatherCtx, cancel := context.WithTimeout(ctx, webRTCICEGatheringTimeout)
	defer cancel()
	select {
	case <-gatherComplete:
	case <-gatherCtx.Done():
		if err := ctx.Err(); err != nil {
			return WebRTCSessionDescription{}, err
		}
		local := b.pc.LocalDescription()
		if local == nil || !hasICECandidate(local.SDP) {
			return WebRTCSessionDescription{}, fmt.Errorf("gather WebRTC ICE candidates: %w", gatherCtx.Err())
		}
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
		b.cancel()
		_ = b.pc.Close()
	})
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

func (b *webRTCBridge) startDownlink() {
	b.downlinkOnce.Do(func() {
		if !b.addWorker() {
			return
		}
		go func() {
			defer b.wg.Done()
			b.runDownlink(b.codec)
		}()
	})
}

func (b *webRTCBridge) startUplink(track *webrtc.TrackRemote, codec bridgeCodec) {
	if !b.addWorker() {
		return
	}
	go func() {
		defer b.wg.Done()
		b.runUplink(track, codec)
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

func (b *webRTCBridge) runDownlink(codec bridgeCodec) {
	if codec.pcmu {
		b.runPCMUPassthroughDownlink()
		return
	}
	amr, err := b.factory.NewCodec(b.ctx, codec.amr)
	if err != nil {
		b.stop()
		return
	}
	defer func() { _ = amr.Close(context.Background()) }()

	sequenceNumber := random16()
	timestamp := random32()
	ssrc := random32()
	for {
		packet, err := b.media.ReadPacket(b.ctx)
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
			pcm, err := amr.Decode(b.ctx, frame)
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

func (b *webRTCBridge) runPCMUPassthroughDownlink() {
	sequenceNumber := random16()
	timestamp := random32()
	ssrc := random32()
	var firstTimestamp uint32
	firstPacket := true
	for {
		packet, err := b.media.ReadPacket(b.ctx)
		if err != nil {
			b.stop()
			return
		}
		var inbound rtp.Packet
		if err := inbound.Unmarshal(packet); err != nil || int(inbound.PayloadType) != b.info.PayloadType {
			continue
		}
		if firstPacket {
			firstTimestamp = inbound.Timestamp
			firstPacket = false
		}
		out := rewriteRTPPacket(inbound, pcmuPayloadType, sequenceNumber, timestamp, firstTimestamp, ssrc)
		if err := b.track.WriteRTP(out); err != nil {
			b.stop()
			return
		}
		sequenceNumber++
	}
}

func (b *webRTCBridge) runUplink(track *webrtc.TrackRemote, codec bridgeCodec) {
	if codec.pcmu {
		b.runPCMUPassthroughUplink(track)
		return
	}
	amr, err := b.factory.NewCodec(b.ctx, codec.amr)
	if err != nil {
		b.stop()
		return
	}
	defer func() { _ = amr.Close(context.Background()) }()

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
			frames, err := amr.Encode(b.ctx, chunk)
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
			if err := b.media.WritePacket(b.ctx, data); errors.Is(err, wificalling.ErrCallOnHold) {
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

func (b *webRTCBridge) runPCMUPassthroughUplink(track *webrtc.TrackRemote) {
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
		out := rewriteRTPPacket(*packet, uint8(b.info.PayloadType), sequenceNumber, timestamp, firstTimestamp, ssrc)
		data, err := out.Marshal()
		if err != nil {
			b.stop()
			return
		}
		if err := b.media.WritePacket(b.ctx, data); errors.Is(err, wificalling.ErrCallOnHold) {
			continue
		} else if err != nil {
			b.stop()
			return
		}
		sequenceNumber++
	}
}

func rewriteRTPPacket(in rtp.Packet, payloadType uint8, sequenceNumber uint16, timestampBase uint32, firstTimestamp uint32, ssrc uint32) *rtp.Packet {
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

func hasICECandidate(sdp string) bool {
	for line := range strings.Lines(sdp) {
		if strings.HasPrefix(line, "a=candidate:") {
			return true
		}
	}
	return false
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
