package doctor

type Check struct {
	Key      string   `json:"key"`
	Title    string   `json:"title"`
	Status   string   `json:"status"`
	Detail   string   `json:"detail"`
	Hint     string   `json:"hint,omitempty"`
	Related  []string `json:"related,omitempty"`
	Blocking bool     `json:"blocking"`
}

type Section struct {
	Title  string  `json:"title"`
	Checks []Check `json:"checks"`
}

type Summary struct {
	OK    int `json:"ok"`
	Warn  int `json:"warn"`
	Fail  int `json:"fail"`
	Info  int `json:"info"`
	Total int `json:"total"`
}

type Report struct {
	Mode     string    `json:"mode"`
	Sections []Section `json:"sections"`
	Summary  Summary   `json:"summary"`
}

func SummarizeSections(sections []Section) Summary {
	var summary Summary
	for _, section := range sections {
		for _, check := range section.Checks {
			summary.Total++
			switch check.Status {
			case "ok":
				summary.OK++
			case "warn":
				summary.Warn++
			case "fail":
				summary.Fail++
			case "info":
				summary.Info++
			}
		}
	}
	return summary
}

func CountChecks(checks []Check, status string) int {
	count := 0
	for _, check := range checks {
		if check.Status == status {
			count++
		}
	}
	return count
}

func (r Report) HasFailures() bool {
	return r.BlockingFailures() > 0
}

func (r Report) BlockingFailures() int {
	count := 0
	for _, section := range r.Sections {
		for _, check := range section.Checks {
			if check.Status == "fail" && check.Blocking {
				count++
			}
		}
	}
	return count
}
