package doctor

import (
	"strconv"
	"strings"
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

func PrintReport(out Renderer, report Report, opts RenderOptions) {
	for idx, section := range report.Sections {
		if opts.UseSectionHeaders {
			if idx > 0 {
				out.Blank()
			}
			if headerOut, ok := out.(sectionHeaderer); ok {
				headerOut.Header(section.Title, "")
			} else {
				out.Summary(section.Title, sectionSummaryParts(section, opts)...)
			}
		} else {
			out.Summary(section.Title, sectionSummaryParts(section, opts)...)
		}
		rows := make([][]string, 0, len(section.Checks))
		for _, check := range section.Checks {
			rows = append(rows, reportRow(check, opts))
		}
		_ = out.Table(reportHeaders(opts), rows)
		if !opts.UseSectionHeaders {
			out.Blank()
		}
	}
}

func PrintSummary(out interface{ Summary(string, ...string) }, report Report, opts RenderOptions) {
	title := opts.SummaryTitle
	if title == "" {
		title = "Summary"
	}
	parts := []string{
		countPart(opts.CountPart, "ok", report.Summary.OK),
		countPart(opts.CountPart, "warn", report.Summary.Warn),
		countPart(opts.CountPart, "fail", report.Summary.Fail),
	}
	if opts.IncludeInfo {
		parts = append(parts, countPart(opts.CountPart, "info", report.Summary.Info))
	}
	out.Summary(title, parts...)
}

func sectionSummaryParts(section Section, opts RenderOptions) []string {
	parts := []string{
		countPart(opts.CountPart, "ok", CountChecks(section.Checks, "ok")),
		countPart(opts.CountPart, "warn", CountChecks(section.Checks, "warn")),
		countPart(opts.CountPart, "fail", CountChecks(section.Checks, "fail")),
	}
	if opts.IncludeInfo {
		parts = append(parts, countPart(opts.CountPart, "info", CountChecks(section.Checks, "info")))
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

func countPart(partFn func(string, int) string, label string, value int) string {
	if partFn != nil {
		return partFn(label, value)
	}
	return label + "=" + strconv.Itoa(value)
}
