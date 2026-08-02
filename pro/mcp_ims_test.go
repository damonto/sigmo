//go:build ims

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/damonto/sigmo/internal/app/mcpauth"
	"github.com/damonto/sigmo/internal/app/mcpserver"
	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	procall "github.com/damonto/sigmo/pro/call"
	pims "github.com/damonto/sigmo/pro/ims"
	wwanmodem "github.com/damonto/wwan-go/modem"
)

type mcpModemFinder struct {
	modem *mmodem.Modem
}

func (f mcpModemFinder) Find(context.Context, string) (*mmodem.Modem, error) {
	return f.modem, nil
}

type mcpConnectivityProbe struct {
	imsMCPConnectivity
	wifiUpdated   bool
	wifiSettings  pims.WiFiCallingSettings
	volteUpdated  bool
	volteSettings pims.VoLTESettings
}

func (p *mcpConnectivityProbe) ReplaceWiFiCallingSettings(_ context.Context, modem *mmodem.Modem, settings pims.WiFiCallingSettings) error {
	settings, err := pims.ResolveWiFiCallingSettings(modem, settings)
	if err != nil {
		return err
	}
	p.wifiUpdated = true
	p.wifiSettings = settings
	return nil
}

func (p *mcpConnectivityProbe) ReplaceVoLTESettings(_ context.Context, modem *mmodem.Modem, settings pims.VoLTESettings) error {
	settings, err := pims.ResolveVoLTESettings(modem, settings)
	if err != nil {
		return err
	}
	p.volteUpdated = true
	p.volteSettings = settings
	return nil
}

func TestRegisterIMSMCPCallPermissionsAreRecordOnly(t *testing.T) {
	catalog := mcpserver.NewCatalog()
	if err := registerIMSMCP(nil, nil, nil)(catalog); err != nil {
		t.Fatalf("registerIMSMCP() error = %v", err)
	}
	names := make([]string, 0)
	for _, permission := range catalog.Permissions() {
		if permission.Module == "calls" {
			names = append(names, permission.Name)
		}
	}
	if want := []string{"calls.delete", "calls.read"}; !slices.Equal(names, want) {
		t.Fatalf("call permissions = %v, want %v", names, want)
	}
	for _, name := range []string{"set_wifi_calling", "reconnect_wifi_calling", "disconnect_wifi_calling", "set_volte", "delete_call_record"} {
		if !catalog.RequiresConfirmation(name) {
			t.Fatalf("tool %q does not require confirmation", name)
		}
	}
}

func TestSetVoLTEUsesSharedValidation(t *testing.T) {
	tests := []struct {
		name         string
		portType     wwanmodem.PortType
		input        volteSettingsInput
		wantErr      bool
		wantSettings pims.VoLTESettings
	}{
		{name: "QMI requires data path", portType: wwanmodem.PortQMI, input: volteSettingsInput{ModemID: "modem-1"}, wantErr: true},
		{name: "QMI rejects unsupported data path", portType: wwanmodem.PortQMI, input: volteSettingsInput{ModemID: "modem-1", DataPath: "auto"}, wantErr: true},
		{
			name: "MBIM derives data path", portType: wwanmodem.PortMBIM,
			input:        volteSettingsInput{ModemID: "modem-1", DataPath: "legacy_bam_dmux"},
			wantSettings: pims.VoLTESettings{DataPath: pims.DataPathMBIM},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modem := &mmodem.Modem{EquipmentIdentifier: "modem-1", Ports: []mmodem.ModemPort{{Device: "cdc-wdm0", PortType: tt.portType}}}
			probe := &mcpConnectivityProbe{}
			tools := &imsMCPTools{registry: mcpModemFinder{modem: modem}, connectivity: probe}

			_, err := tools.setVoLTE(context.Background(), nil, mcpauth.Grant{}, tt.input)
			if tt.wantErr {
				if mcpserver.ErrorCode(err) != "invalid_request" {
					t.Fatalf("setVoLTE() error = %v, want invalid_request", err)
				}
				if probe.volteUpdated {
					t.Fatal("setVoLTE() updated settings after validation error")
				}
				return
			}
			if err != nil {
				t.Fatalf("setVoLTE() error = %v", err)
			}
			if !probe.volteUpdated || probe.volteSettings != tt.wantSettings {
				t.Fatalf("UpdateVoLTESettings = (%v, %+v), want (true, %+v)", probe.volteUpdated, probe.volteSettings, tt.wantSettings)
			}
		})
	}
}

func TestSetWiFiCallingUnderlay(t *testing.T) {
	tests := []struct {
		name         string
		input        wifiSettingsInput
		wantErr      bool
		wantSettings pims.WiFiCallingSettings
	}{
		{
			name:    "rejects omitted underlay",
			input:   wifiSettingsInput{ModemID: "modem-1"},
			wantErr: true,
		},
		{
			name:         "selects system network",
			input:        wifiSettingsInput{ModemID: "modem-1", Underlay: &pims.UnderlaySettings{Mode: pims.UnderlayModeSystem}},
			wantSettings: pims.WiFiCallingSettings{Underlay: pims.UnderlaySettings{Mode: pims.UnderlayModeSystem}},
		},
		{
			name:         "selects another modem",
			input:        wifiSettingsInput{ModemID: "modem-1", Enabled: true, Underlay: &pims.UnderlaySettings{Mode: pims.UnderlayModeModem, ModemID: "modem-2"}},
			wantSettings: pims.WiFiCallingSettings{Enabled: true, Underlay: pims.UnderlaySettings{Mode: pims.UnderlayModeModem, ModemID: "modem-2"}},
		},
		{
			name:    "rejects missing modem id",
			input:   wifiSettingsInput{ModemID: "modem-1", Underlay: &pims.UnderlaySettings{Mode: pims.UnderlayModeModem}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modem := &mmodem.Modem{EquipmentIdentifier: "modem-1"}
			probe := &mcpConnectivityProbe{}
			tools := &imsMCPTools{registry: mcpModemFinder{modem: modem}, connectivity: probe}

			_, err := tools.setWiFiCalling(context.Background(), nil, mcpauth.Grant{}, tt.input)
			if tt.wantErr {
				if mcpserver.ErrorCode(err) != "invalid_request" {
					t.Fatalf("setWiFiCalling() error = %v, want invalid_request", err)
				}
				if probe.wifiUpdated {
					t.Fatal("setWiFiCalling() updated settings after validation error")
				}
				return
			}
			if err != nil {
				t.Fatalf("setWiFiCalling() error = %v", err)
			}
			if !probe.wifiUpdated || probe.wifiSettings != tt.wantSettings {
				t.Fatalf("UpdateWiFiCallingSettings = (%v, %+v), want (true, %+v)", probe.wifiUpdated, probe.wifiSettings, tt.wantSettings)
			}
		})
	}
}

func TestWiFiSettingsModemIDs(t *testing.T) {
	tests := []struct {
		name  string
		input wifiSettingsInput
		want  []string
	}{
		{
			name:  "system uses target modem only",
			input: wifiSettingsInput{ModemID: "voice", Underlay: &pims.UnderlaySettings{Mode: pims.UnderlayModeSystem}},
			want:  []string{"voice"},
		},
		{
			name:  "modem mode authorizes traffic modem",
			input: wifiSettingsInput{ModemID: "voice", Underlay: &pims.UnderlaySettings{Mode: " MODEM ", ModemID: " data "}},
			want:  []string{"voice", "data"},
		},
		{
			name:  "invalid modem mode without id uses target only",
			input: wifiSettingsInput{ModemID: "voice", Underlay: &pims.UnderlaySettings{Mode: pims.UnderlayModeModem}},
			want:  []string{"voice"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := wifiSettingsModemIDs(tt.input); !slices.Equal(got, tt.want) {
				t.Fatalf("wifiSettingsModemIDs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIMSMCPOutputSchemasAndEmptyCalls(t *testing.T) {
	data, err := json.Marshal(callsOutput{Calls: []procall.CallResponse{}})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if !bytes.Contains(data, []byte(`"calls":[]`)) {
		t.Fatalf("empty calls JSON = %s, want calls array", data)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "ims-schema-test", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "get_wifi_calling"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, pims.WiFiCallingSettingsResponse, error) {
		return nil, pims.WiFiCallingSettingsResponse{}, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "get_volte"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, pims.VoLTESettingsResponse, error) {
		return nil, pims.VoLTESettingsResponse{}, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "list_calls"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, callsOutput, error) {
		return nil, callsOutput{}, nil
	})
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect() error = %v", err)
	}
	defer func() { _ = serverSession.Close() }()
	clientSession, err := mcp.NewClient(&mcp.Implementation{Name: "ims-schema-client", Version: "1"}, nil).Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect() error = %v", err)
	}
	defer func() { _ = clientSession.Close() }()
	listed, err := clientSession.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	for _, tool := range listed.Tools {
		schema, ok := tool.OutputSchema.(map[string]any)
		if !ok {
			t.Fatalf("tool %q output schema type = %T", tool.Name, tool.OutputSchema)
		}
		assertIMSSchemaDescriptions(t, schema, schema, tool.Name, make(map[string]bool))
	}
}

func assertIMSSchemaDescriptions(t *testing.T, root map[string]any, schema map[string]any, path string, seen map[string]bool) {
	t.Helper()
	if ref, ok := schema["$ref"].(string); ok {
		if seen[ref] {
			return
		}
		seen[ref] = true
		if defs, ok := root["$defs"].(map[string]any); ok {
			if resolved, ok := defs[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any); ok {
				schema = resolved
			}
		}
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		if items, ok := schema["items"].(map[string]any); ok {
			assertIMSSchemaDescriptions(t, root, items, path+"[]", seen)
		}
		return
	}
	for name, raw := range properties {
		property, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("schema field %s.%s has invalid definition %T", path, name, raw)
		}
		resolved := property
		if ref, ok := property["$ref"].(string); ok {
			if defs, ok := root["$defs"].(map[string]any); ok {
				if value, ok := defs[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any); ok {
					resolved = value
				}
			}
		}
		description, _ := property["description"].(string)
		if description == "" {
			description, _ = resolved["description"].(string)
		}
		if strings.TrimSpace(description) == "" {
			t.Errorf("schema field %s.%s has no description", path, name)
		}
		assertIMSSchemaDescriptions(t, root, resolved, path+"."+name, seen)
	}
}
