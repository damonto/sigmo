package euicc

import "testing"

func TestLookupSASUP(t *testing.T) {
	tests := []struct {
		name                   string
		eid                    string
		sasAccreditationNumber string
		want                   SASUP
	}{
		{
			name:                   "uses supplier default region",
			eid:                    "89049032000000000000000000000000",
			sasAccreditationNumber: "GD-ZZ-0000",
			want:                   SASUP{Name: "Giesecke+Devrient GmbH", Region: "DE"},
		},
		{
			name:                   "uses location region",
			eid:                    "89049032000000000000000000000000",
			sasAccreditationNumber: "GD-MM-0000",
			want:                   SASUP{Name: "Giesecke+Devrient GmbH", Region: "CA"},
		},
		{
			name:                   "uses supplier default region for short accreditation number",
			eid:                    "89049032000000000000000000000000",
			sasAccreditationNumber: "GD",
			want:                   SASUP{Name: "Giesecke+Devrient GmbH", Region: "DE"},
		},
		{
			name:                   "returns raw accreditation number for unknown supplier",
			eid:                    "00000000000000000000000000000000",
			sasAccreditationNumber: "UNKNOWN",
			want:                   SASUP{Name: "UNKNOWN"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LookupSASUP(tt.eid, tt.sasAccreditationNumber)
			if got != tt.want {
				t.Errorf("LookupSASUP() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
