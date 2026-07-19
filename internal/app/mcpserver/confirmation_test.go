package mcpserver

import (
	"context"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type formElicitorFunc func(context.Context, *mcp.ElicitParams) (*mcp.ElicitResult, error)

func (f formElicitorFunc) Elicit(ctx context.Context, params *mcp.ElicitParams) (*mcp.ElicitResult, error) {
	return f(ctx, params)
}

func TestRequireConfirmation(t *testing.T) {
	errElicit := errors.New("elicitation unsupported")
	tests := []struct {
		name     string
		client   formElicitor
		wantCode string
	}{
		{
			name: "accept",
			client: formElicitorFunc(func(_ context.Context, params *mcp.ElicitParams) (*mcp.ElicitResult, error) {
				if params.Message != "Confirm operation" || params.RequestedSchema == nil {
					t.Fatalf("ElicitParams = %+v", params)
				}
				return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"accept": true}}, nil
			}),
		},
		{
			name: "decline",
			client: formElicitorFunc(func(context.Context, *mcp.ElicitParams) (*mcp.ElicitResult, error) {
				return &mcp.ElicitResult{Action: "decline", Content: map[string]any{"accept": false}}, nil
			}),
			wantCode: "cancelled",
		},
		{
			name: "cancel",
			client: formElicitorFunc(func(context.Context, *mcp.ElicitParams) (*mcp.ElicitResult, error) {
				return &mcp.ElicitResult{Action: "cancel"}, nil
			}),
			wantCode: "cancelled",
		},
		{
			name: "missing response",
			client: formElicitorFunc(func(context.Context, *mcp.ElicitParams) (*mcp.ElicitResult, error) {
				return nil, nil
			}),
			wantCode: "interaction_required",
		},
		{
			name: "unsupported",
			client: formElicitorFunc(func(context.Context, *mcp.ElicitParams) (*mcp.ElicitResult, error) {
				return nil, errElicit
			}),
			wantCode: "interaction_required",
		},
		{name: "missing client", wantCode: "interaction_required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := requireConfirmation(context.Background(), tt.client, "Confirm operation")
			if tt.wantCode == "" {
				if err != nil {
					t.Fatalf("requireConfirmation() error = %v", err)
				}
				return
			}
			if got := ErrorCode(err); got != tt.wantCode {
				t.Fatalf("requireConfirmation() error code = %q, want %q; error = %v", got, tt.wantCode, err)
			}
		})
	}
}
