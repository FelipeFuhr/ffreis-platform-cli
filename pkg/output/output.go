package output

import (
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
)

type CommandOutput struct {
	out io.Writer
	err io.Writer
}

func NewCommandOutput(out, err io.Writer) *CommandOutput {
	return &CommandOutput{out: out, err: err}
}

func (o *CommandOutput) Line(text string) {
	writeLine(o.out, text)
}

func (o *CommandOutput) Blank() {
	writeLine(o.out, "")
}

func (o *CommandOutput) Header(title, subtitle string) {
	o.Line(title)
	if subtitle != "" {
		o.Line(subtitle)
	}
}

func (o *CommandOutput) Summary(title string, parts ...string) {
	filtered := filterParts(parts)
	if len(filtered) == 0 {
		o.Line(title)
		return
	}
	o.Line(title + ": " + strings.Join(filtered, "  "))
}

func (o *CommandOutput) Status(_kind, label, detail string) {
	o.Line("[" + label + "] " + detail)
}

func (o *CommandOutput) Table(headers []string, rows [][]string) error {
	w := tabwriter.NewWriter(o.out, 0, 0, 2, ' ', 0)
	_, _ = io.WriteString(w, strings.Join(headers, "\t")+"\n")
	for _, row := range rows {
		_, _ = io.WriteString(w, strings.Join(row, "\t")+"\n")
	}
	return w.Flush()
}

func CountPart(label string, value int) string {
	return label + "=" + strconv.Itoa(value)
}

func EnvAccountRegionSummary(env, accountID, region string) string {
	return "env " + env + "  account " + accountID + "  region " + region
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
