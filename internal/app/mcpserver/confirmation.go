package mcpserver

import (
	"context"
	"errors"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type formElicitor interface {
	Elicit(context.Context, *mcp.ElicitParams) (*mcp.ElicitResult, error)
}

func RequireConfirmation(ctx context.Context, req *mcp.CallToolRequest, message string) error {
	if req == nil || req.Session == nil {
		return NewToolError("interaction_required", "the MCP client must support form elicitation for this operation", nil)
	}
	return requireConfirmation(ctx, req.Session, message)
}

func requireConfirmation(ctx context.Context, client formElicitor, message string) error {
	if client == nil {
		return NewToolError("interaction_required", "the MCP client must support form elicitation for this operation", nil)
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return errors.New("confirmation message is required")
	}
	result, err := client.Elicit(ctx, &mcp.ElicitParams{
		Message: message,
		RequestedSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"accept": map[string]any{"type": "boolean", "description": "Confirm this operation"},
			},
			"required": []string{"accept"},
		},
	})
	if err != nil {
		return NewToolError("interaction_required", "the MCP client must support form elicitation for this operation", err)
	}
	if result == nil {
		return NewToolError("interaction_required", "the MCP client returned no confirmation response", nil)
	}
	accepted, _ := result.Content["accept"].(bool)
	if result.Action != "accept" || !accepted {
		return NewToolError("cancelled", "the operation was not confirmed", nil)
	}
	return nil
}
