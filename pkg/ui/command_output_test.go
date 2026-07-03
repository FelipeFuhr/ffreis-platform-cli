package ui

import (
	"bytes"
	"strings"
	"testing"
)

func TestCommandOutputPlainFallback(t *testing.T) {
	var out, errBuf bytes.Buffer
	o := NewCommandOutput(nil, &out, &errBuf)

	o.Header("Title", "Sub")
	o.Summary("Context", "a", "  ", "b")
	o.Status("ok", "done", "all green")
	o.Blank()
	o.Line("plain line")
	o.ErrLine("err line")

	got := out.String()
	for _, want := range []string{"Title", "Sub", "Context: a  b", "[done] all green", "plain line"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected output to contain %q, got:\n%s", want, got)
		}
	}
	if !strings.Contains(errBuf.String(), "err line") {
		t.Errorf("expected error stream to contain %q, got: %q", "err line", errBuf.String())
	}
}

func TestCommandOutputStatusWithoutDetailUsesLine(t *testing.T) {
	var out bytes.Buffer
	o := NewCommandOutput(nil, &out, &out)
	o.Status("warn", "skip", "")
	if got := out.String(); got != "[skip] \n" {
		t.Errorf("unexpected plain status line: %q", got)
	}
}

func TestCommandOutputHeaderNoSubtitle(t *testing.T) {
	var out bytes.Buffer
	o := NewCommandOutput(nil, &out, &out)
	o.Header("Title", "")
	if got := out.String(); got != "Title\n" {
		t.Errorf("unexpected header output: %q", got)
	}
}

func TestCommandOutputRichDelegatesToPresenter(t *testing.T) {
	p, err := New(ModeRich)
	if err != nil {
		t.Fatalf("New(rich): %v", err)
	}
	var out bytes.Buffer
	o := NewCommandOutput(p, &out, &out)

	o.Header("Title", "Sub")
	o.Summary("Context", "a")
	o.Status("ok", "done", "green")

	got := out.String()
	if !strings.Contains(got, "Title") || !strings.Contains(got, "Sub") {
		t.Errorf("expected rich header content, got: %q", got)
	}
	if !strings.Contains(got, "green") {
		t.Errorf("expected rich status detail, got: %q", got)
	}
}

func TestCommandOutputTablePlain(t *testing.T) {
	var out bytes.Buffer
	o := NewCommandOutput(nil, &out, &out)
	if err := o.Table([]string{"KEY", "VALUE"}, [][]string{{"a", "1"}, {"b", "2"}}); err != nil {
		t.Fatalf("Table: %v", err)
	}
	got := out.String()
	for _, want := range []string{"KEY", "VALUE", "a", "1", "b", "2"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected table output to contain %q, got:\n%s", want, got)
		}
	}
}

func TestCommandOutputTableStripsANSIInPlainMode(t *testing.T) {
	var out bytes.Buffer
	o := NewCommandOutput(nil, &out, &out)
	styled := "\x1b[1mKEY\x1b[0m"
	if err := o.Table([]string{styled}, [][]string{{"\x1b[31mval\x1b[0m"}}); err != nil {
		t.Fatalf("Table: %v", err)
	}
	if got := out.String(); strings.Contains(got, "\x1b[") {
		t.Errorf("expected ANSI codes to be stripped in plain table output, got: %q", got)
	}
}

func TestCommandOutputTableRichRenders(t *testing.T) {
	p, err := New(ModeRich)
	if err != nil {
		t.Fatalf("New(rich): %v", err)
	}
	var out bytes.Buffer
	o := NewCommandOutput(p, &out, &out)
	if err := o.Table([]string{"KEY", "VALUE"}, [][]string{{"a", "1"}, {"bbbbb", "2"}}); err != nil {
		t.Fatalf("Table: %v", err)
	}
	got := out.String()
	for _, want := range []string{"KEY", "VALUE", "a", "1", "bbbbb", "2"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected rich table output to contain %q, got:\n%s", want, got)
		}
	}
}

func TestCommandOutputSummaryNoParts(t *testing.T) {
	var out bytes.Buffer
	o := NewCommandOutput(nil, &out, &out)
	o.Summary("Title Only")
	if got := out.String(); got != "Title Only\n" {
		t.Errorf("unexpected summary output: %q", got)
	}
}
