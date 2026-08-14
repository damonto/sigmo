package modem

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	Qualcomm410InternetQMI       = "/dev/qmi_rmnet0"
	Qualcomm410IMSQMI            = "/dev/qmi_rmnet1"
	Qualcomm410InternetInterface = "wwan0"
	Qualcomm410IMSInterface      = "wwan1"
)

type qualcomm410LayoutProbe struct {
	device          func(string) error
	interfaceByName func(string) error
	sameDevice      func(string, string) (bool, error)
}

func systemQualcomm410LayoutProbe() qualcomm410LayoutProbe {
	return qualcomm410LayoutProbe{
		device:          probeDeviceNode,
		interfaceByName: probeNetworkInterface,
		sameDevice:      sameDeviceNode,
	}
}

func probeDeviceNode(path string) error {
	_, err := os.Stat(path)
	return err
}

func probeNetworkInterface(name string) error {
	_, err := net.InterfaceByName(name)
	return err
}

func sameDeviceNode(a, b string) (bool, error) {
	var aStat unix.Stat_t
	if err := unix.Stat(a, &aStat); err != nil {
		return false, fmt.Errorf("stat %s: %w", a, err)
	}
	var bStat unix.Stat_t
	if err := unix.Stat(b, &bStat); err != nil {
		return false, fmt.Errorf("stat %s: %w", b, err)
	}
	return sameDeviceStat(aStat, bStat), nil
}

func sameDeviceStat(a, b unix.Stat_t) bool {
	if a.Dev == b.Dev && a.Ino == b.Ino {
		return true
	}
	// Device aliases may be separate devtmpfs nodes. Their rdev still names
	// the same kernel character or block device even when the inodes differ.
	aType := a.Mode & unix.S_IFMT
	bType := b.Mode & unix.S_IFMT
	return aType == bType && (aType == unix.S_IFCHR || aType == unix.S_IFBLK) && a.Rdev == b.Rdev
}

// ValidateQualcomm410Layout verifies the fixed dual-QMI layout before either
// Internet or IMS starts changing modem or network state.
func ValidateQualcomm410Layout() error {
	return validateQualcomm410Layout(systemQualcomm410LayoutProbe())
}

// ValidateQualcomm410ModemLayout also verifies that DATA5 is the selected
// primary QMI port. Selecting DATA6 reports valid bearer settings while the
// wwan0 data plane remains unusable.
func ValidateQualcomm410ModemLayout(modem *Modem) error {
	return validateQualcomm410ModemLayout(modem, systemQualcomm410LayoutProbe())
}

func validateQualcomm410Layout(probe qualcomm410LayoutProbe) error {
	var result error
	for _, path := range []string{Qualcomm410InternetQMI, Qualcomm410IMSQMI} {
		if err := probe.device(path); err != nil {
			result = errors.Join(result, fmt.Errorf("find Qualcomm 410 QMI control port %s: %w", path, err))
		}
	}
	for _, name := range []string{Qualcomm410InternetInterface, Qualcomm410IMSInterface} {
		if err := probe.interfaceByName(name); err != nil {
			result = errors.Join(result, fmt.Errorf("find Qualcomm 410 network interface %s: %w", name, err))
		}
	}
	return result
}

func validateQualcomm410ModemLayout(modem *Modem, probe qualcomm410LayoutProbe) error {
	if modem == nil {
		return errors.New("modem is required")
	}
	if err := validateQualcomm410Layout(probe); err != nil {
		return err
	}
	primaryPort := strings.TrimSpace(modem.PrimaryPort)
	if primaryPort == "" {
		return errors.New("primary QMI control port is missing")
	}
	match, err := probe.sameDevice(Qualcomm410InternetQMI, primaryPort)
	if err != nil {
		return fmt.Errorf("compare primary QMI port %s with Qualcomm 410 DATA5 %s: %w", primaryPort, Qualcomm410InternetQMI, err)
	}
	if !match {
		return fmt.Errorf("primary QMI port %s does not resolve to Qualcomm 410 DATA5 %s", primaryPort, Qualcomm410InternetQMI)
	}
	return nil
}
