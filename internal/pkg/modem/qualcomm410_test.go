package modem

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestSameDeviceStat(t *testing.T) {
	tests := []struct {
		name string
		a    unix.Stat_t
		b    unix.Stat_t
		want bool
	}{
		{
			name: "same filesystem object",
			a:    unix.Stat_t{Dev: 1, Ino: 2, Mode: unix.S_IFREG},
			b:    unix.Stat_t{Dev: 1, Ino: 2, Mode: unix.S_IFREG},
			want: true,
		},
		{
			name: "distinct regular files",
			a:    unix.Stat_t{Dev: 1, Ino: 2, Mode: unix.S_IFREG},
			b:    unix.Stat_t{Dev: 1, Ino: 3, Mode: unix.S_IFREG},
		},
		{
			name: "character device aliases",
			a:    unix.Stat_t{Dev: 1, Ino: 2, Mode: unix.S_IFCHR, Rdev: 9},
			b:    unix.Stat_t{Dev: 1, Ino: 3, Mode: unix.S_IFCHR, Rdev: 9},
			want: true,
		},
		{
			name: "different character devices",
			a:    unix.Stat_t{Dev: 1, Ino: 2, Mode: unix.S_IFCHR, Rdev: 9},
			b:    unix.Stat_t{Dev: 1, Ino: 3, Mode: unix.S_IFCHR, Rdev: 10},
		},
		{
			name: "different device types",
			a:    unix.Stat_t{Dev: 1, Ino: 2, Mode: unix.S_IFCHR, Rdev: 9},
			b:    unix.Stat_t{Dev: 1, Ino: 3, Mode: unix.S_IFBLK, Rdev: 9},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sameDeviceStat(tt.a, tt.b); got != tt.want {
				t.Fatalf("sameDeviceStat() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateQualcomm410LayoutChecksEveryEndpoint(t *testing.T) {
	missingDevice := errors.New("missing device")
	missingInterface := errors.New("missing interface")
	var devices []string
	var interfaces []string
	err := validateQualcomm410Layout(qualcomm410LayoutProbe{
		device: func(path string) error {
			devices = append(devices, path)
			if path == Qualcomm410IMSQMI {
				return missingDevice
			}
			return nil
		},
		interfaceByName: func(name string) error {
			interfaces = append(interfaces, name)
			if name == Qualcomm410IMSInterface {
				return missingInterface
			}
			return nil
		},
	})
	if !errors.Is(err, missingDevice) || !errors.Is(err, missingInterface) {
		t.Fatalf("validateQualcomm410Layout() error = %v, want both probe errors", err)
	}
	if want := []string{Qualcomm410InternetQMI, Qualcomm410IMSQMI}; !slices.Equal(devices, want) {
		t.Fatalf("device probes = %v, want %v", devices, want)
	}
	if want := []string{Qualcomm410InternetInterface, Qualcomm410IMSInterface}; !slices.Equal(interfaces, want) {
		t.Fatalf("interface probes = %v, want %v", interfaces, want)
	}
}

func TestValidateQualcomm410ModemLayoutRequiresPrimaryOnDATA5(t *testing.T) {
	compareErr := errors.New("compare devices")
	tests := []struct {
		name        string
		modem       *Modem
		match       bool
		compareErr  error
		wantErr     error
		wantMessage string
	}{
		{name: "nil modem", wantMessage: "modem is required"},
		{name: "missing primary", modem: &Modem{}, wantMessage: "primary QMI control port is missing"},
		{name: "device comparison fails", modem: &Modem{PrimaryPort: "/dev/wwan0qmi1"}, compareErr: compareErr, wantErr: compareErr},
		{name: "primary uses DATA6", modem: &Modem{PrimaryPort: "/dev/wwan0qmi0"}, wantMessage: "does not resolve to Qualcomm 410 DATA5"},
		{name: "primary uses DATA5", modem: &Modem{PrimaryPort: "/dev/wwan0qmi1"}, match: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe := qualcomm410LayoutProbe{
				device:          func(string) error { return nil },
				interfaceByName: func(string) error { return nil },
				sameDevice: func(stable, primary string) (bool, error) {
					if stable != Qualcomm410InternetQMI || primary != tt.modem.PrimaryPort {
						t.Fatalf("sameDevice(%q, %q)", stable, primary)
					}
					return tt.match, tt.compareErr
				},
			}
			err := validateQualcomm410ModemLayout(tt.modem, probe)
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("validateQualcomm410ModemLayout() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantMessage != "" && (err == nil || !strings.Contains(err.Error(), tt.wantMessage)) {
				t.Fatalf("validateQualcomm410ModemLayout() error = %v, want message %q", err, tt.wantMessage)
			}
			if tt.wantErr == nil && tt.wantMessage == "" && err != nil {
				t.Fatalf("validateQualcomm410ModemLayout() error = %v, want nil", err)
			}
		})
	}
}
