package ims

import (
	"context"
	"errors"
	"log/slog"
	"time"

	imsgo "github.com/damonto/ims-go"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

type numberRegistration struct {
	sessionID  uint64
	registered bool
	number     string
	ambiguous  bool
	persisted  bool
}

type modemNumbers struct {
	target     mmodem.SIMIdentity
	generation uint64
	sources    map[Access]numberRegistration
	lastNumber string
	conflict   bool
}

func (c *Connectivity) updateNumber(access Access, session *sessionState, info imsgo.RegistrationInfo) {
	modem := session.modem
	if modem == nil || session.numberTarget.ICCID == "" || session.numberTarget.ICCID != session.profileID {
		return
	}
	c.numberMu.Lock()
	defer c.numberMu.Unlock()
	if modem.Snapshot().SIMIdentity != session.numberTarget {
		return
	}
	if c.numbers == nil {
		c.numbers = make(map[string]*modemNumbers)
	}
	modemID := modem.EquipmentIdentifier
	numbers := c.numbers[modemID]
	if numbers != nil && numbers.generation > session.generation {
		return
	}
	if numbers == nil || numbers.target != session.numberTarget {
		numbers = &modemNumbers{
			target:     session.numberTarget,
			generation: session.generation,
			sources:    make(map[Access]numberRegistration),
		}
		c.numbers[modemID] = numbers
	}
	previous := numbers.sources[access]
	if previous.sessionID > session.id {
		return
	}
	observation := numberRegistration{
		sessionID:  session.id,
		registered: info.Registered,
		number:     info.MSISDN,
		ambiguous:  info.MSISDNAmbiguous,
	}
	wasPersisted := previous.persisted
	previous.persisted = false
	observation.persisted = wasPersisted && observation == previous
	numbers.sources[access] = observation
	previousConflict := numbers.conflict
	number := numbers.resolve()
	if numbers.conflict && !previousConflict {
		slog.Warn("conflicting IMS subscriber numbers", "imei", modemID)
	}
	if c.numberStore == nil {
		modem.SetIMSNumber(numbers.target, number)
		return
	}
	// The resolved value can contain an older observation after a disconnect.
	// Only a fresh, agreed number supplied by the reporting access can persist.
	if !observation.registered || number == "" || observation.number != number || observation.persisted {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := modem.UpdateStoredNumber(numbers.target, func(mmodem.ModemSnapshot) (string, error) {
		return number, c.numberStore.SaveSIMProfileNumber(ctx, numbers.target.ICCID, modemID, number)
	})
	if err == nil {
		observation.persisted = true
		numbers.sources[access] = observation
	} else if !errors.Is(err, mmodem.ErrSIMActivationChanged) {
		slog.Warn("save IMS subscriber number", "imei", modemID, "error", err)
	}
}

func (n *modemNumbers) resolve() string {
	var number string
	registered := false
	n.conflict = false
	for _, source := range n.sources {
		if !source.registered {
			continue
		}
		registered = true
		if source.ambiguous {
			n.conflict = true
		}
		if source.number == "" {
			continue
		}
		if number != "" && number != source.number {
			n.conflict = true
		}
		number = source.number
	}
	if !registered {
		return n.lastNumber
	}
	if n.conflict {
		number = ""
	}
	// A successful registration without a usable number retires the old
	// observation. A transport outage alone retains the last agreed number.
	n.lastNumber = number
	return number
}

func (c *coordinator) syncRegistration(modemID string, sessionID uint64, client *imsgo.Client) {
	c.mu.Lock()
	defer c.mu.Unlock()
	session := c.sessions[modemID]
	if session == nil || session.id != sessionID || session.client != client {
		return
	}
	c.publishRegistrationLocked(session, client.Registration())
}

func (c *coordinator) publishRegistrationLocked(session *sessionState, info imsgo.RegistrationInfo) {
	// Keep this synchronous under the session lock so replacement and cleanup
	// cannot overtake a previously validated registration update.
	if session != nil && c.onRegistration != nil {
		c.onRegistration(session, info)
	}
}
