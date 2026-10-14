package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/damonto/sigmo/internal/pkg/phonenumber"
	"github.com/damonto/sigmo/internal/pkg/storage/query"
)

// Contact is a backend-owned address book entry; deleting it preserves history.
type Contact struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Notes       string          `json:"notes"`
	Numbers     []ContactNumber `json:"numbers"`
	HasAvatar   bool            `json:"hasAvatar"`
	AvatarColor string          `json:"avatarColor"`
	UpdatedAt   string          `json:"updatedAt"`
}

// ContactNumber retains the user's formatting alongside a region-aware lookup key.
type ContactNumber struct {
	Label  string `json:"label"`
	Number string `json:"number"`
	Region string `json:"region"`
	Key    string `json:"key"`
}

// ErrInvalidContact indicates invalid address book input.
var ErrInvalidContact = errors.New("invalid contact")

// ParticipantKey never guesses a country or matches only the end of a number.
func ParticipantKey(value, region string) string {
	value = strings.TrimSpace(value)
	for _, r := range value {
		if unicode.IsLetter(r) {
			return value
		}
	}
	if normalized, err := phonenumber.NormalizeForRegion(value, region); err == nil {
		return normalized
	}
	return value
}

// SaveContact creates an entry when ID is empty, otherwise replaces its editable fields.
func (s *Store) SaveContact(ctx context.Context, contact Contact) (Contact, error) {
	contact.Name = strings.TrimSpace(contact.Name)
	if (contact.Name == "" && len(contact.Numbers) == 0) || len(contact.Name) > 512 || len(contact.Notes) > 32768 || len(contact.Numbers) > 50 {
		return Contact{}, ErrInvalidContact
	}
	for i := range contact.Numbers {
		number := &contact.Numbers[i]
		number.Number = strings.TrimSpace(number.Number)
		number.Region = strings.ToUpper(strings.TrimSpace(number.Region))
		number.Key = ParticipantKey(number.Number, number.Region)
		if number.Number == "" || len(number.Number) > 128 || len(number.Label) > 128 || len(number.Region) > 2 {
			return Contact{}, ErrInvalidContact
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Contact{}, fmt.Errorf("begin contact save: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // A committed contact has nothing to roll back.
	queries := query.New(tx)
	contact.UpdatedAt = nowText()
	if contact.ID == "" {
		contact.ID = rand.Text()
		err = queries.CreateContact(ctx, query.CreateContactParams{ID: contact.ID, Name: contact.Name, Notes: contact.Notes, UpdatedAt: contact.UpdatedAt})
	} else {
		var count int64
		count, err = queries.UpdateContact(ctx, query.UpdateContactParams{ID: contact.ID, Name: contact.Name, Notes: contact.Notes, UpdatedAt: contact.UpdatedAt})
		if err == nil && count == 0 {
			return Contact{}, ErrNotFound
		}
	}
	if err != nil {
		return Contact{}, fmt.Errorf("save contact: %w", err)
	}
	if err := queries.ClearContactNumbers(ctx, contact.ID); err != nil {
		return Contact{}, fmt.Errorf("replace contact numbers: %w", err)
	}
	for i, number := range contact.Numbers {
		if err := queries.AddContactNumber(ctx, query.AddContactNumberParams{ContactID: contact.ID, Position: int64(i), Label: number.Label, Number: number.Number, Region: number.Region, NumberKey: number.Key}); err != nil {
			return Contact{}, fmt.Errorf("save contact number: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Contact{}, fmt.Errorf("commit contact: %w", err)
	}
	return s.Contact(ctx, contact.ID)
}

// Contacts lists matching contacts without exposing avatar blobs.
func (s *Store) Contacts(ctx context.Context, search string) ([]Contact, error) {
	rows, err := query.New(s.db).ListContacts(ctx, query.ListContactsParams{Search: strings.TrimSpace(search), SearchPattern: likePattern(search)})
	if err != nil {
		return nil, fmt.Errorf("list contacts: %w", err)
	}
	contacts := make([]Contact, 0, len(rows))
	for _, row := range rows {
		contact := Contact{ID: row.ID, Name: row.Name, Notes: row.Notes, HasAvatar: row.HasAvatar != 0, AvatarColor: row.AvatarColor, UpdatedAt: row.UpdatedAt}
		if err := s.contactNumbers(ctx, &contact); err != nil {
			return nil, err
		}
		contacts = append(contacts, contact)
	}
	return contacts, nil
}

// Contact returns one backend-local address book entry.
func (s *Store) Contact(ctx context.Context, id string) (Contact, error) {
	row, err := query.New(s.db).ReadContact(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Contact{}, ErrNotFound
	}
	if err != nil {
		return Contact{}, fmt.Errorf("read contact: %w", err)
	}
	contact := Contact{ID: row.ID, Name: row.Name, Notes: row.Notes, HasAvatar: row.HasAvatar != 0, AvatarColor: row.AvatarColor, UpdatedAt: row.UpdatedAt}
	if err := s.contactNumbers(ctx, &contact); err != nil {
		return Contact{}, err
	}
	return contact, nil
}

func (s *Store) contactNumbers(ctx context.Context, contact *Contact) error {
	rows, err := query.New(s.db).ListContactNumbers(ctx, contact.ID)
	if err != nil {
		return fmt.Errorf("read contact numbers: %w", err)
	}
	contact.Numbers = make([]ContactNumber, 0, len(rows))
	for _, row := range rows {
		contact.Numbers = append(contact.Numbers, ContactNumber{Label: row.Label, Number: row.Number, Region: row.Region, Key: row.NumberKey})
	}
	return nil
}

// DeleteContact removes only address book data.
func (s *Store) DeleteContact(ctx context.Context, id string) error {
	err := query.New(s.db).DeleteContact(ctx, id)
	if err != nil {
		return fmt.Errorf("delete contact: %w", err)
	}
	return nil
}

// ContactAvatar returns a validated PNG thumbnail.
func (s *Store) ContactAvatar(ctx context.Context, id string) ([]byte, error) {
	data, err := query.New(s.db).ReadContactAvatar(ctx, id)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && len(data) == 0) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read contact avatar: %w", err)
	}
	return data, nil
}

// SetContactAvatar stores the thumbnail and its color together. Nil removes it.
func (s *Store) SetContactAvatar(ctx context.Context, id string, data []byte, color string) (Contact, error) {
	count, err := query.New(s.db).SetContactAvatar(ctx, query.SetContactAvatarParams{Avatar: data, AvatarColor: color, UpdatedAt: nowText(), ID: id})
	if err != nil {
		return Contact{}, fmt.Errorf("save contact avatar: %w", err)
	}
	if count == 0 {
		return Contact{}, ErrNotFound
	}
	return s.Contact(ctx, id)
}
