package wwan

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	uiccmbim "github.com/damonto/wwan-go/mbim"
	wwanmodem "github.com/damonto/wwan-go/modem"
	usim "github.com/damonto/wwan-go/sim"
	usimcard "github.com/damonto/wwan-go/sim/card"
	stkpkg "github.com/damonto/wwan-go/sim/stk"
)

type mbimNetwork interface {
	SubscriberReadyStatus(ctx context.Context) (uiccmbim.SubscriberReadyStatusResponse, error)
	RegistrationState(ctx context.Context) (uiccmbim.RegistrationStateInfo, error)
	PacketService(ctx context.Context) (uiccmbim.PacketServiceInfo, error)
	ProvisionedContexts(ctx context.Context) ([]uiccmbim.ProvisionedContext, error)
	Close() error
}

type mbimSessionClient interface {
	mbimNetwork
	openUSIM(context.Context) (usimcard.Reader, error)
}

type openedMBIMSessionClient struct {
	*uiccmbim.Client
}

func (c *openedMBIMSessionClient) openUSIM(context.Context) (usimcard.Reader, error) {
	return usim.NewMBIM(c.Client)
}

type mbimSession struct {
	slot       uint8
	openClient func(context.Context, uint8) (mbimSessionClient, error)

	mu        sync.Mutex
	clients   map[uint8]mbimSessionClient
	closed    bool
	closeOnce sync.Once
	closeErr  error
}

func newMBIMSession(cfg Config) *mbimSession {
	return newMBIMSessionWithOpener(cfg, func(ctx context.Context, slot uint8) (mbimSessionClient, error) {
		client, err := openMBIMClient(ctx, cfg.Device, slot)
		if err != nil {
			return nil, err
		}
		return &openedMBIMSessionClient{Client: client}, nil
	})
}

func newMBIMSessionWithOpener(cfg Config, open func(context.Context, uint8) (mbimSessionClient, error)) *mbimSession {
	return &mbimSession{
		slot:       cfg.Slot,
		openClient: open,
	}
}

func newMBIMSessionWithMBIMOpener(cfg Config, open func(context.Context, uint8) (*uiccmbim.Client, error)) *mbimSession {
	return newMBIMSessionWithOpener(cfg, func(ctx context.Context, slot uint8) (mbimSessionClient, error) {
		client, err := open(ctx, slot)
		if err != nil {
			return nil, err
		}
		return &openedMBIMSessionClient{Client: client}, nil
	})
}

func (s *mbimSession) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		clients := make([]mbimSessionClient, 0, len(s.clients))
		for _, client := range s.clients {
			clients = append(clients, client)
		}
		s.clients = nil
		s.mu.Unlock()

		for _, client := range clients {
			s.closeErr = errors.Join(s.closeErr, client.Close())
		}
	})
	return s.closeErr
}

func (s *mbimSession) acquireClient(ctx context.Context, slot uint8) (mbimSessionClient, func(error), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, func(error) {}, wwanmodem.ErrClosed
	}
	if client := s.clients[slot]; client != nil {
		return client, func(error) {}, nil
	}
	client, err := s.openClient(ctx, slot)
	if err != nil {
		return nil, func(error) {}, err
	}
	if s.clients == nil {
		s.clients = make(map[uint8]mbimSessionClient)
	}
	s.clients[slot] = client
	return client, func(error) {}, nil
}

func (s *mbimSession) MSISDN(ctx context.Context) (number string, err error) {
	client, release, err := s.acquireClient(ctx, s.slot)
	if err != nil {
		return "", fmt.Errorf("open MBIM network client: %w", err)
	}
	defer func() { release(err) }()

	status, err := client.SubscriberReadyStatus(ctx)
	if err != nil {
		return "", fmt.Errorf("read MBIM subscriber ready status: %w", err)
	}
	for _, number := range status.TelephoneNumbers {
		if number = strings.TrimSpace(number); number != "" {
			return number, nil
		}
	}
	return "", nil
}

func (s *mbimSession) SIMState(ctx context.Context, target Target) (state SIMState, err error) {
	slot, err := targetSIMSlot(s.slot, target)
	if err != nil {
		return SIMState{Supported: true}, err
	}
	state = SIMState{Supported: true, Slot: slot}

	client, release, err := s.acquireClient(ctx, slot)
	if err != nil {
		return state, fmt.Errorf("open MBIM network client: %w", err)
	}
	defer func() { release(err) }()

	status, err := client.SubscriberReadyStatus(ctx)
	if err != nil {
		return state, fmt.Errorf("read MBIM subscriber ready status: %w", err)
	}
	state.Ready = status.ReadyState == uiccmbim.SubscriberReadyStateInitialized
	state.ICCID = strings.TrimSpace(status.SIMICCID)
	state.Recoverable = state.ICCID != ""
	target.ICCID = strings.TrimSpace(target.ICCID)
	state.Matches = ICCIDMatches(state.ICCID, target.ICCID)
	state.ICCIDMismatch = target.ICCID != "" && state.ICCID != "" && !state.Matches
	return state, nil
}

func (s *mbimSession) USIM(ctx context.Context) (usimcard.Reader, error) {
	client, release, err := s.acquireClient(ctx, s.slot)
	if err != nil {
		return nil, fmt.Errorf("open MBIM UIM client: %w", err)
	}
	defer func() { release(err) }()
	reader, err := client.openUSIM(ctx)
	if err != nil {
		return nil, fmt.Errorf("create MBIM USIM reader: %w", err)
	}
	return &persistentMBIMReader{Reader: reader}, nil
}

func (s *mbimSession) USIMWithCAT(ctx context.Context, _ CATProfile) (usimcard.Reader, error) {
	return s.USIM(ctx)
}

func (s *mbimSession) PacketServiceStatus(ctx context.Context) (result PacketServiceStatus, err error) {
	client, release, err := s.acquireClient(ctx, s.slot)
	if err != nil {
		return PacketServiceStatus{}, fmt.Errorf("open MBIM network client: %w", err)
	}
	defer func() { release(err) }()

	registration, err := client.RegistrationState(ctx)
	if err != nil {
		return PacketServiceStatus{}, fmt.Errorf("read MBIM registration state: %w", err)
	}
	packet, err := client.PacketService(ctx)
	if err != nil {
		return PacketServiceStatus{}, fmt.Errorf("read MBIM packet service: %w", err)
	}
	return PacketServiceStatus{
		Registered: mbimRegistered(registration.RegisterState),
		PSAttached: packet.PacketServiceState == uiccmbim.PacketServiceStateAttached,
		LTE:        packet.HighestAvailableDataClass&mbimDataClassLTE != 0,
	}, nil
}

func (s *mbimSession) IMSProfile(ctx context.Context) (result IMSProfile, err error) {
	client, release, err := s.acquireClient(ctx, s.slot)
	if err != nil {
		return IMSProfile{}, fmt.Errorf("open MBIM network client: %w", err)
	}
	defer func() { release(err) }()

	found, err := mbimIMSContextAvailable(ctx, client)
	if err != nil {
		return IMSProfile{}, err
	}
	if !found {
		return IMSProfile{}, errors.New("MBIM IMS provisioned context is unavailable")
	}
	return IMSProfile{}, nil
}

// persistentMBIMReader keeps the session-owned MBIM client alive after the
// caller closes its reader view. The modem generation owns the actual client.
type persistentMBIMReader struct {
	usimcard.Reader
}

func (r *persistentMBIMReader) Close() error {
	return nil
}

func (r *persistentMBIMReader) STK() (*usim.STK, error) {
	reader, ok := r.Reader.(interface{ STK() (*usim.STK, error) })
	if !ok {
		return nil, ErrUnsupported
	}
	return reader.STK()
}

func (r *persistentMBIMReader) Commands(ctx context.Context, profile stkpkg.Profile) (<-chan usim.STKSession, error) {
	reader, ok := r.Reader.(interface {
		Commands(context.Context, stkpkg.Profile) (<-chan usim.STKSession, error)
	})
	if !ok {
		return nil, ErrUnsupported
	}
	return reader.Commands(ctx, profile)
}

func (r *persistentMBIMReader) Respond(ctx context.Context, session usim.STKSession, response stkpkg.TerminalResponse) error {
	reader, ok := r.Reader.(interface {
		Respond(context.Context, usim.STKSession, stkpkg.TerminalResponse) error
	})
	if !ok {
		return ErrUnsupported
	}
	return reader.Respond(ctx, session, response)
}

func (r *persistentMBIMReader) Envelope(ctx context.Context, envelope []byte) (stkpkg.EnvelopeResponse, error) {
	reader, ok := r.Reader.(interface {
		Envelope(context.Context, []byte) (stkpkg.EnvelopeResponse, error)
	})
	if !ok {
		return stkpkg.EnvelopeResponse{}, ErrUnsupported
	}
	return reader.Envelope(ctx, envelope)
}

const mbimDataClassLTE uint32 = 1 << 5

func mbimRegistered(state uiccmbim.RegisterState) bool {
	switch state {
	case uiccmbim.RegisterStateHome, uiccmbim.RegisterStateRoaming, uiccmbim.RegisterStatePartner:
		return true
	default:
		return false
	}
}

func mbimIMSContextAvailable(ctx context.Context, client mbimNetwork) (bool, error) {
	contexts, err := client.ProvisionedContexts(ctx)
	if err != nil {
		return false, fmt.Errorf("read MBIM provisioned contexts: %w", err)
	}
	for _, profile := range contexts {
		if profile.ContextType != uiccmbim.ContextTypeIMS || !strings.EqualFold(strings.TrimSpace(profile.AccessString), uiccmbim.DefaultIMSPDNAPN) {
			continue
		}
		return true, nil
	}
	return false, nil
}

func openMBIMClient(ctx context.Context, device string, slot uint8) (*uiccmbim.Client, error) {
	return uiccmbim.Open(ctx, uiccmbim.WithAutoDetect(device), uiccmbim.WithSlot(int(slot)))
}
