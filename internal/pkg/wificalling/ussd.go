//go:build wifi_calling

package wificalling

import (
	"context"
	"errors"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	vowifi "github.com/damonto/vowifi-go"
)

func (c *coordinator) ExecuteUSSD(ctx context.Context, modem *mmodem.Modem, action string, code string) (string, error) {
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return "", err
	}
	client, err := c.connectedClient(modem.EquipmentIdentifier, profileID)
	if err != nil {
		return "", err
	}
	switch action {
	case actionUSSDInitialize:
		session, err := client.USSD().Start()
		if err != nil {
			return "", c.handleClientDisconnected(modem.EquipmentIdentifier, client, err)
		}
		result, err := session.Send(ctx, code)
		if err != nil {
			return "", c.handleClientDisconnected(modem.EquipmentIdentifier, client, err)
		}
		c.setUSSDSession(modem.EquipmentIdentifier, session, result.Closed)
		return result.Message.Text, nil
	case actionUSSDReply:
		client, session, err := c.ussdSession(modem.EquipmentIdentifier)
		if err != nil {
			return "", err
		}
		result, err := session.Reply(ctx, code)
		if err != nil {
			return "", c.handleClientDisconnected(modem.EquipmentIdentifier, client, err)
		}
		c.setUSSDSession(modem.EquipmentIdentifier, session, result.Closed)
		return result.Message.Text, nil
	default:
		return "", errors.New("action must be initialize or reply")
	}
}

func (c *coordinator) ussdSession(modemID string) (*vowifi.Client, *vowifi.USSDSession, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	session := c.sessions[modemID]
	if session == nil || session.ussd == nil {
		return nil, nil, vowifi.ErrUSSDNotStarted
	}
	return session.client, session.ussd, nil
}

func (c *coordinator) setUSSDSession(modemID string, ussd *vowifi.USSDSession, closed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	session := c.sessions[modemID]
	if session == nil {
		return
	}
	if closed {
		session.ussd = nil
		return
	}
	session.ussd = ussd
}
