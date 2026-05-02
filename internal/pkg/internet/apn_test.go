package internet

import (
	"maps"
	"testing"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

func TestDefaultAPNsFromXML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		xml  string
		want map[string]string
	}{
		{
			name: "mcc mnc default apn",
			xml: `<apns>
				<apn mcc="001" mnc="01" apn="phone" type="default,ia,mms"/>
			</apns>`,
			want: map[string]string{"00101": "phone"},
		},
		{
			name: "trim fields and default type",
			xml: `<apns>
				<apn mcc=" 310 " mnc=" 260 " apn=" fast.t-mobile.com " type=" ia, default ,supl"/>
			</apns>`,
			want: map[string]string{"310260": "fast.t-mobile.com"},
		},
		{
			name: "ignore non default apns",
			xml: `<apns>
				<apn mcc="001" mnc="01" apn="mms" type="mms"/>
				<apn mcc="001" mnc="01" apn="dun" type="dun"/>
				<apn mcc="001" mnc="01" apn="empty"/>
			</apns>`,
			want: map[string]string{},
		},
		{
			name: "keep first default apn",
			xml: `<apns>
				<apn mcc="001" mnc="01" apn="first" type="default"/>
				<apn mcc="001" mnc="01" apn="second" type="default"/>
			</apns>`,
			want: map[string]string{"00101": "first"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := defaultAPNsFromXML([]byte(tt.xml))
			if err != nil {
				t.Fatalf("defaultAPNsFromXML() error = %v", err)
			}
			if !maps.Equal(got, tt.want) {
				t.Fatalf("defaultAPNsFromXML() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestDefaultAPNsFromXMLInvalid(t *testing.T) {
	t.Parallel()

	if _, err := defaultAPNsFromXML([]byte("<apns>")); err == nil {
		t.Fatal("defaultAPNsFromXML() error = nil, want error")
	}
}

func TestSelectAPN(t *testing.T) {
	t.Parallel()

	defaults := map[string]string{
		"00101": "xml",
	}
	tests := []struct {
		name      string
		selection apnSelection
		want      string
	}{
		{
			name: "requested wins",
			selection: apnSelection{
				Requested:          " user ",
				Bearer:             "bearer",
				Remembered:         "remembered",
				OperatorIdentifier: "00101",
			},
			want: "user",
		},
		{
			name: "bearer wins",
			selection: apnSelection{
				Bearer:             " bearer ",
				Remembered:         "remembered",
				OperatorIdentifier: "00101",
			},
			want: "bearer",
		},
		{
			name: "remembered wins over xml",
			selection: apnSelection{
				Remembered:         " remembered ",
				OperatorIdentifier: "00101",
			},
			want: "remembered",
		},
		{
			name: "xml fallback",
			selection: apnSelection{
				OperatorIdentifier: "00101",
			},
			want: "xml",
		},
		{
			name: "missing xml keeps empty",
			selection: apnSelection{
				OperatorIdentifier: "99999",
			},
			want: "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tt.selection.DefaultAPNs = defaults
			if got := selectAPN(tt.selection); got != tt.want {
				t.Fatalf("selectAPN() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAPNForModem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		modem      *mmodem.Modem
		remembered string
		want       string
	}{
		{
			name: "remembered wins",
			modem: &mmodem.Modem{
				Sim: &mmodem.SIM{OperatorIdentifier: "00101"},
			},
			remembered: "remembered",
			want:       "remembered",
		},
		{
			name: "xml fallback from sim operator",
			modem: &mmodem.Modem{
				Sim: &mmodem.SIM{OperatorIdentifier: "00101"},
			},
			want: "phone",
		},
		{
			name:  "missing sim keeps empty",
			modem: &mmodem.Modem{},
			want:  "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := apnForModem(tt.modem, "", "", tt.remembered); got != tt.want {
				t.Fatalf("apnForModem() = %q, want %q", got, tt.want)
			}
		})
	}
}
