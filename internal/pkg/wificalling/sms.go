//go:build wifi_calling

package wificalling

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
	vowifi "github.com/damonto/vowifi-go"
)

type smsReportKey struct {
	modemID     string
	profileID   string
	recipient   string
	tpReference byte
}

type smsReportTracker struct {
	profileID    string
	externalKey  string
	segmentCount int
	statuses     map[byte]string
	current      string
	pendingStore string
	expiresAt    time.Time
}

type pendingSMSReport struct {
	status    string
	expiresAt time.Time
}

const smsReportRetention = 6 * time.Hour

func (c *coordinator) SendSMS(ctx context.Context, modem *mmodem.Modem, to string, text string) (storage.Message, error) {
	profileID, err := modem.ProfileID(ctx)
	if err != nil {
		return storage.Message{}, err
	}
	client, err := c.connectedClient(modem.EquipmentIdentifier, profileID)
	if err != nil {
		return storage.Message{}, err
	}
	externalKey, err := newOutgoingMessageKey()
	if err != nil {
		return storage.Message{}, err
	}
	submission, err := client.SMS().Send(ctx, to, text)
	if err != nil {
		return storage.Message{}, err
	}
	msg := storage.Message{
		ModemID:     modem.EquipmentIdentifier,
		ProfileID:   profileID,
		Source:      storage.MessageSourceWiFiCalling,
		ExternalKey: externalKey,
		Sender:      modem.Number,
		Recipient:   strings.TrimSpace(to),
		Text:        text,
		Timestamp:   submission.SubmittedAt,
		Status:      "sent",
		Incoming:    false,
		WiFiCalling: true,
	}
	if status := c.trackOutgoingSMSReport(msg, submission); status != "" {
		msg.Status = status
	}
	return msg, nil
}

func (c *coordinator) ApplyPendingSMSStatus(ctx context.Context, msg storage.Message) error {
	if c.store == nil || msg.Source != storage.MessageSourceWiFiCalling {
		return nil
	}
	update, ok := c.pendingSMSStatus(msg)
	if !ok {
		return nil
	}
	updated, err := c.store.UpdateMessageStatus(ctx, storage.MessageStatusUpdate{
		ProfileID:   update.profileID,
		Source:      storage.MessageSourceWiFiCalling,
		ExternalKey: update.externalKey,
		Status:      update.status,
	})
	if err != nil {
		return err
	}
	if updated {
		c.completeStoredSMSStatus(update)
	}
	return nil
}

func (c *coordinator) trackOutgoingSMSReport(msg storage.Message, submission vowifi.SMSSubmission) string {
	if len(submission.Segments) == 0 {
		return ""
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()

	c.ensureSMSReportMaps()
	c.cleanupSMSReportsLocked(now)

	tracker := &smsReportTracker{
		profileID:    msg.ProfileID,
		externalKey:  msg.ExternalKey,
		segmentCount: len(submission.Segments),
		statuses:     make(map[byte]string, len(submission.Segments)),
		expiresAt:    now.Add(smsReportRetention),
	}
	for _, segment := range submission.Segments {
		key := outgoingSMSReportKey(msg.ModemID, msg.ProfileID, msg.Recipient, segment.TPReference)
		tracker.statuses[segment.TPReference] = ""
		c.smsReports[key] = tracker
		if pending, ok := c.pendingSMSReports[key]; ok {
			tracker.statuses[segment.TPReference] = pending.status
			delete(c.pendingSMSReports, key)
		}
	}
	tracker.current = tracker.aggregateStatus()
	if smsStatusFinal(tracker.current) {
		c.removeSMSReportTrackerLocked(tracker)
	}
	return tracker.current
}

func (c *coordinator) forwardSMSReport(ctx context.Context, modemID string, profileID string, report vowifi.SMSReport) {
	status := smsReportStatus(report)
	if status == "" {
		return
	}
	update, ok := c.recordSMSReport(modemID, profileID, report, status)
	if !ok || c.store == nil {
		return
	}
	if updated, err := c.store.UpdateMessageStatus(ctx, storage.MessageStatusUpdate{
		ProfileID:   update.profileID,
		Source:      storage.MessageSourceWiFiCalling,
		ExternalKey: update.externalKey,
		Status:      update.status,
	}); err != nil {
		slog.Warn("update Wi-Fi Calling SMS status", "modem", modemID, "recipient", report.Recipient, "status", update.status, "error", err)
	} else if !updated {
		c.deferStoredSMSStatus(update)
		slog.Debug("Wi-Fi Calling SMS status target not yet stored", "modem", modemID, "recipient", report.Recipient, "status", update.status)
	} else {
		c.completeStoredSMSStatus(update)
	}
}

type smsStatusUpdate struct {
	profileID   string
	externalKey string
	status      string
}

func (c *coordinator) recordSMSReport(modemID string, profileID string, report vowifi.SMSReport, status string) (smsStatusUpdate, bool) {
	now := time.Now()
	key := outgoingSMSReportKey(modemID, profileID, report.Recipient, report.MessageReference)

	c.mu.Lock()
	defer c.mu.Unlock()

	c.ensureSMSReportMaps()
	c.cleanupSMSReportsLocked(now)

	tracker := c.smsReports[key]
	if tracker == nil {
		c.pendingSMSReports[key] = pendingSMSReport{
			status:    status,
			expiresAt: now.Add(smsReportRetention),
		}
		return smsStatusUpdate{}, false
	}
	if _, ok := tracker.statuses[key.tpReference]; !ok {
		return smsStatusUpdate{}, false
	}
	tracker.statuses[key.tpReference] = status
	aggregate := tracker.aggregateStatus()
	if aggregate == "" || aggregate == tracker.current {
		return smsStatusUpdate{}, false
	}
	tracker.current = aggregate
	return smsStatusUpdate{
		profileID:   tracker.profileID,
		externalKey: tracker.externalKey,
		status:      aggregate,
	}, true
}

func (c *coordinator) deferStoredSMSStatus(update smsStatusUpdate) {
	c.mu.Lock()
	defer c.mu.Unlock()

	tracker := c.findSMSReportTrackerLocked(update.profileID, update.externalKey)
	if tracker != nil {
		tracker.pendingStore = update.status
	}
}

func (c *coordinator) pendingSMSStatus(msg storage.Message) (smsStatusUpdate, bool) {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()

	c.ensureSMSReportMaps()
	c.cleanupSMSReportsLocked(now)

	tracker := c.findSMSReportTrackerLocked(msg.ProfileID, msg.ExternalKey)
	if tracker == nil || tracker.pendingStore == "" {
		return smsStatusUpdate{}, false
	}
	return smsStatusUpdate{
		profileID:   tracker.profileID,
		externalKey: tracker.externalKey,
		status:      tracker.pendingStore,
	}, true
}

func (c *coordinator) completeStoredSMSStatus(update smsStatusUpdate) {
	c.mu.Lock()
	defer c.mu.Unlock()

	tracker := c.findSMSReportTrackerLocked(update.profileID, update.externalKey)
	if tracker == nil {
		return
	}
	tracker.pendingStore = ""
	if smsStatusFinal(update.status) {
		c.removeSMSReportTrackerLocked(tracker)
	}
}

func (c *coordinator) findSMSReportTrackerLocked(profileID string, externalKey string) *smsReportTracker {
	profileID = strings.TrimSpace(profileID)
	externalKey = strings.TrimSpace(externalKey)
	for _, tracker := range c.smsReports {
		if tracker.profileID == profileID && tracker.externalKey == externalKey {
			return tracker
		}
	}
	return nil
}

func (c *coordinator) removeSMSReportTrackerLocked(target *smsReportTracker) {
	for key, tracker := range c.smsReports {
		if tracker == target {
			delete(c.smsReports, key)
		}
	}
}

func (c *coordinator) ensureSMSReportMaps() {
	if c.smsReports == nil {
		c.smsReports = make(map[smsReportKey]*smsReportTracker)
	}
	if c.pendingSMSReports == nil {
		c.pendingSMSReports = make(map[smsReportKey]pendingSMSReport)
	}
}

func (c *coordinator) cleanupSMSReportsLocked(now time.Time) {
	for key, tracker := range c.smsReports {
		if now.After(tracker.expiresAt) {
			delete(c.smsReports, key)
		}
	}
	for key, report := range c.pendingSMSReports {
		if now.After(report.expiresAt) {
			delete(c.pendingSMSReports, key)
		}
	}
}

func (t *smsReportTracker) aggregateStatus() string {
	if t == nil || t.segmentCount == 0 {
		return ""
	}
	delivered := 0
	for _, status := range t.statuses {
		switch status {
		case "failed":
			return "failed"
		case "retrying":
			return "retrying"
		case "delivered":
			delivered++
		}
	}
	if delivered == t.segmentCount {
		return "delivered"
	}
	return ""
}

func smsStatusFinal(status string) bool {
	return status == "delivered" || status == "failed"
}

func outgoingSMSReportKey(modemID string, profileID string, recipient string, tpReference byte) smsReportKey {
	return smsReportKey{
		modemID:     strings.TrimSpace(modemID),
		profileID:   strings.TrimSpace(profileID),
		recipient:   strings.TrimSpace(recipient),
		tpReference: tpReference,
	}
}

func smsReportStatus(report vowifi.SMSReport) string {
	switch status := report.Status; {
	case status.Delivered():
		return "delivered"
	case status.Failed():
		return "failed"
	case status.Retrying():
		return "retrying"
	default:
		return ""
	}
}

func (c *coordinator) forwardIncoming(ctx context.Context, modem *mmodem.Modem, profileID string, msg vowifi.SMS) {
	if c.onIncoming == nil {
		return
	}
	stored := storage.Message{
		ModemID:     modem.EquipmentIdentifier,
		ProfileID:   profileID,
		Source:      storage.MessageSourceWiFiCalling,
		ExternalKey: incomingMessageKey(msg),
		Sender:      strings.TrimSpace(msg.From),
		Recipient:   strings.TrimSpace(msg.To),
		Text:        msg.Text,
		Timestamp:   msg.ReceivedAt,
		Status:      "received",
		Incoming:    true,
		WiFiCalling: true,
	}
	if err := c.onIncoming(ctx, IncomingSMS{ModemID: modem.EquipmentIdentifier, Message: stored}); err != nil {
		slog.Warn("forward Wi-Fi Calling SMS", "modem", modem.EquipmentIdentifier, "error", err)
	}
}

func incomingMessageKey(msg vowifi.SMS) string {
	if callID := strings.TrimSpace(msg.CallID); callID != "" {
		return callID
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{
		msg.From,
		msg.To,
		msg.ServiceCenter,
		msg.Text,
		msg.ReceivedAt.UTC().Format(time.RFC3339Nano),
	}, "\x00")))
	return "incoming:" + hex.EncodeToString(sum[:])
}

func newOutgoingMessageKey() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("create outgoing message key: %w", err)
	}
	return "outgoing:" + hex.EncodeToString(b[:]), nil
}
