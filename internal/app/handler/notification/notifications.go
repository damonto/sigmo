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

type Service struct {
	cfg *config.Config
}

func NewService(cfg *config.Config) *Service {
	return &Service{cfg: cfg}
}

func (s *Service) List(modem *mmodem.Modem) ([]NotificationResponse, error) {
	client, err := lpa.New(modem, s.cfg)
	if err != nil {
		slog.Error("failed to create LPA client", "modem", modem.EquipmentIdentifier, "error", err)
		return nil, err
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			slog.Warn("failed to close LPA client", "error", cerr)
		}
	}()
	notifications, err := client.ListNotification()
	if err != nil {
		slog.Error("failed to list notifications", "modem", modem.EquipmentIdentifier, "error", err)
		return nil, err
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

func (s *Service) Resend(modem *mmodem.Modem, sequence sgp22.SequenceNumber) error {
	client, err := lpa.New(modem, s.cfg)
	if err != nil {
		slog.Error("failed to create LPA client", "modem", modem.EquipmentIdentifier, "error", err)
		return err
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			slog.Warn("failed to close LPA client", "error", cerr)
		}
	}()
	if err := client.SendNotification(sequence, false); err != nil {
		slog.Error("failed to resend notification", "modem", modem.EquipmentIdentifier, "sequence", sequence, "error", err)
		return err
	}
	return nil
}

func (s *Service) Delete(modem *mmodem.Modem, sequence sgp22.SequenceNumber) error {
	client, err := lpa.New(modem, s.cfg)
	if err != nil {
		slog.Error("failed to create LPA client", "modem", modem.EquipmentIdentifier, "error", err)
		return err
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			slog.Warn("failed to close LPA client", "error", cerr)
		}
	}()
	if err := client.RemoveNotificationFromList(sequence); err != nil {
		slog.Error("failed to remove notification", "modem", modem.EquipmentIdentifier, "sequence", sequence, "error", err)
		return err
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
