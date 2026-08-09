package network

import (
	"errors"
	"reflect"
	"testing"

	wwanmodem "github.com/damonto/wwan-go/modem"
)

func TestBandsResponse(t *testing.T) {
	lte41 := wwanmodem.Band{Technology: wwanmodem.TechnologyLTE, Number: 41}
	nr78 := wwanmodem.Band{Technology: wwanmodem.TechnologyNR5GSA, Number: 78}
	tests := []struct {
		name      string
		supported []wwanmodem.Band
		current   []wwanmodem.Band
		want      *BandsResponse
	}{
		{
			name:      "preserve complete current set regardless of order",
			supported: []wwanmodem.Band{lte41, nr78},
			current:   []wwanmodem.Band{nr78, lte41},
			want: &BandsResponse{
				Supported: []BandResponse{
					{Value: bandValue(lte41), Label: "LTE B41", Current: true},
					{Value: bandValue(nr78), Label: "NR n78", Current: true},
				},
				Current: []BandValue{bandValue(nr78), bandValue(lte41)},
			},
		},
		{
			name:      "preserve manual subset selection",
			supported: []wwanmodem.Band{lte41, nr78},
			current:   []wwanmodem.Band{nr78},
			want: &BandsResponse{
				Supported: []BandResponse{
					{Value: bandValue(lte41), Label: "LTE B41"},
					{Value: bandValue(nr78), Label: "NR n78", Current: true},
				},
				Current: []BandValue{bandValue(nr78)},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bandsResponse(tt.supported, tt.current); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("bandsResponse() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestBandLabelCombinedNR(t *testing.T) {
	band := wwanmodem.Band{
		Technology: wwanmodem.TechnologyNR5GNSA | wwanmodem.TechnologyNR5GSA,
		Number:     78,
	}
	if got := bandLabel(band); got != "NR n78" {
		t.Fatalf("bandLabel() = %q, want %q", got, "NR n78")
	}
}

func TestValidateBandValues(t *testing.T) {
	tests := []struct {
		name      string
		supported []wwanmodem.Band
		bands     []wwanmodem.Band
		wantErr   error
	}{
		{
			name:      "accept supported bands",
			supported: []wwanmodem.Band{{Technology: wwanmodem.TechnologyLTE, Number: 41}, {Technology: wwanmodem.TechnologyNR5GSA, Number: 78}},
			bands:     []wwanmodem.Band{{Technology: wwanmodem.TechnologyLTE, Number: 41}, {Technology: wwanmodem.TechnologyNR5GSA, Number: 78}},
		},
		{
			name:      "reject empty",
			supported: []wwanmodem.Band{{Technology: wwanmodem.TechnologyLTE, Number: 41}},
			wantErr:   errBandsRequired,
		},
		{
			name:      "reject unsupported",
			supported: []wwanmodem.Band{{Technology: wwanmodem.TechnologyLTE, Number: 41}},
			bands:     []wwanmodem.Band{{Technology: wwanmodem.TechnologyLTE, Number: 42}},
			wantErr:   errUnsupportedBand,
		},
		{
			name:      "reject zero value",
			supported: []wwanmodem.Band{{Technology: wwanmodem.TechnologyLTE, Number: 41}},
			bands:     []wwanmodem.Band{{}},
			wantErr:   errUnsupportedBand,
		},
		{
			name:      "reject duplicate",
			supported: []wwanmodem.Band{{Technology: wwanmodem.TechnologyLTE, Number: 41}},
			bands:     []wwanmodem.Band{{Technology: wwanmodem.TechnologyLTE, Number: 41}, {Technology: wwanmodem.TechnologyLTE, Number: 41}},
			wantErr:   errDuplicateBand,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBandValues(tt.supported, tt.bands)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("validateBandValues() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestFilterCurrentBands(t *testing.T) {
	lte41 := wwanmodem.Band{Technology: wwanmodem.TechnologyLTE, Number: 41}
	nr78 := wwanmodem.Band{Technology: wwanmodem.TechnologyNR5GSA, Number: 78}
	got := filterCurrentBands(
		[]wwanmodem.Band{lte41},
		[]wwanmodem.Band{nr78, lte41, lte41},
	)
	if want := []wwanmodem.Band{lte41}; !reflect.DeepEqual(got, want) {
		t.Fatalf("filterCurrentBands() = %#v, want %#v", got, want)
	}
}
