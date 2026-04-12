package doctor

import (
	"strings"

	sharedoutput "github.com/ffreis/platform-cli/pkg/output"
)

type Renderer interface {
	Summary(title string, parts ...string)
	Table(headers []string, rows [][]string) error
	Blank()
}

type sectionHeaderer interface {
	Header(title, subtitle string)
}

type RenderOptions struct {
	SummaryTitle         string
	IncludeInfo          bool
	CountPart            func(label string, value int) string
	UseSectionHeaders    bool
	TableHeaders         []string
	SeparateHintColumn   bool
	EmptyHintPlaceholder string
	StatusCell           func(string) string
}

func PrintReport(out Renderer, report Report, opts RenderOptions) error {
	for idx, section := range report.Sections {
		renderSectionHeading(out, section, opts, idx)
		if err := out.Table(reportHeaders(opts), reportRows(section.Checks, opts)); err != nil {
			return err
		}
		renderSectionSpacing(out, opts)
	}
	return nil
}

func renderSectionHeading(out Renderer, section Section, opts RenderOptions, index int) {
	if opts.UseSectionHeaders {
		if index > 0 {
			out.Blank()
		}
		if headerOut, ok := out.(sectionHeaderer); ok {
			headerOut.Header(section.Title, "")
			return
		}
	}
	out.Summary(section.Title, sectionSummaryParts(section, opts)...)
}

func reportRows(checks []Check, opts RenderOptions) [][]string {
	rows := make([][]string, 0, len(checks))
	for _, check := range checks {
		rows = append(rows, reportRow(check, opts))
	}
	return rows
}

func renderSectionSpacing(out Renderer, opts RenderOptions) {
	if !opts.UseSectionHeaders {
		out.Blank()
	}
}

func PrintSummary(out interface{ Summary(string, ...string) }, report Report, opts RenderOptions) {
	title := opts.SummaryTitle
	if title == "" {
		title = "Summary"
	}
	parts := []string{
		sharedoutput.CountPartWithFn(opts.CountPart, "ok", report.Summary.OK),
		sharedoutput.CountPartWithFn(opts.CountPart, "warn", report.Summary.Warn),
		sharedoutput.CountPartWithFn(opts.CountPart, "fail", report.Summary.Fail),
	}
	if opts.IncludeInfo {
		parts = append(parts, sharedoutput.CountPartWithFn(opts.CountPart, "info", report.Summary.Info))
	}
	out.Summary(title, parts...)
}

func sectionSummaryParts(section Section, opts RenderOptions) []string {
	parts := []string{
		sharedoutput.CountPartWithFn(opts.CountPart, "ok", CountChecks(section.Checks, "ok")),
		sharedoutput.CountPartWithFn(opts.CountPart, "warn", CountChecks(section.Checks, "warn")),
		sharedoutput.CountPartWithFn(opts.CountPart, "fail", CountChecks(section.Checks, "fail")),
	}
	if opts.IncludeInfo {
		parts = append(parts, sharedoutput.CountPartWithFn(opts.CountPart, "info", CountChecks(section.Checks, "info")))
	}
	return parts
}

func reportHeaders(opts RenderOptions) []string {
	if len(opts.TableHeaders) > 0 {
		return opts.TableHeaders
	}
	if opts.SeparateHintColumn {
		return []string{"Status", "Check", "Detail", "Hint"}
	}
	return []string{"Status", "Check", "Detail"}
}

func reportRow(check Check, opts RenderOptions) []string {
	status := strings.ToUpper(check.Status)
	if opts.StatusCell != nil {
		status = opts.StatusCell(check.Status)
	}
	if opts.SeparateHintColumn {
		hint := check.Hint
		if strings.TrimSpace(hint) == "" {
			hint = opts.EmptyHintPlaceholder
		}
		return []string{status, check.Title, check.Detail, hint}
	}
	detail := check.Detail
	if check.Hint != "" {
		detail += " | hint: " + check.Hint
	}
	return []string{status, check.Title, detail}
}
