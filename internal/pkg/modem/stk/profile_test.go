package stk

import (
	"slices"
	"testing"
)

func TestTerminalCATProfile(t *testing.T) {
	profile := terminalProfile()
	got := terminalCATProfile()
	if !slices.Equal(got.Data, profile.Bytes()) {
		t.Fatalf("terminalCATProfile() data = % X, want profile bytes", got.Data)
	}
	if got.EventMask != profile.QMIEventMask() {
		t.Fatalf("terminalCATProfile() raw mask = 0x%08X, want 0x%08X", got.EventMask, profile.QMIEventMask())
	}
	if got.FullFunctionMask != profile.QMIFullFunctionMask() {
		t.Fatalf("terminalCATProfile() full-function mask = 0x%08X, want 0x%08X", got.FullFunctionMask, profile.QMIFullFunctionMask())
	}
}
