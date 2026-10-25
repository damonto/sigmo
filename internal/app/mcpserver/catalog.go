package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/damonto/sigmo/internal/app/feature"
	"github.com/damonto/sigmo/internal/app/mcpauth"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

const AuditRetention = 90 * 24 * time.Hour

type Permission struct {
	Name    string       `json:"name"`
	Module  string       `json:"module"`
	Feature feature.Name `json:"-"`
}

type Extension func(*Catalog) error

type registration struct {
	name                 string
	permission           string
	requiresConfirmation bool
	register             func(*mcp.Server, *execution)
}

type Catalog struct {
	policy      feature.Policy
	mu          sync.RWMutex
	permissions map[string]Permission
	tools       []registration
}

func NewCatalog() *Catalog {
	return &Catalog{permissions: make(map[string]Permission)}
}

// SetFeaturePolicy configures the shared entitlement boundary before tools are served.
func (c *Catalog) SetFeaturePolicy(policy feature.Policy) { c.policy = policy }

// AddPermission registers a permission and its optional capability requirement.
func (c *Catalog) AddPermission(permission Permission) error {
	permission.Name = strings.TrimSpace(permission.Name)
	permission.Module = strings.TrimSpace(permission.Module)
	if permission.Name == "" || permission.Module == "" {
		return errors.New("MCP permission name and module are required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if current, ok := c.permissions[permission.Name]; ok && current != permission {
		return fmt.Errorf("MCP permission %q already has a different registration", permission.Name)
	}
	c.permissions[permission.Name] = permission
	return nil
}

func (c *Catalog) Permissions() []Permission {
	c.mu.RLock()
	defer c.mu.RUnlock()
	permissions := make([]Permission, 0, len(c.permissions))
	for _, permission := range c.permissions {
		if permission.Feature == "" || c.policy.Allows(permission.Feature) {
			permissions = append(permissions, permission)
		}
	}
	slices.SortFunc(permissions, func(a, b Permission) int {
		return strings.Compare(a.Name, b.Name)
	})
	return permissions
}

func (c *Catalog) SupportsPermission(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.permissions[name]
	return ok
}

func (c *Catalog) RequiresConfirmation(toolName string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, tool := range c.tools {
		if tool.name == toolName {
			return tool.requiresConfirmation
		}
	}
	return false
}

type ToolHandler[In, Out any] func(context.Context, *mcp.CallToolRequest, mcpauth.Grant, In) (Out, error)

func AddTool[In, Out any](c *Catalog, permission string, tool *mcp.Tool, modemIDs func(In) []string, handler ToolHandler[In, Out]) error {
	return addTool(c, permission, tool, modemIDs, GuardedToolPolicy[In]{}, handler)
}

func AddGuardedTool[In, Out any](c *Catalog, permission string, tool *mcp.Tool, modemIDs func(In) []string, policy GuardedToolPolicy[In], handler ToolHandler[In, Out]) error {
	if err := validateGuardedPolicy(policy); err != nil {
		return err
	}
	return addTool(c, permission, tool, modemIDs, policy, handler)
}

func addTool[In, Out any](c *Catalog, permission string, tool *mcp.Tool, modemIDs func(In) []string, policy GuardedToolPolicy[In], handler ToolHandler[In, Out]) error {
	if c == nil || tool == nil || handler == nil {
		return errors.New("MCP catalog, tool, and handler are required")
	}
	permission = strings.TrimSpace(permission)
	if permission != "" && !c.SupportsPermission(permission) {
		return fmt.Errorf("MCP permission %q is not registered", permission)
	}
	name := strings.TrimSpace(tool.Name)
	if name == "" {
		return errors.New("MCP tool name is required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if slices.ContainsFunc(c.tools, func(current registration) bool { return current.name == name }) {
		return fmt.Errorf("MCP tool %q is already registered", name)
	}
	c.tools = append(c.tools, registration{
		name:                 name,
		permission:           permission,
		requiresConfirmation: policy.Confirmation != nil,
		register: func(server *mcp.Server, exec *execution) {
			mcp.AddTool(server, tool, func(ctx context.Context, req *mcp.CallToolRequest, input In) (*mcp.CallToolResult, Out, error) {
				ids := []string(nil)
				if modemIDs != nil {
					ids = modemIDs(input)
				}
				started := time.Now()
				if err := c.authorize(exec, permission, ids); err != nil {
					exec.record(ctx, name, ids, started, err)
					var zero Out
					return nil, zero, err
				}
				authorizedHandler := func(ctx context.Context, req *mcp.CallToolRequest, grant mcpauth.Grant, input In) (Out, error) {
					if err := c.authorize(exec, permission, ids); err != nil {
						var zero Out
						return zero, err
					}
					return handler(ctx, req, grant, input)
				}
				var out Out
				var err error
				if policy.Confirmation == nil {
					out, err = handler(ctx, req, exec.grant, input)
				} else {
					out, err = executeGuardedTool(ctx, req, exec, name, policy, authorizedHandler, input)
				}
				exec.record(ctx, name, ids, started, err)
				if err != nil {
					return nil, out, err
				}
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: name + " completed."}}}, out, nil
			})
		},
	})
	return nil
}

func (c *Catalog) registerTools(server *mcp.Server, exec *execution) {
	c.mu.RLock()
	tools := slices.Clone(c.tools)
	c.mu.RUnlock()
	for _, tool := range tools {
		if c.allowsPermission(tool.permission) && (tool.permission == "" || exec.grant.HasPermission(tool.permission)) {
			tool.register(server, exec)
		}
	}
}

func (c *Catalog) allowsPermission(name string) bool {
	if name == "" {
		return true
	}
	c.mu.RLock()
	permission, ok := c.permissions[name]
	c.mu.RUnlock()
	return ok && (permission.Feature == "" || c.policy.Allows(permission.Feature))
}

func (c *Catalog) authorize(exec *execution, permission string, ids []string) error {
	if !c.allowsPermission(permission) {
		return NewToolError("license_entitlement_inactive", "an active Pro authorization is required", nil)
	}
	return exec.authorize(permission, ids)
}

type execution struct {
	grant  mcpauth.Grant
	db     *storage.Store
	policy executionPolicyState
}

func (e *execution) authorize(permission string, modemIDs []string) error {
	if permission != "" && !e.grant.HasPermission(permission) {
		return NewToolError("permission_denied", "the API key does not grant this operation", nil)
	}
	for _, modemID := range modemIDs {
		if !e.grant.AllowsModem(modemID) {
			return NewToolError("permission_denied", "the API key does not grant access to the requested modem", nil)
		}
	}
	return nil
}

func (e *execution) record(ctx context.Context, tool string, modemIDs []string, started time.Time, toolErr error) {
	if e.db == nil {
		return
	}
	outcome := "success"
	errorCode := ""
	if toolErr != nil {
		outcome = "error"
		errorCode = ErrorCode(toolErr)
		if errorCode == "cancelled" || errors.Is(toolErr, context.Canceled) || errors.Is(toolErr, context.DeadlineExceeded) {
			outcome = "cancelled"
		}
	}
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	createdAt := time.Now().UTC()
	if err := e.db.CreateMCPAuditEvent(auditCtx, storage.MCPAuditEvent{
		KeyID:     e.grant.ID,
		KeyName:   e.grant.Name,
		Tool:      tool,
		ModemIDs:  normalizeModemIDs(modemIDs),
		Outcome:   outcome,
		ErrorCode: errorCode,
		Duration:  time.Since(started),
		CreatedAt: createdAt,
	}, createdAt.Add(-AuditRetention)); err != nil {
		slog.Warn("write MCP audit event", "error", err, "tool", tool, "key_id", e.grant.ID)
	}
}

func normalizeModemIDs(ids []string) []string {
	var normalized []string
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" && !slices.Contains(normalized, id) {
			normalized = append(normalized, id)
		}
	}
	slices.Sort(normalized)
	return normalized
}

type toolError struct {
	code    string
	message string
	cause   error
}

func NewToolError(code string, message string, cause error) error {
	return &toolError{code: strings.TrimSpace(code), message: strings.TrimSpace(message), cause: cause}
}

func (e *toolError) Error() string {
	return e.code + ": " + e.message
}

func (e *toolError) Unwrap() error {
	return e.cause
}

func ErrorCode(err error) string {
	var target *toolError
	if errors.As(err, &target) {
		return target.code
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	return "operation_failed"
}
