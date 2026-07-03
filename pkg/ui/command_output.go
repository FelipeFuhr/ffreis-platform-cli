package ui

import (
	"io"
	"regexp"
	"strings"
	"text/tabwriter"

	"github.com/charmbracelet/lipgloss"
)

// CommandOutput adapts a *Presenter into the Line/Blank/Header/Summary/Status/
// Table shape consumed by pkg/app's standard commands (app.Presenter). It
// renders a styled lipgloss table when the presenter is in rich mode, and
// falls back to a plain tabwriter otherwise.
//
// This is the canonical home for the output wrapper that was previously
// copy-pasted per consumer alongside their own internal/ui.Presenter.
type CommandOutput struct {
	out io.Writer
	err io.Writer
	ui  *Presenter
}

// NewCommandOutput builds a CommandOutput bound to out/err, styled by
// presenter. A nil presenter degrades to plain-text rendering.
func NewCommandOutput(presenter *Presenter, out, err io.Writer) *CommandOutput {
	return &CommandOutput{out: out, err: err, ui: presenter}
}

// Line writes a single line to the output stream.
func (o *CommandOutput) Line(text string) { writeLine(o.out, text) }

// ErrLine writes a single line to the error stream.
func (o *CommandOutput) ErrLine(text string) { writeLine(o.err, text) }

// Blank writes an empty line to the output stream.
func (o *CommandOutput) Blank() { writeLine(o.out, "") }

// Header renders a title with an optional subtitle.
func (o *CommandOutput) Header(title, subtitle string) {
	if o.ui != nil {
		o.Line(o.ui.Header(title, subtitle))
		return
	}
	o.Line(title)
	if subtitle != "" {
		o.Line(subtitle)
	}
}

// Summary renders "title: part  part  …", dropping blank parts.
func (o *CommandOutput) Summary(title string, parts ...string) {
	if o.ui != nil {
		o.Line(o.ui.Summary(title, parts...))
		return
	}
	filtered := filterParts(parts)
	if len(filtered) == 0 {
		o.Line(title)
		return
	}
	o.Line(title + ": " + strings.Join(filtered, "  "))
}

// Status renders a badge followed by an optional detail.
func (o *CommandOutput) Status(kind, label, detail string) {
	if o.ui != nil {
		o.Line(o.ui.Status(kind, label, detail))
		return
	}
	o.Line("[" + label + "] " + detail)
}

// Table renders headers/rows as a styled table in rich mode, or a plain
// tab-aligned table otherwise.
func (o *CommandOutput) Table(headers []string, rows [][]string) error {
	if o.ui != nil && o.ui.Rich() {
		return o.writeRichTable(headers, rows)
	}

	w := tabwriter.NewWriter(o.out, 0, 0, 2, ' ', 0)
	stripped := make([]string, len(headers))
	for i, h := range headers {
		stripped[i] = stripANSI(h)
	}
	_, _ = io.WriteString(w, strings.Join(stripped, "\t")+"\n")
	for _, row := range rows {
		cells := make([]string, len(row))
		for i, cell := range row {
			cells[i] = stripANSI(cell)
		}
		_, _ = io.WriteString(w, strings.Join(cells, "\t")+"\n")
	}
	return w.Flush()
}

func computeColumnWidths(headers []string, rows [][]string) []int {
	colWidths := make([]int, len(headers))
	for i, h := range headers {
		colWidths[i] = lipgloss.Width(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i >= len(colWidths) {
				break
			}
			if width := lipgloss.Width(cell); width > colWidths[i] {
				colWidths[i] = width
			}
		}
	}
	return colWidths
}

func (o *CommandOutput) writeTableRow(row []string, colWidths []int) {
	for i, cell := range row {
		if i >= len(colWidths) {
			break
		}
		if i > 0 {
			_, _ = io.WriteString(o.out, "  ")
		}
		_, _ = io.WriteString(o.out, cell)
		if padding := colWidths[i] - lipgloss.Width(cell); padding > 0 {
			_, _ = io.WriteString(o.out, strings.Repeat(" ", padding))
		}
	}
	_, _ = io.WriteString(o.out, "\n")
}

func (o *CommandOutput) writeRichTable(headers []string, rows [][]string) error {
	styledHeaders := make([]string, len(headers))
	copy(styledHeaders, headers)
	for i, h := range styledHeaders {
		styledHeaders[i] = o.ui.Key(h)
	}
	colWidths := computeColumnWidths(styledHeaders, rows)
	o.writeTableRow(styledHeaders, colWidths)
	for _, row := range rows {
		o.writeTableRow(row, colWidths)
	}
	return nil
}

func writeLine(w io.Writer, text string) {
	_, _ = io.WriteString(w, text+"\n")
}

func filterParts(parts []string) []string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			filtered = append(filtered, part)
		}
	}
	return filtered
}

var ansiEscapeRE = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string {
	return ansiEscapeRE.ReplaceAllString(s, "")
}
