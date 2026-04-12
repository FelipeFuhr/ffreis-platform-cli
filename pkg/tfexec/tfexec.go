package tfexec

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ffreis/platform-cli/pkg/auth"
)

const (
	StackDirName     = "stack"
	EnvsDirName      = "envs"
	varFileArgPrefix = "-var-file="
)

type RunOptions struct {
	StackPath string
	Args      []string
	Creds     auth.RawCreds
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
}

func RepoRoot() (string, error) {
	dir, err := filepath.Abs(".")
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("not inside a git repository (no .git found walking up from %s)", dir)
		}
		dir = parent
	}
}

func StackDir() (string, error) {
	root, err := RepoRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, StackDirName), nil
}

func BackendArgs(stackPath, root, env string) []string {
	envsBackend := filepath.Join(root, EnvsDirName, env, "backend.hcl")
	relEnvs, err := filepath.Rel(stackPath, envsBackend)
	if err != nil {
		relEnvs = filepath.Join("..", EnvsDirName, env, "backend.hcl")
	}
	args := []string{"-backend-config=" + relEnvs}
	local := filepath.Join(stackPath, "backend.local.hcl")
	if _, err := os.Stat(local); err == nil {
		args = append([]string{"-backend-config=backend.local.hcl"}, args...)
	}
	return args
}

func VarFileArgs(stackPath, root, env string) []string {
	envsDir := filepath.Join(root, EnvsDirName, env)
	relPath := func(name string) string {
		abs := filepath.Join(envsDir, name)
		rel, err := filepath.Rel(stackPath, abs)
		if err != nil {
			rel = filepath.Join("..", EnvsDirName, env, name)
		}
		return rel
	}
	args := []string{varFileArgPrefix + relPath("fetched.auto.tfvars.json"), varFileArgPrefix + relPath("terraform.tfvars")}
	fetchedAbs := filepath.Join(envsDir, "fetched.auto.tfvars.json")
	if _, err := os.Stat(fetchedAbs); err != nil {
		args = []string{varFileArgPrefix + relPath("terraform.tfvars")}
	}
	return args
}

func IsInitialised(stackPath string) bool {
	_, err := os.Stat(filepath.Join(stackPath, ".terraform"))
	return err == nil
}

func RunTerraform(ctx context.Context, opts RunOptions) (int, error) {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	cmd := exec.CommandContext(ctx, "terraform", opts.Args...) //nolint:gosec
	cmd.Dir = opts.StackPath
	env := os.Environ()
	for key, value := range opts.Creds.ToEnv() {
		env = append(env, key+"="+value)
	}
	cmd.Env = env
	cmd.Stdin = opts.Stdin
	cmd.Stdout = opts.Stdout
	cmd.Stderr = opts.Stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		return -1, fmt.Errorf("running terraform: %w", err)
	}
	return 0, nil
}

func TerraformInit(ctx context.Context, stackPath, root, env string, creds auth.RawCreds) error {
	args := append([]string{"init", "-upgrade"}, BackendArgs(stackPath, root, env)...)
	code, err := RunTerraform(ctx, RunOptions{StackPath: stackPath, Args: args, Creds: creds})
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("terraform init exited with code %d", code)
	}
	return nil
}

func EnsureInit(ctx context.Context, stackPath, root, env string, creds auth.RawCreds) error {
	if IsInitialised(stackPath) {
		return nil
	}
	return TerraformInit(ctx, stackPath, root, env, creds)
}

func TerraformCommandError(stdout, stderr string) string {
	if msg := strings.TrimSpace(stderr); msg != "" {
		return msg
	}
	if msg := strings.TrimSpace(stdout); msg != "" {
		return msg
	}
	return "no output"
}
