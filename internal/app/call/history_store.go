package call

import (
	"context"

	"github.com/damonto/sigmo/internal/pkg/storage"
)

// History manages archived calls independently of active voice routes.
type History struct{ records *callRecords }

// NewHistory creates an archived call service backed by persistence.
func NewHistory(store *storage.Store) *History { return &History{records: &callRecords{store: store}} }

// List returns a page of archived calls and its continuation cursor.
func (h *History) List(ctx context.Context, filter storage.HistoryFilter) ([]storage.Call, string, error) {
	return h.records.store.CallHistory(ctx, filter)
}

// Delete removes a terminal call while preserving active call records.
func (h *History) Delete(ctx context.Context, id string) error {
	record, err := h.records.store.Call(ctx, id)
	if err != nil {
		return err
	}
	return h.records.deleteCall(ctx, record)
}
