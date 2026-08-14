package wwan

import (
	"context"
	"errors"
	"slices"
	"testing"

	uiccmbim "github.com/damonto/wwan-go/mbim"
	wwanmodem "github.com/damonto/wwan-go/modem"
	usimcard "github.com/damonto/wwan-go/sim/card"
)

func TestSessionMSISDNMBIM(t *testing.T) {
	readErr := errors.New("subscriber status unavailable")
	tests := []struct {
		name    string
		numbers []string
		err     error
		want    string
	}{
		{name: "single number", numbers: []string{"+15551234567"}, want: "+15551234567"},
		{name: "first non-empty number", numbers: []string{" ", " +8613800138000 "}, want: "+8613800138000"},
		{name: "empty list"},
		{name: "query error", err: readErr},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeMBIMNetwork{subscriberReady: uiccmbim.SubscriberReadyStatusResponse{TelephoneNumbers: slices.Clone(tt.numbers)}, subscriberReadyErr: tt.err}
			session := mbimSessionWithNetwork(client)
			got, err := session.MSISDN(t.Context())
			if tt.err != nil {
				if !errors.Is(err, tt.err) {
					t.Fatalf("MSISDN() error = %v, want %v", err, tt.err)
				}
			} else if err != nil {
				t.Fatalf("MSISDN() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("MSISDN() = %q, want %q", got, tt.want)
			}
			if client.closed {
				t.Fatal("session client closed after one operation")
			}
			if err := session.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
			if !client.closed {
				t.Fatal("session client was not closed")
			}
		})
	}
}

func TestSessionSIMStateMBIM(t *testing.T) {
	const iccid = "8901000000000000001"
	readErr := errors.New("subscriber status unavailable")
	tests := []struct {
		name    string
		target  Target
		ready   uiccmbim.SubscriberReadyStatusResponse
		err     error
		want    SIMState
		wantErr error
	}{
		{
			name:   "matching initialized sim",
			target: Target{ICCID: " " + iccid + " "},
			ready: uiccmbim.SubscriberReadyStatusResponse{
				ReadyState: uiccmbim.SubscriberReadyStateInitialized,
				SIMICCID:   " " + iccid + " ",
			},
			want: SIMState{
				Supported:   true,
				Matches:     true,
				Recoverable: true,
				Ready:       true,
				ICCID:       iccid,
				Slot:        1,
			},
		},
		{
			name:   "matching initialized sim with trailing target padding",
			target: Target{ICCID: iccid + "f"},
			ready: uiccmbim.SubscriberReadyStatusResponse{
				ReadyState: uiccmbim.SubscriberReadyStateInitialized,
				SIMICCID:   iccid,
			},
			want: SIMState{
				Supported:   true,
				Matches:     true,
				Recoverable: true,
				Ready:       true,
				ICCID:       iccid,
				Slot:        1,
			},
		},
		{
			name:   "waits for initialized state",
			target: Target{ICCID: iccid},
			ready: uiccmbim.SubscriberReadyStatusResponse{
				ReadyState: uiccmbim.SubscriberReadyStateNotInitialized,
				SIMICCID:   iccid,
			},
			want: SIMState{
				Supported:   true,
				Matches:     true,
				Recoverable: true,
				ICCID:       iccid,
				Slot:        1,
			},
		},
		{
			name:   "reports ICCID mismatch",
			target: Target{ICCID: iccid},
			ready: uiccmbim.SubscriberReadyStatusResponse{
				ReadyState: uiccmbim.SubscriberReadyStateInitialized,
				SIMICCID:   "8901000000000000002",
			},
			want: SIMState{
				Supported:     true,
				Recoverable:   true,
				Ready:         true,
				ICCIDMismatch: true,
				ICCID:         "8901000000000000002",
				Slot:          1,
			},
		},
		{
			name:    "query error",
			target:  Target{ICCID: iccid},
			err:     readErr,
			want:    SIMState{Supported: true, Slot: 1},
			wantErr: readErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeMBIMNetwork{subscriberReady: tt.ready, subscriberReadyErr: tt.err}
			session := mbimSessionWithNetwork(client)
			got, err := session.SIMState(t.Context(), tt.target)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("SIMState() error = %v, want %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("SIMState() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("SIMState() = %+v, want %+v", got, tt.want)
			}
			if client.closed {
				t.Fatal("session client closed after one operation")
			}
			if err := session.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
			if !client.closed {
				t.Fatal("session client was not closed")
			}
		})
	}
}

func TestSessionVoLTEStatusMBIM(t *testing.T) {
	device, err := OpenSession(Config{PortType: PortTypeMBIM, Device: "/dev/cdc-wdm0", Slot: 1})
	if err != nil {
		t.Fatalf("OpenSession() error = %v", err)
	}
	t.Cleanup(func() { _ = device.Close() })

	got, err := device.VoLTEStatus(t.Context())
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("VoLTEStatus() error = %v, want %v", err, ErrUnsupported)
	}
	if got != (VoLTEStatus{}) {
		t.Fatalf("VoLTEStatus() = %+v, want zero status", got)
	}
}

func TestSessionPacketServiceStatusMBIM(t *testing.T) {
	tests := []struct {
		name         string
		registration uiccmbim.RegisterState
		packet       uiccmbim.PacketServiceInfo
		want         PacketServiceStatus
	}{
		{
			name:         "registered attached LTE",
			registration: uiccmbim.RegisterStateHome,
			packet: uiccmbim.PacketServiceInfo{
				PacketServiceState:        uiccmbim.PacketServiceStateAttached,
				HighestAvailableDataClass: mbimDataClassLTE,
			},
			want: PacketServiceStatus{Registered: true, PSAttached: true, LTE: true},
		},
		{name: "searching detached", registration: uiccmbim.RegisterStateSearching},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeMBIMNetwork{
				registration: uiccmbim.RegistrationStateInfo{RegisterState: tt.registration},
				packet:       tt.packet,
			}
			device := mbimSessionWithNetwork(client)
			t.Cleanup(func() { _ = device.Close() })
			got, err := device.PacketServiceStatus(t.Context())
			if err != nil {
				t.Fatalf("PacketServiceStatus() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("PacketServiceStatus() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestSessionIMSProfileMBIM(t *testing.T) {
	tests := []struct {
		name     string
		contexts []uiccmbim.ProvisionedContext
		want     IMSProfile
		wantErr  bool
	}{
		{name: "finds IMS profile", contexts: []uiccmbim.ProvisionedContext{{ContextID: 7, ContextType: uiccmbim.ContextTypeIMS, AccessString: "ims"}}},
		{name: "requires IMS APN", contexts: []uiccmbim.ProvisionedContext{{ContextID: 7, ContextType: uiccmbim.ContextTypeIMS, AccessString: "internet"}}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			device := mbimSessionWithNetwork(&fakeMBIMNetwork{contexts: slices.Clone(tt.contexts)})
			t.Cleanup(func() { _ = device.Close() })
			got, err := device.IMSProfile(t.Context())
			if tt.wantErr {
				if err == nil {
					t.Fatal("IMSProfile() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("IMSProfile() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("IMSProfile() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

type fakeMBIMNetwork struct {
	subscriberReady    uiccmbim.SubscriberReadyStatusResponse
	subscriberReadyErr error
	registration       uiccmbim.RegistrationStateInfo
	registrationErr    error
	packet             uiccmbim.PacketServiceInfo
	packetErr          error
	contexts           []uiccmbim.ProvisionedContext
	contextsErr        error
	closed             bool
}

func (r *fakeMBIMNetwork) SubscriberReadyStatus(context.Context) (uiccmbim.SubscriberReadyStatusResponse, error) {
	return r.subscriberReady, r.subscriberReadyErr
}

func (r *fakeMBIMNetwork) RegistrationState(context.Context) (uiccmbim.RegistrationStateInfo, error) {
	return r.registration, r.registrationErr
}

func (r *fakeMBIMNetwork) PacketService(context.Context) (uiccmbim.PacketServiceInfo, error) {
	return r.packet, r.packetErr
}

func (r *fakeMBIMNetwork) ProvisionedContexts(context.Context) ([]uiccmbim.ProvisionedContext, error) {
	return slices.Clone(r.contexts), r.contextsErr
}

func (r *fakeMBIMNetwork) Close() error {
	r.closed = true
	return nil
}

func mbimSessionWithNetwork(client *fakeMBIMNetwork) *mbimSession {
	return newMBIMSessionWithOpener(Config{Slot: 1}, func(context.Context, uint8) (mbimSessionClient, error) {
		return &fakeMBIMSessionClient{fakeMBIMNetwork: client}, nil
	})
}

func TestMBIMSessionReusesClientPerSlot(t *testing.T) {
	clients := make(map[uint8]*fakeMBIMSessionClient)
	opens := 0
	session := newMBIMSessionWithOpener(Config{Slot: 1}, func(context.Context, uint8) (mbimSessionClient, error) {
		opens++
		client := &fakeMBIMSessionClient{fakeMBIMNetwork: &fakeMBIMNetwork{
			subscriberReady: uiccmbim.SubscriberReadyStatusResponse{
				TelephoneNumbers: []string{"+15551234567"},
			},
		}}
		clients[uint8(opens)] = client
		return client, nil
	})

	for range 2 {
		if _, err := session.MSISDN(t.Context()); err != nil {
			t.Fatalf("MSISDN() error = %v", err)
		}
	}
	if opens != 1 {
		t.Fatalf("MBIM client opens = %d, want 1 for one slot", opens)
	}
	if _, err := session.SIMState(t.Context(), Target{Slot: 2}); err != nil {
		t.Fatalf("SIMState() error = %v", err)
	}
	if _, err := session.SIMState(t.Context(), Target{Slot: 2}); err != nil {
		t.Fatalf("SIMState(slot 2) error = %v", err)
	}
	if opens != 2 {
		t.Fatalf("MBIM client opens = %d, want one client per slot", opens)
	}

	if err := session.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	for slot, client := range clients {
		if !client.closed {
			t.Errorf("MBIM client for slot %d was not closed", slot)
		}
	}
	if _, err := session.MSISDN(t.Context()); !errors.Is(err, wwanmodem.ErrClosed) {
		t.Fatalf("MSISDN() after Close() error = %v, want %v", err, wwanmodem.ErrClosed)
	}
}

func TestOpenSessionAcceptsMBIM(t *testing.T) {
	session, err := OpenSession(Config{PortType: PortTypeMBIM, Device: "/dev/cdc-wdm0", Slot: 1})
	if err != nil {
		t.Fatalf("OpenSession() error = %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	if _, ok := session.backend.(*mbimSession); !ok {
		t.Fatalf("OpenSession() backend = %T, want *mbimSession", session.backend)
	}
}

type fakeMBIMSessionClient struct {
	*fakeMBIMNetwork
}

func (c *fakeMBIMSessionClient) openUSIM(context.Context) (usimcard.Reader, error) {
	return nil, errors.New("fake MBIM USIM reader is unavailable")
}
