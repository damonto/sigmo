package settings

import "testing"

func TestOIDCConfigurationWithoutInstanceURL(t *testing.T) {
	for _, tt := range []struct {
		name, issuer string
		valid        bool
	}{
		{name: "HTTPS issuer", issuer: "https://identity.example", valid: true},
		{name: "issuer path", issuer: "https://identity.example/realms/sigmo", valid: true},
		{name: "missing issuer"},
		{name: "HTTP issuer", issuer: "http://identity.example"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			cfg.Auth.Mode = "oidc"
			cfg.Auth.OIDC = OIDC{Issuer: tt.issuer, ClientID: "sigmo", ClientSecret: "secret", AllowedSubjects: []string{"owner"}}
			if err := cfg.Validate(); (err == nil) != tt.valid {
				t.Fatalf("Validate() error = %v, want valid %v", err, tt.valid)
			}
		})
	}
}
