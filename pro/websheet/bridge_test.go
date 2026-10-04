//go:build esim_transfer || ims

package websheet

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBridgeClientName(t *testing.T) {
	for _, name := range []string{"", "CarrierWFC", "</script>\n\"\\"} {
		t.Run(name, func(t *testing.T) {
			broker := New(Config{AllowPrivateHosts: true})
			session, err := broker.Create(t.Context(), Request{URL: "https://carrier.example", ClientName: name})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { broker.Delete(session.Info().ID) })
			script := session.bridgeScript("token", "https://sigmo.example", session.target)
			if strings.Contains(script, "{{WFC_CLIENT_NAME}}") || strings.Count(script, "</script>") != 1 {
				t.Fatalf("bridge contains an unresolved name or unescaped script delimiter: %s", script)
			}
			encoded, err := json.Marshal(name)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(script, "const wfcClientName = "+string(encoded)+";") {
				t.Errorf("bridge does not contain configured client name %q", name)
			}
		})
	}
}
