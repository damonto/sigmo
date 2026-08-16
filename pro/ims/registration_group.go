//go:build ims

package ims

import (
	"strings"
	"sync"

	imsgo "github.com/damonto/ims-go"
	"github.com/damonto/ims-go/ims/registration"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

// RegistrationGroups owns the registration group shared by access flows for
// the same modem and SIM profile. Its zero value is ready to use.
type RegistrationGroups struct {
	mu        sync.Mutex
	groups    map[registrationGroupKey]*registration.Group
	simStates map[string]registrationGroupSIMState
}

type registrationGroupKey struct {
	modemID   string
	profileID string
}

type registrationGroupSIMState struct {
	generation uint64
	slot       uint32
	identifier string
}

// Group returns the stable registration group for a modem and SIM profile.
func (r *RegistrationGroups) Group(modemID, profileID string) *registration.Group {
	key := registrationGroupKey{
		modemID:   modemID,
		profileID: profileID,
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if group := r.groups[key]; group != nil {
		return group
	}
	if r.groups == nil {
		r.groups = make(map[registrationGroupKey]*registration.Group)
	}
	group := imsgo.NewRegistrationGroup()
	r.groups[key] = group
	return group
}

// RotateForSIMChange prevents registration state from an old SIM application
// lifecycle from blocking its replacement. Each registry event is delivered to
// both IMS access coordinators, so repeated delivery must preserve the group
// already created for the new lifecycle.
func (r *RegistrationGroups) RotateForSIMChange(event mmodem.ModemEvent) {
	if r == nil || event.Type != mmodem.ModemEventSIMChanged || event.Modem == nil {
		return
	}
	modemID := strings.TrimSpace(event.Modem.EquipmentIdentifier)
	if modemID == "" {
		return
	}
	state := registrationGroupSIMState{
		generation: event.Generation,
		slot:       event.SIMSlot,
		identifier: event.SIMIdentifier,
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if previous, ok := r.simStates[modemID]; ok && previous == state {
		return
	}
	if r.simStates == nil {
		r.simStates = make(map[string]registrationGroupSIMState)
	}
	r.simStates[modemID] = state
	for key := range r.groups {
		if key.modemID == modemID {
			delete(r.groups, key)
		}
	}
}
