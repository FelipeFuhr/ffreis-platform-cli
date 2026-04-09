package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestSummaryFormatsParts(t *testing.T) {
	var buf bytes.Buffer
	out := NewCommandOutput(&buf, &buf)
	out.Summary("Summary", CountPart("ok", 2), "", CountPart("fail", 1))
	if got := buf.String(); !strings.Contains(got, "Summary: ok=2  fail=1") {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestEnvAccountRegionSummary(t *testing.T) {
	if got := EnvAccountRegionSummary("prod", "123", "us-east-1"); got != "env prod  account 123  region us-east-1" {
		t.Fatalf("unexpected summary: %q", got)
	}
}
