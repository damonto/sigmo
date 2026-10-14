package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/damonto/sigmo/internal/pkg/storage/query"
)

// SIMProfile stores the last known identity and display metadata of a physical SIM or eSIM.
type SIMProfile struct {
	Icon         string
	DisplayKnown bool

	// DisplayRevision is captured before a card read when DisplayKnown is true.
	// Cached branding must never be passed back as a fresh observation.
	DisplayRevision uint64
	Region          string
	Kind            string
	SEID            string
	ProfileID       string
	ModemID         string
	Label           string
	Number          string
}

// SIMProfileNumbers returns known numbers for the requested ICCIDs, including inactive SIMs.
func (s *Store) SIMProfileNumbers(ctx context.Context, profileIDs []string) (map[string]string, error) {
	numbers := make(map[string]string)
	if len(profileIDs) == 0 {
		return numbers, nil
	}
	// A JSON array keeps the query below SQLite's parameter limit for large eUICCs.
	ids, err := json.Marshal(profileIDs)
	if err != nil {
		return nil, fmt.Errorf("encode SIM profile IDs: %w", err)
	}
	rows, err := query.New(s.db).SIMProfileNumbers(ctx, string(ids))
	if err != nil {
		return nil, fmt.Errorf("read SIM profile numbers: %w", err)
	}
	for _, row := range rows {
		numbers[row.ProfileID] = row.Number
	}
	return numbers, nil
}

// RecordSIMProfile saves confirmed SIM metadata and associates unassigned history.
// Empty metadata preserves previously known values, including numbers of inactive SIMs.
func (s *Store) RecordSIMProfile(ctx context.Context, profile SIMProfile) error {
	profile.ProfileID = strings.TrimSpace(profile.ProfileID)
	profile.ModemID = strings.TrimSpace(profile.ModemID)
	profile.Label = strings.TrimSpace(profile.Label)
	profile.Number = strings.TrimSpace(profile.Number)
	if profile.ProfileID == "" || profile.ModemID == "" {
		return nil
	}
	if profile.DisplayKnown {
		s.simDisplayMu.RLock()
		defer s.simDisplayMu.RUnlock()
		if profile.DisplayRevision != s.simDisplayRevision {
			profile.Label = ""
			profile.DisplayKnown = false
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin SIM profile update: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // Keep SIM metadata and historical attribution together.
	queries := query.New(tx)
	previousRegion := ""
	if profile.Region != "" {
		previousRegion, err = queries.SIMProfileRegion(ctx, profile.ProfileID)
		if err != nil {
			return fmt.Errorf("read previous SIM region: %w", err)
		}
	}
	err = queries.RecordSIMProfile(ctx, query.RecordSIMProfileParams{
		ProfileID: profile.ProfileID, ModemID: profile.ModemID, Label: profile.Label,
		Number: profile.Number, Region: profile.Region, Kind: profile.Kind, SEID: profile.SEID,
	})
	if err != nil {
		return fmt.Errorf("record SIM profile: %w", err)
	}
	if profile.DisplayKnown {
		if err := queries.RecordSIMDisplay(ctx, query.RecordSIMDisplayParams{ProfileID: profile.ProfileID, Icon: profile.Icon}); err != nil {
			return fmt.Errorf("record SIM display: %w", err)
		}
	}
	if err := queries.BackfillMessageModem(ctx, query.BackfillMessageModemParams{ModemID: profile.ModemID, ProfileID: profile.ProfileID}); err != nil {
		return fmt.Errorf("backfill message device: %w", err)
	}
	if profile.Region != "" && profile.Region != previousRegion {
		if err := updateConversationKeys(ctx, queries, profile.ProfileID); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit SIM profile update: %w", err)
	}
	return nil
}

// SIMDisplay is confirmed eSIM branding, cached independently of live availability.
type SIMDisplay struct {
	Label string
	Icon  string
}

// ReadSIMDisplay returns persisted branding without accessing the card.
func (s *Store) ReadSIMDisplay(ctx context.Context, profileID string) (SIMDisplay, bool, error) {
	row, err := query.New(s.db).ReadSIMDisplay(ctx, profileID)
	if errors.Is(err, sql.ErrNoRows) {
		return SIMDisplay{}, false, nil
	}
	if err != nil {
		return SIMDisplay{}, false, fmt.Errorf("read SIM display: %w", err)
	}
	return SIMDisplay{Label: row.Label, Icon: row.Icon}, true, nil
}

// SIMDisplayRevision must be captured before reading branding from a card.
// It only tracks in-flight reads; persisted branding survives process restarts.
func (s *Store) SIMDisplayRevision() uint64 {
	s.simDisplayMu.RLock()
	defer s.simDisplayMu.RUnlock()
	return s.simDisplayRevision
}

// InvalidateSIMDisplay requests a fresh card read after a successful nickname change.
func (s *Store) InvalidateSIMDisplay(ctx context.Context, profileID string) error {
	s.simDisplayMu.Lock()
	defer s.simDisplayMu.Unlock()
	s.simDisplayRevision++
	if err := query.New(s.db).InvalidateSIMDisplay(ctx, profileID); err != nil {
		return fmt.Errorf("invalidate SIM display: %w", err)
	}
	return nil
}

func updateConversationKeys(ctx context.Context, queries *query.Queries, profileID string) error {
	participants, err := queries.ProfileConversationParticipants(ctx, profileID)
	if err != nil {
		return fmt.Errorf("read historical participants: %w", err)
	}
	for _, participant := range participants {
		err := queries.UpdateConversationParticipantKey(ctx, query.UpdateConversationParticipantKeyParams{
			ID: participant.ID, ParticipantKey: ParticipantKey(participant.Participant, participant.Region),
		})
		if err != nil {
			return fmt.Errorf("normalize historical participant: %w", err)
		}
	}
	return nil
}
