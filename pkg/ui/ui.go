// Package ui provides the shared lipgloss-based terminal Presenter used across
// the platform CLI fleet (guardian, configctl, dynamoctl, runner, orchestrator,
// org, bootstrap, and the infra CLIs). It renders headers, badges, status lines
// and durations, honouring auto/plain/rich modes, TTY detection and NO_COLOR.
//
// This is the canonical home for the Presenter that was previously copy-pasted
// (and drifted into several variants) as internal/ui/ui.go in each consumer.
// Consumers should depend on this package instead of vendoring their own copy.
package ui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

const (
	ModeAuto  = "auto"
	ModePlain = "plain"
	ModeRich  = "rich"
)

type contextKey struct{}

// Presenter renders styled terminal output for the resolved mode.
type Presenter struct {
	mode        string
	interactive bool
	header      lipgloss.Style
	subtle      lipgloss.Style
	key         lipgloss.Style
	badges      map[string]lipgloss.Style
}

// New builds a Presenter for the requested mode (auto/plain/rich), resolving
// auto against the current TTYs and NO_COLOR.
func New(requested string) (*Presenter, error) {
	mode, interactive, err := ResolveMode(requested, IsTTY(os.Stdout), IsTTY(os.Stderr), noColor())
	if err != nil {
		return nil, err
	}

	p := &Presenter{
		mode:        mode,
		interactive: interactive,
		header:      lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75")),
		subtle:      lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
		key:         lipgloss.NewStyle().Foreground(lipgloss.Color("110")),
		badges: map[string]lipgloss.Style{
			"ok":      lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42")),
			"running": lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75")),
			"warn":    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214")),
			"error":   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("203")),
			"muted":   lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
			"info":    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75")),
		},
	}
	if mode == ModePlain {
		p.header = lipgloss.NewStyle()
		p.subtle = lipgloss.NewStyle()
		p.key = lipgloss.NewStyle()
		for k := range p.badges {
			p.badges[k] = lipgloss.NewStyle()
		}
	}

	return p, nil
}

// ResolveMode maps the requested mode + TTY/NO_COLOR signals to a concrete mode
// (plain or rich) and whether the session is interactive.
func ResolveMode(requested string, stdoutTTY, stderrTTY, noColorSet bool) (string, bool, error) {
	mode := strings.ToLower(strings.TrimSpace(requested))
	if mode == "" {
		mode = ModeAuto
	}

	switch mode {
	case ModeAuto:
		interactive := stdoutTTY || stderrTTY
		if noColorSet {
			return ModePlain, interactive, nil
		}
		if interactive {
			return ModeRich, true, nil
		}
		return ModePlain, false, nil
	case ModePlain:
		return ModePlain, true, nil
	case ModeRich:
		if noColorSet {
			return ModePlain, true, nil
		}
		return ModeRich, true, nil
	default:
		return "", false, fmt.Errorf("invalid ui mode %q: must be auto, plain, or rich", requested)
	}
}

// IsTTY reports whether f is a character device (terminal).
func IsTTY(f *os.File) bool {
	if f == nil {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

// WithPresenter stores a Presenter in ctx.
func WithPresenter(ctx context.Context, presenter *Presenter) context.Context {
	return context.WithValue(ctx, contextKey{}, presenter)
}

// FromContext returns the Presenter stored in ctx, or a plain-mode Presenter.
func FromContext(ctx context.Context) *Presenter {
	if presenter, ok := ctx.Value(contextKey{}).(*Presenter); ok && presenter != nil {
		return presenter
	}
	presenter, _ := New(ModePlain)
	return presenter
}

// Interactive reports whether the session is attached to a TTY.
func (p *Presenter) Interactive() bool {
	return p != nil && p.interactive
}

// Rich reports whether styled (rich) rendering is active.
func (p *Presenter) Rich() bool {
	return p != nil && p.mode == ModeRich
}

// Key renders value in the key style.
func (p *Presenter) Key(value string) string {
	return p.render(value, p.key)
}

// Header renders a title with an optional subtitle.
func (p *Presenter) Header(title, subtitle string) string {
	if subtitle == "" {
		return p.render(title, p.header)
	}
	if p.mode == ModeRich {
		return fmt.Sprintf("%s  %s", p.render(title, p.header), p.render(subtitle, p.subtle))
	}
	return fmt.Sprintf("%s\n%s", title, subtitle)
}

// Summary renders "title: part  part  …", dropping blank parts.
func (p *Presenter) Summary(title string, parts ...string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			filtered = append(filtered, part)
		}
	}
	if len(filtered) == 0 {
		return title
	}
	return fmt.Sprintf("%s: %s", p.render(title, p.key), strings.Join(filtered, "  "))
}

// Badge renders a coloured [label] for the given kind (ok/running/warn/error/muted/info).
func (p *Presenter) Badge(kind, label string) string {
	label = strings.ToLower(strings.TrimSpace(label))
	if label == "" {
		return ""
	}
	style, ok := p.badges[kind]
	if !ok {
		style = p.badges["info"]
	}
	if p.mode == ModeRich {
		return style.Render(label)
	}
	return "[" + label + "]"
}

// Status renders a badge followed by an optional detail.
func (p *Presenter) Status(kind, label, detail string) string {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return p.Badge(kind, label)
	}
	return strings.TrimSpace(fmt.Sprintf("%s %s", p.Badge(kind, label), p.render(detail, p.subtle)))
}

// Duration renders d at a human-friendly precision.
func (p *Presenter) Duration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	if d < time.Second {
		return d.Round(10 * time.Millisecond).String()
	}
	return d.Round(100 * time.Millisecond).String()
}

func (p *Presenter) render(value string, style lipgloss.Style) string {
	if p == nil || p.mode != ModeRich {
		return value
	}
	return style.Render(value)
}

func noColor() bool {
	value := strings.TrimSpace(os.Getenv("NO_COLOR"))
	return value != "" && value != "0" && strings.ToLower(value) != "false"
}
