package event

import (
	"encoding/json"
	"testing"
)

func TestReminderEventJSON(t *testing.T) {
	tests := []struct {
		name        string
		event       ReminderEvent
		wantModemID string
	}{
		{
			name:        "includes stable modem id",
			event:       ReminderEvent{ModemID: "modem-1"},
			wantModemID: "modem-1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.event)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			var got struct {
				ModemID string `json:"modemId"`
			}
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			if got.ModemID != tt.wantModemID {
				t.Fatalf("ModemID = %q, want %q", got.ModemID, tt.wantModemID)
			}
		})
	}
}
