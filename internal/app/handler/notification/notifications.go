package notification

import (
	"fmt"
	"log/slog"
	"strconv"

	sgp22 "github.com/damonto/euicc-go/v2"
	"github.com/damonto/sigmo/internal/pkg/config"
	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type notification struct {
	store *config.Store
}

func newNotification(store *config.Store) *notification {
	return &notification{store: store}
}

func (n *notification) List(modem *mmodem.Modem) ([]NotificationResponse, error) {
	cfg := n.store.Snapshot()
	client, err := lpa.New(modem, &cfg)
	if err != nil {
		return nil, fmt.Errorf("create LPA client: %w", err)
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			slog.Warn("failed to close LPA client", "error", cerr)
		}
	}()
	notifications, err := client.ListNotification()
	if err != nil {
		return nil, fmt.Errorf("list notifications: %w", err)
	}
	response := make([]NotificationResponse, 0, len(notifications))
	for _, notification := range notifications {
		response = append(response, NotificationResponse{
			SequenceNumber: strconv.FormatUint(uint64(notification.SequenceNumber), 10),
			ICCID:          notification.ICCID.String(),
			SMDP:           notification.Address,
			Operation:      operationLabel(notification.ProfileManagementOperation),
		})
	}
	return response, nil
}

func (n *notification) Resend(modem *mmodem.Modem, sequence sgp22.SequenceNumber) error {
	cfg := n.store.Snapshot()
	client, err := lpa.New(modem, &cfg)
	if err != nil {
		return fmt.Errorf("create LPA client: %w", err)
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			slog.Warn("failed to close LPA client", "error", cerr)
		}
	}()
	if err := client.SendNotification(sequence, false); err != nil {
		return fmt.Errorf("resend notification %d: %w", sequence, err)
	}
	return nil
}

func (n *notification) Delete(modem *mmodem.Modem, sequence sgp22.SequenceNumber) error {
	cfg := n.store.Snapshot()
	client, err := lpa.New(modem, &cfg)
	if err != nil {
		return fmt.Errorf("create LPA client: %w", err)
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			slog.Warn("failed to close LPA client", "error", cerr)
		}
	}()
	if err := client.RemoveNotificationFromList(sequence); err != nil {
		return fmt.Errorf("remove notification %d: %w", sequence, err)
	}
	return nil
}

func operationLabel(event sgp22.NotificationEvent) string {
	switch event {
	case sgp22.NotificationEventInstall:
		return "install"
	case sgp22.NotificationEventEnable:
		return "enable"
	case sgp22.NotificationEventDisable:
		return "disable"
	case sgp22.NotificationEventDelete:
		return "delete"
	default:
		return fmt.Sprint(event)
	}
}
