package feature

import (
	"slices"
	"testing"
)

func TestFeaturePolicyRejectsUnknownNames(t *testing.T) {
	for _, authorized := range []bool{false, true} {
		policy := NewPolicy(func() bool { return authorized })
		for _, name := range []Name{WiFiCalling, VoLTE, ESIMTransfer} {
			if policy.Allows(name) != authorized {
				t.Errorf("Allows(%q) did not follow authorization", name)
			}
		}
		for _, name := range []Name{NativeCalls, CallHistory, Contacts, MessageThreads, HistoryAssignments} {
			if !policy.Allows(name) {
				t.Errorf("base capability %q was denied", name)
			}
		}
		if policy.Allows("wifiCallling") || policy.Allows("") {
			t.Fatal("unknown capability was allowed")
		}
	}
}

func TestFilterUsesOneAuthorizationSnapshot(t *testing.T) {
	calls := 0
	policy := NewPolicy(func() bool { calls++; return calls == 1 })
	names := []Name{Contacts, WiFiCalling, VoLTE, NativeCalls}
	if got := policy.Filter(names); !slices.Equal(got, names) {
		t.Fatalf("filtered capabilities = %v", got)
	}
	if calls != 1 {
		t.Fatalf("authorization checked %d times for one response", calls)
	}
}
