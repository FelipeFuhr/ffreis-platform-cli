package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/FelipeFuhr/ffreis-platform-cli/pkg/auth"
	"github.com/FelipeFuhr/ffreis-platform-cli/pkg/doctor"
	"github.com/FelipeFuhr/ffreis-platform-cli/pkg/tfexec"
)

// recordingPresenter captures every output call so tests can assert on the
// rendered surface without a terminal.
type recordingPresenter struct {
	buf    bytes.Buffer
	tables [][]string
}

func (p *recordingPresenter) Line(text string) { p.buf.WriteString(text + "\n") }
func (p *recordingPresenter) Blank()           { p.buf.WriteString("\n") }
func (p *recordingPresenter) Header(title, subtitle string) {
	p.buf.WriteString(title + " | " + subtitle + "\n")
}
func (p *recordingPresenter) Summary(title string, parts ...string) {
	p.buf.WriteString(title + ": " + strings.Join(parts, " ") + "\n")
}
func (p *recordingPresenter) Status(kind, label, detail string) {
	p.buf.WriteString("[" + kind + "/" + label + "] " + detail + "\n")
}
func (p *recordingPresenter) Table(headers []string, rows [][]string) error {
	p.tables = append(p.tables, headers)
	p.tables = append(p.tables, rows...)
	return nil
}
func (p *recordingPresenter) String() string { return p.buf.String() }

// fakeTerraform records terraform invocations and returns scripted exit codes.
type fakeTerraform struct {
	initErr  error
	initN    int
	runs     [][]string
	exitCode int
	runErr   error
	stdout   string
}

func (f *fakeTerraform) ensureInit(context.Context, string, string, string, auth.RawCreds) error {
	f.initN++
	return f.initErr
}

func (f *fakeTerraform) run(_ context.Context, opts tfexec.RunOptions) (int, error) {
	f.runs = append(f.runs, opts.Args)
	if f.runErr != nil {
		return -1, f.runErr
	}
	if opts.Stdout != nil && f.stdout != "" {
		_, _ = opts.Stdout.Write([]byte(f.stdout))
	}
	return f.exitCode, nil
}

const testDisplayName = "Test Infra"

// baseConfig returns a config wired to a fake terraform and an isolated
// runtime, with the given presenter as the output surface.
func baseConfig(tf *fakeTerraform, p *recordingPresenter) StandardConfig {
	return StandardConfig{
		DisplayName:  testDisplayName,
		StackDirName: "infra",
		Runtime:      &StandardRuntime{Env: "prod", Region: "us-east-1", Org: "ffreis", AccountID: "123456789012"},
		NewOutput:    func(*cobra.Command) Presenter { return p },
		RepoRoot:     func() (string, error) { return "/repo", nil },
		EnsureInit:   tf.ensureInit,
		RunTerraform: tf.run,
	}
}

func runCommand(t *testing.T, cmd *cobra.Command, args ...string) error {
	t.Helper()
	cmd.SetArgs(args)
	cmd.SetContext(context.Background())
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	return cmd.Execute()
}

func TestRegisterStandardCommandsWiresTheFullSet(t *testing.T) {
	root := &cobra.Command{Use: "test-infra"}
	RegisterStandardCommands(root, baseConfig(&fakeTerraform{}, &recordingPresenter{}))

	want := map[string]bool{"version": false, "plan": false, "apply": false, "outputs": false, "nuke": false, "doctor": false}
	for _, c := range root.Commands() {
		if _, ok := want[c.Name()]; ok {
			want[c.Name()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("standard command %q not registered", name)
		}
	}
}

func TestPlanReportsNoChanges(t *testing.T) {
	tf := &fakeTerraform{exitCode: 0}
	p := &recordingPresenter{}
	if err := runCommand(t, NewPlanCommand(baseConfig(tf, p))); err != nil {
		t.Fatalf("plan returned error: %v", err)
	}
	if tf.initN != 1 {
		t.Errorf("expected one init, got %d", tf.initN)
	}
	if got := p.String(); !strings.Contains(got, "no changes detected") {
		t.Errorf("expected no-changes status, got:\n%s", got)
	}
	if !strings.Contains(p.String(), testDisplayName+" Plan") {
		t.Errorf("expected display-name header, got:\n%s", p.String())
	}
}

func TestPlanReportsChanges(t *testing.T) {
	tf := &fakeTerraform{exitCode: 2}
	p := &recordingPresenter{}
	if err := runCommand(t, NewPlanCommand(baseConfig(tf, p))); err != nil {
		t.Fatalf("plan returned error: %v", err)
	}
	if got := p.String(); !strings.Contains(got, "changes detected") || strings.Contains(got, "no changes") {
		t.Errorf("expected changes-detected status, got:\n%s", got)
	}
}

func TestPlanPropagatesTerraformError(t *testing.T) {
	tf := &fakeTerraform{runErr: errors.New("boom")}
	if err := runCommand(t, NewPlanCommand(baseConfig(tf, &recordingPresenter{}))); err == nil {
		t.Fatal("expected error when terraform fails")
	}
}

func TestApplySucceedsAndRunsPostApply(t *testing.T) {
	tf := &fakeTerraform{exitCode: 0}
	p := &recordingPresenter{}
	cfg := baseConfig(tf, p)
	postCalled := false
	cfg.PostApply = func(_ context.Context, out Presenter, root, stack string) error {
		postCalled = true
		if root != "/repo" || stack != "/repo/infra" {
			t.Errorf("unexpected root/stack: %q %q", root, stack)
		}
		out.Status("ok", "post", "verified")
		return nil
	}
	if err := runCommand(t, NewApplyCommand(cfg)); err != nil {
		t.Fatalf("apply returned error: %v", err)
	}
	if !postCalled {
		t.Error("PostApply hook was not invoked")
	}
	if got := p.String(); !strings.Contains(got, "terraform apply complete") || !strings.Contains(got, "verified") {
		t.Errorf("expected success + post-apply output, got:\n%s", got)
	}
}

func TestApplyAbortsOnDoctorFailure(t *testing.T) {
	tf := &fakeTerraform{exitCode: 0}
	p := &recordingPresenter{}
	cfg := baseConfig(tf, p)
	cfg.DoctorSections = func(context.Context, DoctorMode) ([]doctor.Section, error) {
		return []doctor.Section{{Title: "Contract", Checks: []doctor.Check{
			{Key: "x", Title: "backend", Status: "fail", Blocking: true},
		}}}, nil
	}
	err := runCommand(t, NewApplyCommand(cfg))
	if err == nil || !strings.Contains(err.Error(), "doctor preflight failed") {
		t.Fatalf("expected doctor preflight failure, got: %v", err)
	}
	if len(tf.runs) != 0 {
		t.Errorf("terraform must not run after a failed preflight, ran: %v", tf.runs)
	}
}

func TestApplyPropagatesPostApplyError(t *testing.T) {
	tf := &fakeTerraform{exitCode: 0}
	cfg := baseConfig(tf, &recordingPresenter{})
	wantErr := errors.New("dependency check failed")
	cfg.PostApply = func(context.Context, Presenter, string, string) error { return wantErr }
	if err := runCommand(t, NewApplyCommand(cfg)); !errors.Is(err, wantErr) {
		t.Fatalf("expected post-apply error, got: %v", err)
	}
}

func TestApplyAutoApproveFlagThreadsThrough(t *testing.T) {
	tf := &fakeTerraform{exitCode: 0}
	cfg := baseConfig(tf, &recordingPresenter{})
	if err := runCommand(t, NewApplyCommand(cfg), "--auto-approve"); err != nil {
		t.Fatalf("apply returned error: %v", err)
	}
	found := false
	for _, args := range tf.runs {
		for _, a := range args {
			if a == "-auto-approve" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected -auto-approve in terraform args, got: %v", tf.runs)
	}
}

func TestOutputsRendersTable(t *testing.T) {
	tf := &fakeTerraform{exitCode: 0, stdout: `{"bucket":{"value":"my-bucket","sensitive":false},"secret":{"value":"s","sensitive":true}}`}
	p := &recordingPresenter{}
	if err := runCommand(t, NewOutputsCommand(baseConfig(tf, p))); err != nil {
		t.Fatalf("outputs returned error: %v", err)
	}
	// Header row + two output rows.
	if len(p.tables) != 3 {
		t.Fatalf("expected 3 table rows, got %d: %v", len(p.tables), p.tables)
	}
	// Sorted keys: bucket before secret; secret value masked.
	if p.tables[1][0] != "bucket" || p.tables[1][1] != "my-bucket" {
		t.Errorf("unexpected first row: %v", p.tables[1])
	}
	if p.tables[2][0] != "secret" || p.tables[2][1] != "(sensitive)" {
		t.Errorf("expected masked sensitive value, got: %v", p.tables[2])
	}
}

func TestOutputsJSONPassthrough(t *testing.T) {
	raw := `{"a":{"value":"b","sensitive":false}}`
	tf := &fakeTerraform{exitCode: 0, stdout: raw}
	cfg := baseConfig(tf, &recordingPresenter{})
	cmd := NewOutputsCommand(cfg)
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	if err := runCommand(t, cmd, "--json"); err != nil {
		t.Fatalf("outputs --json returned error: %v", err)
	}
	if !strings.Contains(stdout.String(), raw) {
		t.Errorf("expected raw JSON passthrough, got: %q", stdout.String())
	}
}

func TestOutputsFailsOnNonZeroExit(t *testing.T) {
	tf := &fakeTerraform{exitCode: 1, stdout: "boom"}
	if err := runCommand(t, NewOutputsCommand(baseConfig(tf, &recordingPresenter{}))); err == nil {
		t.Fatal("expected error on non-zero terraform exit")
	}
}

func TestDoctorRendersAndPassesWithoutFailures(t *testing.T) {
	tf := &fakeTerraform{}
	p := &recordingPresenter{}
	cfg := baseConfig(tf, p)
	cfg.DoctorSections = func(_ context.Context, mode DoctorMode) ([]doctor.Section, error) {
		if mode.Name != DoctorModes.Command.Name {
			t.Errorf("unexpected mode: %q", mode.Name)
		}
		return []doctor.Section{{Title: "Contract", Checks: []doctor.Check{
			{Key: "ok", Title: "backend", Status: "ok"},
		}}}, nil
	}
	if err := runCommand(t, NewDoctorCommand(cfg)); err != nil {
		t.Fatalf("doctor returned error: %v", err)
	}
	if !strings.Contains(p.String(), "Integrity Summary") {
		t.Errorf("expected integrity summary, got:\n%s", p.String())
	}
}

func TestDoctorJSONOutput(t *testing.T) {
	cfg := baseConfig(&fakeTerraform{}, &recordingPresenter{})
	cfg.DoctorSections = func(context.Context, DoctorMode) ([]doctor.Section, error) {
		return []doctor.Section{{Title: "Contract", Checks: []doctor.Check{{Key: "k", Title: "t", Status: "ok"}}}}, nil
	}
	cmd := NewDoctorCommand(cfg)
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	if err := runCommand(t, cmd, "--json"); err != nil {
		t.Fatalf("doctor --json returned error: %v", err)
	}
	var report doctor.Report
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("doctor --json emitted invalid JSON: %v", err)
	}
	if report.Mode != DoctorModes.Command.Name || report.Summary.OK != 1 {
		t.Errorf("unexpected report: %+v", report)
	}
}

func TestDoctorGatesOnBlockingFailure(t *testing.T) {
	cfg := baseConfig(&fakeTerraform{}, &recordingPresenter{})
	cfg.DoctorSections = func(context.Context, DoctorMode) ([]doctor.Section, error) {
		return []doctor.Section{{Title: "Contract", Checks: []doctor.Check{{Key: "k", Title: "t", Status: "fail", Blocking: true}}}}, nil
	}
	err := runCommand(t, NewDoctorCommand(cfg))
	if err == nil || !strings.Contains(err.Error(), "blocking issue") {
		t.Fatalf("expected blocking-issue error, got: %v", err)
	}
}

func TestDoctorPropagatesSectionError(t *testing.T) {
	cfg := baseConfig(&fakeTerraform{}, &recordingPresenter{})
	wantErr := errors.New("cannot read backend")
	cfg.DoctorSections = func(context.Context, DoctorMode) ([]doctor.Section, error) { return nil, wantErr }
	if err := runCommand(t, NewDoctorCommand(cfg)); !errors.Is(err, wantErr) {
		t.Fatalf("expected section error, got: %v", err)
	}
}

func TestNukeDestroysAfterConfirmation(t *testing.T) {
	tf := &fakeTerraform{exitCode: 0}
	p := &recordingPresenter{}
	cfg := baseConfig(tf, p)
	cfg.Stdin = strings.NewReader("nuke-prod\n")
	cfg.Stdout = &bytes.Buffer{}
	if err := runCommand(t, NewNukeCommand(cfg)); err != nil {
		t.Fatalf("nuke returned error: %v", err)
	}
	if got := p.String(); !strings.Contains(got, "prod infrastructure destroyed") {
		t.Errorf("expected destroy success, got:\n%s", got)
	}
	sawDestroy := false
	for _, args := range tf.runs {
		if len(args) > 0 && args[0] == "destroy" {
			sawDestroy = true
		}
	}
	if !sawDestroy {
		t.Errorf("expected a terraform destroy run, got: %v", tf.runs)
	}
}

func TestNukeCancelsOnBadConfirmation(t *testing.T) {
	tf := &fakeTerraform{exitCode: 0}
	p := &recordingPresenter{}
	cfg := baseConfig(tf, p)
	cfg.Stdin = strings.NewReader("wrong\n")
	cfg.Stdout = &bytes.Buffer{}
	if err := runCommand(t, NewNukeCommand(cfg)); err != nil {
		t.Fatalf("declined confirmation should not error, got: %v", err)
	}
	if len(tf.runs) != 0 {
		t.Errorf("terraform must not run when confirmation is declined, ran: %v", tf.runs)
	}
	if !strings.Contains(p.String(), "cancelled") {
		t.Errorf("expected cancellation notice, got:\n%s", p.String())
	}
}

func TestNukeInvokesFallbackOnDestroyFailure(t *testing.T) {
	tf := &fakeTerraform{exitCode: 3}
	p := &recordingPresenter{}
	cfg := baseConfig(tf, p)
	cfg.Stdin = strings.NewReader("nuke-prod\n")
	cfg.Stdout = &bytes.Buffer{}
	fallbackCalled := false
	cfg.NukeFallback = func(_ context.Context, _ Presenter, root, stack string, cause error) error {
		fallbackCalled = true
		if cause == nil {
			t.Error("expected a non-nil cause")
		}
		return nil
	}
	if err := runCommand(t, NewNukeCommand(cfg)); err != nil {
		t.Fatalf("nuke with recovering fallback should succeed, got: %v", err)
	}
	if !fallbackCalled {
		t.Error("NukeFallback was not invoked on destroy failure")
	}
}

func TestNukePreflightGatesDestroy(t *testing.T) {
	tf := &fakeTerraform{exitCode: 0}
	cfg := baseConfig(tf, &recordingPresenter{})
	cfg.Stdin = strings.NewReader("nuke-prod\n")
	cfg.Stdout = &bytes.Buffer{}
	cfg.DoctorSections = func(context.Context, DoctorMode) ([]doctor.Section, error) {
		return []doctor.Section{{Title: "c", Checks: []doctor.Check{{Key: "k", Title: "t", Status: "fail", Blocking: true}}}}, nil
	}
	if err := runCommand(t, NewNukeCommand(cfg)); err == nil {
		t.Fatal("expected preflight to gate the destroy")
	}
	if len(tf.runs) != 0 {
		t.Errorf("terraform must not run after a failed preflight, ran: %v", tf.runs)
	}
}

func TestNukeCustomLongHelp(t *testing.T) {
	cfg := baseConfig(&fakeTerraform{}, &recordingPresenter{})
	cfg.NukeLong = "custom nuke docs"
	if got := NewNukeCommand(cfg).Long; got != "custom nuke docs" {
		t.Errorf("expected custom long help, got: %q", got)
	}
	if got := NewNukeCommand(baseConfig(&fakeTerraform{}, &recordingPresenter{})).Long; !strings.Contains(got, "irreversible") {
		t.Errorf("expected default long help, got: %q", got)
	}
}

func TestConfigDefaultsFillInMissingKnobs(t *testing.T) {
	cfg := StandardConfig{DisplayName: "X"}
	if cfg.stackDirName() != tfexec.StackDirName {
		t.Errorf("expected default stack dir %q, got %q", tfexec.StackDirName, cfg.stackDirName())
	}
	if cfg.runtime() == nil {
		t.Error("runtime() must never be nil")
	}
	if cfg.logger() == nil {
		t.Error("logger() must never be nil")
	}
	if cfg.stdin() == nil || cfg.stdout() == nil {
		t.Error("stdin()/stdout() must default to the process streams")
	}
	if cfg.runTerraformFn() == nil || cfg.ensureInitFn() == nil {
		t.Error("terraform seams must default to the real implementations")
	}
}

func TestStackDirJoinsRepoRootAndStackName(t *testing.T) {
	cfg := StandardConfig{StackDirName: "infra", RepoRoot: func() (string, error) { return "/repo", nil }}
	stack, err := cfg.stackDir()
	if err != nil || stack != "/repo/infra" {
		t.Fatalf("unexpected stack dir: %q err=%v", stack, err)
	}
}

func TestStackDirPropagatesRepoRootError(t *testing.T) {
	wantErr := errors.New("no repo")
	cfg := StandardConfig{RepoRoot: func() (string, error) { return "", wantErr }}
	if _, err := cfg.stackDir(); !errors.Is(err, wantErr) {
		t.Fatalf("expected repo-root error, got: %v", err)
	}
}

func TestDefaultNewOutputSatisfiesPresenter(t *testing.T) {
	// A config without NewOutput must still yield a working presenter.
	cfg := StandardConfig{Runtime: &StandardRuntime{Env: "prod"}}
	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	out := cfg.newOutput(cmd)
	out.Header("Title", "sub")
	if !strings.Contains(buf.String(), "Title") {
		t.Errorf("default presenter did not render, got: %q", buf.String())
	}
}

// erroringTablePresenter fails on Table to exercise the render error path.
type erroringTablePresenter struct{ recordingPresenter }

func (p *erroringTablePresenter) Table([]string, [][]string) error { return errors.New("render boom") }

func TestCommandsPropagateRepoRootError(t *testing.T) {
	wantErr := errors.New("not in a repo")
	build := map[string]func(StandardConfig) *cobra.Command{
		"plan":    NewPlanCommand,
		"apply":   NewApplyCommand,
		"outputs": NewOutputsCommand,
		"nuke":    NewNukeCommand,
	}
	for name, newCmd := range build {
		t.Run(name, func(t *testing.T) {
			cfg := baseConfig(&fakeTerraform{}, &recordingPresenter{})
			cfg.RepoRoot = func() (string, error) { return "", wantErr }
			cfg.Stdin = strings.NewReader("nuke-prod\n")
			cfg.Stdout = &bytes.Buffer{}
			if err := runCommand(t, newCmd(cfg)); !errors.Is(err, wantErr) {
				t.Fatalf("%s: expected repo-root error, got: %v", name, err)
			}
		})
	}
}

func TestDoctorReportRenderErrorSurfacesAsStatus(t *testing.T) {
	p := &erroringTablePresenter{}
	cfg := baseConfig(&fakeTerraform{}, &recordingPresenter{})
	cfg.NewOutput = func(*cobra.Command) Presenter { return p }
	cfg.DoctorSections = func(context.Context, DoctorMode) ([]doctor.Section, error) {
		return []doctor.Section{{Title: "c", Checks: []doctor.Check{{Key: "k", Title: "t", Status: "ok"}}}}, nil
	}
	if err := runCommand(t, NewDoctorCommand(cfg)); err != nil {
		t.Fatalf("doctor returned error: %v", err)
	}
	if !strings.Contains(p.String(), "error rendering doctor report") {
		t.Errorf("expected render error surfaced as status, got:\n%s", p.String())
	}
}

func TestLoggerUsesRuntimeLoggerWhenSet(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	cfg := StandardConfig{Runtime: &StandardRuntime{Log: logger}}
	if cfg.logger() != logger {
		t.Error("expected the runtime-provided logger to be used")
	}
}

func TestRunApplyThreadsExtraArgs(t *testing.T) {
	tf := &fakeTerraform{exitCode: 0}
	p := &recordingPresenter{}
	cfg := baseConfig(tf, p)

	if err := RunApply(context.Background(), cfg, p, true, []string{"-var", "domain_name=example.com"}); err != nil {
		t.Fatalf("RunApply returned error: %v", err)
	}

	found := false
	for _, args := range tf.runs {
		for i, a := range args {
			if a == "-var" && i+1 < len(args) && args[i+1] == "domain_name=example.com" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected extra -var args in terraform apply, got: %v", tf.runs)
	}
}

func TestRunApplyAbortsOnDoctorFailureBeforeTerraform(t *testing.T) {
	tf := &fakeTerraform{exitCode: 0}
	p := &recordingPresenter{}
	cfg := baseConfig(tf, p)
	cfg.DoctorSections = func(context.Context, DoctorMode) ([]doctor.Section, error) {
		return []doctor.Section{{Title: "Contract", Checks: []doctor.Check{
			{Key: "x", Title: "backend", Status: "fail", Blocking: true},
		}}}, nil
	}

	err := RunApply(context.Background(), cfg, p, false, []string{"-var", "x=1"})
	if err == nil || !strings.Contains(err.Error(), "doctor preflight failed") {
		t.Fatalf("expected doctor preflight failure, got: %v", err)
	}
	if len(tf.runs) != 0 {
		t.Errorf("terraform must not run after a failed preflight, ran: %v", tf.runs)
	}
}

func TestFormatOutputValue(t *testing.T) {
	cases := []struct {
		name string
		in   tfOutput
		want string
	}{
		{"sensitive", tfOutput{Value: "x", Sensitive: true}, "(sensitive)"},
		{"string", tfOutput{Value: "hello"}, "hello"},
		{"nil", tfOutput{Value: nil}, "(null)"},
		{"number", tfOutput{Value: 42.0}, "42"},
		{"list", tfOutput{Value: []any{"a", "b"}}, `["a","b"]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatOutputValue(tc.in); got != tc.want {
				t.Errorf("formatOutputValue(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
