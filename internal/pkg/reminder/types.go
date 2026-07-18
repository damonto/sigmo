package reminder

import (
	"errors"
	"strings"
	"time"

	"github.com/damonto/sigmo/internal/pkg/storage"
)

const maxRepeatDays = 3650

type UpdateRequest struct {
	ScheduledAt time.Time `json:"scheduledAt"`
	RepeatDays  *int      `json:"repeatDays" validate:"omitempty,gte=1,lte=3650"`
	Content     string    `json:"content"`
}

type Details struct {
	NextAt     time.Time `json:"nextAt"`
	RepeatDays *int      `json:"repeatDays,omitempty"`
	Content    string    `json:"content"`
}

func (r UpdateRequest) Record(profileType ProfileType, profileID, modemID, seID, profileName string) (storage.Reminder, error) {
	if r.ScheduledAt.IsZero() {
		return storage.Reminder{}, errors.New("reminder time is required")
	}
	if !r.ScheduledAt.After(time.Now()) {
		return storage.Reminder{}, errors.New("reminder time must be in the future")
	}
	content := strings.TrimSpace(r.Content)
	if content == "" {
		return storage.Reminder{}, errors.New("reminder content is required")
	}
	return storage.Reminder{
		ProfileType: profileType.String(),
		ProfileID:   strings.TrimSpace(profileID),
		ModemID:     strings.TrimSpace(modemID),
		SEID:        strings.TrimSpace(seID),
		ProfileName: strings.TrimSpace(profileName),
		NextAt:      r.ScheduledAt.UTC(),
		RepeatDays:  r.RepeatDays,
		Content:     content,
	}, nil
}

func DetailsFrom(value storage.Reminder) Details {
	return Details{
		NextAt:     value.NextAt.UTC(),
		RepeatDays: value.RepeatDays,
		Content:    value.Content,
	}
}
