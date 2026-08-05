package notification

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"

	sgp22 "github.com/damonto/euicc-go/v2"
	"github.com/damonto/sigmo/internal/pkg/lpa"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type notification struct {
	clients *lpa.Pool
}

func newNotification(clients *lpa.Pool) *notification {
	return &notification{clients: clients}
}

func (n *notification) List(ctx context.Context, modem *mmodem.Modem) (*NotificationsResponse, error) {
	ses, err := n.clients.SecureElements(ctx, modem)
	if err != nil {
		return nil, fmt.Errorf("discover eUICC SEs: %w", err)
	}
	response := &NotificationsResponse{SEs: make([]NotificationGroupResponse, 0, len(ses))}
	for _, se := range ses {
		group := NotificationGroupResponse{
			ID:            se.ID,
			Label:         se.Label,
			AID:           hex.EncodeToString(se.AID),
			Notifications: []NotificationResponse{},
		}
		client, err := n.clients.Acquire(ctx, modem, se.ID)
		if err != nil {
			modem.Logger().Warn("create LPA client for notifications", "seId", se.ID, "error", err)
			return nil, fmt.Errorf("create LPA client for %s: %w", se.ID, err)
		}
		eid, err := client.EID()
		if err != nil {
			if cerr := client.Close(); cerr != nil {
				client.Logger().Warn("failed to close LPA client", "error", cerr)
			}
			err = fmt.Errorf("read EID for %s: %w", se.ID, err)
			modem.Logger().Warn("read eUICC EID for notifications", "seId", se.ID, "error", err)
			return nil, err
		}
		group.EID = hex.EncodeToString(eid)
		notifications, err := client.ListNotification()
		if err != nil {
			if cerr := client.Close(); cerr != nil {
				client.Logger().Warn("failed to close LPA client", "error", cerr)
			}
			err = fmt.Errorf("list notifications for %s: %w", se.ID, err)
			modem.Logger().Warn("list notifications", "seId", se.ID, "error", err)
			return nil, err
		}
		for _, notification := range notifications {
			group.Notifications = append(group.Notifications, NotificationResponse{
				SEID:           se.ID,
				SELabel:        se.Label,
				EID:            group.EID,
				SequenceNumber: strconv.FormatUint(uint64(notification.SequenceNumber), 10),
				ICCID:          notification.ICCID.String(),
				SMDP:           notification.Address,
				Operation:      operationLabel(notification.ProfileManagementOperation),
			})
		}
		if cerr := client.Close(); cerr != nil {
			client.Logger().Warn("failed to close LPA client", "error", cerr)
		}
		response.SEs = append(response.SEs, group)
	}
	return response, nil
}

func (n *notification) Resend(ctx context.Context, modem *mmodem.Modem, seID string, sequence sgp22.SequenceNumber) error {
	client, err := n.clients.Acquire(ctx, modem, seID)
	if err != nil {
		return fmt.Errorf("create LPA client: %w", err)
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			client.Logger().Warn("failed to close LPA client", "error", cerr)
		}
	}()
	if err := client.SendNotification(sequence, false); err != nil {
		return fmt.Errorf("resend notification %d: %w", sequence, err)
	}
	return nil
}

func (n *notification) Delete(ctx context.Context, modem *mmodem.Modem, seID string, sequence sgp22.SequenceNumber) error {
	client, err := n.clients.Acquire(ctx, modem, seID)
	if err != nil {
		return fmt.Errorf("create LPA client: %w", err)
	}
	defer func() {
		if cerr := client.Close(); cerr != nil {
			client.Logger().Warn("failed to close LPA client", "error", cerr)
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
