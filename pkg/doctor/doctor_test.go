package doctor

import "testing"

func TestSummarizeSections(t *testing.T) {
	summary := SummarizeSections([]Section{{Checks: []Check{{Status: "ok"}, {Status: "warn"}, {Status: "fail"}, {Status: "info"}}}})
	if summary.OK != 1 || summary.Warn != 1 || summary.Fail != 1 || summary.Info != 1 || summary.Total != 4 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func TestCountChecks(t *testing.T) {
	checks := []Check{{Status: "ok"}, {Status: "warn"}, {Status: "ok"}}
	if got := CountChecks(checks, "ok"); got != 2 {
		t.Fatalf("CountChecks() = %d, want 2", got)
	}
}

func TestReportFailures(t *testing.T) {
	report := Report{Sections: []Section{{Checks: []Check{{Status: "fail", Blocking: true}, {Status: "fail", Blocking: false}}}}}
	if got := report.BlockingFailures(); got != 1 {
		t.Fatalf("BlockingFailures() = %d, want 1", got)
	}
	if !report.HasFailures() {
		t.Fatal("HasFailures() should be true")
	}
}
