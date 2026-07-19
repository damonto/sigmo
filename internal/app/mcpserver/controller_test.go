package mcpserver

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/damonto/sigmo/internal/app/mcpauth"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

type controllerTestInput struct {
	ModemID string `json:"modemId"`
}

type controllerTestOutput struct {
	ModemID string `json:"modemId"`
}

func TestControllerToolFilteringAuthorizationAndAudit(t *testing.T) {
	ctx := context.Background()
	env := newControllerTestEnv(t)
	grant, token := env.issue(t, "Read agent", []string{"test.read"}, []string{"imei-a"}, false)
	server := httptest.NewServer(env.controller.Handler())
	defer server.Close()

	status, _ := controllerRequest(t, server.Client(), http.MethodGet, server.URL, "", "", "", "")
	if status != http.StatusNotFound {
		t.Fatalf("disabled MCP status = %d, want %d", status, http.StatusNotFound)
	}
	env.controller.SetEnabled(true)

	session := connectControllerClient(t, server, token)
	t.Cleanup(func() { _ = session.Close() })
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	names := make([]string, 0, len(listed.Tools))
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	if want := []string{"list_authorized_modems", "read_status"}; !slices.Equal(names, want) {
		t.Fatalf("ListTools() = %v, want %v", names, want)
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "read_status", Arguments: map[string]any{"modemId": "imei-a"}})
	if err != nil {
		t.Fatalf("CallTool(authorized) error = %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool(authorized) IsError = true, content = %v", result.Content)
	}
	if got := resultText(t, result); got != "read_status completed." {
		t.Fatalf("CallTool(authorized) text = %q", got)
	}

	result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "read_status", Arguments: map[string]any{"modemId": "imei-b"}})
	if err != nil {
		t.Fatalf("CallTool(unauthorized) protocol error = %v", err)
	}
	if !result.IsError || !strings.HasPrefix(resultText(t, result), "permission_denied:") {
		t.Fatalf("CallTool(unauthorized) = %+v, want permission_denied tool error", result)
	}

	events, err := env.db.ListMCPAuditEvents(ctx, 0, 10)
	if err != nil {
		t.Fatalf("ListMCPAuditEvents() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("audit event count = %d, want 2", len(events))
	}
	if events[0].KeyID != grant.ID || events[0].Outcome != "error" || events[0].ErrorCode != "permission_denied" {
		t.Fatalf("latest audit event = %+v", events[0])
	}
	if events[1].Outcome != "success" || events[1].ErrorCode != "" {
		t.Fatalf("successful audit event = %+v", events[1])
	}
}

func TestControllerPreventsCrossKeySessionReuse(t *testing.T) {
	env := newControllerTestEnv(t)
	_, firstToken := env.issue(t, "First", []string{"test.read"}, nil, true)
	_, secondToken := env.issue(t, "Second", []string{"test.read"}, nil, true)
	env.controller.SetEnabled(true)
	server := httptest.NewServer(env.controller.Handler())
	defer server.Close()

	session := connectControllerClient(t, server, firstToken)
	t.Cleanup(func() { _ = session.Close() })
	if session.ID() == "" {
		t.Fatal("MCP client session ID is empty")
	}
	status, body := controllerRequest(t, server.Client(), http.MethodPost, server.URL, secondToken, session.ID(), "", `{"jsonrpc":"2.0","id":2,"method":"ping"}`)
	if status != http.StatusForbidden {
		t.Fatalf("cross-key session status = %d, want %d; body = %s", status, http.StatusForbidden, body)
	}
}

func TestControllerClosesSessionsAndRejectsRevokedKeys(t *testing.T) {
	env := newControllerTestEnv(t)
	grant, token := env.issue(t, "Agent", []string{"test.read"}, nil, true)
	env.controller.SetEnabled(true)
	server := httptest.NewServer(env.controller.Handler())
	defer server.Close()

	session := connectControllerClient(t, server, token)
	t.Cleanup(func() { _ = session.Close() })
	sessionID := session.ID()
	env.controller.CloseKey(grant.ID)
	status, _ := controllerRequest(t, server.Client(), http.MethodPost, server.URL, token, sessionID, "", `{"jsonrpc":"2.0","id":2,"method":"ping"}`)
	if status != http.StatusNotFound {
		t.Fatalf("closed key session status = %d, want %d", status, http.StatusNotFound)
	}

	if _, err := env.keys.Revoke(context.Background(), grant.ID); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	status, _ = controllerRequest(t, server.Client(), http.MethodPost, server.URL, token, "", "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"test","version":"1"}}}`)
	if status != http.StatusUnauthorized {
		t.Fatalf("revoked key status = %d, want %d", status, http.StatusUnauthorized)
	}

	_, activeToken := env.issue(t, "Active", []string{"test.read"}, nil, true)
	activeSession := connectControllerClient(t, server, activeToken)
	t.Cleanup(func() { _ = activeSession.Close() })
	env.controller.SetEnabled(false)
	status, _ = controllerRequest(t, server.Client(), http.MethodPost, server.URL, activeToken, activeSession.ID(), "", `{"jsonrpc":"2.0","id":3,"method":"ping"}`)
	if status != http.StatusNotFound {
		t.Fatalf("disabled active session status = %d, want %d", status, http.StatusNotFound)
	}
}

func TestControllerHTTPProtection(t *testing.T) {
	env := newControllerTestEnv(t)
	_, token := env.issue(t, "Agent", []string{"test.read"}, nil, true)
	env.controller.SetEnabled(true)
	server := httptest.NewServer(env.controller.Handler())
	defer server.Close()

	tests := []struct {
		name       string
		token      string
		origin     string
		body       string
		modify     func(*http.Request)
		wantStatus int
	}{
		{name: "missing bearer", body: `{}`, wantStatus: http.StatusUnauthorized},
		{name: "query token rejected", body: `{}`, modify: func(req *http.Request) { req.URL.RawQuery = "token=" + token }, wantStatus: http.StatusUnauthorized},
		{name: "cross origin", token: token, origin: "https://evil.example", body: `{}`, wantStatus: http.StatusForbidden},
		{name: "localhost host mismatch", token: token, body: `{}`, modify: func(req *http.Request) { req.Host = "evil.example" }, wantStatus: http.StatusForbidden},
		{name: "body over one MiB", token: token, body: strings.Repeat("x", maxRequestBodySize+1), wantStatus: http.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, server.URL, strings.NewReader(tt.body))
			if err != nil {
				t.Fatalf("NewRequest() error = %v", err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			if tt.token != "" {
				req.Header.Set("Authorization", "Bearer "+tt.token)
			}
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			if tt.modify != nil {
				tt.modify(req)
			}
			resp, err := server.Client().Do(req)
			if err != nil {
				t.Fatalf("Do() error = %v", err)
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			if err := resp.Body.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}

type controllerTestEnv struct {
	db         *storage.Store
	keys       *mcpauth.Store
	controller *Controller
}

func newControllerTestEnv(t *testing.T) controllerTestEnv {
	t.Helper()
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "sigmo.db"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	store, err := settings.NewStore(ctx, db)
	if err != nil {
		t.Fatalf("settings.NewStore() error = %v", err)
	}
	keys, err := mcpauth.NewStore(db)
	if err != nil {
		t.Fatalf("mcpauth.NewStore() error = %v", err)
	}
	catalog := NewCatalog()
	for _, permission := range []string{"test.read", "test.write"} {
		if err := catalog.AddPermission(permission, "test"); err != nil {
			t.Fatalf("AddPermission(%q) error = %v", permission, err)
		}
	}
	if err := AddTool(catalog, "", ReadTool("list_authorized_modems", "List authorized modems"), nil, func(_ context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, _ struct{}) (controllerTestOutput, error) {
		return controllerTestOutput{}, nil
	}); err != nil {
		t.Fatalf("AddTool(global) error = %v", err)
	}
	modemIDs := func(input controllerTestInput) []string { return []string{input.ModemID} }
	if err := AddTool(catalog, "test.read", ReadTool("read_status", "Read status"), modemIDs, func(_ context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input controllerTestInput) (controllerTestOutput, error) {
		return controllerTestOutput{ModemID: input.ModemID}, nil
	}); err != nil {
		t.Fatalf("AddTool(read) error = %v", err)
	}
	if err := AddTool(catalog, "test.write", WriteTool("change_status", "Change status", true, false), modemIDs, func(_ context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input controllerTestInput) (controllerTestOutput, error) {
		return controllerTestOutput{ModemID: input.ModemID}, nil
	}); err != nil {
		t.Fatalf("AddTool(write) error = %v", err)
	}
	controller, err := New(Config{BuildVersion: "test", Settings: store, Keys: keys, Storage: db, Catalog: catalog})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(controller.Close)
	return controllerTestEnv{db: db, keys: keys, controller: controller}
}

func (e controllerTestEnv) issue(t *testing.T, name string, permissions []string, modemIDs []string, allModems bool) (mcpauth.Grant, string) {
	t.Helper()
	grant, token, err := e.keys.Issue(context.Background(), mcpauth.IssueRequest{Name: name, ValidityDays: mcpauth.DefaultValidityDays, AllModems: allModems, ModemIDs: modemIDs, Permissions: permissions})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	return grant, token
}

func connectControllerClient(t *testing.T, server *httptest.Server, token string) *mcp.ClientSession {
	t.Helper()
	httpClient := *server.Client()
	httpClient.Transport = bearerRoundTripper{token: token, base: httpClient.Transport}
	client := mcp.NewClient(&mcp.Implementation{Name: "controller-test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL, HTTPClient: &httpClient, MaxRetries: -1, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	return session
}

type bearerRoundTripper struct {
	token string
	base  http.RoundTripper
}

func (t bearerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	clone.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(clone)
}

func controllerRequest(t *testing.T, client *http.Client, method string, url string, token string, sessionID string, origin string, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	data, readErr := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()
	if readErr != nil {
		t.Fatalf("ReadAll() error = %v", readErr)
	}
	if closeErr != nil {
		t.Fatalf("Close() error = %v", closeErr)
	}
	return resp.StatusCode, string(data)
}

func resultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) != 1 {
		t.Fatalf("result content length = %d, want 1", len(result.Content))
	}
	content, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("result content type = %T, want *mcp.TextContent", result.Content[0])
	}
	return content.Text
}

func TestControllerSessionTimeoutConfigured(t *testing.T) {
	if sessionTimeout != 30*time.Minute {
		t.Fatalf("sessionTimeout = %v, want 30m", sessionTimeout)
	}
}
