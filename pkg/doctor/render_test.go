package doctor

import (
	"reflect"
	"testing"
)

type fakeRenderer struct {
	summaries    [][2]any
	headers      [][2]string
	tableHeaders [][]string
	tables       [][][]string
	blanks       int
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
	return nil
}

func (f *fakeRenderer) Blank() { f.blanks++ }

func TestPrintReportAndSummary(t *testing.T) {
	renderer := &fakeRenderer{}
	report := Report{Summary: Summary{OK: 1, Warn: 1, Fail: 1, Info: 1}, Sections: []Section{{Title: "Contract", Checks: []Check{{Status: "ok", Title: "ready", Detail: "yes"}, {Status: "info", Title: "hint", Detail: "optional", Hint: "fine"}}}}}
	PrintReport(renderer, report, RenderOptions{IncludeInfo: true})
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
	PrintReport(renderer, report, RenderOptions{
		UseSectionHeaders:    true,
		SeparateHintColumn:   true,
		EmptyHintPlaceholder: "-",
		TableHeaders:         []string{"STATUS", "CHECK", "DETAIL", "HINT"},
		StatusCell: func(status string) string {
			return "badge:" + status
		},
	})
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
