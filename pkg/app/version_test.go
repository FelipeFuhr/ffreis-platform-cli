package app

import "testing"

func TestFormatBuildInfoDefaults(t *testing.T) {
	got := formatBuildInfo(BuildInfo{})
	if got != "dev (commit=unknown built=unknown)" {
		t.Fatalf("unexpected build info: %q", got)
	}
}

func TestFormatBuildInfoUsesProvidedValues(t *testing.T) {
	got := formatBuildInfo(BuildInfo{Version: "1.2.3", Commit: "abc123", BuildTime: "2026-04-08T00:00:00Z"})
	if got != "1.2.3 (commit=abc123 built=2026-04-08T00:00:00Z)" {
		t.Fatalf("unexpected build info: %q", got)
	}
}
