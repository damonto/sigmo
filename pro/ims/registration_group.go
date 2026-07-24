//go:build ims

package ims

import (
	"sync"

	imsgo "github.com/damonto/ims-go"
)

// RegistrationGroups owns the registration group shared by access flows for
// the same modem and SIM profile. Its zero value is ready to use.
type RegistrationGroups struct {
	mu     sync.Mutex
	groups map[registrationGroupKey]*imsgo.RegistrationGroup
}

type registrationGroupKey struct {
	modemID   string
	profileID string
}

// Group returns the stable registration group for a modem and SIM profile.
func (r *RegistrationGroups) Group(modemID, profileID string) *imsgo.RegistrationGroup {
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
		r.groups = make(map[registrationGroupKey]*imsgo.RegistrationGroup)
	}
	group := imsgo.NewRegistrationGroup()
	r.groups[key] = group
	return group
}
