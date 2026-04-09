package nuke

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/ffreis/platform-cli/pkg/auth"
	"github.com/ffreis/platform-cli/pkg/tfexec"
)

func TestConfirm(t *testing.T) {
	var prompt bytes.Buffer
	ok, err := Confirm(bytes.NewBufferString("nuke-prod\n"), &prompt, "nuke-prod")
	if err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	if !ok {
		t.Fatal("Confirm() = false, want true")
	}
	if prompt.String() != "Type \"nuke-prod\" to confirm: " {
		t.Fatalf("unexpected prompt: %q", prompt.String())
	}
}

func TestRunDestroyDeclined(t *testing.T) {
	err := RunDestroy(context.Background(), DestroyOptions{Env: "prod", ConfirmReader: bytes.NewBufferString("cancel\n")})
	if !errors.Is(err, ErrConfirmationDeclined) {
		t.Fatalf("RunDestroy() error = %v, want ErrConfirmationDeclined", err)
	}
}

func TestRunDestroyUsesFailureHandler(t *testing.T) {
	var captured tfexec.RunOptions
	err := RunDestroy(context.Background(), DestroyOptions{
		Root:          "/repo",
		Stack:         "/repo/stack",
		Env:           "prod",
		Creds:         auth.RawCreds{},
		ConfirmReader: bytes.NewBufferString("nuke-prod\n"),
		Init: func(context.Context, string, string, string, auth.RawCreds) error {
			return nil
		},
		RunTerraform: func(_ context.Context, opts tfexec.RunOptions) (int, error) {
			captured = opts
			return 12, nil
		},
		OnFailure: func(_ context.Context, cause error) error {
			return cause
		},
	})
	if err == nil || err.Error() != "terraform destroy exited with code 12" {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := []string{"destroy", "-auto-approve", "-var-file=../envs/prod/terraform.tfvars"}
	if !reflect.DeepEqual(captured.Args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", captured.Args, wantArgs)
	}
}
