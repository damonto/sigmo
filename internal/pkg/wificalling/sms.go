//go:build wifi_calling

package wificalling

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
	"github.com/damonto/sigmo/internal/pkg/storage"
	vowifi "github.com/damonto/vowifi-go"
	imssms "github.com/damonto/vowifi-go/ims/sms"
)

type smsSubmissionKey struct {
	modemID      string
	profileID    string
	submissionID string
}

type smsSubmissionTracker struct {
	profileID    string
	externalKey  string
	segmentCount int
	statuses     map[int]string
	current      string
	pendingStore string
	expiresAt    time.Time
}

const (
	smsSubmissionRetention = 6 * time.Hour
	smsStatusUpdateTimeout = 5 * time.Second
)

func smsDeliveryReportTimeout() time.Duration {
	return imssms.DefaultDeliveryReportTimeout()
}

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
		if errors.Is(err, vowifi.ErrClientNotConnected) {
			return storage.Message{}, c.handleClientDisconnected(modem.EquipmentIdentifier, client, err)
		}
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
	if status := c.trackOutgoingSMSSubmission(msg, submission); status != "" {
		msg.Status = status
	}
	go c.watchSMSSubmissionUpdates(modem.EquipmentIdentifier, profileID, submission)
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

func (c *coordinator) trackOutgoingSMSSubmission(msg storage.Message, submission vowifi.SMSSubmission) string {
	if len(submission.Segments) == 0 {
		return ""
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()

	c.ensureSMSSubmissionMap()
	c.cleanupSMSSubmissionsLocked(now)

	tracker := &smsSubmissionTracker{
		profileID:    msg.ProfileID,
		externalKey:  msg.ExternalKey,
		segmentCount: len(submission.Segments),
		statuses:     make(map[int]string, len(submission.Segments)),
		expiresAt:    now.Add(smsSubmissionRetention),
	}
	for _, segment := range submission.Segments {
		tracker.statuses[segment.Index] = ""
	}
	tracker.current = tracker.aggregateStatus()
	key := outgoingSMSSubmissionKey(msg.ModemID, msg.ProfileID, submission.ID)
	c.smsSubmissions[key] = tracker
	if smsStatusFinal(tracker.current) {
		c.removeSMSSubmissionTrackerLocked(tracker)
	}
	return tracker.current
}

func (c *coordinator) forwardSMSReport(ctx context.Context, modemID string, profileID string, report vowifi.SMSReport) {
	_ = ctx
	slog.Info("Wi-Fi Calling SMS report",
		"modem", modemID,
		"profile_id", profileID,
		"submission_id", report.SubmissionID,
		"segment", report.SegmentIndex+1,
		"segments", report.SegmentCount,
		"recipient", report.Recipient,
		"service_center", report.ServiceCenter,
		"tp_ref", report.MessageReference,
		"tp_status", report.Status,
		"received_at", report.ReceivedAt.Format(time.RFC3339),
	)
}

type smsStatusUpdate struct {
	profileID   string
	externalKey string
	status      string
}

func (c *coordinator) watchSMSSubmissionUpdates(modemID string, profileID string, submission vowifi.SMSSubmission) {
	c.watchSMSSubmissionUpdatesWithTimeout(modemID, profileID, submission, smsDeliveryReportTimeout())
}

func (c *coordinator) watchSMSSubmissionUpdatesWithTimeout(modemID string, profileID string, submission vowifi.SMSSubmission, timeout time.Duration) {
	defer func() {
		c.stopSMSSubmissionTracking(modemID, profileID, submission.ID)
		submission.Close()
	}()
	if submission.Updates == nil {
		return
	}

	// Requested delivery reports are not guaranteed to arrive, so the caller
	// must bound how long it keeps vowifi-go's per-submission tracking alive.
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case update, ok := <-submission.Updates:
			if !ok {
				return
			}
			logSMSSubmissionUpdate(modemID, profileID, update)
			status := smsSubmissionUpdateStatus(update)
			if status == "" {
				continue
			}
			statusUpdate, ok := c.recordSMSSubmissionUpdate(modemID, profileID, update, status)
			if !ok {
				continue
			}
			c.updateStoredSMSStatus(modemID, update.Recipient, statusUpdate)
		case <-timer.C:
			slog.Warn("wait Wi-Fi Calling SMS delivery report",
				"modem", modemID,
				"profile_id", profileID,
				"submission_id", submission.ID,
				"timeout", timeout,
			)
			return
		}
	}
}

func logSMSSubmissionUpdate(modemID string, profileID string, update vowifi.SMSSubmissionUpdate) {
	attrs := []any{
		"modem", modemID,
		"profile_id", profileID,
		"submission_id", update.SubmissionID,
		"segment", update.SegmentIndex + 1,
		"segments", update.SegmentCount,
		"state", update.State,
		"recipient", update.Recipient,
		"service_center", update.ServiceCenter,
		"call_id", update.CallID,
		"rp_ref", update.RPReference,
		"tp_ref", update.TPReference,
		"received_at", update.ReceivedAt.Format(time.RFC3339),
	}
	if update.HasRPCause {
		attrs = append(attrs, "rp_cause", update.RPCause)
	}
	if update.HasStatus {
		attrs = append(attrs, "tp_status", update.Status)
	}
	slog.Info("Wi-Fi Calling SMS submission update", attrs...)
}

func smsSubmissionUpdateStatus(update vowifi.SMSSubmissionUpdate) string {
	switch update.State {
	case vowifi.SMSRejectedBySMSC, vowifi.SMSDeliveryFailed:
		return "failed"
	case vowifi.SMSDelivered:
		return "delivered"
	default:
		return ""
	}
}

func (c *coordinator) recordSMSSubmissionUpdate(modemID string, profileID string, update vowifi.SMSSubmissionUpdate, status string) (smsStatusUpdate, bool) {
	now := time.Now()
	key := outgoingSMSSubmissionKey(modemID, profileID, update.SubmissionID)

	c.mu.Lock()
	defer c.mu.Unlock()

	c.ensureSMSSubmissionMap()
	c.cleanupSMSSubmissionsLocked(now)

	tracker := c.smsSubmissions[key]
	if tracker == nil {
		return smsStatusUpdate{}, false
	}
	if _, ok := tracker.statuses[update.SegmentIndex]; !ok {
		return smsStatusUpdate{}, false
	}
	tracker.statuses[update.SegmentIndex] = status
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

func (c *coordinator) updateStoredSMSStatus(modemID string, recipient string, update smsStatusUpdate) {
	if c.store == nil {
		c.completeStoredSMSStatus(update)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), smsStatusUpdateTimeout)
	defer cancel()
	if updated, err := c.store.UpdateMessageStatus(ctx, storage.MessageStatusUpdate{
		ProfileID:   update.profileID,
		Source:      storage.MessageSourceWiFiCalling,
		ExternalKey: update.externalKey,
		Status:      update.status,
	}); err != nil {
		slog.Warn("update Wi-Fi Calling SMS status", "modem", modemID, "recipient", recipient, "status", update.status, "error", err)
	} else if !updated {
		c.deferStoredSMSStatus(update)
		slog.Debug("Wi-Fi Calling SMS status target not yet stored", "modem", modemID, "recipient", recipient, "status", update.status)
	} else {
		c.completeStoredSMSStatus(update)
	}
}

func (c *coordinator) deferStoredSMSStatus(update smsStatusUpdate) {
	c.mu.Lock()
	defer c.mu.Unlock()

	tracker := c.findSMSSubmissionTrackerLocked(update.profileID, update.externalKey)
	if tracker != nil {
		tracker.pendingStore = update.status
	}
}

func (c *coordinator) pendingSMSStatus(msg storage.Message) (smsStatusUpdate, bool) {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()

	c.ensureSMSSubmissionMap()
	c.cleanupSMSSubmissionsLocked(now)

	tracker := c.findSMSSubmissionTrackerLocked(msg.ProfileID, msg.ExternalKey)
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

	tracker := c.findSMSSubmissionTrackerLocked(update.profileID, update.externalKey)
	if tracker == nil {
		return
	}
	tracker.pendingStore = ""
	if smsStatusFinal(update.status) {
		c.removeSMSSubmissionTrackerLocked(tracker)
	}
}

func (c *coordinator) stopSMSSubmissionTracking(modemID string, profileID string, submissionID string) {
	key := outgoingSMSSubmissionKey(modemID, profileID, submissionID)

	c.mu.Lock()
	defer c.mu.Unlock()

	tracker := c.smsSubmissions[key]
	if tracker == nil || tracker.pendingStore != "" {
		return
	}
	delete(c.smsSubmissions, key)
}

func (c *coordinator) findSMSSubmissionTrackerLocked(profileID string, externalKey string) *smsSubmissionTracker {
	profileID = strings.TrimSpace(profileID)
	externalKey = strings.TrimSpace(externalKey)
	for _, tracker := range c.smsSubmissions {
		if tracker.profileID == profileID && tracker.externalKey == externalKey {
			return tracker
		}
	}
	return nil
}

func (c *coordinator) removeSMSSubmissionTrackerLocked(target *smsSubmissionTracker) {
	for key, tracker := range c.smsSubmissions {
		if tracker == target {
			delete(c.smsSubmissions, key)
		}
	}
}

func (c *coordinator) ensureSMSSubmissionMap() {
	if c.smsSubmissions == nil {
		c.smsSubmissions = make(map[smsSubmissionKey]*smsSubmissionTracker)
	}
}

func (c *coordinator) cleanupSMSSubmissionsLocked(now time.Time) {
	for key, tracker := range c.smsSubmissions {
		if now.After(tracker.expiresAt) {
			delete(c.smsSubmissions, key)
		}
	}
}

func (t *smsSubmissionTracker) aggregateStatus() string {
	if t == nil || t.segmentCount == 0 {
		return ""
	}
	delivered := 0
	for _, status := range t.statuses {
		switch status {
		case "failed":
			return "failed"
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

func outgoingSMSSubmissionKey(modemID string, profileID string, submissionID string) smsSubmissionKey {
	return smsSubmissionKey{
		modemID:      strings.TrimSpace(modemID),
		profileID:    strings.TrimSpace(profileID),
		submissionID: strings.TrimSpace(submissionID),
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
