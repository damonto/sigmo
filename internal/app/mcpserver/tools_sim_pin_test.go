package mcpserver

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/damonto/sigmo/internal/app/mcpauth"
	modemcore "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSIMPINToolsRequireIndependentPermission(t *testing.T) {
	c := NewCatalog()
	for _, p := range corePermissions() {
		if err := c.AddPermission(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := (&coreTools{}).registerSIMPIN(c); err != nil {
		t.Fatal(err)
	}
	for _, tool := range c.tools {
		want := "sim.pin"
		if tool.name == "get_sim_pin_status" {
			want = "sim.read"
		}
		if tool.permission != want {
			t.Fatalf("%s permission=%s", tool.name, tool.permission)
		}
		if want == "sim.pin" && !tool.requiresConfirmation {
			t.Fatal("missing confirmation")
		}
	}
	exec := &execution{grant: mcpauth.Grant{AllModems: true, Permissions: []string{"sim.update"}}}
	if err := exec.authorize("sim.pin", []string{"device"}); err == nil {
		t.Fatal("sim.update expanded to sim.pin")
	}
	tool := pinWriteTool("test", "Test")
	if tool.Annotations.IdempotentHint || tool.Annotations.ReadOnlyHint || !*tool.Annotations.DestructiveHint {
		t.Fatal("incorrect PIN write annotations")
	}
}

func TestSIMPINConfirmationAndDeduplication(t *testing.T) {
	policy := (&coreTools{}).simPINUnblockPolicy()
	input := simPINUnblockInput{ModemID: "device", SIMToken: "target", IdempotencyKey: "submission", PUK: "00123456", NewPIN: "9876"}
	message := policy.Confirmation(input)
	if strings.Contains(message, input.PUK) || strings.Contains(message, input.NewPIN) {
		t.Fatal("confirmation exposed credential")
	}
	// Domain and registry validation are covered separately; exercise the actual
	// PIN policy's cache and confirmation boundary with a deterministic operation.
	policy.Validate = nil
	exec := acceptingExecution()
	calls := 0
	handler := func(context.Context, *mcp.CallToolRequest, mcpauth.Grant, simPINUnblockInput) (successOutput, error) {
		calls++
		return successOutput{}, pinToolError(modemcore.ErrSIMPINResultUnknown)
	}
	for range 2 {
		if _, err := executeGuardedTool(t.Context(), nil, exec, "unblock_sim_pin", policy, handler, input); err == nil {
			t.Fatal("missing unknown result")
		}
	}
	if calls != 1 {
		t.Fatalf("credential sent %d times", calls)
	}
	denied := acceptingExecution()
	denied.policy.confirm = func(context.Context, *mcp.CallToolRequest, string) error { return errors.New("declined") }
	if _, err := executeGuardedTool(t.Context(), nil, denied, "unblock_sim_pin", policy, handler, input); err == nil {
		t.Fatal("confirmation accepted")
	}
	if calls != 1 {
		t.Fatal("declined confirmation executed credential")
	}
	if err := pinToolError(errors.New("private PIN 00123456")); strings.Contains(err.Error(), input.PUK) {
		t.Fatal("error exposed credential")
	}
}
