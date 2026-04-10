package output

import (
	"bytes"
	"fmt"
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

func TestBlank(t *testing.T) {
	var buf bytes.Buffer
	out := NewCommandOutput(&buf, &buf)
	out.Blank()
	if got := buf.String(); got != "\n" {
		t.Fatalf("expected blank line, got: %q", got)
	}
}

func TestLine(t *testing.T) {
	var buf bytes.Buffer
	out := NewCommandOutput(&buf, &buf)
	out.Line("test message")
	if got := buf.String(); got != "test message\n" {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestHeader(t *testing.T) {
	tests := []struct {
		title    string
		subtitle string
		want     string
	}{
		{"Title", "", "Title\n"},
		{"Title", "Subtitle", "Title\nSubtitle\n"},
		{"Important", "Details", "Important\nDetails\n"},
	}
	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			var buf bytes.Buffer
			out := NewCommandOutput(&buf, &buf)
			out.Header(tt.title, tt.subtitle)
			if got := buf.String(); got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestStatus(t *testing.T) {
	var buf bytes.Buffer
	out := NewCommandOutput(&buf, &buf)
	out.Status("ok", "label", "detail message")
	if got := buf.String(); !strings.Contains(got, "[label]") || !strings.Contains(got, "detail message") {
		t.Fatalf("unexpected status output: %q", got)
	}
}

func TestTable(t *testing.T) {
	var buf bytes.Buffer
	out := NewCommandOutput(&buf, &buf)
	headers := []string{"Name", "Value"}
	rows := [][]string{{"foo", "1"}, {"bar", "2"}}
	if err := out.Table(headers, rows); err != nil {
		t.Fatalf("Table() error = %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "Name") || !strings.Contains(got, "Value") {
		t.Fatalf("headers missing from table output: %q", got)
	}
	if !strings.Contains(got, "foo") || !strings.Contains(got, "bar") {
		t.Fatalf("rows missing from table output: %q", got)
	}
}

func TestTableEmpty(t *testing.T) {
	var buf bytes.Buffer
	out := NewCommandOutput(&buf, &buf)
	headers := []string{"Name", "Value"}
	if err := out.Table(headers, nil); err != nil {
		t.Fatalf("Table() error = %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "Name") || !strings.Contains(got, "Value") {
		t.Fatalf("headers missing from empty table: %q", got)
	}
}

func TestCountPartWithFn(t *testing.T) {
	tests := []struct {
		name   string
		partFn func(string, int) string
		label  string
		value  int
		want   string
	}{
		{"nil function", nil, "test", 42, "test=42"},
		{"with function", func(l string, v int) string { return l + ":" + fmt.Sprint(v*2) }, "count", 21, "count:42"},
		{"empty label", nil, "", 5, "=5"},
		{"zero value", nil, "items", 0, "items=0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CountPartWithFn(tt.partFn, tt.label, tt.value)
			if got != tt.want {
				t.Errorf("CountPartWithFn() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSummaryWithEmptyParts(t *testing.T) {
	var buf bytes.Buffer
	out := NewCommandOutput(&buf, &buf)
	out.Summary("Result", "", "", "")
	if got := buf.String(); got != "Result\n" {
		t.Fatalf("expected title-only summary, got: %q", got)
	}
}

func TestSummaryWithMixedParts(t *testing.T) {
	var buf bytes.Buffer
	out := NewCommandOutput(&buf, &buf)
	out.Summary("Status", "active=1", "", "  ", "pending=2")
	got := buf.String()
	if !strings.Contains(got, "Status:") || !strings.Contains(got, "active=1") || !strings.Contains(got, "pending=2") {
		t.Fatalf("unexpected summary output: %q", got)
	}
}
