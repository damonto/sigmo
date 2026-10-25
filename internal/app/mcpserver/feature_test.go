package mcpserver

import (
	"context"
	"testing"

	"github.com/damonto/sigmo/internal/app/feature"
	"github.com/damonto/sigmo/internal/app/mcpauth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestFeatureRevocationBlocksExistingMCPSession(t *testing.T) {
	authorized := true
	catalog := NewCatalog()
	catalog.SetFeaturePolicy(feature.NewPolicy(func() bool { return authorized }))
	if err := catalog.AddPermission(Permission{Name: "wifi_calling.read", Module: "wifi_calling", Feature: feature.WiFiCalling}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.AddPermission(Permission{Name: "wifi_calling.read", Module: "wifi_calling"}); err == nil {
		t.Fatal("duplicate registration removed a capability requirement")
	}
	called := false
	if err := AddTool(catalog, "wifi_calling.read", ReadTool("test_wifi_calling", "Read Wi-Fi Calling status"), nil, func(context.Context, *mcp.CallToolRequest, mcpauth.Grant, struct{}) (struct{}, error) {
		called = true
		return struct{}{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "feature-test", Version: "1"}, nil)
	catalog.registerTools(server, &execution{grant: mcpauth.Grant{AllModems: true, Permissions: []string{"wifi_calling.read"}}})
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ss, err := server.Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	if _, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "test_wifi_calling", Arguments: struct{}{}}); err != nil || !called {
		t.Fatalf("authorized call = %v, called=%v", err, called)
	}
	authorized = false
	called = false
	if len(catalog.Permissions()) != 0 {
		t.Fatal("revoked feature permissions remain visible")
	}
	result, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "test_wifi_calling", Arguments: struct{}{}})
	if err == nil && (result == nil || !result.IsError) {
		t.Fatal("existing session bypassed revocation")
	}
	if called {
		t.Fatal("revoked tool executed")
	}
}
