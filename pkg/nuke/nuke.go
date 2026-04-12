package nuke

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ffreis/platform-cli/pkg/auth"
	"github.com/ffreis/platform-cli/pkg/tfexec"
)

var ErrConfirmationDeclined = errors.New("nuke confirmation declined")

type InitFunc func(context.Context, string, string, string, auth.RawCreds) error
type RunTerraformFunc func(context.Context, tfexec.RunOptions) (int, error)
type FailureHandler func(context.Context, error) error

type DestroyOptions struct {
	Root          string
	Stack         string
	Env           string
	Creds         auth.RawCreds
	ConfirmReader io.Reader
	ConfirmWriter io.Writer
	Stdin         io.Reader
	Stdout        io.Writer
	Stderr        io.Writer
	ExtraArgs     []string
	Init          InitFunc
	RunTerraform  RunTerraformFunc
	OnFailure     FailureHandler
}

func Confirm(reader io.Reader, writer io.Writer, expected string) (bool, error) {
	if writer != nil {
		_, _ = fmt.Fprintf(writer, "Type %q to confirm: ", expected)
	}
	scanner := bufio.NewScanner(reader)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return false, err
		}
		return false, fmt.Errorf("no input received")
	}
	return strings.TrimSpace(scanner.Text()) == expected, nil
}

func RunDestroy(ctx context.Context, opts DestroyOptions) error {
	confirmed, err := Confirm(opts.ConfirmReader, opts.ConfirmWriter, "nuke-"+opts.Env)
	if err != nil {
		return err
	}
	if !confirmed {
		return ErrConfirmationDeclined
	}
	initFn := opts.Init
	if initFn == nil {
		initFn = tfexec.TerraformInit
	}
	runTerraform := opts.RunTerraform
	if runTerraform == nil {
		runTerraform = tfexec.RunTerraform
	}
	if err := initFn(ctx, opts.Stack, opts.Root, opts.Env, opts.Creds); err != nil {
		return err
	}
	args := append([]string{"destroy", "-auto-approve"}, tfexec.VarFileArgs(opts.Stack, opts.Root, opts.Env)...)
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
		return err
	}
	if code == 0 {
		return nil
	}
	destroyErr := fmt.Errorf("terraform destroy exited with code %d", code)
	if opts.OnFailure != nil {
		return opts.OnFailure(ctx, destroyErr)
	}
	return destroyErr
}
