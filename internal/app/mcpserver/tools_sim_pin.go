package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	modemhandler "github.com/damonto/sigmo/internal/app/handler/modem"
	"github.com/damonto/sigmo/internal/app/mcpauth"
	modemcore "github.com/damonto/sigmo/internal/pkg/modem"
)

type simPINProtectionInput struct {
	ModemID        string `json:"modemId"`
	SIMToken       string `json:"simToken" jsonschema:"token returned by get_sim_pin_status"`
	IdempotencyKey string `json:"idempotencyKey" jsonschema:"unique key for this submission; reuse only for delivery of the identical request"`
	Enabled        bool   `json:"enabled"`
	PIN            string `json:"pin" jsonschema:"current PIN: 4-8 ASCII digits; never include it in confirmation or logs"`
}

type simPINChangeInput struct {
	ModemID        string `json:"modemId"`
	SIMToken       string `json:"simToken"`
	IdempotencyKey string `json:"idempotencyKey"`
	OldPIN         string `json:"oldPin" jsonschema:"current PIN: 4-8 ASCII digits"`
	NewPIN         string `json:"newPin" jsonschema:"replacement PIN: 4-8 ASCII digits"`
}

type simPINUnlockInput struct {
	ModemID        string `json:"modemId"`
	SIMToken       string `json:"simToken"`
	IdempotencyKey string `json:"idempotencyKey"`
	PIN            string `json:"pin" jsonschema:"PIN: 4-8 ASCII digits"`
}

type simPINUnblockInput struct {
	ModemID        string `json:"modemId"`
	SIMToken       string `json:"simToken"`
	IdempotencyKey string `json:"idempotencyKey"`
	PUK            string `json:"puk" jsonschema:"carrier-provided PUK: exactly 8 ASCII digits"`
	NewPIN         string `json:"newPin" jsonschema:"replacement PIN: 4-8 ASCII digits"`
}

func (t *coreTools) registerSIMPIN(c *Catalog) error {
	registrations := []func() error{
		func() error {
			return AddTool(c, "sim.read", ReadTool("get_sim_pin_status", "Get active SIM PIN protection, lock, attempts and SIM token"), modemID, t.getSIMPINStatus)
		},
		func() error {
			return AddGuardedTool(c, "sim.pin", pinWriteTool("set_sim_pin_protection", "Set SIM PIN protection"), func(i simPINProtectionInput) []string { return []string{i.ModemID} }, t.simPINProtectionPolicy(), t.setSIMPINProtection)
		},
		func() error {
			return AddGuardedTool(c, "sim.pin", pinWriteTool("change_sim_pin", "Change SIM PIN"), func(i simPINChangeInput) []string { return []string{i.ModemID} }, t.simPINChangePolicy(), t.changeSIMPIN)
		},
		func() error {
			return AddGuardedTool(c, "sim.pin", pinWriteTool("unlock_sim_pin", "Unlock SIM with PIN"), func(i simPINUnlockInput) []string { return []string{i.ModemID} }, t.simPINUnlockPolicy(), t.unlockSIMPIN)
		},
		func() error {
			return AddGuardedTool(c, "sim.pin", pinWriteTool("unblock_sim_pin", "Unblock SIM with PUK"), func(i simPINUnblockInput) []string { return []string{i.ModemID} }, t.simPINUnblockPolicy(), t.unblockSIMPIN)
		},
	}
	for _, register := range registrations {
		if err := register(); err != nil {
			return err
		}
	}
	return nil
}

func pinWriteTool(name, title string) *mcp.Tool {
	tool := WriteTool(name, title, false, false)
	tool.Description += " Read get_sim_pin_status first. A wrong credential consumes a finite attempt. After any error or timeout, read status; never automatically retry or try another code. Credentials are used only for this operation."
	return tool
}

func pinConfirmation(action, id string) string {
	return fmt.Sprintf("%s on modem %q? An incorrect code can consume an attempt and permanently block the SIM. This uses the active SIM without switching cards.", action, id)
}

func pinToolError(err error) error {
	_, code, detail := modemhandler.SIMPINProblem(err)
	if code == "" {
		code, detail = "sim_pin_state_conflict", "read get_sim_pin_status before continuing"
	}
	// Do not retain an arbitrary protocol error in cached results or audit records.
	return NewToolError(code, detail, nil)
}

func (t *coreTools) validateSIMPINTarget(ctx context.Context, id, token string) error {
	if token == "" {
		return pinToolError(modemcore.ErrSIMPINInvalid)
	}
	if err := t.validateModem(ctx, id); err != nil {
		return err
	}
	device, err := t.registry.Find(ctx, id)
	if err != nil {
		return OperationError("find modem", err)
	}
	s, err := device.SIMPINStatus(ctx)
	if err != nil {
		return pinToolError(err)
	}
	if s.SIMToken != token {
		return pinToolError(modemcore.ErrSIMChanged)
	}
	if !s.Supported {
		return pinToolError(modemcore.ErrSIMPINUnsupported)
	}
	return nil
}

func (t *coreTools) simPINProtectionPolicy() GuardedToolPolicy[simPINProtectionInput] {
	return GuardedToolPolicy[simPINProtectionInput]{
		Validate: func(ctx context.Context, i simPINProtectionInput) error {
			if !modemcore.ValidSIMPIN(i.PIN) {
				return pinToolError(modemcore.ErrSIMPINInvalid)
			}
			return t.validateSIMPINTarget(ctx, i.ModemID, i.SIMToken)
		},
		Confirmation: func(i simPINProtectionInput) string {
			action := "Disable SIM PIN protection"
			if i.Enabled {
				action = "Enable SIM PIN protection"
			}
			return pinConfirmation(action, i.ModemID)
		},
		IdempotencyKey: func(i simPINProtectionInput) string { return i.IdempotencyKey },
	}
}

func (t *coreTools) simPINChangePolicy() GuardedToolPolicy[simPINChangeInput] {
	return GuardedToolPolicy[simPINChangeInput]{
		Validate: func(ctx context.Context, i simPINChangeInput) error {
			if !modemcore.ValidSIMPIN(i.OldPIN) || !modemcore.ValidSIMPIN(i.NewPIN) {
				return pinToolError(modemcore.ErrSIMPINInvalid)
			}
			return t.validateSIMPINTarget(ctx, i.ModemID, i.SIMToken)
		},
		Confirmation:   func(i simPINChangeInput) string { return pinConfirmation("Change SIM PIN", i.ModemID) },
		IdempotencyKey: func(i simPINChangeInput) string { return i.IdempotencyKey },
	}
}
func (t *coreTools) simPINUnlockPolicy() GuardedToolPolicy[simPINUnlockInput] {
	return GuardedToolPolicy[simPINUnlockInput]{
		Validate: func(ctx context.Context, i simPINUnlockInput) error {
			if !modemcore.ValidSIMPIN(i.PIN) {
				return pinToolError(modemcore.ErrSIMPINInvalid)
			}
			return t.validateSIMPINTarget(ctx, i.ModemID, i.SIMToken)
		},
		Confirmation:   func(i simPINUnlockInput) string { return pinConfirmation("Unlock SIM and enable modem", i.ModemID) },
		IdempotencyKey: func(i simPINUnlockInput) string { return i.IdempotencyKey },
	}
}
func (t *coreTools) simPINUnblockPolicy() GuardedToolPolicy[simPINUnblockInput] {
	return GuardedToolPolicy[simPINUnblockInput]{
		Validate: func(ctx context.Context, i simPINUnblockInput) error {
			if !modemcore.ValidSIMPUK(i.PUK) || !modemcore.ValidSIMPIN(i.NewPIN) {
				return pinToolError(modemcore.ErrSIMPINInvalid)
			}
			return t.validateSIMPINTarget(ctx, i.ModemID, i.SIMToken)
		},
		Confirmation: func(i simPINUnblockInput) string {
			return pinConfirmation("Unblock SIM with PUK and enable modem", i.ModemID)
		},
		IdempotencyKey: func(i simPINUnblockInput) string { return i.IdempotencyKey },
	}
}

func (t *coreTools) getSIMPINStatus(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, i modemInput) (modemhandler.SIMPINResponse, error) {
	device, err := t.registry.Find(ctx, i.ModemID)
	if err != nil {
		return modemhandler.SIMPINResponse{}, OperationError("find modem", err)
	}
	s, err := device.SIMPINStatus(ctx)
	if err != nil {
		return modemhandler.SIMPINResponse{}, pinToolError(err)
	}
	return modemhandler.NewSIMPINResponse(s), nil
}

func (t *coreTools) setSIMPINProtection(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, i simPINProtectionInput) (successOutput, error) {
	device, err := t.registry.Find(ctx, i.ModemID)
	if err != nil {
		return successOutput{}, OperationError("find modem", err)
	}
	if err := device.SetSIMPINProtection(ctx, modemcore.SIMPINProtection{Enabled: i.Enabled, PIN: i.PIN, SIMToken: i.SIMToken}); err != nil {
		return successOutput{}, pinToolError(err)
	}
	return successOutput{Success: true}, nil
}

func (t *coreTools) changeSIMPIN(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, i simPINChangeInput) (successOutput, error) {
	device, err := t.registry.Find(ctx, i.ModemID)
	if err != nil {
		return successOutput{}, OperationError("find modem", err)
	}
	if err := device.ChangeSIMPIN(ctx, modemcore.SIMPINChange{OldPIN: i.OldPIN, NewPIN: i.NewPIN, SIMToken: i.SIMToken}); err != nil {
		return successOutput{}, pinToolError(err)
	}
	return successOutput{Success: true}, nil
}

func (t *coreTools) unlockSIMPIN(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, i simPINUnlockInput) (successOutput, error) {
	device, err := t.registry.Find(ctx, i.ModemID)
	if err != nil {
		return successOutput{}, OperationError("find modem", err)
	}
	if err := device.UnlockSIM(ctx, modemcore.SIMUnlock{PIN: i.PIN, SIMToken: i.SIMToken}); err != nil {
		return successOutput{}, pinToolError(err)
	}
	return successOutput{Success: true}, nil
}

func (t *coreTools) unblockSIMPIN(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, i simPINUnblockInput) (successOutput, error) {
	device, err := t.registry.Find(ctx, i.ModemID)
	if err != nil {
		return successOutput{}, OperationError("find modem", err)
	}
	if err := device.UnlockSIM(ctx, modemcore.SIMUnlock{PUK: i.PUK, NewPIN: i.NewPIN, SIMToken: i.SIMToken}); err != nil {
		return successOutput{}, pinToolError(err)
	}
	return successOutput{Success: true}, nil
}
