package tfaction

import (
	"context"
	"io"

	"github.com/ffreis/platform-cli/pkg/auth"
	"github.com/ffreis/platform-cli/pkg/tfexec"
)

type EnsureInitFunc func(context.Context, string, string, string, auth.RawCreds) error
type RunTerraformFunc func(context.Context, tfexec.RunOptions) (int, error)

type PlanOptions struct {
	Root         string
	Stack        string
	Env          string
	Creds        auth.RawCreds
	Stdin        io.Reader
	Stdout       io.Writer
	Stderr       io.Writer
	ExtraArgs    []string
	EnsureInit   EnsureInitFunc
	RunTerraform RunTerraformFunc
}

type PlanResult struct {
	ExitCode   int
	HasChanges bool
}

type ApplyOptions struct {
	Root         string
	Stack        string
	Env          string
	Creds        auth.RawCreds
	Stdin        io.Reader
	Stdout       io.Writer
	Stderr       io.Writer
	PlanFile     string
	ExtraArgs    []string
	AutoApprove  bool
	EnsureInit   EnsureInitFunc
	RunTerraform RunTerraformFunc
}

type ApplyResult struct {
	ExitCode int
}

func RunPlan(ctx context.Context, opts PlanOptions) (PlanResult, error) {
	ensureInit := opts.EnsureInit
	if ensureInit == nil {
		ensureInit = tfexec.EnsureInit
	}
	runTerraform := opts.RunTerraform
	if runTerraform == nil {
		runTerraform = tfexec.RunTerraform
	}
	if err := ensureInit(ctx, opts.Stack, opts.Root, opts.Env, opts.Creds); err != nil {
		return PlanResult{}, err
	}
	args := append([]string{"plan", "-detailed-exitcode"}, tfexec.VarFileArgs(opts.Stack, opts.Root, opts.Env)...)
	args = append(args, opts.ExtraArgs...)
	code, err := runTerraform(ctx, tfexec.RunOptions{
		StackPath: opts.Stack,
		Args:      args,
		Creds:     opts.Creds,
		Stdin:     opts.Stdin,
		Stdout:    opts.Stdout,
		Stderr:    opts.Stderr,
	})
	if err != nil {
		return PlanResult{}, err
	}
	return PlanResult{ExitCode: code, HasChanges: code == 2}, nil
}

func RunApply(ctx context.Context, opts ApplyOptions) (ApplyResult, error) {
	ensureInit := opts.EnsureInit
	if ensureInit == nil {
		ensureInit = tfexec.EnsureInit
	}
	runTerraform := opts.RunTerraform
	if runTerraform == nil {
		runTerraform = tfexec.RunTerraform
	}
	if err := ensureInit(ctx, opts.Stack, opts.Root, opts.Env, opts.Creds); err != nil {
		return ApplyResult{}, err
	}
	args := []string{"apply"}
	if opts.PlanFile != "" {
		args = append(args, opts.PlanFile)
	} else {
		args = append(args, tfexec.VarFileArgs(opts.Stack, opts.Root, opts.Env)...)
	}
	args = append(args, opts.ExtraArgs...)
	if opts.AutoApprove {
		args = append(args, "-auto-approve")
	}
	code, err := runTerraform(ctx, tfexec.RunOptions{
		StackPath: opts.Stack,
		Args:      args,
		Creds:     opts.Creds,
		Stdin:     opts.Stdin,
		Stdout:    opts.Stdout,
		Stderr:    opts.Stderr,
	})
	if err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{ExitCode: code}, nil
}
