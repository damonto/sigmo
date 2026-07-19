package netlink

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const fibRuleHeaderSize = 12

// PolicyRule selects a routing table for locally-created packets bound to an
// output interface.
type PolicyRule struct {
	Family          int
	Priority        uint32
	Table           uint32
	OutputInterface string
	Protocol        int
}

func AddPolicyRule(rule PolicyRule) error {
	msg, err := policyRuleMessage(rule)
	if err != nil {
		return err
	}
	if err := send(unix.RTM_NEWRULE, unix.NLM_F_REQUEST|unix.NLM_F_ACK|unix.NLM_F_CREATE|unix.NLM_F_EXCL, msg); err != nil {
		return fmt.Errorf("add policy rule: %w", err)
	}
	return nil
}

func DeletePolicyRule(rule PolicyRule) error {
	msg, err := policyRuleMessage(rule)
	if err != nil {
		return err
	}
	err = send(unix.RTM_DELRULE, unix.NLM_F_REQUEST|unix.NLM_F_ACK, msg)
	if errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ESRCH) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete policy rule: %w", err)
	}
	return nil
}

func PolicyRules() ([]PolicyRule, error) {
	var rules []PolicyRule
	for _, family := range []int{FamilyIPv4, FamilyIPv6} {
		messages, err := ruleDump(family)
		if err != nil {
			return nil, err
		}
		for _, msg := range messages {
			if msg.Header.Type != unix.RTM_NEWRULE {
				continue
			}
			rule, ok := parsePolicyRule(msg.Data)
			if ok && !policyRuleExists(rule, rules) {
				rules = append(rules, rule)
			}
		}
	}
	return rules, nil
}

func policyRuleMessage(rule PolicyRule) ([]byte, error) {
	if rule.Family != FamilyIPv4 && rule.Family != FamilyIPv6 {
		return nil, errors.New("policy rule family is invalid")
	}
	if rule.Priority == 0 {
		return nil, errors.New("policy rule priority is required")
	}
	if rule.Protocol < 0 || rule.Protocol > 255 {
		return nil, errors.New("policy rule protocol is invalid")
	}
	if err := validatePolicyRouteTable(rule.Table); err != nil {
		return nil, fmt.Errorf("policy rule: %w", err)
	}
	interfaceName := strings.TrimSpace(rule.OutputInterface)
	if interfaceName == "" {
		return nil, errors.New("policy rule output interface is required")
	}

	msg := make([]byte, fibRuleHeaderSize)
	msg[0] = byte(rule.Family)
	if rule.Table <= 255 {
		msg[4] = byte(rule.Table)
	} else {
		table := make([]byte, 4)
		binary.NativeEndian.PutUint32(table, rule.Table)
		msg = appendAttr(msg, unix.FRA_TABLE, table)
	}
	msg[7] = unix.FR_ACT_TO_TBL

	priority := make([]byte, 4)
	binary.NativeEndian.PutUint32(priority, rule.Priority)
	msg = appendAttr(msg, unix.FRA_PRIORITY, priority)
	msg = appendAttr(msg, unix.FRA_OIFNAME, append([]byte(interfaceName), 0))
	if rule.Protocol > 0 {
		msg = appendAttr(msg, unix.FRA_PROTOCOL, []byte{byte(rule.Protocol)})
	}
	return msg, nil
}

func parsePolicyRule(data []byte) (PolicyRule, bool) {
	if len(data) < fibRuleHeaderSize {
		return PolicyRule{}, false
	}
	family := int(data[0])
	if family != FamilyIPv4 && family != FamilyIPv6 {
		return PolicyRule{}, false
	}
	attrs := parseAttrs(data[fibRuleHeaderSize:])
	table := attrUint32(attrs[unix.FRA_TABLE])
	if table == 0 {
		table = uint32(data[4])
	}
	return PolicyRule{
		Family:          family,
		Priority:        attrUint32(attrs[unix.FRA_PRIORITY]),
		Table:           table,
		OutputInterface: strings.TrimRight(string(attrs[unix.FRA_OIFNAME]), "\x00"),
		Protocol:        attrUint8(attrs[unix.FRA_PROTOCOL]),
	}, true
}

func policyRuleExists(rule PolicyRule, rules []PolicyRule) bool {
	for _, existing := range rules {
		if existing == rule {
			return true
		}
	}
	return false
}

func ruleDump(family int) ([]syscall.NetlinkMessage, error) {
	data, err := syscall.NetlinkRIB(int(unix.RTM_GETRULE), family)
	if err != nil {
		return nil, fmt.Errorf("read netlink policy rule dump: %w", err)
	}
	messages, err := syscall.ParseNetlinkMessage(data)
	if err != nil {
		return nil, fmt.Errorf("parse netlink policy rule dump: %w", err)
	}
	return messages, nil
}
