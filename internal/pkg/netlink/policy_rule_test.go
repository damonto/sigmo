package netlink

import (
	"encoding/binary"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPolicyRuleMessage(t *testing.T) {
	tests := []struct {
		name    string
		rule    PolicyRule
		wantErr bool
	}{
		{
			name: "extended table with output interface",
			rule: PolicyRule{Family: FamilyIPv6, Priority: 10_003, Table: 20_003, OutputInterface: "qmimux3", Protocol: 200},
		},
		{name: "rejects missing output interface", rule: PolicyRule{Family: FamilyIPv4, Priority: 10_000, Table: 20_000}, wantErr: true},
		{name: "rejects reserved table", rule: PolicyRule{Family: FamilyIPv4, Priority: 10_000, Table: unix.RT_TABLE_MAIN, OutputInterface: "qmimux0"}, wantErr: true},
		{name: "rejects invalid family", rule: PolicyRule{Priority: 10_000, Table: 20_000, OutputInterface: "qmimux0"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := policyRuleMessage(tt.rule)
			if tt.wantErr {
				if err == nil {
					t.Fatal("policyRuleMessage() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("policyRuleMessage() error = %v", err)
			}
			if msg[0] != byte(tt.rule.Family) || msg[4] != unix.RT_TABLE_UNSPEC || msg[7] != unix.FR_ACT_TO_TBL {
				t.Fatalf("rule header = family %d table %d action %d", msg[0], msg[4], msg[7])
			}
			attrs := parseAttrs(msg[fibRuleHeaderSize:])
			if got := attrUint32(attrs[unix.FRA_TABLE]); got != tt.rule.Table {
				t.Fatalf("rule table = %d, want %d", got, tt.rule.Table)
			}
			if got := attrUint32(attrs[unix.FRA_PRIORITY]); got != tt.rule.Priority {
				t.Fatalf("rule priority = %d, want %d", got, tt.rule.Priority)
			}
			if got := string(attrs[unix.FRA_OIFNAME]); got != tt.rule.OutputInterface+"\x00" {
				t.Fatalf("rule output interface = %q, want %q", got, tt.rule.OutputInterface)
			}
			if got := attrUint8(attrs[unix.FRA_PROTOCOL]); got != tt.rule.Protocol {
				t.Fatalf("rule protocol = %d, want %d", got, tt.rule.Protocol)
			}
		})
	}
}

func TestParsePolicyRule(t *testing.T) {
	tests := []struct {
		name   string
		rule   PolicyRule
		table  uint32
		attr   bool
		action byte
		wantOK bool
	}{
		{
			name:   "extended table",
			rule:   PolicyRule{Family: FamilyIPv4, Priority: 10_001, Table: 20_001, OutputInterface: "qmimux1", Protocol: 200},
			attr:   true,
			wantOK: true,
		},
		{
			name:   "header table",
			rule:   PolicyRule{Family: FamilyIPv6, Priority: 100, Table: 100, OutputInterface: "wwan0"},
			table:  100,
			wantOK: true,
		},
		{
			name:   "priority-only rule remains visible for collision checks",
			rule:   PolicyRule{Family: FamilyIPv4, Priority: 10_010},
			action: unix.FR_ACT_GOTO,
			wantOK: true,
		},
		{name: "rejects truncated header"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.wantOK {
				if _, ok := parsePolicyRule(nil); ok {
					t.Fatal("parsePolicyRule() ok = true, want false")
				}
				return
			}
			msg := make([]byte, fibRuleHeaderSize)
			msg[0] = byte(tt.rule.Family)
			msg[4] = byte(tt.table)
			msg[7] = unix.FR_ACT_TO_TBL
			if tt.action != 0 {
				msg[7] = tt.action
			}
			priority := make([]byte, 4)
			binary.NativeEndian.PutUint32(priority, tt.rule.Priority)
			msg = appendAttr(msg, unix.FRA_PRIORITY, priority)
			msg = appendAttr(msg, unix.FRA_OIFNAME, append([]byte(tt.rule.OutputInterface), 0))
			if tt.attr {
				table := make([]byte, 4)
				binary.NativeEndian.PutUint32(table, tt.rule.Table)
				msg = appendAttr(msg, unix.FRA_TABLE, table)
			}
			if tt.rule.Protocol != 0 {
				msg = appendAttr(msg, unix.FRA_PROTOCOL, []byte{byte(tt.rule.Protocol)})
			}

			got, ok := parsePolicyRule(msg)
			if !ok || got != tt.rule {
				t.Fatalf("parsePolicyRule() = %+v, %v; want %+v, true", got, ok, tt.rule)
			}
		})
	}
}
