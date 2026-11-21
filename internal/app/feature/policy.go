// Package feature defines application capabilities and their authorization policy.
package feature

// Name identifies a registered capability in feature policy checks.
type Name string

const (
	WiFiCalling  Name = "wifiCalling"
	VoLTE        Name = "volte"
	ESIMTransfer Name = "esimTransfer"
	MDN          Name = "mdn"
)

// Policy evaluates the current user's entitlement on every request.
// A zero policy denies all licensed features.
type Policy struct{ authorized func() bool }

// NewPolicy uses the live entitlement, including its expiry and revocation.
func NewPolicy(authorized func() bool) Policy { return Policy{authorized: authorized} }

// Allows reports whether a capability is available under the current entitlement.
func (p Policy) Allows(name Name) bool {
	return allows(name, p.authorized != nil && p.authorized())
}

func allows(name Name, authorized bool) bool {
	switch name {
	case WiFiCalling, VoLTE, ESIMTransfer, MDN:
		return authorized
	default:
		return false
	}
}

// Filter returns the capabilities available at the time of the request.
func (p Policy) Filter(names []Name) []Name {
	authorized := p.authorized != nil && p.authorized()
	result := make([]Name, 0, len(names))
	for _, name := range names {
		if allows(name, authorized) {
			result = append(result, name)
		}
	}
	return result
}
