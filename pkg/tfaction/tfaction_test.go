package tfaction

import (
	"context"
	"reflect"
	"testing"

	"github.com/ffreis/platform-cli/pkg/auth"
	"github.com/ffreis/platform-cli/pkg/tfexec"
)

func TestRunPlanBuildsExpectedArgs(t *testing.T) {
	var captured tfexec.RunOptions
	result, err := RunPlan(context.Background(), PlanOptions{
		Root:  "/repo",
		Stack: "/repo/stack",
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
		t.Fatalf("unexpected result: %+v", result)
	}
	wantArgs := []string{"plan", "-detailed-exitcode", "-var-file=../envs/prod/terraform.tfvars", "-out=tfplan"}
	if !reflect.DeepEqual(captured.Args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", captured.Args, wantArgs)
	}
}

func TestRunApplyUsesPlanFile(t *testing.T) {
	var captured tfexec.RunOptions
	result, err := RunApply(context.Background(), ApplyOptions{
		Root:        "/repo",
		Stack:       "/repo/stack",
		Env:         "prod",
		Creds:       auth.RawCreds{},
		PlanFile:    "../envs/prod/tfplan",
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
		t.Fatalf("RunApply() error = %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	wantArgs := []string{"apply", "../envs/prod/tfplan", "-auto-approve"}
	if !reflect.DeepEqual(captured.Args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", captured.Args, wantArgs)
	}
}
