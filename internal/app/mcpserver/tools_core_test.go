package mcpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/damonto/wwan-go/qcom"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	modemhandler "github.com/damonto/sigmo/internal/app/handler/modem"
	"github.com/damonto/sigmo/internal/app/mcpauth"
)

func TestCorePermissions(t *testing.T) {
	var got []string
	for _, permission := range corePermissions() {
		got = append(got, permission.Name)
	}
	want := []string{
		"modem.read",
		"sim.read", "sim.switch", "sim.update",
		"esim.read", "esim.download", "esim.manage", "esim.delete",
		"sms.read", "sms.send", "sms.delete", "ussd.execute",
		"network.read", "network.register", "network.power",
		"internet.read", "internet.connect", "internet.configure",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("corePermissions() = %v, want %v", got, want)
	}
}

func TestModemDiscoveryOutputExcludesOtherPermissionDomains(t *testing.T) {
	full := &modemhandler.ModemResponse{
		ID:                "123456789012345",
		Name:              "Office modem",
		Number:            "+12025550123",
		Slots:             []modemhandler.SlotResponse{{Identifier: "8901000000000000000"}},
		RegistrationState: "home",
	}
	full.WiFiCallingConnected = true

	outputs := []struct {
		name  string
		value any
	}{
		{name: "authorized modem", value: authorizedModem{ID: full.ID, Name: full.Name}},
		{name: "modem status", value: modemStatus{
			ID: full.ID, Name: full.Name, Manufacturer: full.Manufacturer,
			FirmwareRevision: full.FirmwareRevision, HardwareRevision: full.HardwareRevision,
			State: full.State, UnlockRequired: full.UnlockRequired, UnlockSupported: full.UnlockSupported,
		}},
	}
	for _, tt := range outputs {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := json.Marshal(tt.value)
			if err != nil {
				t.Fatalf("marshal output: %v", err)
			}
			for _, field := range []string{"number", "sim", "slots", "registrationState", "registeredOperator", "wifiCallingConnected"} {
				if strings.Contains(string(encoded), `"`+field+`"`) {
					t.Fatalf("output %s exposes %q", encoded, field)
				}
			}
		})
	}
}

func TestDownloadESIMInputDoesNotAcceptConfirmationCode(t *testing.T) {
	typ := reflect.TypeFor[downloadESIMInput]()
	for i := range typ.NumField() {
		field := typ.Field(i)
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "confirmationCode" {
			t.Fatal("download eSIM input exposes confirmationCode")
		}
	}
}

func TestToolAnnotations(t *testing.T) {
	read := ReadTool("read", "Read")
	if read.Annotations == nil || !read.Annotations.ReadOnlyHint || !read.Annotations.IdempotentHint || read.Annotations.OpenWorldHint == nil || *read.Annotations.OpenWorldHint {
		t.Fatalf("ReadTool() annotations = %+v", read.Annotations)
	}
	openRead := ReadOpenWorldTool("open-read", "Open read")
	if openRead.Annotations == nil || openRead.Annotations.OpenWorldHint == nil || !*openRead.Annotations.OpenWorldHint {
		t.Fatalf("ReadOpenWorldTool() annotations = %+v", openRead.Annotations)
	}
	write := WriteTool("write", "Write", true, false)
	if write.Annotations == nil || write.Annotations.ReadOnlyHint || write.Annotations.DestructiveHint == nil || !*write.Annotations.DestructiveHint || !write.Annotations.IdempotentHint {
		t.Fatalf("WriteTool() annotations = %+v", write.Annotations)
	}
}

func TestCoreToolSchemasRegister(t *testing.T) {
	catalog := NewCatalog()
	permissions := corePermissions()
	for _, permission := range permissions {
		if err := catalog.AddPermission(permission.Name, permission.Module); err != nil {
			t.Fatalf("AddPermission(%q) error = %v", permission.Name, err)
		}
	}
	if err := (&coreTools{}).register(catalog); err != nil {
		t.Fatalf("register() error = %v", err)
	}
	gotTools := make([]string, 0, len(catalog.tools))
	for _, tool := range catalog.tools {
		gotTools = append(gotTools, tool.name)
	}
	wantTools := []string{
		"get_network_status",
		"list_authorized_modems", "get_modem_status",
		"list_sim_cards", "list_secure_elements", "switch_sim_slot", "update_msisdn",
		"list_esim_profiles", "discover_esim_profiles", "download_esim_profile", "enable_esim_profile", "rename_esim_profile", "delete_esim_profile",
		"list_sms_conversations", "list_sms_messages", "send_sms", "delete_sms_conversation", "execute_ussd",
		"list_networks", "get_network_registration", "get_network_modes", "get_network_bands", "get_airplane_mode", "set_network_registration", "set_airplane_mode",
		"get_internet_connection", "get_public_ip", "connect_internet", "disconnect_internet", "set_internet_preferences",
	}
	if !slices.Equal(gotTools, wantTools) {
		t.Fatalf("core tools = %v, want %v", gotTools, wantTools)
	}
	grant := mcpauth.Grant{AllModems: true, Permissions: make([]string, 0, len(permissions))}
	for _, permission := range permissions {
		grant.Permissions = append(grant.Permissions, permission.Name)
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "schema-test", Version: "1"}, nil)
	catalog.registerTools(server, &execution{grant: grant})

	for _, name := range []string{
		"switch_sim_slot", "enable_esim_profile", "delete_esim_profile",
		"send_sms", "delete_sms_conversation", "execute_ussd",
		"set_network_registration", "set_airplane_mode", "connect_internet",
		"disconnect_internet", "set_internet_preferences",
	} {
		if !catalog.RequiresConfirmation(name) {
			t.Fatalf("tool %q does not require confirmation", name)
		}
	}
}

func TestCoreToolOutputSchemasDescribeEveryField(t *testing.T) {
	catalog := NewCatalog()
	for _, permission := range corePermissions() {
		if err := catalog.AddPermission(permission.Name, permission.Module); err != nil {
			t.Fatalf("AddPermission(%q) error = %v", permission.Name, err)
		}
	}
	if err := (&coreTools{}).register(catalog); err != nil {
		t.Fatalf("register() error = %v", err)
	}
	grant := mcpauth.Grant{AllModems: true, Permissions: make([]string, 0, len(corePermissions()))}
	for _, permission := range corePermissions() {
		grant.Permissions = append(grant.Permissions, permission.Name)
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "schema-test", Version: "1"}, nil)
	catalog.registerTools(server, &execution{grant: grant})
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect() error = %v", err)
	}
	defer func() { _ = serverSession.Close() }()
	client := mcp.NewClient(&mcp.Implementation{Name: "schema-client", Version: "1"}, nil)
	clientSession, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect() error = %v", err)
	}
	defer func() { _ = clientSession.Close() }()
	listed, err := clientSession.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	for _, tool := range listed.Tools {
		schema, ok := tool.OutputSchema.(map[string]any)
		if !ok {
			t.Fatalf("tool %q output schema type = %T, want object", tool.Name, tool.OutputSchema)
		}
		assertSchemaFieldsDescribed(t, schema, schema, tool.Name, make(map[string]bool))
	}
}

func assertSchemaFieldsDescribed(t *testing.T, root map[string]any, schema map[string]any, path string, seen map[string]bool) {
	t.Helper()
	resolved := resolveSchemaRef(root, schema)
	if ref, ok := schema["$ref"].(string); ok {
		if seen[ref] {
			return
		}
		seen[ref] = true
	}
	properties, ok := resolved["properties"].(map[string]any)
	if !ok {
		return
	}
	for name, raw := range properties {
		property, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("schema field %s.%s has invalid definition %T", path, name, raw)
		}
		field := resolveSchemaRef(root, property)
		description, _ := property["description"].(string)
		if strings.TrimSpace(description) == "" {
			description, _ = field["description"].(string)
		}
		if strings.TrimSpace(description) == "" {
			t.Errorf("schema field %s.%s has no description", path, name)
		}
		assertSchemaFieldsDescribed(t, root, field, path+"."+name, seen)
		if items, ok := field["items"].(map[string]any); ok {
			assertSchemaFieldsDescribed(t, root, items, path+"."+name+"[]", seen)
		}
	}
}

func resolveSchemaRef(root map[string]any, schema map[string]any) map[string]any {
	ref, ok := schema["$ref"].(string)
	if !ok || !strings.HasPrefix(ref, "#/$defs/") {
		return schema
	}
	definitions, ok := root["$defs"].(map[string]any)
	if !ok {
		return schema
	}
	definition, ok := definitions[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any)
	if !ok {
		return schema
	}
	return definition
}

func TestOperationErrorDistinguishesWDSFailures(t *testing.T) {
	tests := []struct {
		name     string
		cause    error
		wantCode string
	}{
		{name: "network rejects call", cause: qcom.QMIErrorCallFailed, wantCode: "network_rejected"},
		{name: "modem internal error", cause: qcom.QMIErrorInternal, wantCode: "operation_failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cause := &qcom.WDSStartNetworkError{Err: fmt.Errorf("private credential: %w", tt.cause)}
			got := OperationError("connect Internet", cause)
			if code := ErrorCode(got); code != tt.wantCode {
				t.Fatalf("code = %q, want %q", code, tt.wantCode)
			}
			if !errors.Is(got, tt.cause) {
				t.Fatalf("error chain lost %v: %v", tt.cause, got)
			}
			if strings.Contains(got.Error(), "private credential") {
				t.Fatalf("public message includes private backend text: %v", got)
			}
		})
	}
}
