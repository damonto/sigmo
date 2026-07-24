//go:build ims

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/damonto/sigmo/internal/app/mcpauth"
	"github.com/damonto/sigmo/internal/app/mcpserver"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	procall "github.com/damonto/sigmo/pro/call"
	pims "github.com/damonto/sigmo/pro/ims"
)

type proModemInput struct {
	ModemID string `json:"modemId"`
}

func proModemIDs(input proModemInput) []string { return []string{input.ModemID} }

func registerIMSMCP(registry *mmodem.Registry, wifiCalling pims.Coordinator, volte pims.Coordinator, calls *procall.Calls) mcpserver.Extension {
	return func(catalog *mcpserver.Catalog) error {
		for _, permission := range []mcpserver.Permission{
			{Name: "wifi_calling.read", Module: "wifi_calling"},
			{Name: "wifi_calling.write", Module: "wifi_calling"},
			{Name: "volte.read", Module: "volte"},
			{Name: "volte.write", Module: "volte"},
			{Name: "calls.read", Module: "calls"},
			{Name: "calls.delete", Module: "calls"},
		} {
			if err := catalog.AddPermission(permission.Name, permission.Module); err != nil {
				return err
			}
		}
		tools := &imsMCPTools{registry: registry, wifiCalling: wifiCalling, volte: volte, calls: calls}
		registrations := []func() error{
			func() error {
				return mcpserver.AddTool(catalog, "wifi_calling.read", mcpserver.ReadTool("get_wifi_calling", "Get Wi-Fi Calling status"), proModemIDs, tools.getWiFiCalling)
			},
			func() error {
				return mcpserver.AddGuardedTool(catalog, "wifi_calling.write", mcpserver.WriteTool("set_wifi_calling", "Set Wi-Fi Calling", true, true), wifiSettingsModemIDs, tools.setWiFiCallingPolicy(), tools.setWiFiCalling)
			},
			func() error {
				return mcpserver.AddGuardedTool(catalog, "wifi_calling.write", mcpserver.WriteTool("reconnect_wifi_calling", "Reconnect Wi-Fi Calling", false, true), proModemIDs, tools.reconnectWiFiCallingPolicy(), tools.reconnectWiFiCalling)
			},
			func() error {
				return mcpserver.AddGuardedTool(catalog, "wifi_calling.write", mcpserver.WriteTool("disconnect_wifi_calling", "Disconnect Wi-Fi Calling", true, true), proModemIDs, tools.disconnectWiFiCallingPolicy(), tools.disconnectWiFiCalling)
			},
			func() error {
				return mcpserver.AddTool(catalog, "volte.read", mcpserver.ReadTool("get_volte", "Get VoLTE status"), proModemIDs, tools.getVoLTE)
			},
			func() error {
				return mcpserver.AddGuardedTool(catalog, "volte.write", mcpserver.WriteTool("set_volte", "Set VoLTE", true, true), volteSettingsModemIDs, tools.setVoLTEPolicy(), tools.setVoLTE)
			},
			func() error {
				return mcpserver.AddTool(catalog, "calls.read", mcpserver.ReadTool("list_calls", "List calls"), callListModemIDs, tools.listCalls)
			},
			func() error {
				return mcpserver.AddGuardedTool(catalog, "calls.delete", mcpserver.WriteTool("delete_call_record", "Delete call record", true, false), callActionModemIDs, tools.deleteCallRecordPolicy(), tools.deleteCallRecord)
			},
		}
		for _, register := range registrations {
			if err := register(); err != nil {
				return err
			}
		}
		return nil
	}
}

type imsMCPTools struct {
	registry    modemFinder
	wifiCalling pims.Coordinator
	volte       pims.Coordinator
	calls       *procall.Calls
}

type modemFinder interface {
	Find(context.Context, string) (*mmodem.Modem, error)
}

func (t *imsMCPTools) modem(ctx context.Context, id string) (*mmodem.Modem, error) {
	device, err := t.registry.Find(ctx, id)
	if err != nil {
		return nil, mcpserver.OperationError("find modem", err)
	}
	return device, nil
}

func (t *imsMCPTools) getWiFiCalling(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input proModemInput) (pims.SettingsResponse, error) {
	device, err := t.modem(ctx, input.ModemID)
	if err != nil {
		return pims.SettingsResponse{}, err
	}
	response, err := pims.ReadWiFiCallingSettings(ctx, device, t.wifiCalling)
	if err != nil {
		return pims.SettingsResponse{}, mcpserver.OperationError("get Wi-Fi Calling status", err)
	}
	response.Websheet = nil
	return response, nil
}

type wifiSettingsInput struct {
	ModemID string `json:"modemId"`
	Enabled bool   `json:"enabled"`
}

func wifiSettingsModemIDs(input wifiSettingsInput) []string { return []string{input.ModemID} }
func (t *imsMCPTools) setWiFiCallingPolicy() mcpserver.GuardedToolPolicy[wifiSettingsInput] {
	return mcpserver.GuardedToolPolicy[wifiSettingsInput]{
		Validate: func(ctx context.Context, input wifiSettingsInput) error {
			_, err := t.modem(ctx, input.ModemID)
			return err
		},
		Confirmation: func(input wifiSettingsInput) string {
			return fmt.Sprintf("Set Wi-Fi Calling on modem %q to enabled=%t? This can interrupt IMS service.", input.ModemID, input.Enabled)
		},
	}
}
func (t *imsMCPTools) setWiFiCalling(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input wifiSettingsInput) (proSuccessOutput, error) {
	device, err := t.modem(ctx, input.ModemID)
	if err != nil {
		return proSuccessOutput{}, err
	}
	if err := t.wifiCalling.UpdateSettings(ctx, device, pims.Settings{Enabled: input.Enabled}); err != nil {
		return proSuccessOutput{}, mcpserver.OperationError("set Wi-Fi Calling", err)
	}
	return proSuccessOutput{Success: true}, nil
}
func (t *imsMCPTools) reconnectWiFiCallingPolicy() mcpserver.GuardedToolPolicy[proModemInput] {
	return mcpserver.GuardedToolPolicy[proModemInput]{
		Validate: func(ctx context.Context, input proModemInput) error {
			_, err := t.modem(ctx, input.ModemID)
			return err
		},
		Confirmation: func(input proModemInput) string {
			return fmt.Sprintf("Reconnect Wi-Fi Calling on modem %q? This briefly interrupts IMS service.", input.ModemID)
		},
	}
}
func (t *imsMCPTools) reconnectWiFiCalling(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input proModemInput) (proSuccessOutput, error) {
	device, err := t.modem(ctx, input.ModemID)
	if err != nil {
		return proSuccessOutput{}, err
	}
	if err := t.wifiCalling.Reconnect(ctx, device); err != nil {
		return proSuccessOutput{}, mcpserver.OperationError("reconnect Wi-Fi Calling", err)
	}
	return proSuccessOutput{Success: true}, nil
}
func (t *imsMCPTools) disconnectWiFiCallingPolicy() mcpserver.GuardedToolPolicy[proModemInput] {
	return mcpserver.GuardedToolPolicy[proModemInput]{
		Validate: func(ctx context.Context, input proModemInput) error {
			_, err := t.modem(ctx, input.ModemID)
			return err
		},
		Confirmation: func(input proModemInput) string {
			return fmt.Sprintf("Disconnect Wi-Fi Calling on modem %q? Calls and SMS over Wi-Fi will become unavailable.", input.ModemID)
		},
	}
}
func (t *imsMCPTools) disconnectWiFiCalling(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input proModemInput) (proSuccessOutput, error) {
	device, err := t.modem(ctx, input.ModemID)
	if err != nil {
		return proSuccessOutput{}, err
	}
	if err := t.wifiCalling.Disconnect(ctx, device); err != nil {
		return proSuccessOutput{}, mcpserver.OperationError("disconnect Wi-Fi Calling", err)
	}
	return proSuccessOutput{Success: true}, nil
}

func (t *imsMCPTools) getVoLTE(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input proModemInput) (pims.VoLTESettingsResponse, error) {
	device, err := t.modem(ctx, input.ModemID)
	if err != nil {
		return pims.VoLTESettingsResponse{}, err
	}
	response, err := pims.ReadVoLTESettings(ctx, device, t.volte)
	if err != nil {
		return pims.VoLTESettingsResponse{}, mcpserver.OperationError("get VoLTE status", err)
	}
	return response, nil
}

type volteSettingsInput struct {
	ModemID            string `json:"modemId"`
	Enabled            bool   `json:"enabled"`
	DataPath           string `json:"dataPath,omitempty"`
	SetIMSAPNAsDefault bool   `json:"setImsApnAsDefault"`
	EnablePCSCFViaPCO  bool   `json:"enablePcscfViaPco"`
}

func volteSettingsModemIDs(input volteSettingsInput) []string { return []string{input.ModemID} }
func volteSettings(input volteSettingsInput) pims.Settings {
	return pims.Settings{
		Enabled: input.Enabled, DataPath: pims.DataPath(input.DataPath), SetIMSAPNAsDefault: input.SetIMSAPNAsDefault,
		EnablePCSCFViaPCO: input.EnablePCSCFViaPCO,
	}
}

func voLTEInputError(err error) error {
	if errors.Is(err, pims.ErrVoLTEDataPathRequired) ||
		errors.Is(err, pims.ErrVoLTEDataPathUnsupported) ||
		errors.Is(err, pims.ErrVoLTEProfileOptionsUnsupported) {
		return mcpserver.NewToolError("invalid_request", "VoLTE settings are not supported by this modem", err)
	}
	return mcpserver.OperationError("set VoLTE", err)
}

func (t *imsMCPTools) setVoLTEPolicy() mcpserver.GuardedToolPolicy[volteSettingsInput] {
	return mcpserver.GuardedToolPolicy[volteSettingsInput]{
		Validate: func(ctx context.Context, input volteSettingsInput) error {
			device, err := t.modem(ctx, input.ModemID)
			if err != nil {
				return err
			}
			_, err = pims.ResolveVoLTESettings(device, volteSettings(input))
			if err != nil {
				return voLTEInputError(err)
			}
			return nil
		},
		Confirmation: func(input volteSettingsInput) string {
			return fmt.Sprintf("Set VoLTE on modem %q to enabled=%t with data path %q? This can interrupt IMS service and routing.", input.ModemID, input.Enabled, input.DataPath)
		},
	}
}
func (t *imsMCPTools) setVoLTE(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input volteSettingsInput) (proSuccessOutput, error) {
	device, err := t.modem(ctx, input.ModemID)
	if err != nil {
		return proSuccessOutput{}, err
	}
	if err := pims.UpdateVoLTESettings(ctx, device, t.volte, volteSettings(input)); err != nil {
		return proSuccessOutput{}, voLTEInputError(err)
	}
	return proSuccessOutput{Success: true}, nil
}

type callListInput struct {
	ModemID string `json:"modemId"`
	Query   string `json:"query,omitempty"`
}

func callListModemIDs(input callListInput) []string { return []string{input.ModemID} }

type callActionInput struct {
	ModemID string `json:"modemId"`
	CallID  string `json:"callId"`
}

func callActionModemIDs(input callActionInput) []string { return []string{input.ModemID} }

type callsOutput struct {
	Calls []procall.CallResponse `json:"calls" jsonschema:"call records matching the request"`
}

func (t *imsMCPTools) listCalls(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input callListInput) (callsOutput, error) {
	device, err := t.modem(ctx, input.ModemID)
	if err != nil {
		return callsOutput{}, err
	}
	values, err := t.calls.List(ctx, device, input.Query)
	if err != nil {
		return callsOutput{}, mcpserver.OperationError("list calls", err)
	}
	return callsOutput{Calls: procall.ResponsesFromCalls(values)}, nil
}

func (t *imsMCPTools) deleteCallRecordPolicy() mcpserver.GuardedToolPolicy[callActionInput] {
	return mcpserver.GuardedToolPolicy[callActionInput]{
		Validate: func(ctx context.Context, input callActionInput) error {
			if strings.TrimSpace(input.CallID) == "" {
				return mcpserver.NewToolError("invalid_request", "callId is required", nil)
			}
			_, err := t.modem(ctx, input.ModemID)
			return err
		},
		Confirmation: func(input callActionInput) string {
			return fmt.Sprintf("Permanently delete call record %q from modem %q? This cannot be undone.", input.CallID, input.ModemID)
		},
	}
}
func (t *imsMCPTools) deleteCallRecord(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input callActionInput) (proSuccessOutput, error) {
	device, err := t.modem(ctx, input.ModemID)
	if err != nil {
		return proSuccessOutput{}, err
	}
	if err := t.calls.Delete(ctx, device, input.CallID); err != nil {
		return proSuccessOutput{}, mcpserver.OperationError("delete call record", err)
	}
	return proSuccessOutput{Success: true}, nil
}
