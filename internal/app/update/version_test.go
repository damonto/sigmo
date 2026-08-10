package update

import "testing"

func TestUpdateAvailable(t *testing.T) {
	tests := []struct {
		name           string
		currentChannel string
		currentVersion string
		currentCommit  string
		latest         Manifest
		want           bool
	}{
		{
			name:           "stable upgrade",
			currentChannel: "stable",
			currentVersion: "v1.2.3",
			latest:         Manifest{Channel: "stable", Version: "v1.3.0"},
			want:           true,
		},
		{
			name:           "stable downgrade",
			currentChannel: "stable",
			currentVersion: "v1.3.0",
			latest:         Manifest{Channel: "stable", Version: "v1.2.3"},
			want:           false,
		},
		{
			name:           "stable release after prerelease",
			currentChannel: "stable",
			currentVersion: "v2.0.0-rc.1",
			latest:         Manifest{Channel: "stable", Version: "v2.0.0"},
			want:           true,
		},
		{
			name:           "same dev commit",
			currentChannel: "dev",
			currentVersion: "dev-11111111",
			currentCommit:  "1111111111111111111111111111111111111111",
			latest: Manifest{
				Channel: "dev",
				Version: "dev-11111111",
				Commit:  "1111111111111111111111111111111111111111",
			},
			want: false,
		},
		{
			name:           "new dev commit",
			currentChannel: "dev",
			currentVersion: "dev-11111111",
			currentCommit:  "1111111111111111111111111111111111111111",
			latest: Manifest{
				Channel: "dev",
				Version: "dev-22222222",
				Commit:  "2222222222222222222222222222222222222222",
			},
			want: true,
		},
		{
			name:           "switch dev back to stable",
			currentChannel: "dev",
			currentVersion: "dev-22222222",
			currentCommit:  "2222222222222222222222222222222222222222",
			latest:         Manifest{Channel: "stable", Version: "v1.2.3"},
			want:           true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := updateAvailable(tt.currentChannel, tt.currentVersion, tt.currentCommit, tt.latest); got != tt.want {
				t.Fatalf("updateAvailable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCompareSemver(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want int
	}{
		{name: "without v prefix", a: "1.2.4", b: "v1.2.3", want: 1},
		{name: "large numeric component", a: "v999999999999999999999.0.0", b: "v2.0.0", want: 1},
		{name: "numeric prerelease", a: "v1.0.0-rc.10", b: "v1.0.0-rc.2", want: 1},
		{name: "invalid current", a: "v1.0.0", b: "dev", want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := compareSemver(tt.a, tt.b); got != tt.want {
				t.Fatalf("compareSemver(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
