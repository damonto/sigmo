package email

import (
	"testing"

	"github.com/wneessen/go-mail"
)

func TestTLSPolicyText(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want mail.TLSPolicy
		text string
	}{
		{name: "default mandatory", want: mail.TLSMandatory, text: "mandatory"},
		{name: "mandatory", raw: "mandatory", want: mail.TLSMandatory, text: "mandatory"},
		{name: "opportunistic", raw: "opportunistic", want: mail.TLSOpportunistic, text: "opportunistic"},
		{name: "none", raw: "none", want: mail.NoTLS, text: "none"},
		{name: "notls alias", raw: "notls", want: mail.NoTLS, text: "none"},
		{name: "no_tls alias", raw: "no_tls", want: mail.NoTLS, text: "none"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var policy tlsPolicy
			if err := policy.UnmarshalText([]byte(tt.raw)); err != nil {
				t.Fatalf("tlsPolicy.UnmarshalText() error = %v", err)
			}
			if policy.TLSPolicy != tt.want {
				t.Fatalf("tlsPolicy = %v, want %v", policy.TLSPolicy, tt.want)
			}
			text, err := policy.MarshalText()
			if err != nil {
				t.Fatalf("tlsPolicy.MarshalText() error = %v", err)
			}
			if string(text) != tt.text {
				t.Fatalf("tlsPolicy.MarshalText() = %q, want %q", text, tt.text)
			}
		})
	}
}

func TestTLSPolicyTextRejectsUnknown(t *testing.T) {
	var policy tlsPolicy
	if err := policy.UnmarshalText([]byte("strict")); err == nil {
		t.Fatal("tlsPolicy.UnmarshalText() error = nil, want error")
	}
}
