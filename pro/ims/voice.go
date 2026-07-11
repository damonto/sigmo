//go:build ims

package ims

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"maps"
	"slices"
	"strings"
	"time"

	imsgo "github.com/damonto/ims-go"
	imssip "github.com/damonto/ims-go/ims/sip"
	imsvoice "github.com/damonto/ims-go/ims/voice"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type voiceCallState struct {
	call      *imsvoice.Call
	info      VoiceCall
	updatedAt time.Time
}

type voiceCallTransition struct {
	state     string
	hold      string
	reason    string
	reasonSet bool
	answered  bool
	ended     bool
	at        time.Time
}

type pendingVoiceDial struct {
	profileID string
	number    string
	startedAt time.Time
}

func (c *coordinator) DialCall(ctx context.Context, modem *mmodem.Modem, to string) (VoiceCall, error) {
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return VoiceCall{}, err
	}
	client, err := c.connectedClient(modem.EquipmentIdentifier, profileID)
	if err != nil {
		return VoiceCall{}, err
	}
	pending := c.setPendingVoiceDial(modem.EquipmentIdentifier, profileID, to)
	defer c.clearPendingVoiceDial(modem.EquipmentIdentifier, pending)
	call, err := client.Voice().Dial(ctx, imsvoice.DialRequest{To: to, Media: browserVoiceMediaOffer()})
	if err != nil {
		err = normalizeVoiceError(c.handleClientDisconnected(modem.EquipmentIdentifier, client, err))
		if errors.Is(err, ErrNotConnected) {
			return VoiceCall{}, err
		}
		if info, ok := c.finishFailedPendingVoiceDial(modem.EquipmentIdentifier, pending, err); ok {
			return info, err
		}
		return failedOutgoingVoiceCall(modem.EquipmentIdentifier, profileID, c.routeName(), to, err), err
	}
	info := c.storeVoiceCall(modem.EquipmentIdentifier, profileID, call, strings.TrimSpace(to), string(call.Direction()), string(call.State()), "")
	info.Hold = voiceHoldState(call)
	info = initialDialedVoiceCallState(info, call.State())
	if !info.AnsweredAt.IsZero() {
		c.updateVoiceCall(modem.EquipmentIdentifier, info.ID, info)
	}
	c.publishVoiceEvent(info)
	return info, nil
}

func initialDialedVoiceCallState(info VoiceCall, state imsvoice.CallState) VoiceCall {
	next, _ := advanceVoiceCall(info, voiceCallTransition{state: string(state), at: info.UpdatedAt})
	return next
}

func advanceVoiceCall(info VoiceCall, transition voiceCallTransition) (VoiceCall, bool) {
	if info.ID == "" {
		return info, false
	}
	if isTerminalVoiceCallState(info.State) {
		return info, false
	}
	next := info
	if transition.state != "" {
		next.State = transition.state
	}
	if transition.hold != "" {
		next.Hold = transition.hold
	}
	if transition.reasonSet {
		next.Reason = transition.reason
	}
	if transition.at.IsZero() {
		transition.at = time.Now()
	}
	next.UpdatedAt = transition.at
	if (transition.answered || isAnsweredVoiceCallState(next.State)) && next.AnsweredAt.IsZero() {
		next.AnsweredAt = next.UpdatedAt
	}
	if transition.ended || isTerminalVoiceCallState(next.State) {
		next.EndedAt = next.UpdatedAt
	}
	return next, next != info
}

func failVoiceCall(info VoiceCall, reason string, at time.Time) (VoiceCall, bool) {
	return advanceVoiceCall(info, voiceCallTransition{
		state:     string(imsvoice.CallStateFailed),
		reason:    reason,
		reasonSet: true,
		at:        at,
	})
}

func isAnsweredVoiceCallState(state string) bool {
	return state == string(imsvoice.CallStateActive) || state == string(imsvoice.CallStateConfirmed)
}

func failedOutgoingVoiceCall(modemID string, profileID string, route string, to string, err error) VoiceCall {
	return failedOutgoingVoiceCallAt(modemID, profileID, route, to, err, time.Now())
}

func failedOutgoingVoiceCallAt(modemID string, profileID string, route string, to string, err error, at time.Time) VoiceCall {
	number := strings.TrimSpace(to)
	reason := ""
	if err != nil {
		reason = err.Error()
	}
	return VoiceCall{
		ID:        failedVoiceCallID(modemID, profileID, number, at),
		Route:     route,
		ModemID:   modemID,
		ProfileID: profileID,
		Direction: string(imsvoice.CallDirectionOutgoing),
		Number:    number,
		State:     string(imsvoice.CallStateFailed),
		Reason:    reason,
		StartedAt: at,
		EndedAt:   at,
		UpdatedAt: at,
	}
}

func failedVoiceCallID(modemID string, profileID string, number string, at time.Time) string {
	sum := sha256.Sum256([]byte(modemID + "\x00" + profileID + "\x00" + number + "\x00" + at.UTC().Format(time.RFC3339Nano)))
	return "failed:" + hex.EncodeToString(sum[:12])
}

func (c *coordinator) finishFailedPendingVoiceDial(modemID string, pending pendingVoiceDial, err error) (VoiceCall, bool) {
	reason := ""
	if err != nil {
		reason = err.Error()
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	session := c.sessions[modemID]
	if session == nil || session.calls == nil {
		return VoiceCall{}, false
	}
	for _, state := range session.calls {
		if state == nil || !samePendingVoiceDial(state.info, pending) {
			continue
		}
		info := state.info
		info, _ = failVoiceCall(info, reason, time.Now())
		if strings.TrimSpace(info.Reason) == "" {
			info.Reason = reason
		}
		state.info = info
		state.updatedAt = info.UpdatedAt
		if session.pendingDial != nil && *session.pendingDial == pending {
			session.pendingDial = nil
		}
		return info, true
	}
	if session.pendingDial != nil && *session.pendingDial == pending {
		session.pendingDial = nil
	}
	return VoiceCall{}, false
}

func samePendingVoiceDial(call VoiceCall, pending pendingVoiceDial) bool {
	return call.ID != "" &&
		call.ProfileID == pending.profileID &&
		call.Direction == string(imsvoice.CallDirectionOutgoing) &&
		call.Number == pending.number &&
		call.StartedAt.Equal(pending.startedAt)
}

func browserVoiceMediaOffer() imsvoice.MediaOffer {
	return imsvoice.MediaOffer{
		Codecs: []imsvoice.AudioCodec{imsvoice.CodecEVS, imsvoice.CodecAMRWB, imsvoice.CodecAMR, imsvoice.CodecPCMU},
	}
}

func browserVoiceConfig() imsvoice.Config {
	return imsvoice.Config{
		Codecs: []imsvoice.AudioCodecConfig{
			{
				Name:         imsvoice.CodecEVS,
				PayloadTypes: []int{127},
				ClockRate:    16000,
				Bitrate:      "5.9-13.2",
				Bandwidth:    "nb-swb",
			},
			{
				Name:         imsvoice.CodecAMRWB,
				PayloadTypes: []int{104},
				ClockRate:    16000,
			},
			{
				Name:         imsvoice.CodecAMR,
				PayloadTypes: []int{102},
				ClockRate:    8000,
				ModeSet:      "0,2,4,7",
			},
			{
				Name:         imsvoice.CodecTelephoneEvent,
				PayloadTypes: []int{101},
				ClockRate:    8000,
			},
			{
				Name:         imsvoice.CodecPCMU,
				PayloadTypes: []int{0},
				ClockRate:    8000,
			},
		},
		PTime:    20 * time.Millisecond,
		MaxPTime: 240 * time.Millisecond,
	}
}

func (c *coordinator) AnswerCall(ctx context.Context, modem *mmodem.Modem, callID string) (VoiceCall, error) {
	client, call, info, err := c.lookupVoiceCall(ctx, modem, callID)
	if err != nil {
		return VoiceCall{}, err
	}
	if err := call.Answer(ctx, browserVoiceMediaOffer()); err != nil {
		return VoiceCall{}, normalizeVoiceError(c.handleClientDisconnected(modem.EquipmentIdentifier, client, err))
	}
	info, _ = advanceVoiceCall(info, voiceCallTransition{
		state:    string(call.State()),
		hold:     voiceHoldState(call),
		answered: true,
		at:       time.Now(),
	})
	c.updateVoiceCall(modem.EquipmentIdentifier, callID, info)
	c.publishVoiceEvent(info)
	return info, nil
}

func (c *coordinator) RejectCall(ctx context.Context, modem *mmodem.Modem, callID string) (VoiceCall, error) {
	client, call, info, err := c.lookupVoiceCall(ctx, modem, callID)
	if err != nil {
		return VoiceCall{}, err
	}
	if err := call.Reject(ctx, 486, "Busy Here"); err != nil {
		return VoiceCall{}, normalizeVoiceError(c.handleClientDisconnected(modem.EquipmentIdentifier, client, err))
	}
	info, _ = advanceVoiceCall(info, voiceCallTransition{
		state:     string(call.State()),
		hold:      voiceHoldState(call),
		reason:    "Busy Here",
		reasonSet: true,
		ended:     true,
		at:        time.Now(),
	})
	c.updateVoiceCall(modem.EquipmentIdentifier, callID, info)
	c.publishVoiceEvent(info)
	return info, nil
}

func (c *coordinator) HangupCall(ctx context.Context, modem *mmodem.Modem, callID string) (VoiceCall, error) {
	client, call, info, err := c.lookupVoiceCall(ctx, modem, callID)
	if err != nil {
		return VoiceCall{}, err
	}
	if err := call.Hangup(ctx); err != nil {
		return VoiceCall{}, normalizeVoiceError(c.handleClientDisconnected(modem.EquipmentIdentifier, client, err))
	}
	info, _ = advanceVoiceCall(info, voiceCallTransition{
		state: string(call.State()),
		hold:  voiceHoldState(call),
		ended: true,
		at:    time.Now(),
	})
	c.updateVoiceCall(modem.EquipmentIdentifier, callID, info)
	c.publishVoiceEvent(info)
	return info, nil
}

func (c *coordinator) HoldCall(ctx context.Context, modem *mmodem.Modem, callID string) (VoiceCall, error) {
	client, call, info, err := c.lookupVoiceCall(ctx, modem, callID)
	if err != nil {
		return VoiceCall{}, err
	}
	if err := call.Hold(ctx); err != nil {
		return VoiceCall{}, normalizeVoiceError(c.handleClientDisconnected(modem.EquipmentIdentifier, client, err))
	}
	info, _ = advanceVoiceCall(info, voiceCallTransition{
		state: string(call.State()),
		hold:  voiceHoldState(call),
		at:    time.Now(),
	})
	c.updateVoiceCall(modem.EquipmentIdentifier, callID, info)
	c.publishVoiceEvent(info)
	return info, nil
}

func (c *coordinator) ResumeCall(ctx context.Context, modem *mmodem.Modem, callID string) (VoiceCall, error) {
	client, call, info, err := c.lookupVoiceCall(ctx, modem, callID)
	if err != nil {
		return VoiceCall{}, err
	}
	if err := call.Resume(ctx); err != nil {
		return VoiceCall{}, normalizeVoiceError(c.handleClientDisconnected(modem.EquipmentIdentifier, client, err))
	}
	info, _ = advanceVoiceCall(info, voiceCallTransition{
		state: string(call.State()),
		hold:  voiceHoldState(call),
		at:    time.Now(),
	})
	c.updateVoiceCall(modem.EquipmentIdentifier, callID, info)
	c.publishVoiceEvent(info)
	return info, nil
}

func (c *coordinator) SendCallDTMF(ctx context.Context, modem *mmodem.Modem, callID string, digits string) error {
	client, call, _, err := c.lookupVoiceCall(ctx, modem, callID)
	if err != nil {
		return err
	}
	if err := call.SendDTMF(ctx, imsvoice.DTMFRequest{Digits: digits}); err != nil {
		return normalizeVoiceError(c.handleClientDisconnected(modem.EquipmentIdentifier, client, err))
	}
	return nil
}

func (c *coordinator) OpenCallMedia(ctx context.Context, modem *mmodem.Modem, callID string) (MediaSession, error) {
	_, call, _, err := c.lookupVoiceCall(ctx, modem, callID)
	if err != nil {
		return nil, err
	}
	media := call.Media()
	if !isSupportedCallMediaCodec(media.Codec) {
		return nil, ErrUnsupportedCodec
	}
	return callMediaSession{call: call, media: media}, nil
}

func isSupportedCallMediaCodec(codec imsvoice.AudioCodec) bool {
	return codec == imsvoice.CodecEVS || codec == imsvoice.CodecAMRWB || codec == imsvoice.CodecAMR || codec == imsvoice.CodecPCMU
}

func (c *coordinator) SubscribeVoiceEvents(fn VoiceEventFunc) func() {
	if fn == nil {
		return func() {}
	}
	c.mu.Lock()
	c.nextVoiceSubID++
	id := c.nextVoiceSubID
	c.voiceSubscribers[id] = fn
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		delete(c.voiceSubscribers, id)
		c.mu.Unlock()
	}
}

func normalizeVoiceError(err error) error {
	if errors.Is(err, imsgo.ErrClientNotConnected) {
		return ErrNotConnected
	}
	var responseErr *imssip.ResponseError
	if errors.As(err, &responseErr) {
		if text := strings.TrimSpace(responseErr.WarningText()); text != "" {
			return errors.New(text)
		}
		if text := strings.TrimSpace(responseErr.Error()); text != "" {
			return errors.New(text)
		}
	}
	return err
}

type callMediaSession struct {
	call  *imsvoice.Call
	media imsvoice.NegotiatedMedia
}

func (s callMediaSession) Info() MediaInfo {
	return MediaInfo{
		Codec:           string(s.media.Codec),
		PayloadType:     s.media.PayloadType,
		ClockRate:       s.media.ClockRate,
		Channels:        s.media.Channels,
		OctetAlign:      s.media.OctetAlign,
		HFOnly:          s.media.HFOnly,
		DTMFPayloadType: s.media.DTMFPayloadType,
		DTMFClockRate:   s.media.DTMFClockRate,
		PTimeMillis:     int(s.media.PTime / time.Millisecond),
	}
}

func (s callMediaSession) ReadPacket(ctx context.Context) ([]byte, error) {
	packet, _, err := s.call.ReadRTP(ctx)
	if err != nil {
		return nil, err
	}
	return packet, nil
}

func (s callMediaSession) WritePacket(ctx context.Context, packet []byte) error {
	if s.call.HoldState() != imsvoice.CallHoldNone {
		return ErrCallOnHold
	}
	return s.call.WriteRTP(ctx, packet)
}

func (c *coordinator) lookupVoiceCall(ctx context.Context, modem *mmodem.Modem, callID string) (*imsgo.Client, *imsvoice.Call, VoiceCall, error) {
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return nil, nil, VoiceCall{}, err
	}
	callID = strings.TrimSpace(callID)
	c.mu.Lock()
	defer c.mu.Unlock()
	session := c.sessions[modem.EquipmentIdentifier]
	if session == nil || session.profileID != profileID || session.calls == nil {
		return nil, nil, VoiceCall{}, ErrNotConnected
	}
	state := session.calls[callID]
	if state == nil || state.call == nil {
		return nil, nil, VoiceCall{}, ErrUnavailable
	}
	return session.client, state.call, state.info, nil
}

func (c *coordinator) storeVoiceCall(modemID string, profileID string, call *imsvoice.Call, number string, direction string, state string, reason string) VoiceCall {
	info, _ := c.storeVoiceCallForSession(modemID, 0, profileID, call, number, direction, state, reason)
	return info
}

func (c *coordinator) storeVoiceCallForSession(modemID string, sessionID uint64, profileID string, call *imsvoice.Call, number string, direction string, state string, reason string) (VoiceCall, bool) {
	now := time.Now()
	info := VoiceCall{
		ID:        call.ID(),
		Route:     c.routeName(),
		ModemID:   modemID,
		ProfileID: profileID,
		Direction: direction,
		Number:    number,
		State:     state,
		Hold:      voiceHoldState(call),
		Reason:    reason,
		StartedAt: now,
		UpdatedAt: now,
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	session := c.sessions[modemID]
	if !sameSession(session, sessionID) {
		return VoiceCall{}, false
	}
	if existing := voiceCallInfoLocked(session, call.ID()); existing.ID != "" {
		info.StartedAt = existing.StartedAt
		if info.StartedAt.IsZero() {
			info.StartedAt = now
		}
		if !existing.AnsweredAt.IsZero() {
			info.AnsweredAt = existing.AnsweredAt
		}
		if strings.TrimSpace(existing.Hold) != "" {
			info.Hold = existing.Hold
		}
	}
	updateVoiceCallWithPointerLocked(session, call.ID(), call, info)
	return info, true
}

func voiceHoldState(call *imsvoice.Call) string {
	if call == nil {
		return string(imsvoice.CallHoldNone)
	}
	hold := strings.TrimSpace(string(call.HoldState()))
	if hold == "" {
		return string(imsvoice.CallHoldNone)
	}
	return hold
}

func (c *coordinator) voiceCallInfo(modemID string, callID string) VoiceCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	return voiceCallInfoLocked(c.sessions[modemID], callID)
}

func voiceCallInfoLocked(session *sessionState, callID string) VoiceCall {
	if session == nil || session.calls == nil {
		return VoiceCall{}
	}
	state := session.calls[callID]
	if state == nil {
		return VoiceCall{}
	}
	return state.info
}

func (c *coordinator) setPendingVoiceDial(modemID string, profileID string, number string) pendingVoiceDial {
	pending := pendingVoiceDial{
		profileID: profileID,
		number:    strings.TrimSpace(number),
		startedAt: time.Now(),
	}
	c.mu.Lock()
	if session := c.sessions[modemID]; session != nil {
		session.pendingDial = &pending
	}
	c.mu.Unlock()
	return pending
}

func (c *coordinator) clearPendingVoiceDial(modemID string, pending pendingVoiceDial) {
	c.mu.Lock()
	defer c.mu.Unlock()
	session := c.sessions[modemID]
	if session == nil || session.pendingDial == nil {
		return
	}
	if *session.pendingDial == pending {
		session.pendingDial = nil
	}
}

func (c *coordinator) updateVoiceCall(modemID string, callID string, info VoiceCall) {
	c.updateVoiceCallWithPointer(modemID, callID, nil, info)
}

func (c *coordinator) updateVoiceCallWithPointer(modemID string, callID string, call *imsvoice.Call, info VoiceCall) {
	c.updateVoiceCallWithPointerForSession(modemID, 0, callID, call, info)
}

func (c *coordinator) updateVoiceCallForSession(modemID string, sessionID uint64, callID string, info VoiceCall) bool {
	return c.updateVoiceCallWithPointerForSession(modemID, sessionID, callID, nil, info)
}

func (c *coordinator) updateVoiceCallWithPointerForSession(modemID string, sessionID uint64, callID string, call *imsvoice.Call, info VoiceCall) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	session := c.sessions[modemID]
	if !sameSession(session, sessionID) {
		return false
	}
	updateVoiceCallWithPointerLocked(session, callID, call, info)
	return true
}

func updateVoiceCallWithPointerLocked(session *sessionState, callID string, call *imsvoice.Call, info VoiceCall) {
	if session.calls == nil {
		session.calls = make(map[string]*voiceCallState)
	}
	state := session.calls[callID]
	if state == nil {
		state = &voiceCallState{}
		session.calls[callID] = state
	}
	if call != nil {
		state.call = call
	}
	state.info = info
	state.updatedAt = info.UpdatedAt
}

func (c *coordinator) publishVoiceEvent(call VoiceCall) {
	c.mu.Lock()
	subscribers := slices.Collect(maps.Values(c.voiceSubscribers))
	c.mu.Unlock()
	event := VoiceEvent{Call: call}
	for _, fn := range subscribers {
		fn(event)
	}
}

func (c *coordinator) forwardIncomingCall(modem *mmodem.Modem, profileID string, sessionID uint64, incoming imsgo.IncomingCall) {
	if incoming.Call == nil {
		return
	}
	info, ok := c.storeVoiceCallForSession(modem.EquipmentIdentifier, sessionID, profileID, incoming.Call, incoming.FromNumber, string(incoming.Call.Direction()), string(incoming.Call.State()), "")
	if !ok {
		return
	}
	info.Hold = voiceHoldState(incoming.Call)
	if !incoming.ReceivedAt.IsZero() {
		info.StartedAt = incoming.ReceivedAt
		info.UpdatedAt = incoming.ReceivedAt
		c.updateVoiceCallForSession(modem.EquipmentIdentifier, sessionID, info.ID, info)
	}
	c.publishVoiceEvent(info)
}

func (c *coordinator) forwardCallEvent(modemID string, sessionID uint64, event imsgo.CallEvent) {
	c.mu.Lock()
	session := c.sessions[modemID]
	var info VoiceCall
	changed := false
	if sameSession(session, sessionID) && session.calls != nil {
		state := session.calls[event.CallID]
		if state == nil && session.pendingDial != nil {
			info = VoiceCall{
				ID:        event.CallID,
				Route:     c.routeName(),
				ModemID:   modemID,
				ProfileID: session.pendingDial.profileID,
				Direction: string(imsvoice.CallDirectionOutgoing),
				Number:    session.pendingDial.number,
				Hold:      string(imsvoice.CallHoldNone),
				StartedAt: session.pendingDial.startedAt,
			}
			state = &voiceCallState{info: info, updatedAt: info.StartedAt}
			session.calls[event.CallID] = state
		}
		if state != nil {
			if event.Call != nil {
				state.call = event.Call
			}
			info = state.info
			hold := strings.TrimSpace(string(event.Hold))
			if hold == "" && event.Call != nil {
				hold = voiceHoldState(event.Call)
			}
			if hold == "" && info.Hold == "" {
				hold = string(imsvoice.CallHoldNone)
			}
			info, changed = advanceVoiceCall(info, voiceCallTransition{
				state:     string(event.State),
				hold:      hold,
				reason:    event.Cause,
				reasonSet: true,
				at:        event.At,
			})
			if changed {
				state.info = info
				state.updatedAt = info.UpdatedAt
			}
		}
	}
	c.mu.Unlock()
	if changed {
		c.publishVoiceEvent(info)
	}
}

func sameSession(session *sessionState, sessionID uint64) bool {
	return session != nil && (sessionID == 0 || session.id == sessionID)
}

func isTerminalVoiceCallState(state string) bool {
	return state == string(imsvoice.CallStateEnded) || state == string(imsvoice.CallStateFailed)
}
