package buildinfo

import "testing"

func TestCurrentDefaults(t *testing.T) {
	oldVersion, oldCommit, oldChannel := Version, Commit, Channel
	oldTarget, oldDistribution := Target, Distribution
	t.Cleanup(func() {
		Version, Commit, Channel = oldVersion, oldCommit, oldChannel
		Target, Distribution = oldTarget, oldDistribution
	})

	Version, Commit, Channel = "", "ABCDEF", "unknown"
	Target, Distribution = "", "unknown"
	got := Current()
	if got.Version != "dev" || got.Channel != ChannelStable {
		t.Fatalf("Current() = %+v", got)
	}
	if got.Target == "" || got.Distribution != DistributionDeveloper {
		t.Fatalf("Current() = %+v", got)
	}
	if got.Commit != "abcdef" {
		t.Fatalf("Current().Commit = %q, want abcdef", got.Commit)
	}
}

func TestSelfUpdateSupported(t *testing.T) {
	if !(Info{Distribution: DistributionStandalone}).SelfUpdateSupported() {
		t.Fatal("standalone build should support self-update")
	}
	if (Info{Distribution: DistributionContainer}).SelfUpdateSupported() {
		t.Fatal("container build should not support self-update")
	}
}
