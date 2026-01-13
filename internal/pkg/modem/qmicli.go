package modem

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
)

func qmicliActivateProvisioningIfSimMissing(m *Modem) error {
	status, err := qmicliCardStatus(m)
	if err != nil {
		return err
	}
	slot := qmicliSimSlot(m)
	missing, err := qmicliIsSimMissing(status, slot)
	if err != nil {
		return fmt.Errorf("parse qmicli personalization state: %w", err)
	}
	if !missing {
		return nil
	}
	aid, err := qmicliApplicationID(status)
	if err != nil {
		return fmt.Errorf("parse qmicli application id: %w", err)
	}
	slog.Info("sim missing, activate provisioning session", "modem", m.EquipmentIdentifier, "slot", slot)
	if result, err := qmicliRun(
		m,
		fmt.Sprintf("--uim-change-provisioning-session=slot=%d,activate=yes,session-type=primary-gw-provisioning,aid=%s", slot, aid),
	); err != nil {
		slog.Error("failed to activate provisioning session", "error", err, "result", string(result))
		return err
	}
	return nil
}

func qmicliRepowerSimCard(m *Modem) error {
	slot := qmicliSimSlot(m)
	if result, err := qmicliRun(m, fmt.Sprintf("--uim-sim-power-off=%d", slot)); err != nil {
		slog.Error("failed to power off sim", "error", err, "result", string(result))
		return err
	}
	slog.Info("sim powered off", "modem", m.EquipmentIdentifier, "slot", slot)
	if result, err := qmicliRun(m, fmt.Sprintf("--uim-sim-power-on=%d", slot)); err != nil {
		slog.Error("failed to power on sim", "error", err, "result", string(result))
		return err
	}
	slog.Info("sim powered on", "modem", m.EquipmentIdentifier, "slot", slot)
	return nil
}

func qmicliCardStatus(m *Modem) (string, error) {
	result, err := qmicliRun(m, "--uim-get-card-status")
	if err != nil {
		slog.Error("failed to get card status", "error", err, "result", string(result))
		return "", err
	}
	return string(result), nil
}

func qmicliApplicationID(status string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(status))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, "Application ID:") {
			continue
		}
		if !scanner.Scan() {
			return "", errors.New("application id line missing")
		}
		aidLine := strings.TrimSpace(scanner.Text())
		if aidLine == "" {
			return "", errors.New("application id empty")
		}
		aid := strings.Join(strings.Fields(aidLine), "")
		if aid == "" {
			return "", errors.New("application id empty")
		}
		return aid, nil
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", errors.New("application id not found")
}

func qmicliIsSimMissing(status string, slot uint32) (bool, error) {
	state, err := qmicliPersonalizationState(status, slot)
	if err != nil {
		return false, err
	}
	slog.Info("personalization state", "state", state)
	return !strings.EqualFold(state, "ready"), nil
}

func qmicliSimSlot(m *Modem) uint32 {
	// QMI SIM slots are 1-based; ModemManager returns 0 when slots aren't supported.
	if m.PrimarySimSlot > 0 {
		return m.PrimarySimSlot
	}
	return 1
}

func qmicliRun(m *Modem, args ...string) ([]byte, error) {
	bin, err := exec.LookPath("qmicli")
	if err != nil {
		slog.Error("qmicli not found in PATH", "error", err)
		return nil, err
	}
	commandArgs := append([]string{"-d", m.PrimaryPort, "-p"}, args...)
	return exec.Command(bin, commandArgs...).Output()
}

func qmicliPersonalizationState(status string, slot uint32) (string, error) {
	slotHeader := fmt.Sprintf("Slot [%d]:", slot)
	scanner := bufio.NewScanner(strings.NewReader(status))
	inSlot := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "Slot [") {
			inSlot = strings.HasPrefix(line, slotHeader)
			continue
		}
		if !inSlot {
			continue
		}
		if after, ok := strings.CutPrefix(line, "Personalization state:"); ok {
			state := strings.TrimSpace(after)
			state = strings.Trim(state, "'")
			if state == "" {
				return "", errors.New("personalization state empty")
			}
			return state, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", errors.New("personalization state not found")
}
