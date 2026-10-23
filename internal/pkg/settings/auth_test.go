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

func TestOIDCAllowedSubjectsMayBeEmptyButAccessRequiresOne(t *testing.T) {
	oidc := Default()
	oidc.Auth.Mode = "oidc"
	oidc.Auth.OIDC = OIDC{
		Issuer: "https://identity.example", ClientID: "sigmo", ClientSecret: "secret",
	}
	if err := oidc.Validate(); err != nil {
		t.Fatalf("empty OIDC subject list should accept any authenticated subject: %v", err)
	}
	oidc.Auth.OIDC.AllowedSubjects = []string{" "}
	if err := oidc.Validate(); err == nil {
		t.Fatal("blank OIDC subject was accepted")
	}

	access := Default()
	access.Auth.Mode = "cloudflare_access"
	access.Auth.CloudflareAccess = CloudflareAccess{
		TeamDomain: "https://team.cloudflareaccess.com", Audience: "aud",
	}
	if err := access.Validate(); err == nil {
		t.Fatal("empty Access subject list was accepted")
	}
	if err := access.ValidateWithOptions(ValidationOptions{AllowEmptyAccessSubjects: true}); err != nil {
		t.Fatalf("Access test validation rejected an empty subject list: %v", err)
	}
}
