package modem

import (
	"context"
	"errors"
	"testing"

	wwan "github.com/damonto/sigmo/internal/pkg/modem/wwan"
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
					{PortType: ModemPortTypeAt, Device: "/dev/ttyUSB0"},
				},
			},
			wantErr: wwan.ErrUnsupported,
		},
		{
			name: "slot too large",
			modem: &Modem{
				PrimaryPort:    "/dev/cdc-wdm0",
				PrimarySimSlot: maxSIMSlot + 1,
				Ports: []ModemPort{
					{PortType: ModemPortTypeQmi, Device: "/dev/cdc-wdm0"},
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

func TestMBIMDeviceUnsupportedOperations(t *testing.T) {
	device, err := OpenDevice(&Modem{
		PrimaryPort:    "/dev/cdc-wdm0",
		PrimarySimSlot: 1,
		Ports: []ModemPort{
			{PortType: ModemPortTypeMbim, Device: "/dev/cdc-wdm0"},
		},
	})
	if err != nil {
		t.Fatalf("OpenDevice() error = %v", err)
	}

	tests := []struct {
		name string
		run  func(context.Context) error
	}{
		{name: "power cycle", run: device.PowerCycleSIM},
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
			if err := tt.run(context.Background()); !errors.Is(err, wwan.ErrUnsupported) {
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
		wantType   ModemPortType
	}{
		{
			name: "uses primary QMI port",
			modem: &Modem{
				PrimaryPort: "/dev/cdc-wdm1",
				Ports: []ModemPort{
					{PortType: ModemPortTypeMbim, Device: "/dev/cdc-wdm0"},
					{PortType: ModemPortTypeQmi, Device: "/dev/cdc-wdm1"},
				},
			},
			wantDevice: "/dev/cdc-wdm1",
			wantType:   ModemPortTypeQmi,
		},
		{
			name: "falls back to QMI port",
			modem: &Modem{
				PrimaryPort: "/dev/ttyUSB0",
				Ports: []ModemPort{
					{PortType: ModemPortTypeAt, Device: "/dev/ttyUSB0"},
					{PortType: ModemPortTypeQmi, Device: "/dev/cdc-wdm1"},
				},
			},
			wantDevice: "/dev/cdc-wdm1",
			wantType:   ModemPortTypeQmi,
		},
		{
			name: "falls back to MBIM port",
			modem: &Modem{
				PrimaryPort: "/dev/ttyUSB0",
				Ports: []ModemPort{
					{PortType: ModemPortTypeAt, Device: "/dev/ttyUSB0"},
					{PortType: ModemPortTypeMbim, Device: "/dev/cdc-wdm0"},
				},
			},
			wantDevice: "/dev/cdc-wdm0",
			wantType:   ModemPortTypeMbim,
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

func TestQMIDeviceConfigPrefersQMI(t *testing.T) {
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
					{PortType: ModemPortTypeMbim, Device: "/dev/cdc-wdm0"},
					{PortType: ModemPortTypeQmi, Device: "/dev/cdc-wdm1"},
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
					{PortType: ModemPortTypeMbim, Device: "/dev/cdc-wdm0"},
				},
			},
			wantErr: wwan.ErrUnsupported,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := qmiDeviceConfig(tt.modem)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("qmiDeviceConfig() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("qmiDeviceConfig() error = %v", err)
			}
			if cfg.PortType != tt.wantType {
				t.Fatalf("qmiDeviceConfig() port type = %d, want %d", cfg.PortType, tt.wantType)
			}
			if cfg.Device != tt.wantDevice {
				t.Fatalf("qmiDeviceConfig() device = %q, want %q", cfg.Device, tt.wantDevice)
			}
		})
	}
}

func TestResolveVoLTEEndpointPrefersQMIFallsBackToMBIM(t *testing.T) {
	tests := []struct {
		name       string
		modem      *Modem
		wantDevice string
		wantType   ModemPortType
		wantErr    error
	}{
		{name: "nil modem", wantErr: errModemRequired},
		{
			name: "prefers QMI for IMS takeover",
			modem: &Modem{PrimaryPort: "/dev/cdc-wdm0", Ports: []ModemPort{
				{PortType: ModemPortTypeMbim, Device: "/dev/cdc-wdm0"},
				{PortType: ModemPortTypeQmi, Device: "/dev/cdc-wdm1"},
			}},
			wantDevice: "/dev/cdc-wdm1",
			wantType:   ModemPortTypeQmi,
		},
		{
			name: "uses MBIM when QMI is unavailable",
			modem: &Modem{PrimaryPort: "/dev/cdc-wdm0", Ports: []ModemPort{
				{PortType: ModemPortTypeMbim, Device: "/dev/cdc-wdm0"},
			}},
			wantDevice: "/dev/cdc-wdm0",
			wantType:   ModemPortTypeMbim,
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
		PrimarySimSlot: maxSIMSlot + 1,
		Ports: []ModemPort{{
			PortType: ModemPortTypeMbim,
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
		{name: "primary slot", modem: &Modem{PrimarySimSlot: 2}, want: 2},
		{name: "unspecified slot defaults to first slot", modem: &Modem{}, want: 1},
		{name: "slot out of range", modem: &Modem{PrimarySimSlot: maxSIMSlot + 1}, wantErr: true},
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
