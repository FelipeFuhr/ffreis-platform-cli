package app

import (
	"io"
	"strings"

	"github.com/spf13/cobra"
)

type BuildInfo struct {
	Version   string
	Commit    string
	BuildTime string
}

func NewVersionCommand(info BuildInfo) *cobra.Command {
	return &cobra.Command{
		Use:         "version",
		Short:       "Print build information",
		Annotations: map[string]string{LocalCommandAnnotation: "true"},
		Run: func(cmd *cobra.Command, _ []string) {
			_, _ = io.WriteString(cmd.OutOrStdout(), formatBuildInfo(info)+"\n")
		},
	}
}

func formatBuildInfo(info BuildInfo) string {
	v := strings.TrimSpace(info.Version)
	if v == "" {
		v = "dev"
	}
	c := strings.TrimSpace(info.Commit)
	if c == "" {
		c = "unknown"
	}
	t := strings.TrimSpace(info.BuildTime)
	if t == "" {
		t = "unknown"
	}
	return v + " (commit=" + c + " built=" + t + ")"
}
