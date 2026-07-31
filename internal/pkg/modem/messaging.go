package modem

import (
	"context"
	"errors"
	"slices"
	"time"

	wwanmodem "github.com/damonto/wwan-go/modem"
)

type Messaging struct{ modem *Modem }

func (m *Modem) Messaging() *Messaging { return &Messaging{modem: m} }

func (m *Messaging) List(ctx context.Context) ([]*SMS, error) {
	messages, err := m.modem.core.ListMessages(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]*SMS, 0, len(messages))
	for _, message := range messages {
		result = append(result, smsFromWWAN(m.modem, message))
	}
	return result, nil
}

func (m *Messaging) Retrieve(ctx context.Context, ref MessageRef) (*SMS, error) {
	message, err := m.modem.core.ReadStoredMessage(ctx, ref)
	if err != nil {
		return nil, err
	}
	return smsFromWWAN(m.modem, message), nil
}

func (m *Messaging) Delete(ctx context.Context, refs []MessageRef) error {
	if len(refs) == 0 {
		return errors.New("SMS references are required")
	}
	return m.modem.core.DeleteMessages(ctx, slices.Clone(refs))
}

func (m *Messaging) SetDefaultStorage(_ context.Context, storage SMSStorage) error {
	value, ok := semanticSMSStorage(storage)
	if !ok {
		return wwanmodem.ErrNotSupported
	}
	m.modem.smsMu.Lock()
	m.modem.smsStorage = value
	m.modem.smsMu.Unlock()
	return nil
}

func (m *Messaging) SupportedStorages(ctx context.Context) ([]SMSStorage, error) {
	info, err := m.modem.core.MessageStorages(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]SMSStorage, 0, len(info.Supported))
	for _, storage := range info.Supported {
		result = append(result, legacySMSStorage(storage))
	}
	return result, nil
}

func (m *Messaging) DefaultStorage(ctx context.Context) (SMSStorage, error) {
	m.modem.smsMu.RLock()
	storage := m.modem.smsStorage
	m.modem.smsMu.RUnlock()
	if storage != wwanmodem.MessageStorageUnknown {
		return legacySMSStorage(storage), nil
	}
	info, err := m.modem.core.MessageStorages(ctx)
	if err != nil {
		return SMSStorageUnknown, err
	}
	return legacySMSStorage(info.Default), nil
}

func (m *Messaging) Send(ctx context.Context, to, text string) (*SMS, error) {
	m.modem.smsMu.RLock()
	storage := m.modem.smsStorage
	m.modem.smsMu.RUnlock()
	result, err := m.modem.core.SendMessage(ctx, wwanmodem.MessageConfig{Number: to, Text: text, Storage: storage})
	if err != nil {
		return nil, err
	}
	return sentSMSFromWWAN(sentSMSConfig{
		modem:   m.modem,
		storage: storage,
		to:      to,
		text:    text,
		result:  result,
		now:     time.Now(),
	}), nil
}

type sentSMSConfig struct {
	modem   *Modem
	storage wwanmodem.MessageStorage
	to      string
	text    string
	result  wwanmodem.SendResult
	now     time.Time
}

func sentSMSFromWWAN(cfg sentSMSConfig) *SMS {
	if len(cfg.result.Messages) == 0 {
		generation := uint64(0)
		if cfg.modem != nil {
			generation = cfg.modem.Generation()
		}
		return &SMS{
			Generation:        generation,
			MessageReferences: slices.Clone(cfg.result.References),
			State:             SMSStateSent,
			Storage:           legacySMSStorage(cfg.storage),
			Number:            cfg.to,
			Text:              cfg.text,
			Timestamp:         cfg.now,
		}
	}

	sms := smsFromWWAN(cfg.modem, cfg.result.Messages[0])
	sms.MessageReferences = slices.Clone(cfg.result.References)
	seen := make(map[MessageRef]struct{})
	refs := make([]MessageRef, 0)
	for _, part := range cfg.result.Messages {
		for _, ref := range part.Refs {
			if _, ok := seen[ref]; ok {
				continue
			}
			seen[ref] = struct{}{}
			refs = append(refs, ref)
		}
		if sms.Number == "" {
			sms.Number = part.Number
		}
		if sms.Timestamp.IsZero() || (!part.Timestamp.IsZero() && part.Timestamp.Before(sms.Timestamp)) {
			sms.Timestamp = part.Timestamp
		}
	}
	sms.Refs = refs
	if sms.Number == "" {
		sms.Number = cfg.to
	}
	if sms.Storage == SMSStorageUnknown {
		sms.Storage = legacySMSStorage(cfg.storage)
	}
	sms.Text = cfg.text
	if sms.Timestamp.IsZero() {
		sms.Timestamp = cfg.now
	}
	return sms
}

func (m *Messaging) Subscribe(ctx context.Context, subscriber func(message *SMS) error) error {
	if subscriber == nil {
		return errors.New("SMS subscriber is required")
	}
	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := m.modem.core.WatchMessages(watchCtx)
	if err != nil {
		return err
	}
	if stream == nil {
		return errors.New("modem message watcher returned a nil stream")
	}
	// Establish the live stream before replaying stored messages so an SMS
	// arriving during reconciliation is buffered by the watcher instead of
	// falling into a list/subscribe gap.
	messages, err := m.List(watchCtx)
	if err != nil {
		return err
	}
	for _, message := range messages {
		if err := subscriber(message); err != nil {
			return err
		}
	}
	for {
		select {
		case <-watchCtx.Done():
			return watchCtx.Err()
		case result, ok := <-stream:
			if !ok {
				return errors.New("modem message stream closed")
			}
			if result.Err != nil {
				return result.Err
			}
			if err := subscriber(smsFromWWAN(m.modem, result.Value)); err != nil {
				return err
			}
		}
	}
}

func legacySMSStorage(storage wwanmodem.MessageStorage) SMSStorage {
	switch storage {
	case wwanmodem.MessageStorageSIM:
		return SMSStorageSM
	case wwanmodem.MessageStorageDevice:
		return SMSStorageME
	default:
		return SMSStorageUnknown
	}
}

func semanticSMSStorage(storage SMSStorage) (wwanmodem.MessageStorage, bool) {
	switch storage {
	case SMSStorageUnknown:
		return wwanmodem.MessageStorageUnknown, true
	case SMSStorageSM:
		return wwanmodem.MessageStorageSIM, true
	case SMSStorageME:
		return wwanmodem.MessageStorageDevice, true
	default:
		return wwanmodem.MessageStorageUnknown, false
	}
}
