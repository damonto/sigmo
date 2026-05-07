package modem

import "testing"

func TestSMSStorage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		storage    SMSStorage
		wantValue  uint32
		wantString string
	}{
		{
			name:       "unknown",
			storage:    SMSStorageUnknown,
			wantValue:  0,
			wantString: "unknown",
		},
		{
			name:       "sim",
			storage:    SMSStorageSM,
			wantValue:  1,
			wantString: "sm",
		},
		{
			name:       "mobile equipment",
			storage:    SMSStorageME,
			wantValue:  2,
			wantString: "me",
		},
		{
			name:       "combined",
			storage:    SMSStorageMT,
			wantValue:  3,
			wantString: "mt",
		},
		{
			name:       "status report",
			storage:    SMSStorageSR,
			wantValue:  4,
			wantString: "sr",
		},
		{
			name:       "broadcast",
			storage:    SMSStorageBM,
			wantValue:  5,
			wantString: "bm",
		},
		{
			name:       "terminal adaptor",
			storage:    SMSStorageTA,
			wantValue:  6,
			wantString: "ta",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := uint32(tt.storage); got != tt.wantValue {
				t.Fatalf("uint32(%s) = %d, want %d", tt.storage, got, tt.wantValue)
			}
			if got := tt.storage.String(); got != tt.wantString {
				t.Fatalf("SMSStorage.String() = %q, want %q", got, tt.wantString)
			}
		})
	}
}
