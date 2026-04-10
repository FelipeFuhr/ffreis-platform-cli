package app

import (
	"bytes"
	"testing"
)

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

func TestFormatBuildInfoTrimsWhitespace(t *testing.T) {
	got := formatBuildInfo(BuildInfo{Version: " 1.2.3 ", Commit: " abc123 ", BuildTime: " 2026-04-08T00:00:00Z "})
	if got != "1.2.3 (commit=abc123 built=2026-04-08T00:00:00Z)" {
		t.Fatalf("unexpected build info: %q", got)
	}
}

func TestNewVersionCommandWritesBuildInfo(t *testing.T) {
	cmd := NewVersionCommand(BuildInfo{Version: "1.0.0", Commit: "abc", BuildTime: "2026-04-10T00:00:00Z"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.Run(cmd, nil)
	if got := out.String(); got != "1.0.0 (commit=abc built=2026-04-10T00:00:00Z)\n" {
		t.Fatalf("unexpected output: %q", got)
	}
	if cmd.Annotations[LocalCommandAnnotation] != "true" {
		t.Fatalf("expected local command annotation, got %#v", cmd.Annotations)
	}
}
