package update

import (
	"strings"

	"golang.org/x/mod/semver"
)

func updateAvailable(currentChannel string, currentVersion string, currentCommit string, latest Manifest) bool {
	if latest.Channel != currentChannel {
		return true
	}
	if latest.Channel == "dev" {
		return latest.Commit != "" && latest.Commit != currentCommit
	}
	return compareSemver(latest.Version, currentVersion) > 0
}

func compareSemver(a string, b string) int {
	return semver.Compare(normalizeSemver(a), normalizeSemver(b))
}

func normalizeSemver(version string) string {
	version = strings.TrimSpace(version)
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	return version
}
