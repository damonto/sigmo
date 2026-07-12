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
		wantType   wwan.PortType
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
			wantType:   wwan.PortTypeQMI,
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
			wantType:   wwan.PortTypeQMI,
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
			wantType:   wwan.PortTypeMBIM,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := deviceConfig(tt.modem)
			if err != nil {
				t.Fatalf("deviceConfig() error = %v", err)
			}
			if cfg.PortType != tt.wantType {
				t.Fatalf("deviceConfig() port type = %d, want %d", cfg.PortType, tt.wantType)
			}
			if cfg.Device != tt.wantDevice {
				t.Fatalf("deviceConfig() device = %q, want %q", cfg.Device, tt.wantDevice)
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

func TestVoLTEDeviceConfigPrefersQMIFallsBackToMBIM(t *testing.T) {
	tests := []struct {
		name       string
		modem      *Modem
		wantDevice string
		wantType   wwan.PortType
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
			wantType:   wwan.PortTypeQMI,
		},
		{
			name: "uses MBIM when QMI is unavailable",
			modem: &Modem{PrimaryPort: "/dev/cdc-wdm0", Ports: []ModemPort{
				{PortType: ModemPortTypeMbim, Device: "/dev/cdc-wdm0"},
			}},
			wantDevice: "/dev/cdc-wdm0",
			wantType:   wwan.PortTypeMBIM,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := voLTEDeviceConfig(tt.modem)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("voLTEDeviceConfig() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("voLTEDeviceConfig() error = %v", err)
			}
			if cfg.PortType != tt.wantType || cfg.Device != tt.wantDevice {
				t.Fatalf("voLTEDeviceConfig() = (%d, %q), want (%d, %q)", cfg.PortType, cfg.Device, tt.wantType, tt.wantDevice)
			}
		})
	}
}
