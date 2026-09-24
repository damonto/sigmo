package locale

import "testing"

func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want Tag
	}{
		{name: "english", in: "en", want: English},
		{name: "english region", in: "en-GB", want: English},
		{name: "chinese", in: "zh", want: Chinese},
		{name: "chinese region with odd casing and spaces", in: " zh-CN ", want: Chinese},
		{name: "traditional chinese follows the web ui", in: "zh-Hant-TW", want: Chinese},
		{name: "unsupported language", in: "fr-FR", want: ""},
		{name: "empty", in: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := Parse(tt.in); got != tt.want {
				t.Fatalf("Parse(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
