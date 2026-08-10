package buildinfo

import (
	"runtime"
	"strings"
)

const (
	EditionCommunity = "community"
	EditionPro       = "pro"

	ChannelStable = "stable"
	ChannelDev    = "dev"

	DistributionStandalone = "standalone"
	DistributionContainer  = "container"
	DistributionDeveloper  = "developer"
)

// These values are set by release builds with -ldflags -X. Keep them as
// strings because cmd/link can only replace string variables.
var (
	Version          string
	Commit           string
	Channel          string
	Edition          string
	Target           string
	Distribution     string
	ReleasePublicKey string
)

type Info struct {
	Version          string `json:"version"`
	Commit           string `json:"commit"`
	Channel          string `json:"channel"`
	Edition          string `json:"edition"`
	Target           string `json:"target"`
	Distribution     string `json:"distribution"`
	ReleasePublicKey string `json:"-"`
}

func Current() Info {
	info := Info{
		Version:          strings.TrimSpace(Version),
		Commit:           strings.ToLower(strings.TrimSpace(Commit)),
		Channel:          strings.ToLower(strings.TrimSpace(Channel)),
		Edition:          strings.ToLower(strings.TrimSpace(Edition)),
		Target:           strings.ToLower(strings.TrimSpace(Target)),
		Distribution:     strings.ToLower(strings.TrimSpace(Distribution)),
		ReleasePublicKey: strings.TrimSpace(ReleasePublicKey),
	}
	if info.Version == "" {
		info.Version = "dev"
	}
	if info.Channel != ChannelDev {
		info.Channel = ChannelStable
	}
	if info.Edition != EditionPro {
		info.Edition = EditionCommunity
	}
	if info.Target == "" {
		info.Target = runtime.GOOS + "-" + runtime.GOARCH
	}
	switch info.Distribution {
	case DistributionStandalone, DistributionContainer, DistributionDeveloper:
	default:
		info.Distribution = DistributionDeveloper
	}
	return info
}

func (i Info) SelfUpdateSupported() bool {
	return i.Distribution == DistributionStandalone
}
