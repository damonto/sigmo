//go:build ims

package call

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

var (
	// ErrCallInUse prevents another endpoint from answering or consuming the same audio.
	ErrCallInUse = errors.New("call is in use by another client")
	// ErrCallProfileChanged prevents dialing with a replacement SIM.
	ErrCallProfileChanged = errors.New("call SIM has changed")
	// ErrInvalidCallClient rejects malformed endpoint identities.
	ErrInvalidCallClient = errors.New("invalid call client identifier")
	callClientPattern    = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
)

func clientID(value string) (string, error) {
	if value == "" {
		return "legacy", nil
	}
	if !callClientPattern.MatchString(value) {
		return "", ErrInvalidCallClient
	}
	return value, nil
}

// Get returns a snapshot only for the modem's current SIM.
func (c *Calls) Get(ctx context.Context, modem *mmodem.Modem, id string) (storage.Call, error) {
	return c.records.callForAction(ctx, modem, id)
}

// DialForClient pins the line and assigns ownership before media can open.
func (c *Calls) DialForClient(ctx context.Context, modem *mmodem.Modem, req DialRequest) (storage.Call, error) {
	owner, err := clientID(req.ClientID)
	if err != nil {
		return storage.Call{}, err
	}
	release, err := modem.ReserveSIMSlot(ctx)
	if err != nil {
		return storage.Call{}, fmt.Errorf("reserve call SIM: %w", err)
	}
	defer release()
	if req.ExpectedProfileID != "" {
		profile, err := modem.ProfileID(ctx)
		if err != nil {
			return storage.Call{}, fmt.Errorf("read call SIM: %w", err)
		}
		if profile != req.ExpectedProfileID {
			return storage.Call{}, ErrCallProfileChanged
		}
	}
	call, err := c.Dial(ctx, modem, req.To, req.Route)
	if err != nil {
		return call, err
	}
	// The carrier can publish progress before Dial returns. Claim the latest row,
	// rather than overwriting it with an older dialing snapshot.
	if _, err := c.claimCall(ctx, call.ID, owner); err != nil {
		return storage.Call{}, err
	}
	call, err = c.Get(ctx, modem, call.ID)
	if err != nil {
		return storage.Call{}, err
	}
	c.events.publish(Event{Call: call})
	return call, nil
}

// UpdateForClient serializes competing answers with SIM changes and media opens.
func (c *Calls) UpdateForClient(ctx context.Context, modem *mmodem.Modem, req ClientUpdate) (storage.Call, error) {
	owner, err := clientID(req.ClientID)
	if err != nil {
		return storage.Call{}, err
	}
	release, err := modem.ReserveSIMSlot(ctx)
	if err != nil {
		return storage.Call{}, fmt.Errorf("reserve call action: %w", err)
	}
	defer release()
	call, err := c.Get(ctx, modem, req.CallID)
	if err != nil {
		return storage.Call{}, err
	}
	if call.OwnerClientID != "" && call.OwnerClientID != owner {
		return storage.Call{}, ErrCallInUse
	}
	answering := strings.TrimSpace(req.Update.State) == StateActive && req.Update.Hold == ""
	if answering {
		if isTerminalCallState(call.State) {
			return storage.Call{}, ErrInvalidCallState
		}
		if call.State == StateActive || call.State == StateConfirmed {
			return call, nil
		}
		if call.Direction != DirectionIncoming || (call.State != StateRinging && call.State != StateAnswering) {
			return storage.Call{}, ErrInvalidCallState
		}
		if _, err := c.claimCall(ctx, call.ID, owner); err != nil {
			return storage.Call{}, err
		}
	}
	result, err := c.Update(ctx, modem, call.ID, req.Update)
	if err != nil && answering {
		cleanup, cancel := mediaCleanupContext(ctx)
		defer cancel()
		err = errors.Join(err, c.records.store.ReleaseCallClaim(cleanup, call.ID, owner))
	}
	return result, err
}

// ClientUpdate supplies call identity separately from the existing state changes.
type ClientUpdate struct {
	CallID   string
	ClientID string
	Update   UpdateRequest
}

// ClientMedia identifies the endpoint consuming a call's single RTP stream.
type ClientMedia struct {
	CallID   string
	ClientID string
}

func (c *Calls) claimCall(ctx context.Context, id, owner string) (storage.Call, error) {
	if _, err := c.records.store.ClaimCall(ctx, id, owner); err != nil {
		return storage.Call{}, err
	}
	call, err := c.records.store.Call(ctx, id)
	if err != nil {
		return storage.Call{}, err
	}
	if !isTerminalCallState(call.State) && call.OwnerClientID != owner {
		return storage.Call{}, ErrCallInUse
	}
	return call, nil
}

// SendDTMFForClient keeps keypad events on the owning endpoint and pinned SIM.
func (c *Calls) SendDTMFForClient(ctx context.Context, modem *mmodem.Modem, req ClientDTMF) error {
	owner, err := clientID(req.ClientID)
	if err != nil {
		return err
	}
	release, err := modem.ReserveSIMSlot(ctx)
	if err != nil {
		return fmt.Errorf("reserve DTMF SIM: %w", err)
	}
	defer release()
	call, err := c.Get(ctx, modem, req.CallID)
	if err != nil {
		return err
	}
	if call.OwnerClientID != "" && call.OwnerClientID != owner {
		return ErrCallInUse
	}
	return c.SendDTMF(ctx, modem, req.CallID, req.Digits)
}

// ClientDTMF associates ordered keypad input with the owning endpoint.
type ClientDTMF struct {
	CallID   string
	ClientID string
	Digits   string
}
