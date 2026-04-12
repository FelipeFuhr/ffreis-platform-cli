package doctor

import (
	"errors"
	"reflect"
	"testing"
)

const errPrintReportFmt = "PrintReport returned error: %v"

type fakeRenderer struct {
	summaries    [][2]any
	headers      [][2]string
	tableHeaders [][]string
	tables       [][][]string
	blanks       int
	tableErr     error
}

type summaryOnlyRenderer struct {
	base *fakeRenderer
}

func (s *summaryOnlyRenderer) Summary(title string, parts ...string) {
	s.base.Summary(title, parts...)
}

func (s *summaryOnlyRenderer) Table(headers []string, rows [][]string) error {
	return s.base.Table(headers, rows)
}

func (s *summaryOnlyRenderer) Blank() {
	s.base.Blank()
}

func (f *fakeRenderer) Summary(title string, parts ...string) {
	f.summaries = append(f.summaries, [2]any{title, parts})
}

func (f *fakeRenderer) Header(title, subtitle string) {
	f.headers = append(f.headers, [2]string{title, subtitle})
}

func (f *fakeRenderer) Table(headers []string, rows [][]string) error {
	f.tableHeaders = append(f.tableHeaders, headers)
	f.tables = append(f.tables, rows)
	return f.tableErr
}

func (f *fakeRenderer) Blank() { f.blanks++ }

func TestPrintReportAndSummary(t *testing.T) {
	renderer := &fakeRenderer{}
	report := Report{Summary: Summary{OK: 1, Warn: 1, Fail: 1, Info: 1}, Sections: []Section{{Title: "Contract", Checks: []Check{{Status: "ok", Title: "ready", Detail: "yes"}, {Status: "info", Title: "hint", Detail: "optional", Hint: "fine"}}}}}
	if err := PrintReport(renderer, report, RenderOptions{IncludeInfo: true}); err != nil {
		t.Fatalf(errPrintReportFmt, err)
	}
	PrintSummary(renderer, report, RenderOptions{SummaryTitle: "Integrity Summary", IncludeInfo: true})
	if len(renderer.summaries) != 2 {
		t.Fatalf("summary calls = %d, want 2", len(renderer.summaries))
	}
	parts, ok := renderer.summaries[0][1].([]string)
	if !ok {
		t.Fatalf("unexpected summary parts type: %T", renderer.summaries[0][1])
	}
	wantParts := []string{"ok=1", "warn=0", "fail=0", "info=1"}
	if !reflect.DeepEqual(parts, wantParts) {
		t.Fatalf("section parts = %#v, want %#v", parts, wantParts)
	}
	if renderer.blanks != 1 {
		t.Fatalf("blank calls = %d, want 1", renderer.blanks)
	}
	if got := renderer.tables[0][1][2]; got != "optional | hint: fine" {
		t.Fatalf("detail = %q, want hint suffix", got)
	}
}

func TestPrintReportWithSectionHeadersAndHintColumn(t *testing.T) {
	renderer := &fakeRenderer{}
	report := Report{Sections: []Section{{Title: "Runtime", Checks: []Check{{Status: "ok", Title: "ready", Detail: "yes"}}}}}
	if err := PrintReport(renderer, report, RenderOptions{
		UseSectionHeaders:    true,
		SeparateHintColumn:   true,
		EmptyHintPlaceholder: "-",
		TableHeaders:         []string{"STATUS", "CHECK", "DETAIL", "HINT"},
		StatusCell: func(status string) string {
			return "badge:" + status
		},
	}); err != nil {
		t.Fatalf(errPrintReportFmt, err)
	}
	if len(renderer.headers) != 1 || renderer.headers[0][0] != "Runtime" {
		t.Fatalf("headers = %#v, want Runtime header", renderer.headers)
	}
	if len(renderer.summaries) != 0 {
		t.Fatalf("summary calls = %d, want 0 in header mode", len(renderer.summaries))
	}
	if !reflect.DeepEqual(renderer.tableHeaders[0], []string{"STATUS", "CHECK", "DETAIL", "HINT"}) {
		t.Fatalf("table headers = %#v", renderer.tableHeaders[0])
	}
	if got := renderer.tables[0][0]; !reflect.DeepEqual(got, []string{"badge:ok", "ready", "yes", "-"}) {
		t.Fatalf("row = %#v", got)
	}
}

func TestPrintReportSectionHeaderFallbackUsesSummary(t *testing.T) {
	base := &fakeRenderer{}
	renderer := &summaryOnlyRenderer{base: base}
	report := Report{Sections: []Section{
		{Title: "Runtime", Checks: []Check{{Status: "ok", Title: "ready", Detail: "yes"}}},
		{Title: "Config", Checks: []Check{{Status: "warn", Title: "missing", Detail: "optional"}}},
	}}
	if err := PrintReport(renderer, report, RenderOptions{UseSectionHeaders: true}); err != nil {
		t.Fatalf(errPrintReportFmt, err)
	}
	if len(base.headers) != 0 {
		t.Fatalf("headers = %#v, want no header calls", base.headers)
	}
	if len(base.summaries) != 2 {
		t.Fatalf("summary calls = %d, want 2", len(base.summaries))
	}
	if base.blanks != 1 {
		t.Fatalf("blank calls = %d, want 1", base.blanks)
	}
	if got := base.summaries[0][0]; got != "Runtime" {
		t.Fatalf("first summary title = %v, want Runtime", got)
	}
	if got := base.summaries[1][0]; got != "Config" {
		t.Fatalf("second summary title = %v, want Config", got)
	}
}

func TestPrintReportReturnsTableError(t *testing.T) {
	wantErr := errors.New("table failed")
	renderer := &fakeRenderer{tableErr: wantErr}
	report := Report{Sections: []Section{{Title: "Contract", Checks: []Check{{Status: "ok", Title: "ready", Detail: "yes"}}}}}
	if err := PrintReport(renderer, report, RenderOptions{}); !errors.Is(err, wantErr) {
		t.Fatalf("PrintReport() error = %v, want %v", err, wantErr)
	}
	if renderer.blanks != 0 {
		t.Fatalf("blank calls = %d, want 0 after table error", renderer.blanks)
	}
}
