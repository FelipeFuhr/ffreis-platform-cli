package ui

import (
	"context"
	"testing"
	"time"
)

func TestResolveMode(t *testing.T) {
	cases := []struct {
		name      string
		requested string
		stdoutTTY bool
		stderrTTY bool
		noColor   bool
		wantMode  string
		wantInter bool
		wantErr   bool
	}{
		{"empty defaults to auto, no tty -> plain", "", false, false, false, ModePlain, false, false},
		{"auto with tty -> rich", "auto", true, false, false, ModeRich, true, false},
		{"auto with tty but NO_COLOR -> plain interactive", "auto", true, false, true, ModePlain, true, false},
		{"explicit plain -> plain interactive", "plain", false, false, false, ModePlain, true, false},
		{"explicit rich -> rich", "rich", false, false, false, ModeRich, true, false},
		{"explicit rich with NO_COLOR -> plain", "rich", false, false, true, ModePlain, true, false},
		{"invalid -> error", "fancy", false, false, false, "", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mode, inter, err := ResolveMode(tc.requested, tc.stdoutTTY, tc.stderrTTY, tc.noColor)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if mode != tc.wantMode || inter != tc.wantInter {
				t.Fatalf("got (%q,%v), want (%q,%v)", mode, inter, tc.wantMode, tc.wantInter)
			}
		})
	}
}

func TestPresenterPlainRendering(t *testing.T) {
	p, err := New(ModePlain)
	if err != nil {
		t.Fatalf("New(plain): %v", err)
	}
	// Explicit plain mode is still interactive (see ResolveMode ModePlain case);
	// interactivity resolution is covered exhaustively by TestResolveMode.
	if p.Rich() {
		t.Errorf("plain presenter should not be rich")
	}
	if got := p.Badge("ok", "Passed"); got != "[passed]" {
		t.Errorf("Badge plain = %q, want [passed]", got)
	}
	if got := p.Badge("unknown-kind", "X"); got != "[x]" {
		t.Errorf("Badge unknown kind should fall back, got %q", got)
	}
	if got := p.Badge("ok", "   "); got != "" {
		t.Errorf("blank label should render empty, got %q", got)
	}
	if got := p.Status("ok", "done", "all green"); got != "[done] all green" {
		t.Errorf("Status = %q", got)
	}
	if got := p.Status("ok", "done", ""); got != "[done]" {
		t.Errorf("Status without detail = %q", got)
	}
	if got := p.Header("Title", ""); got != "Title" {
		t.Errorf("Header no subtitle = %q", got)
	}
	if got := p.Header("Title", "Sub"); got != "Title\nSub" {
		t.Errorf("Header plain with subtitle = %q", got)
	}
	if got := p.Summary("Repos"); got != "Repos" {
		t.Errorf("Summary no parts = %q", got)
	}
	if got := p.Summary("Repos", "a", "  ", "b"); got != "Repos: a  b" {
		t.Errorf("Summary = %q", got)
	}
	if got := p.Key("k"); got != "k" {
		t.Errorf("Key plain = %q", got)
	}
}

func TestDuration(t *testing.T) {
	p, _ := New(ModePlain)
	cases := map[time.Duration]string{
		0:                       "0s",
		-5 * time.Second:        "0s",
		500 * time.Millisecond:  "500ms",
		2500 * time.Millisecond: "2.5s",
	}
	for d, want := range cases {
		if got := p.Duration(d); got != want {
			t.Errorf("Duration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestIsTTYNil(t *testing.T) {
	if IsTTY(nil) {
		t.Errorf("IsTTY(nil) should be false")
	}
}

func TestContextRoundTrip(t *testing.T) {
	p, _ := New(ModePlain)
	ctx := WithPresenter(context.Background(), p)
	if FromContext(ctx) != p {
		t.Errorf("FromContext should return the stored presenter")
	}
	// Missing presenter -> a non-nil plain fallback.
	if FromContext(context.Background()) == nil {
		t.Errorf("FromContext fallback should be non-nil")
	}
}
