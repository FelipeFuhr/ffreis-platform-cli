package tfaction

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/ffreis/platform-cli/pkg/auth"
	"github.com/ffreis/platform-cli/pkg/tfexec"
)

const (
	testStackPath          = "/repo/stack"
	errUnexpectedResultFmt = "unexpected result: %+v"
	errArgsMismatchFmt     = "args = %#v, want %#v"
	testPlanFile           = "../envs/prod/tfplan"
	errRunApplyFmt         = "RunApply() error = %v"
)

func TestRunPlanBuildsExpectedArgs(t *testing.T) {
	var captured tfexec.RunOptions
	result, err := RunPlan(context.Background(), PlanOptions{
		Root:  "/repo",
		Stack: testStackPath,
		Env:   "prod",
		Creds: auth.RawCreds{},
		EnsureInit: func(context.Context, string, string, string, auth.RawCreds) error {
			return nil
		},
		RunTerraform: func(_ context.Context, opts tfexec.RunOptions) (int, error) {
			captured = opts
			return 2, nil
		},
		ExtraArgs: []string{"-out=tfplan"},
	})
	if err != nil {
		t.Fatalf("RunPlan() error = %v", err)
	}
	if !result.HasChanges || result.ExitCode != 2 {
		t.Fatalf(errUnexpectedResultFmt, result)
	}
	wantArgs := []string{"plan", "-detailed-exitcode", "-var-file=../envs/prod/terraform.tfvars", "-out=tfplan"}
	if !reflect.DeepEqual(captured.Args, wantArgs) {
		t.Fatalf(errArgsMismatchFmt, captured.Args, wantArgs)
	}
}

func TestRunApplyUsesPlanFile(t *testing.T) {
	var captured tfexec.RunOptions
	result, err := RunApply(context.Background(), ApplyOptions{
		Root:        "/repo",
		Stack:       testStackPath,
		Env:         "prod",
		Creds:       auth.RawCreds{},
		PlanFile:    testPlanFile,
		AutoApprove: true,
		EnsureInit: func(context.Context, string, string, string, auth.RawCreds) error {
			return nil
		},
		RunTerraform: func(_ context.Context, opts tfexec.RunOptions) (int, error) {
			captured = opts
			return 0, nil
		},
	})
	if err != nil {
		t.Fatalf(errRunApplyFmt, err)
	}
	if result.ExitCode != 0 {
		t.Fatalf(errUnexpectedResultFmt, result)
	}
	wantArgs := []string{"apply", testPlanFile, "-auto-approve"}
	if !reflect.DeepEqual(captured.Args, wantArgs) {
		t.Fatalf(errArgsMismatchFmt, captured.Args, wantArgs)
	}
}

func TestRunPlanEnsureInitError(t *testing.T) {
	wantErr := errors.New("init failed")
	_, err := RunPlan(context.Background(), PlanOptions{
		EnsureInit: func(context.Context, string, string, string, auth.RawCreds) error {
			return wantErr
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected init error, got %v", err)
	}
}

func TestRunPlanRunTerraformError(t *testing.T) {
	wantErr := errors.New("terraform failed")
	_, err := RunPlan(context.Background(), PlanOptions{
		Root:  "/repo",
		Stack: testStackPath,
		Env:   "prod",
		EnsureInit: func(context.Context, string, string, string, auth.RawCreds) error {
			return nil
		},
		RunTerraform: func(context.Context, tfexec.RunOptions) (int, error) {
			return 1, wantErr
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected terraform error, got %v", err)
	}
}

func TestRunApplyWithoutPlanFileUsesVarArgs(t *testing.T) {
	var captured tfexec.RunOptions
	result, err := RunApply(context.Background(), ApplyOptions{
		Root:  "/repo",
		Stack: testStackPath,
		Env:   "prod",
		EnsureInit: func(context.Context, string, string, string, auth.RawCreds) error {
			return nil
		},
		RunTerraform: func(_ context.Context, opts tfexec.RunOptions) (int, error) {
			captured = opts
			return 0, nil
		},
		ExtraArgs: []string{"-lock=false"},
	})
	if err != nil {
		t.Fatalf(errRunApplyFmt, err)
	}
	if result.ExitCode != 0 {
		t.Fatalf(errUnexpectedResultFmt, result)
	}
	wantArgs := []string{"apply", "-var-file=../envs/prod/terraform.tfvars", "-lock=false"}
	if !reflect.DeepEqual(captured.Args, wantArgs) {
		t.Fatalf(errArgsMismatchFmt, captured.Args, wantArgs)
	}
}

func TestRunPlanWithNoChanges(t *testing.T) {
	// Exit code 0 means no changes
	result, err := RunPlan(context.Background(), PlanOptions{
		Root:  "/repo",
		Stack: testStackPath,
		Env:   "prod",
		Creds: auth.RawCreds{},
		EnsureInit: func(context.Context, string, string, string, auth.RawCreds) error {
			return nil
		},
		RunTerraform: func(context.Context, tfexec.RunOptions) (int, error) {
			return 0, nil
		},
	})
	if err != nil {
		t.Fatalf("RunPlan() error = %v", err)
	}
	if result.HasChanges {
		t.Fatalf("expected HasChanges=false for exit code 0")
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected ExitCode=0, got %d", result.ExitCode)
	}
}

func TestRunApplyWithAutoApproveFalse(t *testing.T) {
	var captured tfexec.RunOptions
	result, err := RunApply(context.Background(), ApplyOptions{
		Root:        "/repo",
		Stack:       testStackPath,
		Env:         "prod",
		Creds:       auth.RawCreds{},
		PlanFile:    testPlanFile,
		AutoApprove: false, // Explicitly false
		EnsureInit: func(context.Context, string, string, string, auth.RawCreds) error {
			return nil
		},
		RunTerraform: func(_ context.Context, opts tfexec.RunOptions) (int, error) {
			captured = opts
			return 0, nil
		},
	})
	if err != nil {
		t.Fatalf(errRunApplyFmt, err)
	}
	if result.ExitCode != 0 {
		t.Fatalf(errUnexpectedResultFmt, result)
	}
	wantArgs := []string{"apply", testPlanFile}
	if !reflect.DeepEqual(captured.Args, wantArgs) {
		t.Fatalf("args = %#v, want %#v (should not have -auto-approve)", captured.Args, wantArgs)
	}
}

func TestRunApplyEnsureInitError(t *testing.T) {
	wantErr := errors.New("init failed")
	_, err := RunApply(context.Background(), ApplyOptions{
		EnsureInit: func(context.Context, string, string, string, auth.RawCreds) error {
			return wantErr
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected init error, got %v", err)
	}
}

func TestRunApplyRunTerraformError(t *testing.T) {
	wantErr := errors.New("terraform failed")
	_, err := RunApply(context.Background(), ApplyOptions{
		Root:  "/repo",
		Stack: testStackPath,
		Env:   "prod",
		EnsureInit: func(context.Context, string, string, string, auth.RawCreds) error {
			return nil
		},
		RunTerraform: func(context.Context, tfexec.RunOptions) (int, error) {
			return 1, wantErr
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected terraform error, got %v", err)
	}
}

func TestRunPlanUsesDefaultFunctions(t *testing.T) {
	// Test that when EnsureInit and RunTerraform are nil, defaults are used
	_, err := RunPlan(context.Background(), PlanOptions{
		Root:  "/repo",
		Stack: testStackPath,
		Env:   "prod",
		Creds: auth.RawCreds{},
		// EnsureInit: nil - should use default
		// RunTerraform: nil - should use default
	})
	// This will fail because we don't have actual terraform binary, but the nil-check logic is executed
	if err == nil {
		t.Fatal("expected error when terraform is not available")
	}
}

func TestRunApplyUsesDefaultFunctions(t *testing.T) {
	// Test that when EnsureInit and RunTerraform are nil, defaults are used
	_, err := RunApply(context.Background(), ApplyOptions{
		Root:  "/repo",
		Stack: testStackPath,
		Env:   "prod",
		Creds: auth.RawCreds{},
		// EnsureInit: nil - should use default
		// RunTerraform: nil - should use default
	})
	// This will fail because we don't have actual terraform binary, but the nil-check logic is executed
	if err == nil {
		t.Fatal("expected error when terraform is not available")
	}
}
