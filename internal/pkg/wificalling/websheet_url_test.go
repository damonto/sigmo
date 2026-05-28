package wificalling

import (
	"net/url"
	"testing"
)

func TestWFCUserActionURL(t *testing.T) {
	tests := []struct {
		name      string
		rawURL    string
		rawData   string
		wantQuery map[string]string
	}{
		{
			name:    "plain query data",
			rawURL:  "https://example.com/nsds",
			rawData: "token=abc",
			wantQuery: map[string]string{
				"token": "abc",
			},
		},
		{
			name:    "keeps existing query data",
			rawURL:  "https://example.com/nsds?existing=1",
			rawData: "token=abc",
			wantQuery: map[string]string{
				"existing": "1",
				"token":    "abc",
			},
		},
		{
			name:    "encoded query blob from nsds",
			rawURL:  "https://attdashboard.wireless.att.com/softphone/primary/reseller/r017",
			rawData: "method%3Dupdate-tc-loc%26devicetype%3Dwfc%26authtoken%3DA%2BB%2FC%3D%3D",
			wantQuery: map[string]string{
				"method":     "update-tc-loc",
				"devicetype": "wfc",
				"authtoken":  "A+B/C==",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, err := url.Parse(wfcUserActionURL(tt.rawURL, tt.rawData))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			query := gotURL.Query()
			for key, want := range tt.wantQuery {
				if got := query.Get(key); got != want {
					t.Fatalf("query[%q] = %q, want %q; URL = %s", key, got, want, gotURL.String())
				}
			}
			if query.Has("method=update-tc-loc&devicetype=wfc") {
				t.Fatalf("query contains encoded blob key: %s", gotURL.String())
			}
		})
	}
}

func TestWFCUserActionData(t *testing.T) {
	tests := []struct {
		name    string
		rawData string
		want    map[string]string
	}{
		{
			name:    "plain query data",
			rawData: "token=abc",
			want: map[string]string{
				"token": "abc",
			},
		},
		{
			name:    "encoded query blob from nsds",
			rawData: "method%3Dupdate-tc-loc%26devicetype%3Dwfc%26authtoken%3DA%2BB%2FC%3D%3D",
			want: map[string]string{
				"method":     "update-tc-loc",
				"devicetype": "wfc",
				"authtoken":  "A+B/C==",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values, err := url.ParseQuery(wfcUserActionData(tt.rawData))
			if err != nil {
				t.Fatalf("ParseQuery() error = %v", err)
			}
			for key, want := range tt.want {
				if got := values.Get(key); got != want {
					t.Fatalf("query[%q] = %q, want %q", key, got, want)
				}
			}
		})
	}
}
