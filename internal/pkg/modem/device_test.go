package modem

import (
	"context"
	"errors"
	"sync"
	"testing"

	wwan "github.com/damonto/sigmo/internal/pkg/modem/wwan"
	wwanmodem "github.com/damonto/wwan-go/modem"
)

func TestOpenDeviceRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		modem   *Modem
		wantErr error
	}{
		{name: "nil modem", wantErr: errModemRequired},
		{
			name: "unsupported port",
			modem: &Modem{
				PrimaryPort: "/dev/ttyUSB0",
				Ports: []ModemPort{
					{PortType: wwanmodem.PortAT, Device: "/dev/ttyUSB0"},
				},
			},
			wantErr: wwan.ErrUnsupported,
		},
		{
			name: "slot too large",
			modem: &Modem{
				PrimaryPort:    "/dev/cdc-wdm0",
				PrimarySIMSlot: maxSIMSlot + 1,
				Ports: []ModemPort{
					{PortType: wwanmodem.PortQMI, Device: "/dev/cdc-wdm0"},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			device, err := OpenDevice(tt.modem)
			if err == nil {
				t.Fatal("OpenDevice() error = nil, want error")
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("OpenDevice() error = %v, want %v", err, tt.wantErr)
			}
			if device != nil {
				t.Fatalf("OpenDevice() device = %v, want nil", device)
			}
		})
	}
}

func TestOpenDeviceReusesQMISessionForModemGeneration(t *testing.T) {
	modem := &Modem{
		EquipmentIdentifier: "123456789012345",
		PrimaryPort:         "/dev/cdc-wdm0",
		PrimarySIMSlot:      1,
		Ports: []ModemPort{
			{PortType: wwanmodem.PortQMI, Device: "/dev/cdc-wdm0"},
		},
	}

	first, err := OpenDevice(modem)
	if err != nil {
		t.Fatalf("OpenDevice(first) error = %v", err)
	}
	second, err := OpenDevice(modem)
	if err != nil {
		t.Fatalf("OpenDevice(second) error = %v", err)
	}
	firstSession, ok := first.(*wwan.Session)
	if !ok {
		t.Fatalf("OpenDevice(first) type = %T, want *wwan.Session", first)
	}
	secondSession, ok := second.(*wwan.Session)
	if !ok {
		t.Fatalf("OpenDevice(second) type = %T, want *wwan.Session", second)
	}
	if firstSession != secondSession {
		t.Fatal("OpenDevice() returned different sessions for the same modem generation and SIM slot")
	}

	control, err := openQMIDeviceForSlot(modem, 1, nil)
	if err != nil {
		t.Fatalf("openQMIDeviceForSlot() error = %v", err)
	}
	if control != firstSession {
		t.Fatal("slot-specific control path did not reuse the generation session")
	}

	modem.PrimarySIMSlot = 2
	third, err := OpenDevice(modem)
	if err != nil {
		t.Fatalf("OpenDevice(slot 2) error = %v", err)
	}
	thirdSession, ok := third.(*wwan.Session)
	if !ok {
		t.Fatalf("OpenDevice(slot 2) type = %T, want *wwan.Session", third)
	}
	if thirdSession == firstSession {
		t.Fatal("OpenDevice() reused one QMI session across different SIM slots")
	}

	if err := modem.Close(); err != nil {
		t.Fatalf("Modem.Close() error = %v", err)
	}
	for name, device := range map[string]Device{"slot 1": first, "slot 2": third} {
		t.Run(name, func(t *testing.T) {
			if _, err := device.PacketServiceStatus(t.Context()); err == nil {
				t.Fatal("PacketServiceStatus() after Modem.Close() error = nil, want error")
			}
		})
	}
	if _, err := OpenDevice(modem); !errors.Is(err, wwanmodem.ErrClosed) {
		t.Fatalf("OpenDevice() after Modem.Close() error = %v, want %v", err, wwanmodem.ErrClosed)
	}
}

func TestOpenDeviceConcurrentQMISessionReuse(t *testing.T) {
	modem := &Modem{
		PrimaryPort:    "/dev/cdc-wdm0",
		PrimarySIMSlot: 1,
		Ports: []ModemPort{
			{PortType: wwanmodem.PortQMI, Device: "/dev/cdc-wdm0"},
		},
	}
	const count = 32
	sessions := make([]*wwan.Session, count)
	errs := make([]error, count)

	var wg sync.WaitGroup
	for i := range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			device, err := OpenDevice(modem)
			if err != nil {
				errs[i] = err
				return
			}
			var ok bool
			sessions[i], ok = device.(*wwan.Session)
			if !ok {
				errs[i] = errors.New("device is not a QMI session")
			}
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("OpenDevice(%d) error = %v", i, err)
		}
		if sessions[i] != sessions[0] {
			t.Fatalf("OpenDevice(%d) returned a different session", i)
		}
	}
	if err := modem.Close(); err != nil {
		t.Fatalf("Modem.Close() error = %v", err)
	}
}

func TestOpenDeviceReusesMBIMSessionForModemGeneration(t *testing.T) {
	modem := &Modem{
		PrimaryPort:    "/dev/cdc-wdm0",
		PrimarySIMSlot: 1,
		Ports: []ModemPort{
			{PortType: wwanmodem.PortMBIM, Device: "/dev/cdc-wdm0"},
		},
	}
	first, err := OpenDevice(modem)
	if err != nil {
		t.Fatalf("OpenDevice(first) error = %v", err)
	}
	second, err := OpenDevice(modem)
	if err != nil {
		t.Fatalf("OpenDevice(second) error = %v", err)
	}
	firstSession, ok := first.(*wwan.Session)
	if !ok {
		t.Fatalf("OpenDevice(first) type = %T, want *wwan.Session", first)
	}
	secondSession, ok := second.(*wwan.Session)
	if !ok {
		t.Fatalf("OpenDevice(second) type = %T, want *wwan.Session", second)
	}
	if firstSession != secondSession {
		t.Fatal("OpenDevice() returned different MBIM sessions for the same modem generation and SIM slot")
	}
	modem.PrimarySIMSlot = 2
	third, err := OpenDevice(modem)
	if err != nil {
		t.Fatalf("OpenDevice(slot 2) error = %v", err)
	}
	thirdSession, ok := third.(*wwan.Session)
	if !ok {
		t.Fatalf("OpenDevice(slot 2) type = %T, want *wwan.Session", third)
	}
	if thirdSession == firstSession {
		t.Fatal("OpenDevice() reused one MBIM session across different SIM slots")
	}
	if err := modem.Close(); err != nil {
		t.Fatalf("Modem.Close() error = %v", err)
	}
}

func TestOpenVoLTEDeviceReusesGenerationQMISession(t *testing.T) {
	modem := &Modem{
		EquipmentIdentifier: "123456789012345",
		PrimaryPort:         "/dev/cdc-wdm1",
		PrimarySIMSlot:      1,
		Ports: []ModemPort{
			{PortType: wwanmodem.PortQMI, Device: "/dev/cdc-wdm1"},
			{PortType: wwanmodem.PortMBIM, Device: "/dev/cdc-wdm0"},
		},
	}
	device, err := OpenDevice(modem)
	if err != nil {
		t.Fatalf("OpenDevice() error = %v", err)
	}
	volte, err := OpenVoLTEDevice(modem)
	if err != nil {
		t.Fatalf("OpenVoLTEDevice() error = %v", err)
	}
	deviceSession, ok := device.(*wwan.Session)
	if !ok {
		t.Fatalf("OpenDevice() type = %T, want *wwan.Session", device)
	}
	volteSession, ok := volte.(*wwan.Session)
	if !ok {
		t.Fatalf("OpenVoLTEDevice() type = %T, want *wwan.Session", volte)
	}
	if deviceSession != volteSession {
		t.Fatal("OpenVoLTEDevice() did not reuse the ordinary QMI control session")
	}
	if err := modem.Close(); err != nil {
		t.Fatalf("Modem.Close() error = %v", err)
	}
}

func TestMBIMDeviceUnsupportedOperations(t *testing.T) {
	device, err := OpenDevice(&Modem{
		PrimaryPort:    "/dev/cdc-wdm0",
		PrimarySIMSlot: 1,
		Ports: []ModemPort{
			{PortType: wwanmodem.PortMBIM, Device: "/dev/cdc-wdm0"},
		},
	})
	if err != nil {
		t.Fatalf("OpenDevice() error = %v", err)
	}

	tests := []struct {
		name string
		run  func(context.Context) error
	}{
		{name: "activate provisioning", run: device.ActivateProvisioningIfSIMMissing},
		{name: "update MSISDN", run: func(ctx context.Context) error {
			return device.UpdateMSISDN(ctx, "+15551234567")
		}},
		{name: "read IMSS test mode", run: func(ctx context.Context) error {
			_, err := device.IMSSTestMode(ctx)
			return err
		}},
		{name: "set IMSS test mode", run: func(ctx context.Context) error {
			return device.SetIMSSTestMode(ctx, true)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(t.Context()); !errors.Is(err, wwan.ErrUnsupported) {
				t.Fatalf("%s error = %v, want %v", tt.name, err, wwan.ErrUnsupported)
			}
		})
	}
}

func TestOpenDeviceSelectsModemDevicePort(t *testing.T) {
	tests := []struct {
		name       string
		modem      *Modem
		wantDevice string
		wantType   wwanmodem.PortType
	}{
		{
			name: "uses primary QMI port",
			modem: &Modem{
				PrimaryPort: "/dev/cdc-wdm1",
				Ports: []ModemPort{
					{PortType: wwanmodem.PortMBIM, Device: "/dev/cdc-wdm0"},
					{PortType: wwanmodem.PortQMI, Device: "/dev/cdc-wdm1"},
				},
			},
			wantDevice: "/dev/cdc-wdm1",
			wantType:   wwanmodem.PortQMI,
		},
		{
			name: "falls back to QMI port",
			modem: &Modem{
				PrimaryPort: "/dev/ttyUSB0",
				Ports: []ModemPort{
					{PortType: wwanmodem.PortAT, Device: "/dev/ttyUSB0"},
					{PortType: wwanmodem.PortQMI, Device: "/dev/cdc-wdm1"},
				},
			},
			wantDevice: "/dev/cdc-wdm1",
			wantType:   wwanmodem.PortQMI,
		},
		{
			name: "falls back to MBIM port",
			modem: &Modem{
				PrimaryPort: "/dev/ttyUSB0",
				Ports: []ModemPort{
					{PortType: wwanmodem.PortAT, Device: "/dev/ttyUSB0"},
					{PortType: wwanmodem.PortMBIM, Device: "/dev/cdc-wdm0"},
				},
			},
			wantDevice: "/dev/cdc-wdm0",
			wantType:   wwanmodem.PortMBIM,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoint, err := ResolveDeviceEndpoint(tt.modem)
			if err != nil {
				t.Fatalf("ResolveDeviceEndpoint() error = %v", err)
			}
			if endpoint.Port.PortType != tt.wantType {
				t.Fatalf("ResolveDeviceEndpoint() port type = %d, want %d", endpoint.Port.PortType, tt.wantType)
			}
			if endpoint.Port.Device != tt.wantDevice {
				t.Fatalf("ResolveDeviceEndpoint() device = %q, want %q", endpoint.Port.Device, tt.wantDevice)
			}
			if endpoint.SIMSlot != 1 {
				t.Fatalf("ResolveDeviceEndpoint() SIM slot = %d, want 1", endpoint.SIMSlot)
			}
		})
	}
}

func TestQMIDeviceConfigForSlotPrefersQMI(t *testing.T) {
	tests := []struct {
		name       string
		modem      *Modem
		wantDevice string
		wantType   wwan.PortType
		wantErr    error
	}{
		{
			name:    "rejects nil modem",
			wantErr: errModemRequired,
		},
		{
			name: "uses QMI even when primary is MBIM",
			modem: &Modem{
				PrimaryPort: "/dev/cdc-wdm0",
				Ports: []ModemPort{
					{PortType: wwanmodem.PortMBIM, Device: "/dev/cdc-wdm0"},
					{PortType: wwanmodem.PortQMI, Device: "/dev/cdc-wdm1"},
				},
			},
			wantDevice: "/dev/cdc-wdm1",
			wantType:   wwan.PortTypeQMI,
		},
		{
			name: "rejects modem without QMI",
			modem: &Modem{
				PrimaryPort: "/dev/cdc-wdm0",
				Ports: []ModemPort{
					{PortType: wwanmodem.PortMBIM, Device: "/dev/cdc-wdm0"},
				},
			},
			wantErr: wwan.ErrUnsupported,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := qmiDeviceConfigForSlot(tt.modem, 1)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("qmiDeviceConfigForSlot() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("qmiDeviceConfigForSlot() error = %v", err)
			}
			if cfg.PortType != tt.wantType {
				t.Fatalf("qmiDeviceConfigForSlot() port type = %d, want %d", cfg.PortType, tt.wantType)
			}
			if cfg.Device != tt.wantDevice {
				t.Fatalf("qmiDeviceConfigForSlot() device = %q, want %q", cfg.Device, tt.wantDevice)
			}
		})
	}
}

func TestResolveVoLTEEndpointPrefersQMIFallsBackToMBIM(t *testing.T) {
	tests := []struct {
		name       string
		modem      *Modem
		wantDevice string
		wantType   wwanmodem.PortType
		wantErr    error
	}{
		{name: "nil modem", wantErr: errModemRequired},
		{
			name: "prefers QMI for IMS takeover",
			modem: &Modem{PrimaryPort: "/dev/cdc-wdm0", Ports: []ModemPort{
				{PortType: wwanmodem.PortMBIM, Device: "/dev/cdc-wdm0"},
				{PortType: wwanmodem.PortQMI, Device: "/dev/cdc-wdm1"},
			}},
			wantDevice: "/dev/cdc-wdm1",
			wantType:   wwanmodem.PortQMI,
		},
		{
			name: "uses MBIM when QMI is unavailable",
			modem: &Modem{PrimaryPort: "/dev/cdc-wdm0", Ports: []ModemPort{
				{PortType: wwanmodem.PortMBIM, Device: "/dev/cdc-wdm0"},
			}},
			wantDevice: "/dev/cdc-wdm0",
			wantType:   wwanmodem.PortMBIM,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoint, err := ResolveVoLTEEndpoint(tt.modem)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("ResolveVoLTEEndpoint() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveVoLTEEndpoint() error = %v", err)
			}
			if endpoint.Port.PortType != tt.wantType || endpoint.Port.Device != tt.wantDevice {
				t.Fatalf("ResolveVoLTEEndpoint() = (%d, %q), want (%d, %q)", endpoint.Port.PortType, endpoint.Port.Device, tt.wantType, tt.wantDevice)
			}
		})
	}
}

func TestResolveVoLTEPortDoesNotRequireSIMSlot(t *testing.T) {
	modem := &Modem{
		PrimarySIMSlot: maxSIMSlot + 1,
		Ports: []ModemPort{{
			PortType: wwanmodem.PortMBIM,
			Device:   "/dev/cdc-wdm0",
		}},
	}

	port, err := ResolveVoLTEPort(modem)
	if err != nil {
		t.Fatalf("ResolveVoLTEPort() error = %v", err)
	}
	if port != modem.Ports[0] {
		t.Fatalf("ResolveVoLTEPort() = %+v, want %+v", port, modem.Ports[0])
	}
}

func TestActiveSIMSlot(t *testing.T) {
	tests := []struct {
		name    string
		modem   *Modem
		want    uint8
		wantErr bool
	}{
		{name: "primary slot", modem: &Modem{PrimarySIMSlot: 2}, want: 2},
		{name: "unspecified slot defaults to first slot", modem: &Modem{}, want: 1},
		{name: "slot out of range", modem: &Modem{PrimarySIMSlot: maxSIMSlot + 1}, wantErr: true},
		{name: "nil modem", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ActiveSIMSlot(tt.modem)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ActiveSIMSlot() error = %v, wantErr %t", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("ActiveSIMSlot() = %d, want %d", got, tt.want)
			}
		})
	}
}
