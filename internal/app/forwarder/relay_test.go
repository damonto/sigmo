package forwarder

import (
	"testing"

	"github.com/damonto/sigmo/internal/pkg/config"
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
