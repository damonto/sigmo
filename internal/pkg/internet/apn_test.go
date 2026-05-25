package internet

import (
	"maps"
	"testing"

	mmodem "github.com/damonto/sigmo/internal/pkg/modem"
)

func TestDefaultAPNsFromJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		json string
		want map[string]apnProfile
	}{
		{
			name: "mcc mnc apn",
			json: `[{
				"mcc": "001",
				"mnc": "01",
				"apn": "phone"
			}]`,
			want: map[string]apnProfile{"00101": {APN: "phone"}},
		},
		{
			name: "trim fields",
			json: `[{
				"mcc": " 310 ",
				"mnc": " 260 ",
				"apn": " fast.t-mobile.com "
			}]`,
			want: map[string]apnProfile{"310260": {APN: "fast.t-mobile.com"}},
		},
		{
			name: "default apn credentials",
			json: `[{
				"mcc": "234",
				"mnc": "91",
				"apn": "wap.vodafone.co.uk",
				"protocol": "IPV4V6",
				"user": "wap",
				"pass": "*wap",
				"authType": 1
			}]`,
			want: map[string]apnProfile{"23491": {
				APN:      "wap.vodafone.co.uk",
				IPType:   "ipv4v6",
				Username: "wap",
				Password: "*wap",
				Auth:     "pap",
			}},
		},
		{
			name: "restricted entries keep separate apns",
			json: `[
				{"mcc": "001", "mnc": "01", "apn": "generic"},
				{"mcc": "001", "mnc": "01", "gid1": " a0 ", "apn": "branded"},
				{"mcc": "001", "mnc": "01", "spn": " Example ", "apn": "spn"},
				{"mcc": "001", "mnc": "01", "iccid": " 894410 ", "apn": "iccid"},
				{"mcc": "001", "mnc": "01", "imsi": " 21670xx1 ", "apn": "imsi"}
			]`,
			want: map[string]apnProfile{
				"00101":               {APN: "generic"},
				"00101|gid1=A0":       {APN: "branded"},
				"00101|spn=EXAMPLE":   {APN: "spn"},
				"00101|iccid=894410":  {APN: "iccid"},
				"00101|imsi=21670XX1": {APN: "imsi"},
			},
		},
		{
			name: "skip incomplete entries",
			json: `[
				{"mcc": "", "mnc": "01", "apn": "missing-mcc"},
				{"mcc": "001", "mnc": "", "apn": "missing-mnc"},
				{"mcc": "001", "mnc": "01", "apn": " "}
			]`,
			want: map[string]apnProfile{},
		},
		{
			name: "keep first apn",
			json: `[
				{"mcc": "001", "mnc": "01", "apn": "first"},
				{"mcc": "001", "mnc": "01", "apn": "second"}
			]`,
			want: map[string]apnProfile{"00101": {APN: "first"}},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := defaultAPNsFromJSON([]byte(tt.json))
			if err != nil {
				t.Fatalf("defaultAPNsFromJSON() error = %v", err)
			}
			if got := apnProfilesByKey(got); !maps.Equal(got, tt.want) {
				t.Fatalf("defaultAPNsFromJSON() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func apnProfilesByKey(records []apnRecord) map[string]apnProfile {
	profiles := make(map[string]apnProfile, len(records))
	for _, record := range records {
		profiles[apnKey(record.OperatorIdentifier, record.Criteria)] = record.Profile
	}
	return profiles
}

func TestDefaultAPNsFromJSONInvalid(t *testing.T) {
	t.Parallel()

	if _, err := defaultAPNsFromJSON([]byte("{")); err == nil {
		t.Fatal("defaultAPNsFromJSON() error = nil, want error")
	}
}

func TestAndroidAuthType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value *int
		want  string
	}{
		{name: "missing", want: ""},
		{name: "unknown", value: intPtr(-1), want: ""},
		{name: "none", value: intPtr(0), want: "none"},
		{name: "pap", value: intPtr(1), want: "pap"},
		{name: "chap", value: intPtr(2), want: "chap"},
		{name: "pap chap", value: intPtr(3), want: "pap|chap"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := androidAuthType(tt.value); got != tt.want {
				t.Fatalf("androidAuthType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func intPtr(value int) *int {
	return &value
}

func TestSelectAPN(t *testing.T) {
	t.Parallel()

	defaults := []apnRecord{
		{OperatorIdentifier: "00101", Profile: apnProfile{APN: "json"}},
		{OperatorIdentifier: "00101", Criteria: apnCriteria{GID1: "A0"}, Profile: apnProfile{APN: "gid"}},
		{OperatorIdentifier: "00101", Criteria: apnCriteria{SPN: "ESIM GO"}, Profile: apnProfile{APN: "spn"}},
		{OperatorIdentifier: "00101", Criteria: apnCriteria{ICCID: "894410"}, Profile: apnProfile{APN: "iccid"}},
		{OperatorIdentifier: "00101", Criteria: apnCriteria{IMSI: "21670XX1"}, Profile: apnProfile{APN: "imsi"}},
		{
			OperatorIdentifier: "00101",
			Criteria:           apnCriteria{GID1: "A0", SPN: "ESIM GO"},
			Profile:            apnProfile{APN: "gid-spn"},
		},
		{
			OperatorIdentifier: "00101",
			Criteria:           apnCriteria{GID1: "A0", ICCID: "894410"},
			Profile:            apnProfile{APN: "gid-iccid"},
		},
		{
			OperatorIdentifier: "00101",
			Criteria:           apnCriteria{SPN: "ESIM GO", ICCID: "894410"},
			Profile:            apnProfile{APN: "spn-iccid"},
		},
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
			name: "remembered wins over default",
			selection: apnSelection{
				Remembered:         " remembered ",
				OperatorIdentifier: "00101",
			},
			want: "remembered",
		},
		{
			name: "default fallback",
			selection: apnSelection{
				OperatorIdentifier: "00101",
			},
			want: "json",
		},
		{
			name: "gid1 exact match wins over operator default",
			selection: apnSelection{
				OperatorIdentifier: "00101",
				GID1:               " a0 ",
			},
			want: "gid",
		},
		{
			name: "spn exact match wins over operator default",
			selection: apnSelection{
				OperatorIdentifier: "00101",
				SPN:                " eSIM Go ",
			},
			want: "spn",
		},
		{
			name: "iccid prefix match wins over operator default",
			selection: apnSelection{
				OperatorIdentifier: "00101",
				ICCID:              "89441000400308655036",
			},
			want: "iccid",
		},
		{
			name: "imsi wildcard match wins over operator default",
			selection: apnSelection{
				OperatorIdentifier: "00101",
				IMSI:               "21670191",
			},
			want: "imsi",
		},
		{
			name: "gid1 and spn wins over gid1",
			selection: apnSelection{
				OperatorIdentifier: "00101",
				GID1:               "A0",
				SPN:                "eSIM Go",
			},
			want: "gid-spn",
		},
		{
			name: "iccid combination wins over gid1 and spn",
			selection: apnSelection{
				OperatorIdentifier: "00101",
				GID1:               "A0",
				SPN:                "eSIM Go",
				ICCID:              "89441000400308655036",
			},
			want: "gid-iccid",
		},
		{
			name: "missing default keeps empty",
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

func TestPreferencesWithDefaultAPNCredentials(t *testing.T) {
	t.Parallel()

	modem := modemAccess{modem: &mmodem.Modem{Sim: &mmodem.SIM{OperatorIdentifier: "23491"}}}
	prefs := Preferences{APN: "wap.vodafone.co.uk"}

	got := preferencesWithDefaultAPNCredentials(modem, prefs)
	if got.APNUsername != "wap" || got.APNPassword != "wap" || got.APNAuth != "pap" {
		t.Fatalf("preferencesWithDefaultAPNCredentials() = %#v, want Vodafone credentials", got)
	}
	if got.IPType != "ipv4v6" {
		t.Fatalf("preferencesWithDefaultAPNCredentials() IPType = %q, want ipv4v6", got.IPType)
	}

	manual := Preferences{
		APN:         "wap.vodafone.co.uk",
		IPType:      "ipv4",
		APNUsername: "custom",
		APNPassword: "secret",
		APNAuth:     "chap",
	}
	got = preferencesWithDefaultAPNCredentials(modem, manual)
	if got.APNUsername != "custom" || got.APNPassword != "secret" || got.APNAuth != "chap" {
		t.Fatalf("preferencesWithDefaultAPNCredentials() = %#v, want manual credentials", got)
	}
	if got.IPType != "ipv4" {
		t.Fatalf("preferencesWithDefaultAPNCredentials() IPType = %q, want ipv4", got.IPType)
	}

	gid1Modem := modemAccess{modem: &mmodem.Modem{Sim: &mmodem.SIM{OperatorIdentifier: "23415", GID1: "A0"}}}
	gid1Prefs := Preferences{APN: "MY.INTERNET"}
	got = preferencesWithDefaultAPNCredentials(gid1Modem, gid1Prefs)
	if got.APNUsername != "wap" || got.APNPassword != "wap" || got.APNAuth != "pap" {
		t.Fatalf("preferencesWithDefaultAPNCredentials() = %#v, want ASDA credentials", got)
	}
}

func TestPreferencesWithSelectedAPN(t *testing.T) {
	t.Parallel()

	modem := modemAccess{modem: &mmodem.Modem{Sim: &mmodem.SIM{OperatorIdentifier: "23491"}}}

	got := preferencesWithSelectedAPN(modem, Preferences{})
	if got.APN != "wap.vodafone.co.uk" {
		t.Fatalf("preferencesWithSelectedAPN() APN = %q, want Vodafone APN", got.APN)
	}
	if got.APNUsername != "wap" || got.APNPassword != "wap" || got.APNAuth != "pap" {
		t.Fatalf("preferencesWithSelectedAPN() = %#v, want Vodafone credentials", got)
	}
	if got.IPType != "ipv4v6" {
		t.Fatalf("preferencesWithSelectedAPN() IPType = %q, want ipv4v6", got.IPType)
	}

	vodafone := modemAccess{modem: &mmodem.Modem{Sim: &mmodem.SIM{
		Identifier:         "89441000400308655036",
		OperatorIdentifier: "23415",
		GID1:               "E1",
	}}}
	got = preferencesWithSelectedAPN(vodafone, Preferences{})
	if got.APN != "wap.vodafone.co.uk" {
		t.Fatalf("preferencesWithSelectedAPN() APN = %q, want Vodafone APN", got.APN)
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
			name: "default fallback from sim operator",
			modem: &mmodem.Modem{
				Sim: &mmodem.SIM{OperatorIdentifier: "00101"},
			},
			want: "default",
		},
		{
			name: "gid1 exact match from sim",
			modem: &mmodem.Modem{
				Sim: &mmodem.SIM{OperatorIdentifier: "23415", GID1: "A1"},
			},
			want: "MY.INTERNET",
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

			if got := apnForModem(modemAccess{modem: tt.modem}, "", "", tt.remembered); got != tt.want {
				t.Fatalf("apnForModem() = %q, want %q", got, tt.want)
			}
		})
	}
}
