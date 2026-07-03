package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/spf13/cobra"

	"github.com/FelipeFuhr/ffreis-platform-cli/pkg/auth"
	"github.com/FelipeFuhr/ffreis-platform-cli/pkg/doctor"
	"github.com/FelipeFuhr/ffreis-platform-cli/pkg/nuke"
	"github.com/FelipeFuhr/ffreis-platform-cli/pkg/output"
	"github.com/FelipeFuhr/ffreis-platform-cli/pkg/tfaction"
	"github.com/FelipeFuhr/ffreis-platform-cli/pkg/tfexec"
)

// Presenter is the output surface the standard commands render through. A
// consumer's rich terminal output (lipgloss-backed) satisfies this interface,
// and the plain-text output.CommandOutput is used when no factory is supplied.
type Presenter interface {
	Line(text string)
	Blank()
	Header(title, subtitle string)
	Summary(title string, parts ...string)
	Status(kind, label, detail string)
	Table(headers []string, rows [][]string) error
}

// StandardRuntime is the post-auth state the standard commands read at RunE
// time. The consumer's AfterAuth callback populates the pointer held in
// StandardConfig.Runtime; every standard command reads the current values from
// it when invoked.
type StandardRuntime struct {
	Env       string
	Region    string
	Org       string
	AccountID string
	CallerARN string
	Creds     auth.RawCreds
	AWSConfig sdkaws.Config
	Log       *slog.Logger
}

// DoctorMode identifies which doctor run is executing: a standalone invocation
// or a preflight embedded in apply/nuke. Consumers switch their section
// composition on the mode name.
type DoctorMode struct {
	Name string
}

// DoctorModes are the canonical doctor invocation modes passed to
// StandardConfig.DoctorSections.
var DoctorModes = struct {
	Command DoctorMode
	Apply   DoctorMode
	Nuke    DoctorMode
}{
	Command: DoctorMode{Name: "doctor"},
	Apply:   DoctorMode{Name: "apply-preflight"},
	Nuke:    DoctorMode{Name: "nuke-preflight"},
}

// DoctorSectionsFunc builds the repo-specific doctor sections for a mode. The
// standard doctor command (and apply/nuke preflight) assemble these into a
// report, summarise, render, and gate on blocking failures.
type DoctorSectionsFunc func(ctx context.Context, mode DoctorMode) ([]doctor.Section, error)

// PostApplyFunc runs repo-specific verification after a successful terraform
// apply and before the final success status. Returning an error fails the
// command. root and stack are the resolved repo root and stack directory.
type PostApplyFunc func(ctx context.Context, out Presenter, root, stack string) error

// NukeFallbackFunc runs repo-specific SDK cleanup after a terraform destroy
// fails, mirroring the tagged-resource fallback used by the infra repos.
type NukeFallbackFunc func(ctx context.Context, out Presenter, root, stack string, cause error) error

// StandardConfig carries the repo-specific knobs that vary across the infra
// repos while the command skeletons stay shared. Only DisplayName and Runtime
// are always required; every other field has a sensible default so a minimal
// consumer wires the standard set in a few lines.
type StandardConfig struct {
	// DisplayName prefixes command headers, e.g. "Flemming Infra".
	DisplayName string
	// StackDirName is the terraform stack directory relative to the repo root.
	// Defaults to tfexec.StackDirName when empty.
	StackDirName string
	// Runtime is the post-auth state the consumer's AfterAuth populates.
	Runtime *StandardRuntime
	// BuildInfo backs the version command.
	BuildInfo BuildInfo
	// NewOutput builds the presenter for a command. Defaults to the plain-text
	// output.CommandOutput bound to the command's stdout/stderr.
	NewOutput func(cmd *cobra.Command) Presenter

	// DoctorSections supplies the repo's doctor sections. When nil, doctor and
	// the apply/nuke preflight report zero checks (always passing).
	DoctorSections DoctorSectionsFunc
	// PostApply runs after a successful apply (optional).
	PostApply PostApplyFunc
	// NukeFallback runs after a failed destroy (optional).
	NukeFallback NukeFallbackFunc
	// NukeLong overrides the nuke command's long help (optional).
	NukeLong string

	// Stdin and Stdout are the streams terraform subprocesses and the nuke
	// confirmation prompt read/write. Default to os.Stdin / os.Stdout.
	Stdin  io.Reader
	Stdout io.Writer

	// Terraform seams. All optional; default to the real pkg/tfexec
	// implementations. Primarily injection points for tests.
	RepoRoot     func() (string, error)
	RunTerraform func(context.Context, tfexec.RunOptions) (int, error)
	EnsureInit   func(context.Context, string, string, string, auth.RawCreds) error
}

// RegisterStandardCommands wires the canonical infra-repo lifecycle commands
// (version, plan, apply, outputs, nuke, doctor) onto root using cfg. Repos that
// need a bespoke variant of one command (e.g. platform-org's backup-aware nuke)
// omit it by registering the individual builders they want plus their own
// command, instead of calling this helper.
func RegisterStandardCommands(root *cobra.Command, cfg StandardConfig) {
	root.AddCommand(
		NewVersionCommand(cfg.BuildInfo),
		NewPlanCommand(cfg),
		NewApplyCommand(cfg),
		NewOutputsCommand(cfg),
		NewNukeCommand(cfg),
		NewDoctorCommand(cfg),
	)
}

// --- config accessors (normalise optional fields to working defaults) ---

func (c StandardConfig) runtime() *StandardRuntime {
	if c.Runtime != nil {
		return c.Runtime
	}
	return &StandardRuntime{}
}

func (c StandardConfig) logger() *slog.Logger {
	if rt := c.runtime(); rt.Log != nil {
		return rt.Log
	}
	return slog.Default()
}

func (c StandardConfig) stackDirName() string {
	if c.StackDirName != "" {
		return c.StackDirName
	}
	return tfexec.StackDirName
}

func (c StandardConfig) repoRoot() (string, error) {
	if c.RepoRoot != nil {
		return c.RepoRoot()
	}
	return tfexec.RepoRoot()
}

func (c StandardConfig) stackDir() (string, error) {
	root, err := c.repoRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, c.stackDirName()), nil
}

func (c StandardConfig) runTerraformFn() tfaction.RunTerraformFunc {
	if c.RunTerraform != nil {
		return c.RunTerraform
	}
	return tfexec.RunTerraform
}

func (c StandardConfig) ensureInitFn() tfaction.EnsureInitFunc {
	if c.EnsureInit != nil {
		return c.EnsureInit
	}
	return tfexec.EnsureInit
}

func (c StandardConfig) stdin() io.Reader {
	if c.Stdin != nil {
		return c.Stdin
	}
	return os.Stdin
}

func (c StandardConfig) stdout() io.Writer {
	if c.Stdout != nil {
		return c.Stdout
	}
	return os.Stdout
}

func (c StandardConfig) newOutput(cmd *cobra.Command) Presenter {
	if c.NewOutput != nil {
		return c.NewOutput(cmd)
	}
	return output.NewCommandOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())
}

func (c StandardConfig) headerSummary() string {
	rt := c.runtime()
	return output.EnvAccountRegionSummary(rt.Env, rt.AccountID, rt.Region)
}

// --- doctor helpers (shared by the doctor command and apply/nuke preflight) ---

func (c StandardConfig) runDoctor(ctx context.Context, mode DoctorMode) (doctor.Report, error) {
	var sections []doctor.Section
	if c.DoctorSections != nil {
		built, err := c.DoctorSections(ctx, mode)
		if err != nil {
			return doctor.Report{}, err
		}
		sections = built
	}
	report := doctor.Report{Mode: mode.Name, Sections: sections}
	report.Summary = doctor.SummarizeSections(sections)
	return report, nil
}

func (c StandardConfig) printDoctorReport(out Presenter, report doctor.Report) {
	if err := doctor.PrintReport(out, report, doctor.RenderOptions{IncludeInfo: true, CountPart: output.CountPart}); err != nil {
		out.Status("warn", "warn", "error rendering doctor report: "+err.Error())
	}
}

func (c StandardConfig) printDoctorSummary(out Presenter, report doctor.Report) {
	doctor.PrintSummary(out, report, doctor.RenderOptions{SummaryTitle: "Integrity Summary", IncludeInfo: true, CountPart: output.CountPart})
}

// runDoctorPreflight runs the given preflight mode, prints its summary, and
// returns an error if the report has blocking failures.
func (c StandardConfig) runDoctorPreflight(ctx context.Context, out Presenter, mode DoctorMode) error {
	report, err := c.runDoctor(ctx, mode)
	if err != nil {
		return fmt.Errorf("doctor preflight: %w", err)
	}
	c.printDoctorSummary(out, report)
	if report.HasFailures() {
		out.Blank()
		c.printDoctorReport(out, report)
		return fmt.Errorf("doctor preflight failed with %d blocking issue(s)", report.BlockingFailures())
	}
	return nil
}

// --- command builders ---

// NewPlanCommand builds the standard `plan` command: run terraform plan for the
// active environment and report whether changes were detected.
func NewPlanCommand(cfg StandardConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "plan",
		Short: "Run terraform plan for the given environment",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cfg.newOutput(cmd)
			rt := cfg.runtime()

			root, err := cfg.repoRoot()
			if err != nil {
				return err
			}
			stack, err := cfg.stackDir()
			if err != nil {
				return err
			}

			out.Header(cfg.DisplayName+" Plan", cfg.headerSummary())
			out.Summary("Context", "stack="+stack)
			out.Blank()

			cfg.logger().Info("running terraform plan", "env", rt.Env, "stack", stack)
			result, err := tfaction.RunPlan(ctx, tfaction.PlanOptions{
				Root:         root,
				Stack:        stack,
				Env:          rt.Env,
				Creds:        rt.Creds,
				Stdin:        cfg.stdin(),
				EnsureInit:   cfg.ensureInitFn(),
				RunTerraform: cfg.runTerraformFn(),
			})
			if err != nil {
				return fmt.Errorf("terraform plan: %w", err)
			}

			if result.HasChanges {
				cfg.logger().Info("plan complete: changes detected")
				out.Blank()
				out.Status("warn", "plan", "terraform plan complete; changes detected")
				return nil
			}
			cfg.logger().Info("plan complete: no changes")
			out.Blank()
			out.Status("ok", "ok", "terraform plan complete; no changes detected")
			return nil
		},
	}
}

const applyLong = `apply runs terraform apply, creating or updating all managed infrastructure.

State is stored in the bootstrap-managed S3 bucket and locked via DynamoDB.
A doctor preflight runs first; a blocking failure aborts before any change.`

// RunApply runs the doctor-gated terraform apply flow used by the standard
// apply command: header/summary, doctor preflight, terraform apply (with the
// given extraArgs appended), and the optional PostApply verification.
//
// Exported so a repo-specific command that needs extra terraform arguments
// (e.g. a one-time -var override during a domain migration) can reuse the same
// orchestration instead of re-implementing the doctor-preflight-and-gate
// pattern locally.
func RunApply(ctx context.Context, cfg StandardConfig, out Presenter, autoApprove bool, extraArgs []string) error {
	rt := cfg.runtime()

	root, err := cfg.repoRoot()
	if err != nil {
		return err
	}
	stack, err := cfg.stackDir()
	if err != nil {
		return err
	}

	out.Header(cfg.DisplayName+" Apply", cfg.headerSummary())
	out.Summary("Context", "stack="+stack, "auto-approve="+strconv.FormatBool(autoApprove))
	out.Blank()

	out.Status("info", "doctor", "running "+cfg.DisplayName+" preflight checks")
	if err := cfg.runDoctorPreflight(ctx, out, DoctorModes.Apply); err != nil {
		return err
	}
	out.Blank()

	cfg.logger().Info("running terraform apply", "env", rt.Env, "auto_approve", autoApprove)
	result, err := tfaction.RunApply(ctx, tfaction.ApplyOptions{
		Root:         root,
		Stack:        stack,
		Env:          rt.Env,
		Creds:        rt.Creds,
		Stdin:        cfg.stdin(),
		AutoApprove:  autoApprove,
		ExtraArgs:    extraArgs,
		EnsureInit:   cfg.ensureInitFn(),
		RunTerraform: cfg.runTerraformFn(),
	})
	if err != nil {
		out.Status("info", "hint", "run the doctor command before retrying to verify backend and input wiring")
		return fmt.Errorf("terraform apply: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("terraform apply exited with code %d", result.ExitCode)
	}

	if cfg.PostApply != nil {
		if err := cfg.PostApply(ctx, out, root, stack); err != nil {
			return err
		}
	}

	cfg.logger().Info("apply complete")
	out.Blank()
	out.Status("ok", "ok", "terraform apply complete")
	return nil
}

// NewApplyCommand builds the standard `apply` command: doctor preflight, then
// terraform apply, then the optional repo-specific PostApply verification.
func NewApplyCommand(cfg StandardConfig) *cobra.Command {
	var autoApprove bool
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Provision all infrastructure for the given environment",
		Long:  applyLong,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return RunApply(cmd.Context(), cfg, cfg.newOutput(cmd), autoApprove, nil)
		},
	}
	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "Skip interactive approval")
	return cmd
}

// tfOutput is the shape of each entry in `terraform output -json`.
type tfOutput struct {
	Value     any  `json:"value"`
	Type      any  `json:"type"`
	Sensitive bool `json:"sensitive"`
}

// NewOutputsCommand builds the standard `outputs` command: print terraform
// outputs as a table, or raw JSON with --json.
func NewOutputsCommand(cfg StandardConfig) *cobra.Command {
	var outputsJSON bool
	cmd := &cobra.Command{
		Use:   "outputs",
		Short: "Print Terraform outputs for the given environment",
		Long: `outputs runs terraform output -json and prints the results as a table.

Use --json to emit raw JSON suitable for scripting or secret configuration.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cfg.newOutput(cmd)
			rt := cfg.runtime()

			root, err := cfg.repoRoot()
			if err != nil {
				return err
			}
			stack, err := cfg.stackDir()
			if err != nil {
				return err
			}

			if err := cfg.ensureInitFn()(ctx, stack, root, rt.Env, rt.Creds); err != nil {
				return fmt.Errorf("terraform init: %w", err)
			}

			var stdout, stderr bytes.Buffer
			code, err := cfg.runTerraformFn()(ctx, tfexec.RunOptions{
				StackPath: stack,
				Args:      []string{"output", "-json"},
				Creds:     rt.Creds,
				Stdout:    &stdout,
				Stderr:    &stderr,
			})
			if err != nil {
				return fmt.Errorf("terraform output: %w", err)
			}
			if code != 0 {
				return fmt.Errorf("terraform output failed: %s", tfexec.TerraformCommandError(stdout.String(), stderr.String()))
			}

			var raw map[string]tfOutput
			if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
				return fmt.Errorf("parsing terraform output: %w", err)
			}

			if outputsJSON {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), stdout.String())
				return nil
			}

			keys := make([]string, 0, len(raw))
			for k := range raw {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			out.Header(cfg.DisplayName+" Outputs", cfg.headerSummary())
			out.Blank()

			rows := make([][]string, 0, len(keys))
			for _, k := range keys {
				rows = append(rows, []string{k, formatOutputValue(raw[k])})
			}
			return out.Table([]string{"OUTPUT", "VALUE"}, rows)
		},
	}
	cmd.Flags().BoolVar(&outputsJSON, "json", false, "Emit raw JSON output")
	return cmd
}

func formatOutputValue(o tfOutput) string {
	if o.Sensitive {
		return "(sensitive)"
	}
	switch v := o.Value.(type) {
	case string:
		return v
	case nil:
		return "(null)"
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(b)
	}
}

const defaultNukeLong = `nuke initialises terraform and runs destroy -auto-approve.

This is irreversible. State is stored in the bootstrap-managed S3 bucket;
destroying the bootstrap layer before running nuke prevents clean teardown.`

// NewNukeCommand builds the standard `nuke` command: doctor preflight, then a
// confirmed terraform destroy, with an optional SDK fallback on failure.
func NewNukeCommand(cfg StandardConfig) *cobra.Command {
	long := cfg.NukeLong
	if long == "" {
		long = defaultNukeLong
	}
	return &cobra.Command{
		Use:   "nuke",
		Short: "Destroy all infrastructure for the given environment (IRREVERSIBLE)",
		Long:  long,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cfg.newOutput(cmd)
			rt := cfg.runtime()

			root, err := cfg.repoRoot()
			if err != nil {
				return err
			}
			stack, err := cfg.stackDir()
			if err != nil {
				return err
			}

			out.Header(cfg.DisplayName+" Nuke", cfg.headerSummary())
			out.Blank()
			out.Status("warn", "warn", fmt.Sprintf("this will DESTROY all %s infrastructure", rt.Env))
			out.Status("info", "doctor", "running "+cfg.DisplayName+" preflight checks")
			if err := cfg.runDoctorPreflight(ctx, out, DoctorModes.Nuke); err != nil {
				return err
			}
			out.Blank()

			opts := nuke.DestroyOptions{
				Root:          root,
				Stack:         stack,
				Env:           rt.Env,
				Creds:         rt.Creds,
				ConfirmReader: cfg.stdin(),
				ConfirmWriter: cfg.stdout(),
				Stdin:         cfg.stdin(),
				Init:          cfg.EnsureInit,
				RunTerraform:  cfg.RunTerraform,
			}
			if cfg.NukeFallback != nil {
				opts.OnFailure = func(ctx context.Context, cause error) error {
					out.Status("warn", "warn", cause.Error())
					out.Status("info", "hint", "attempting SDK fallback cleanup for resources still tagged to this stack")
					return cfg.NukeFallback(ctx, out, root, stack, cause)
				}
			}
			if err := nuke.RunDestroy(ctx, opts); err != nil {
				if errors.Is(err, nuke.ErrConfirmationDeclined) {
					out.Status("muted", "skip", "confirmation did not match; cancelled")
					return nil
				}
				return err
			}

			out.Blank()
			out.Status("ok", "ok", fmt.Sprintf("%s infrastructure destroyed", rt.Env))
			return nil
		},
	}
}

const doctorLong = `doctor runs read-only checks against the local Terraform contract used by
this stack: backend files, environment inputs, fetched platform config, and the
current workspace initialisation state.

This command does not create, modify, or delete AWS resources.`

// NewDoctorCommand builds the standard `doctor` command: assemble the repo's
// sections into a report, render it (or emit JSON with --json), and gate on
// blocking failures.
func NewDoctorCommand(cfg StandardConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Validate backend, inputs, and Terraform workspace wiring",
		Long:  doctorLong,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			jsonOut, _ := cmd.Flags().GetBool("json")
			out := cfg.newOutput(cmd)
			rt := cfg.runtime()

			report, err := cfg.runDoctor(ctx, DoctorModes.Command)
			if err != nil {
				return err
			}

			if jsonOut {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(report)
			}

			out.Header(cfg.DisplayName+" Doctor", cfg.headerSummary())
			out.Summary("Context", "org="+rt.Org, "mode="+report.Mode)
			out.Blank()
			cfg.printDoctorReport(out, report)
			out.Blank()
			cfg.printDoctorSummary(out, report)
			if report.HasFailures() {
				return fmt.Errorf("doctor found %d blocking issue(s)", report.BlockingFailures())
			}
			return nil
		},
	}
	cmd.Flags().Bool("json", false, "output the doctor report as JSON")
	return cmd
}
