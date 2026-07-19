package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	elpa "github.com/damonto/euicc-go/lpa"
	sgp22 "github.com/damonto/euicc-go/v2"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/damonto/sigmo/internal/app/forwarder"
	esimhandler "github.com/damonto/sigmo/internal/app/handler/esim"
	euicchandler "github.com/damonto/sigmo/internal/app/handler/euicc"
	internethandler "github.com/damonto/sigmo/internal/app/handler/internet"
	messagehandler "github.com/damonto/sigmo/internal/app/handler/message"
	modemhandler "github.com/damonto/sigmo/internal/app/handler/modem"
	networkhandler "github.com/damonto/sigmo/internal/app/handler/network"
	ussdhandler "github.com/damonto/sigmo/internal/app/handler/ussd"
	"github.com/damonto/sigmo/internal/app/mcpauth"
	"github.com/damonto/sigmo/internal/app/modemstatus"
	internetcore "github.com/damonto/sigmo/internal/pkg/internet"
	messagecore "github.com/damonto/sigmo/internal/pkg/message"
	modemcore "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/reminder"
	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
	ussdcore "github.com/damonto/sigmo/internal/pkg/ussd"
)

type CoreToolsConfig struct {
	Store              *settings.Store
	Registry           *modemcore.Registry
	Internet           *internetcore.Connector
	Relay              *forwarder.Relay
	NetworkPreferences *modemcore.NetworkPreferences
	Storage            *storage.Store
	Reminders          *reminder.Scheduler
	MessageRoute       messagecore.Route
	USSDRoute          ussdcore.Route
	ModemOverview      []modemstatus.Extension
}

type coreTools struct {
	registry *modemcore.Registry
	modems   *modemhandler.Handler
	network  *networkhandler.Handler
	euicc    *euicchandler.Handler
	esim     *esimhandler.Handler
	internet *internetcore.Connector
	messages *messagecore.Messenger
	ussd     *ussdcore.Executor
}

func RegisterCoreTools(catalog *Catalog, cfg CoreToolsConfig) error {
	if catalog == nil || cfg.Store == nil || cfg.Registry == nil || cfg.Internet == nil || cfg.Storage == nil {
		return errors.New("MCP core tool dependencies are required")
	}
	networks, err := networkhandler.New(cfg.Registry, cfg.NetworkPreferences, cfg.Storage)
	if err != nil {
		return fmt.Errorf("configure MCP network tools: %w", err)
	}
	tools := &coreTools{
		registry: cfg.Registry,
		modems:   modemhandler.New(cfg.Store, cfg.Registry, cfg.Internet, cfg.Reminders, cfg.ModemOverview...),
		network:  networks,
		euicc:    euicchandler.New(cfg.Store, cfg.Registry),
		esim: esimhandler.New(esimhandler.Config{
			Store: cfg.Store, Registry: cfg.Registry, Internet: cfg.Internet, Reminders: cfg.Reminders,
		}),
		internet: cfg.Internet,
		messages: messagecore.New(cfg.Storage, cfg.MessageRoute),
		ussd:     ussdcore.New(cfg.USSDRoute),
	}
	for _, permission := range corePermissions() {
		if err := catalog.AddPermission(permission.Name, permission.Module); err != nil {
			return err
		}
	}
	return tools.register(catalog)
}

func corePermissions() []Permission {
	return []Permission{
		{Name: "modem.read", Module: "modem"},
		{Name: "sim.read", Module: "sim"},
		{Name: "sim.switch", Module: "sim"},
		{Name: "sim.update", Module: "sim"},
		{Name: "esim.read", Module: "esim"},
		{Name: "esim.download", Module: "esim"},
		{Name: "esim.manage", Module: "esim"},
		{Name: "esim.delete", Module: "esim"},
		{Name: "sms.read", Module: "sms"},
		{Name: "sms.send", Module: "sms"},
		{Name: "sms.delete", Module: "sms"},
		{Name: "ussd.execute", Module: "ussd"},
		{Name: "network.read", Module: "network"},
		{Name: "network.register", Module: "network"},
		{Name: "network.power", Module: "network"},
		{Name: "internet.read", Module: "internet"},
		{Name: "internet.connect", Module: "internet"},
		{Name: "internet.configure", Module: "internet"},
	}
}

type modemInput struct {
	ModemID string `json:"modemId" jsonschema:"exact modem IMEI returned by list_authorized_modems"`
}

type successOutput struct {
	Success bool `json:"success" jsonschema:"whether Sigmo completed the requested operation"`
}

func (t *coreTools) register(c *Catalog) error {
	registrations := []func() error{
		func() error {
			return AddTool(c, "", ReadTool("list_authorized_modems", "List authorized modems"), nil, t.listAuthorizedModems)
		},
		func() error {
			return AddTool(c, "modem.read", ReadTool("get_modem_status", "Get modem status"), modemID, t.getModemStatus)
		},
		func() error {
			return AddTool(c, "sim.read", ReadTool("list_sim_cards", "List SIM cards"), modemID, t.listSIMCards)
		},
		func() error {
			return AddTool(c, "sim.read", ReadTool("list_secure_elements", "List secure elements"), modemID, t.listSecureElements)
		},
		func() error {
			return AddGuardedTool(c, "sim.switch", WriteTool("switch_sim_slot", "Switch SIM slot", true, false), simSlotModemIDs, t.switchSIMPolicy(), t.switchSIMSlot)
		},
		func() error {
			return AddTool(c, "sim.update", WriteTool("update_msisdn", "Update locally stored SIM phone number", false, false), updateMSISDNModemIDs, t.updateMSISDN)
		},
		func() error {
			return AddTool(c, "esim.read", ReadTool("list_esim_profiles", "List eSIM profiles"), modemID, t.listESIMProfiles)
		},
		func() error {
			return AddTool(c, "esim.read", ReadOpenWorldTool("discover_esim_profiles", "Discover eSIM profiles"), esimSEModemIDs, t.discoverESIMProfiles)
		},
		func() error {
			return AddTool(c, "esim.download", WriteTool("download_esim_profile", "Download eSIM profile", false, true), downloadESIMModemIDs, t.downloadESIMProfile)
		},
		func() error {
			return AddGuardedTool(c, "esim.manage", WriteTool("enable_esim_profile", "Enable eSIM profile", true, false), esimProfileModemIDs, t.enableESIMPolicy(), t.enableESIMProfile)
		},
		func() error {
			return AddTool(c, "esim.manage", WriteTool("rename_esim_profile", "Rename eSIM profile", true, false), renameESIMModemIDs, t.renameESIMProfile)
		},
		func() error {
			return AddGuardedTool(c, "esim.delete", WriteTool("delete_esim_profile", "Delete eSIM profile", true, false), esimProfileModemIDs, t.deleteESIMPolicy(), t.deleteESIMProfile)
		},
		func() error {
			return AddTool(c, "sms.read", ReadTool("list_sms_conversations", "List SMS conversations"), smsSearchModemIDs, t.listSMSConversations)
		},
		func() error {
			return AddTool(c, "sms.read", ReadTool("list_sms_messages", "List SMS messages"), smsParticipantModemIDs, t.listSMSMessages)
		},
		func() error {
			return AddGuardedTool(c, "sms.send", WriteTool("send_sms", "Send SMS", false, true), sendSMSModemIDs, t.sendSMSPolicy(), t.sendSMS)
		},
		func() error {
			return AddGuardedTool(c, "sms.delete", WriteTool("delete_sms_conversation", "Delete SMS conversation", true, false), smsParticipantModemIDs, t.deleteSMSPolicy(), t.deleteSMSConversation)
		},
		func() error {
			return AddGuardedTool(c, "ussd.execute", WriteTool("execute_ussd", "Execute USSD", false, true), ussdModemIDs, t.ussdPolicy(), t.executeUSSD)
		},
		func() error {
			return AddTool(c, "network.read", ReadOpenWorldTool("list_networks", "Scan mobile networks"), modemID, t.listNetworks)
		},
		func() error {
			return AddTool(c, "network.read", ReadTool("get_network_modes", "Get network modes"), modemID, t.getNetworkModes)
		},
		func() error {
			return AddTool(c, "network.read", ReadTool("get_network_bands", "Get network bands"), modemID, t.getNetworkBands)
		},
		func() error {
			return AddTool(c, "network.read", ReadTool("get_airplane_mode", "Get airplane mode"), modemID, t.getAirplaneMode)
		},
		func() error {
			return AddGuardedTool(c, "network.register", WriteTool("register_network", "Register mobile network", false, true), registerNetworkModemIDs, t.registerNetworkPolicy(), t.registerNetwork)
		},
		func() error {
			return AddGuardedTool(c, "network.power", WriteTool("set_airplane_mode", "Set airplane mode", true, true), airplaneModeModemIDs, t.airplaneModePolicy(), t.setAirplaneMode)
		},
		func() error {
			return AddTool(c, "internet.read", ReadTool("get_internet_connection", "Get Internet connection"), modemID, t.getInternetConnection)
		},
		func() error {
			return AddTool(c, "internet.read", ReadOpenWorldTool("get_public_ip", "Get public IP"), modemID, t.getPublicIP)
		},
		func() error {
			return AddGuardedTool(c, "internet.connect", WriteTool("connect_internet", "Connect mobile Internet", false, true), connectInternetModemIDs, t.connectInternetPolicy(), t.connectInternet)
		},
		func() error {
			return AddGuardedTool(c, "internet.connect", WriteTool("disconnect_internet", "Disconnect mobile Internet", true, false), modemID, t.disconnectInternetPolicy(), t.disconnectInternet)
		},
		func() error {
			return AddGuardedTool(c, "internet.configure", WriteTool("set_internet_preferences", "Set Internet preferences", true, true), internetPreferencesModemIDs, t.internetPreferencesPolicy(), t.setInternetPreferences)
		},
	}
	for _, register := range registrations {
		if err := register(); err != nil {
			return err
		}
	}
	return nil
}

func ReadTool(name string, title string) *mcp.Tool {
	closed := false
	return &mcp.Tool{Name: name, Title: title, Description: title + " for an authorized Sigmo modem.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &closed}}
}

func ReadOpenWorldTool(name string, title string) *mcp.Tool {
	tool := ReadTool(name, title)
	open := true
	tool.Annotations.OpenWorldHint = &open
	return tool
}

func WriteTool(name string, title string, idempotent bool, openWorld bool) *mcp.Tool {
	destructive := true
	return &mcp.Tool{Name: name, Title: title, Description: title + " for an authorized Sigmo modem.", Annotations: &mcp.ToolAnnotations{DestructiveHint: &destructive, IdempotentHint: idempotent, OpenWorldHint: &openWorld}}
}

func modemID(input modemInput) []string { return []string{input.ModemID} }

func (t *coreTools) validateModem(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return NewToolError("invalid_request", "modemId is required", nil)
	}
	if _, err := t.registry.Find(ctx, id); err != nil {
		return OperationError("find modem", err)
	}
	return nil
}

type authorizedModem struct {
	ID   string `json:"id" jsonschema:"modem IMEI; use this exact value as modemId in other tools"`
	Name string `json:"name" jsonschema:"configured modem alias, or the modem model when no alias is set"`
}

type modemsOutput struct {
	Modems []authorizedModem `json:"modems" jsonschema:"currently available modems within this API key grant"`
}

type modemStatus struct {
	ID               string `json:"id" jsonschema:"modem IMEI"`
	Name             string `json:"name" jsonschema:"configured modem alias, or the modem model when no alias is set"`
	Manufacturer     string `json:"manufacturer" jsonschema:"modem manufacturer reported by the device"`
	FirmwareRevision string `json:"firmwareRevision" jsonschema:"firmware revision reported by the modem"`
	HardwareRevision string `json:"hardwareRevision" jsonschema:"hardware revision reported by the modem"`
	State            string `json:"state" jsonschema:"current modem lifecycle state such as locked, registered, connected, or disabled"`
	UnlockRequired   string `json:"unlockRequired" jsonschema:"lock type currently preventing modem use; none when no unlock is required"`
	UnlockSupported  bool   `json:"unlockSupported" jsonschema:"whether Sigmo can unlock the current SIM PIN lock"`
}

type modemOutput struct {
	Modem modemStatus `json:"modem" jsonschema:"current device status of the requested modem"`
}

func (t *coreTools) listAuthorizedModems(ctx context.Context, _ *mcp.CallToolRequest, grant mcpauth.Grant, _ struct{}) (modemsOutput, error) {
	values, err := t.modems.ListModems(ctx)
	if err != nil {
		return modemsOutput{}, OperationError("list modems", err)
	}
	filtered := make([]authorizedModem, 0, len(values))
	for _, value := range values {
		if grant.AllowsModem(value.ID) {
			filtered = append(filtered, authorizedModem{ID: value.ID, Name: value.Name})
		}
	}
	return modemsOutput{Modems: filtered}, nil
}

func (t *coreTools) getModemStatus(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input modemInput) (modemOutput, error) {
	value, err := t.modems.Modem(ctx, input.ModemID)
	if err != nil {
		return modemOutput{}, OperationError("get modem status", err)
	}
	return modemOutput{Modem: modemStatus{
		ID:               value.ID,
		Name:             value.Name,
		Manufacturer:     value.Manufacturer,
		FirmwareRevision: value.FirmwareRevision,
		HardwareRevision: value.HardwareRevision,
		State:            value.State,
		UnlockRequired:   value.UnlockRequired,
		UnlockSupported:  value.UnlockSupported,
	}}, nil
}

type simCardsOutput struct {
	SIMs []modemhandler.SlotResponse `json:"sims" jsonschema:"SIM cards reported by the modem"`
}

func (t *coreTools) listSIMCards(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input modemInput) (simCardsOutput, error) {
	value, err := t.modems.Modem(ctx, input.ModemID)
	if err != nil {
		return simCardsOutput{}, OperationError("list SIM cards", err)
	}
	sims := value.Slots
	if len(sims) == 0 && value.SIM.Identifier != "" {
		sims = []modemhandler.SlotResponse{value.SIM}
	} else if sims == nil {
		sims = []modemhandler.SlotResponse{}
	}
	return simCardsOutput{SIMs: sims}, nil
}

func (t *coreTools) listSecureElements(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input modemInput) (*euicchandler.SEsResponse, error) {
	value, err := t.euicc.SecureElements(ctx, input.ModemID)
	if err != nil {
		return nil, OperationError("list secure elements", err)
	}
	return value, nil
}

type simSlotInput struct {
	ModemID    string `json:"modemId"`
	Identifier string `json:"identifier"`
}

func simSlotModemIDs(input simSlotInput) []string { return []string{input.ModemID} }
func (t *coreTools) switchSIMPolicy() GuardedToolPolicy[simSlotInput] {
	return GuardedToolPolicy[simSlotInput]{
		Validate: func(ctx context.Context, input simSlotInput) error {
			if strings.TrimSpace(input.Identifier) == "" {
				return NewToolError("invalid_request", "SIM identifier is required", nil)
			}
			return t.validateModem(ctx, input.ModemID)
		},
		Confirmation: func(input simSlotInput) string {
			return fmt.Sprintf("Switch modem %q to SIM %q? This can interrupt mobile network, SMS, and IMS services.", input.ModemID, input.Identifier)
		},
	}
}
func (t *coreTools) switchSIMSlot(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input simSlotInput) (successOutput, error) {
	if err := t.modems.SwitchSIM(ctx, input.ModemID, input.Identifier); err != nil {
		return successOutput{}, OperationError("switch SIM slot", err)
	}
	return successOutput{Success: true}, nil
}

type updateMSISDNInput struct {
	ModemID string `json:"modemId"`
	Number  string `json:"number"`
}

func updateMSISDNModemIDs(input updateMSISDNInput) []string { return []string{input.ModemID} }
func (t *coreTools) updateMSISDN(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input updateMSISDNInput) (successOutput, error) {
	if err := t.modems.UpdateNumber(ctx, input.ModemID, input.Number); err != nil {
		return successOutput{}, OperationError("update SIM phone number", err)
	}
	return successOutput{Success: true}, nil
}

func (t *coreTools) listESIMProfiles(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input modemInput) (*esimhandler.ProfilesResponse, error) {
	value, err := t.esim.Profiles(ctx, input.ModemID)
	if err != nil {
		return nil, OperationError("list eSIM profiles", err)
	}
	return value, nil
}

type esimSEInput struct {
	ModemID string `json:"modemId"`
	SEID    string `json:"seId"`
}

func esimSEModemIDs(input esimSEInput) []string { return []string{input.ModemID} }

type discoveriesOutput struct {
	Profiles []esimhandler.DiscoverResponse `json:"profiles" jsonschema:"profiles advertised by the eSIM discovery service"`
}

func (t *coreTools) discoverESIMProfiles(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input esimSEInput) (discoveriesOutput, error) {
	values, err := t.esim.DiscoverProfiles(ctx, input.ModemID, input.SEID)
	if err != nil {
		return discoveriesOutput{}, OperationError("discover eSIM profiles", err)
	}
	return discoveriesOutput{Profiles: append([]esimhandler.DiscoverResponse{}, values...)}, nil
}

type downloadESIMInput struct {
	ModemID    string `json:"modemId"`
	SEID       string `json:"seId"`
	SMDP       string `json:"smdp"`
	MatchingID string `json:"matchingId,omitempty"`
}

func downloadESIMModemIDs(input downloadESIMInput) []string { return []string{input.ModemID} }
func (t *coreTools) downloadESIMProfile(ctx context.Context, req *mcp.CallToolRequest, _ mcpauth.Grant, input downloadESIMInput) (successOutput, error) {
	activationCode, err := t.esim.ActivationCode(ctx, input.ModemID, input.SMDP, input.MatchingID)
	if err != nil {
		return successOutput{}, OperationError("prepare eSIM download", err)
	}
	var interactionErr error
	opts := &elpa.DownloadOptions{
		OnConfirm: func(info *sgp22.ProfileInfo) bool {
			message := fmt.Sprintf("Install eSIM profile %q from %q with ICCID %s?", info.ProfileName, info.ServiceProviderName, info.ICCID.String())
			result, elicitErr := req.Session.Elicit(ctx, &mcp.ElicitParams{Message: message, RequestedSchema: booleanSchema("accept", "Confirm profile installation")})
			if elicitErr != nil {
				interactionErr = NewToolError("interaction_required", "the MCP client must support form elicitation for eSIM download", elicitErr)
				return false
			}
			if result == nil {
				interactionErr = NewToolError("interaction_required", "the MCP client returned no eSIM download confirmation", nil)
				return false
			}
			accepted, _ := result.Content["accept"].(bool)
			if result.Action != "accept" || !accepted {
				interactionErr = NewToolError("cancelled", "eSIM download was not confirmed", nil)
				return false
			}
			return true
		},
		OnEnterConfirmationCode: func() string {
			result, elicitErr := req.Session.Elicit(ctx, &mcp.ElicitParams{Message: "Enter the eSIM confirmation code.", RequestedSchema: stringSchema("code", "Confirmation code")})
			if elicitErr != nil {
				interactionErr = NewToolError("interaction_required", "the MCP client must support form elicitation for confirmation codes", elicitErr)
				return ""
			}
			if result == nil {
				interactionErr = NewToolError("interaction_required", "the MCP client returned no confirmation code response", nil)
				return ""
			}
			if result.Action != "accept" {
				interactionErr = NewToolError("cancelled", "confirmation code entry was cancelled", nil)
				return ""
			}
			code, _ := result.Content["code"].(string)
			code = strings.TrimSpace(code)
			if code == "" {
				interactionErr = NewToolError("interaction_required", "a non-empty eSIM confirmation code is required", nil)
			}
			return code
		},
	}
	downloadErr := t.esim.DownloadProfile(ctx, input.ModemID, input.SEID, activationCode, opts)
	if interactionErr != nil {
		return successOutput{}, interactionErr
	}
	if downloadErr != nil {
		return successOutput{}, OperationError("download eSIM profile", downloadErr)
	}
	return successOutput{Success: true}, nil
}

func booleanSchema(name string, description string) map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{name: map[string]any{"type": "boolean", "description": description}}, "required": []string{name}}
}
func stringSchema(name string, description string) map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{name: map[string]any{"type": "string", "description": description, "minLength": 1}}, "required": []string{name}}
}

type esimProfileInput struct {
	ModemID string `json:"modemId"`
	SEID    string `json:"seId"`
	ICCID   string `json:"iccid"`
}

func esimProfileModemIDs(input esimProfileInput) []string { return []string{input.ModemID} }
func (t *coreTools) validateESIMProfile(ctx context.Context, input esimProfileInput) error {
	if strings.TrimSpace(input.SEID) == "" {
		return NewToolError("invalid_request", "seId is required", nil)
	}
	if _, err := sgp22.NewICCID(strings.TrimSpace(input.ICCID)); err != nil {
		return NewToolError("invalid_request", "a valid ICCID is required", err)
	}
	return t.validateModem(ctx, input.ModemID)
}
func (t *coreTools) enableESIMPolicy() GuardedToolPolicy[esimProfileInput] {
	return GuardedToolPolicy[esimProfileInput]{
		Validate: t.validateESIMProfile,
		Confirmation: func(input esimProfileInput) string {
			return fmt.Sprintf("Enable eSIM profile %q on modem %q? This can interrupt the current mobile connection.", input.ICCID, input.ModemID)
		},
	}
}
func (t *coreTools) enableESIMProfile(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input esimProfileInput) (successOutput, error) {
	if err := t.esim.ActivateProfile(ctx, input.ModemID, input.SEID, input.ICCID); err != nil {
		return successOutput{}, OperationError("enable eSIM profile", err)
	}
	return successOutput{Success: true}, nil
}
func (t *coreTools) deleteESIMPolicy() GuardedToolPolicy[esimProfileInput] {
	return GuardedToolPolicy[esimProfileInput]{
		Validate: t.validateESIMProfile,
		Confirmation: func(input esimProfileInput) string {
			return fmt.Sprintf("Permanently delete eSIM profile %q from modem %q? This cannot be undone.", input.ICCID, input.ModemID)
		},
	}
}
func (t *coreTools) deleteESIMProfile(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input esimProfileInput) (successOutput, error) {
	if err := t.esim.RemoveProfile(ctx, input.ModemID, input.SEID, input.ICCID); err != nil {
		return successOutput{}, OperationError("delete eSIM profile", err)
	}
	return successOutput{Success: true}, nil
}

type renameESIMInput struct {
	ModemID  string `json:"modemId"`
	SEID     string `json:"seId"`
	ICCID    string `json:"iccid"`
	Nickname string `json:"nickname"`
}

func renameESIMModemIDs(input renameESIMInput) []string { return []string{input.ModemID} }
func (t *coreTools) renameESIMProfile(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input renameESIMInput) (successOutput, error) {
	if err := t.esim.RenameProfile(ctx, input.ModemID, input.SEID, input.ICCID, input.Nickname); err != nil {
		return successOutput{}, OperationError("rename eSIM profile", err)
	}
	return successOutput{Success: true}, nil
}

type smsSearchInput struct {
	ModemID string `json:"modemId"`
	Query   string `json:"query,omitempty"`
}

func smsSearchModemIDs(input smsSearchInput) []string { return []string{input.ModemID} }

type smsParticipantInput struct {
	ModemID     string `json:"modemId"`
	Participant string `json:"participant"`
}

func smsParticipantModemIDs(input smsParticipantInput) []string { return []string{input.ModemID} }

type messagesOutput struct {
	Messages []messagehandler.MessageResponse `json:"messages" jsonschema:"SMS messages matching the request"`
}

func (t *coreTools) listSMSConversations(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input smsSearchInput) (messagesOutput, error) {
	device, err := t.registry.Find(ctx, input.ModemID)
	if err != nil {
		return messagesOutput{}, OperationError("find modem", err)
	}
	values, err := t.messages.ListConversations(ctx, device, input.Query)
	if err != nil {
		return messagesOutput{}, OperationError("list SMS conversations", err)
	}
	return messagesOutput{Messages: messagehandler.ResponsesFromMessages(values)}, nil
}
func (t *coreTools) listSMSMessages(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input smsParticipantInput) (messagesOutput, error) {
	device, err := t.registry.Find(ctx, input.ModemID)
	if err != nil {
		return messagesOutput{}, OperationError("find modem", err)
	}
	values, err := t.messages.ListByParticipant(ctx, device, input.Participant)
	if err != nil {
		return messagesOutput{}, OperationError("list SMS messages", err)
	}
	return messagesOutput{Messages: messagehandler.ResponsesFromMessages(values)}, nil
}

type sendSMSInput struct {
	ModemID        string `json:"modemId"`
	To             string `json:"to"`
	Text           string `json:"text"`
	IdempotencyKey string `json:"idempotencyKey" jsonschema:"unique key for this intended SMS; reuse only when retrying the same send"`
}

func sendSMSModemIDs(input sendSMSInput) []string { return []string{input.ModemID} }

func (t *coreTools) sendSMSPolicy() GuardedToolPolicy[sendSMSInput] {
	return GuardedToolPolicy[sendSMSInput]{
		Validate: func(ctx context.Context, input sendSMSInput) error {
			if strings.TrimSpace(input.To) == "" {
				return NewToolError("invalid_request", "SMS recipient is required", nil)
			}
			if strings.TrimSpace(input.Text) == "" {
				return NewToolError("invalid_request", "SMS text is required", nil)
			}
			return t.validateModem(ctx, input.ModemID)
		},
		Confirmation: func(input sendSMSInput) string {
			return fmt.Sprintf("Send this SMS from modem %q to %q?\n\n%s", input.ModemID, input.To, input.Text)
		},
		RateLimit:      RateLimit{Requests: 10, Window: time.Minute},
		IdempotencyKey: func(input sendSMSInput) string { return input.IdempotencyKey },
	}
}
func (t *coreTools) sendSMS(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input sendSMSInput) (messagehandler.SendMessageResponse, error) {
	device, err := t.registry.Find(ctx, input.ModemID)
	if err != nil {
		return messagehandler.SendMessageResponse{}, OperationError("find modem", err)
	}
	to, err := t.messages.Send(ctx, device, input.To, input.Text)
	if err != nil {
		return messagehandler.SendMessageResponse{}, OperationError("send SMS", err)
	}
	return messagehandler.SendMessageResponse{To: to}, nil
}
func (t *coreTools) deleteSMSPolicy() GuardedToolPolicy[smsParticipantInput] {
	return GuardedToolPolicy[smsParticipantInput]{
		Validate: func(ctx context.Context, input smsParticipantInput) error {
			if strings.TrimSpace(input.Participant) == "" {
				return NewToolError("invalid_request", "SMS participant is required", nil)
			}
			return t.validateModem(ctx, input.ModemID)
		},
		Confirmation: func(input smsParticipantInput) string {
			return fmt.Sprintf("Permanently delete the SMS conversation with %q from modem %q? This cannot be undone.", input.Participant, input.ModemID)
		},
	}
}
func (t *coreTools) deleteSMSConversation(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input smsParticipantInput) (successOutput, error) {
	device, err := t.registry.Find(ctx, input.ModemID)
	if err != nil {
		return successOutput{}, OperationError("find modem", err)
	}
	if err := t.messages.DeleteByParticipant(ctx, device, input.Participant); err != nil {
		return successOutput{}, OperationError("delete SMS conversation", err)
	}
	return successOutput{Success: true}, nil
}

type ussdInput struct {
	ModemID        string `json:"modemId"`
	Action         string `json:"action" jsonschema:"initialize or reply"`
	Code           string `json:"code"`
	IdempotencyKey string `json:"idempotencyKey" jsonschema:"unique key for this intended USSD action; reuse only when retrying the same action"`
}

func ussdModemIDs(input ussdInput) []string { return []string{input.ModemID} }

func (t *coreTools) ussdPolicy() GuardedToolPolicy[ussdInput] {
	return GuardedToolPolicy[ussdInput]{
		Validate: func(ctx context.Context, input ussdInput) error {
			action := strings.TrimSpace(input.Action)
			if action != "initialize" && action != "reply" {
				return NewToolError("invalid_request", "USSD action must be initialize or reply", nil)
			}
			if strings.TrimSpace(input.Code) == "" {
				return NewToolError("invalid_request", "USSD code is required", nil)
			}
			return t.validateModem(ctx, input.ModemID)
		},
		Confirmation: func(input ussdInput) string {
			return fmt.Sprintf("Execute USSD action %q with code %q on modem %q? USSD can change carrier services or incur charges.", input.Action, input.Code, input.ModemID)
		},
		RateLimit:      RateLimit{Requests: 5, Window: time.Minute},
		IdempotencyKey: func(input ussdInput) string { return input.IdempotencyKey },
	}
}
func (t *coreTools) executeUSSD(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input ussdInput) (ussdhandler.ExecuteResponse, error) {
	device, err := t.registry.Find(ctx, input.ModemID)
	if err != nil {
		return ussdhandler.ExecuteResponse{}, OperationError("find modem", err)
	}
	requestCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	reply, err := t.ussd.Execute(requestCtx, device, input.Action, input.Code)
	if err != nil {
		return ussdhandler.ExecuteResponse{}, OperationError("execute USSD", err)
	}
	return ussdhandler.ExecuteResponse{Reply: reply}, nil
}

type networksOutput struct {
	Networks []networkhandler.NetworkResponse `json:"networks" jsonschema:"mobile networks visible to the modem"`
}

func (t *coreTools) listNetworks(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input modemInput) (networksOutput, error) {
	values, err := t.network.ListNetworks(ctx, input.ModemID)
	if err != nil {
		return networksOutput{}, OperationError("list networks", err)
	}
	return networksOutput{Networks: append([]networkhandler.NetworkResponse{}, values...)}, nil
}
func (t *coreTools) getNetworkModes(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input modemInput) (*networkhandler.ModesResponse, error) {
	value, err := t.network.NetworkModes(ctx, input.ModemID)
	if err != nil {
		return nil, OperationError("get network modes", err)
	}
	return value, nil
}
func (t *coreTools) getNetworkBands(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input modemInput) (*networkhandler.BandsResponse, error) {
	value, err := t.network.NetworkBands(ctx, input.ModemID)
	if err != nil {
		return nil, OperationError("get network bands", err)
	}
	return value, nil
}
func (t *coreTools) getAirplaneMode(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input modemInput) (*networkhandler.AirplaneModeResponse, error) {
	value, err := t.network.AirplaneModeValue(ctx, input.ModemID)
	if err != nil {
		return nil, OperationError("get airplane mode", err)
	}
	return value, nil
}

type registerNetworkInput struct {
	ModemID      string `json:"modemId"`
	OperatorCode string `json:"operatorCode"`
}

func registerNetworkModemIDs(input registerNetworkInput) []string { return []string{input.ModemID} }
func (t *coreTools) registerNetworkPolicy() GuardedToolPolicy[registerNetworkInput] {
	return GuardedToolPolicy[registerNetworkInput]{
		Validate: func(ctx context.Context, input registerNetworkInput) error {
			if strings.TrimSpace(input.OperatorCode) == "" {
				return NewToolError("invalid_request", "operatorCode is required", nil)
			}
			return t.validateModem(ctx, input.ModemID)
		},
		Confirmation: func(input registerNetworkInput) string {
			return fmt.Sprintf("Register modem %q with operator %q? This can interrupt mobile service.", input.ModemID, input.OperatorCode)
		},
	}
}
func (t *coreTools) registerNetwork(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input registerNetworkInput) (successOutput, error) {
	if err := t.network.RegisterNetwork(ctx, input.ModemID, input.OperatorCode); err != nil {
		return successOutput{}, OperationError("register network", err)
	}
	return successOutput{Success: true}, nil
}

type airplaneModeInput struct {
	ModemID string `json:"modemId"`
	Enabled bool   `json:"enabled"`
}

func airplaneModeModemIDs(input airplaneModeInput) []string { return []string{input.ModemID} }
func (t *coreTools) airplaneModePolicy() GuardedToolPolicy[airplaneModeInput] {
	return GuardedToolPolicy[airplaneModeInput]{
		Validate: func(ctx context.Context, input airplaneModeInput) error {
			return t.validateModem(ctx, input.ModemID)
		},
		Confirmation: func(input airplaneModeInput) string {
			return fmt.Sprintf("Set airplane mode on modem %q to %t? This can interrupt mobile network, SMS, and IMS services.", input.ModemID, input.Enabled)
		},
	}
}
func (t *coreTools) setAirplaneMode(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input airplaneModeInput) (successOutput, error) {
	if err := t.network.UpdateAirplaneMode(ctx, input.ModemID, input.Enabled); err != nil {
		return successOutput{}, OperationError("set airplane mode", err)
	}
	return successOutput{Success: true}, nil
}

func safeConnection(value *internetcore.Connection) *internethandler.ConnectionResponse {
	response := internethandler.ResponseFromConnection(value)
	response.APNPassword = ""
	response.Proxy.Password = ""
	return &response
}
func (t *coreTools) getInternetConnection(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input modemInput) (*internethandler.ConnectionResponse, error) {
	device, err := t.registry.Find(ctx, input.ModemID)
	if err != nil {
		return nil, OperationError("find modem", err)
	}
	value, err := t.internet.Current(ctx, device)
	if err != nil {
		return nil, OperationError("get Internet connection", err)
	}
	return safeConnection(value), nil
}

func (t *coreTools) getPublicIP(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input modemInput) (internethandler.PublicResponse, error) {
	device, err := t.registry.Find(ctx, input.ModemID)
	if err != nil {
		return internethandler.PublicResponse{}, OperationError("find modem", err)
	}
	value, err := t.internet.Public(ctx, device)
	if err != nil {
		return internethandler.PublicResponse{}, OperationError("get public IP", err)
	}
	return internethandler.ResponseFromPublic(value), nil
}

type connectInternetInput struct {
	ModemID      string `json:"modemId"`
	APN          string `json:"apn,omitempty"`
	IPType       string `json:"ipType,omitempty"`
	APNUsername  string `json:"apnUsername,omitempty"`
	APNPassword  string `json:"apnPassword,omitempty"`
	APNAuth      string `json:"apnAuth,omitempty"`
	DefaultRoute bool   `json:"defaultRoute"`
	ProxyEnabled bool   `json:"proxyEnabled"`
	AlwaysOn     bool   `json:"alwaysOn"`
}

func connectInternetModemIDs(input connectInternetInput) []string { return []string{input.ModemID} }
func connectPreferences(input connectInternetInput) internetcore.Preferences {
	return internetcore.Preferences{
		APN: input.APN, IPType: input.IPType, APNUsername: input.APNUsername, APNPassword: input.APNPassword,
		APNAuth: input.APNAuth, DefaultRoute: input.DefaultRoute, ProxyEnabled: input.ProxyEnabled, AlwaysOn: input.AlwaysOn,
	}
}
func (t *coreTools) connectInternetPolicy() GuardedToolPolicy[connectInternetInput] {
	return GuardedToolPolicy[connectInternetInput]{
		Validate: func(ctx context.Context, input connectInternetInput) error {
			if err := internetcore.ValidatePreferences(connectPreferences(input)); err != nil {
				return NewToolError("invalid_request", "IP type or APN authentication method is unsupported", err)
			}
			return t.validateModem(ctx, input.ModemID)
		},
		Confirmation: func(input connectInternetInput) string {
			return fmt.Sprintf("Connect modem %q to the Internet using APN %q (default route: %t, proxy: %t)?", input.ModemID, input.APN, input.DefaultRoute, input.ProxyEnabled)
		},
	}
}
func (t *coreTools) connectInternet(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input connectInternetInput) (*internethandler.ConnectionResponse, error) {
	device, err := t.registry.Find(ctx, input.ModemID)
	if err != nil {
		return nil, OperationError("find modem", err)
	}
	value, err := t.internet.Connect(ctx, device, connectPreferences(input))
	if err != nil {
		return nil, OperationError("connect Internet", err)
	}
	return safeConnection(value), nil
}
func (t *coreTools) disconnectInternetPolicy() GuardedToolPolicy[modemInput] {
	return GuardedToolPolicy[modemInput]{
		Validate: func(ctx context.Context, input modemInput) error {
			return t.validateModem(ctx, input.ModemID)
		},
		Confirmation: func(input modemInput) string {
			return fmt.Sprintf("Disconnect modem %q from the Internet? This can interrupt network access.", input.ModemID)
		},
	}
}
func (t *coreTools) disconnectInternet(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input modemInput) (successOutput, error) {
	device, err := t.registry.Find(ctx, input.ModemID)
	if err != nil {
		return successOutput{}, OperationError("find modem", err)
	}
	if err := t.internet.Disconnect(ctx, device); err != nil {
		return successOutput{}, OperationError("disconnect Internet", err)
	}
	return successOutput{Success: true}, nil
}

type internetPreferencesInput struct {
	ModemID      string `json:"modemId"`
	DefaultRoute bool   `json:"defaultRoute"`
	ProxyEnabled bool   `json:"proxyEnabled"`
	AlwaysOn     bool   `json:"alwaysOn"`
}

func internetPreferencesModemIDs(input internetPreferencesInput) []string {
	return []string{input.ModemID}
}
func (t *coreTools) internetPreferencesPolicy() GuardedToolPolicy[internetPreferencesInput] {
	return GuardedToolPolicy[internetPreferencesInput]{
		Validate: func(ctx context.Context, input internetPreferencesInput) error {
			return t.validateModem(ctx, input.ModemID)
		},
		Confirmation: func(input internetPreferencesInput) string {
			return fmt.Sprintf("Update Internet preferences for modem %q (default route: %t, proxy: %t, always-on: %t)? This can affect host connectivity.", input.ModemID, input.DefaultRoute, input.ProxyEnabled, input.AlwaysOn)
		},
	}
}
func (t *coreTools) setInternetPreferences(ctx context.Context, _ *mcp.CallToolRequest, _ mcpauth.Grant, input internetPreferencesInput) (*internethandler.ConnectionResponse, error) {
	device, err := t.registry.Find(ctx, input.ModemID)
	if err != nil {
		return nil, OperationError("find modem", err)
	}
	value, err := t.internet.UpdatePreferences(ctx, device, internetcore.ConnectionPreferences{DefaultRoute: input.DefaultRoute, ProxyEnabled: input.ProxyEnabled, AlwaysOn: input.AlwaysOn})
	if err != nil {
		return nil, OperationError("set Internet preferences", err)
	}
	return safeConnection(value), nil
}

func OperationError(action string, err error) error {
	if errors.Is(err, modemcore.ErrNotFound) {
		return NewToolError("modem_not_found", "the requested modem is not available", err)
	}
	if errors.Is(err, context.Canceled) {
		return NewToolError("cancelled", action+" was cancelled", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return NewToolError("deadline_exceeded", action+" timed out", err)
	}
	slog.Error("MCP tool operation", "action", action, "error", err)
	return NewToolError("operation_failed", action+" could not be completed", err)
}
