package forwarder

import (
	"testing"
	"time"

	"github.com/damonto/sigmo/internal/pkg/config"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

func TestNewRequiresMessageStorage(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "nil message storage"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(config.NewStore(config.Default()), nil, nil)
			if err == nil {
				t.Fatal("New() error = nil, want error")
			}
		})
	}
}

func TestFreshIncomingMessage(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		message storage.Message
		want    bool
	}{
		{
			name: "recent incoming",
			message: storage.Message{
				Timestamp: now.Add(-29 * time.Minute),
				Incoming:  true,
			},
			want: true,
		},
		{
			name: "old incoming",
			message: storage.Message{
				Timestamp: now.Add(-31 * time.Minute),
				Incoming:  true,
			},
		},
		{
			name: "future incoming",
			message: storage.Message{
				Timestamp: now.Add(31 * time.Minute),
				Incoming:  true,
			},
		},
		{
			name: "outgoing",
			message: storage.Message{
				Timestamp: now.Add(-time.Hour),
			},
			want: true,
		},
		{
			name: "unknown timestamp",
			message: storage.Message{
				Incoming: true,
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := freshIncomingMessage(tt.message, now); got != tt.want {
				t.Fatalf("freshIncomingMessage() = %v, want %v", got, tt.want)
			}
		})
	}
}
