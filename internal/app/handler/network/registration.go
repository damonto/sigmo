package network

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/modemtask"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

var (
	errOperatorCodeRequired               = errors.New("operator code is required")
	errNetworkRegistrationStorageRequired = errors.New("network registration storage is required")
)

const (
	networkRegistrationKey     = "network.registration"
	networkRegistrationPrefix  = "profile:"
	networkRegistrationModemID = "modem:"
)

var networkRegistrationRestoreRetryInterval = 5 * time.Second

type networkRegistrationPreference struct {
	Mode         string `json:"mode"`
	OperatorCode string `json:"operatorCode"`
}

func (n *network) Register(ctx context.Context, modem *mmodem.Modem, operatorCode string) error {
	operatorCode = strings.TrimSpace(operatorCode)
	if operatorCode == "" {
		return errOperatorCodeRequired
	}
	if err := modem.ThreeGPP().RegisterNetwork(ctx, operatorCode); err != nil {
		return fmt.Errorf("register network %s: %w", operatorCode, err)
	}
	if err := n.saveRegistration(ctx, modem, operatorCode); err != nil {
		return fmt.Errorf("save network registration: %w", err)
	}
	return nil
}

func (n *network) saveRegistration(ctx context.Context, modem *mmodem.Modem, operatorCode string) error {
	scope := registrationScope(modem)
	if scope == "" {
		return nil
	}
	return n.store.Put(ctx, scope, networkRegistrationKey, networkRegistrationPreference{
		Mode:         "manual",
		OperatorCode: operatorCode,
	})
}

func RunRegistrationRestore(ctx context.Context, registry *mmodem.Registry, store *storage.Store) error {
	if store == nil {
		return errNetworkRegistrationStorageRequired
	}
	restorer := &registrationRestorer{store: store}
	return modemtask.Run(ctx, registry, restorer.restoreWithRetry)
}

type registrationRestorer struct {
	store *storage.Store
}

func (r *registrationRestorer) restoreWithRetry(ctx context.Context, modem *mmodem.Modem) {
	warned := false
	for {
		err := r.restoreModem(ctx, modem)
		if err == nil || ctx.Err() != nil {
			return
		}
		if warned {
			slog.Debug("retry network registration restore", "imei", modem.EquipmentIdentifier, "error", err)
		} else {
			slog.Warn("restore network registration", "imei", modem.EquipmentIdentifier, "error", err)
			warned = true
		}
		if err := sleepContext(ctx, networkRegistrationRestoreRetryInterval); err != nil {
			return
		}
	}
}

func (r *registrationRestorer) restoreModem(ctx context.Context, modem *mmodem.Modem) error {
	scope := registrationScope(modem)
	if scope == "" {
		return nil
	}
	var pref networkRegistrationPreference
	err := r.store.Get(ctx, scope, networkRegistrationKey, &pref)
	if errors.Is(err, storage.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	operatorCode := strings.TrimSpace(pref.OperatorCode)
	if pref.Mode != "manual" || operatorCode == "" {
		return nil
	}
	current, err := modem.ThreeGPP().OperatorCode(ctx)
	if err == nil && strings.TrimSpace(current) == operatorCode {
		return nil
	}
	if err := modem.ThreeGPP().RegisterNetwork(ctx, operatorCode); err != nil {
		return fmt.Errorf("register network %s: %w", operatorCode, err)
	}
	return nil
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func registrationScope(modem *mmodem.Modem) string {
	if sim := modem.Snapshot().SIM; sim != nil {
		if profileID := strings.TrimSpace(sim.Identifier); profileID != "" {
			return networkRegistrationPrefix + profileID
		}
	}
	if modemID := strings.TrimSpace(modem.EquipmentIdentifier); modemID != "" {
		return networkRegistrationModemID + modemID
	}
	return ""
}
