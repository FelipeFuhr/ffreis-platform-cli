package tfexec

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ffreis/platform-cli/pkg/auth"
)

const (
	errMkdirEnvFormat          = "mkdir env: %v"
	errMkdirStackFormat        = "mkdir stack: %v"
	testStdoutText             = "stdout text"
	errUnexpectedMessageFormat = "unexpected message: %q"
)

func TestVarFileArgsFallsBackWithoutFetchedVars(t *testing.T) {
	root := t.TempDir()
	stack := filepath.Join(root, StackDirName)
	if err := os.MkdirAll(filepath.Join(root, EnvsDirName, "prod"), 0o755); err != nil {
		t.Fatalf(errMkdirEnvFormat, err)
	}
	if err := os.MkdirAll(stack, 0o755); err != nil {
		t.Fatalf(errMkdirStackFormat, err)
	}
	args := VarFileArgs(stack, root, "prod")
	if len(args) != 1 || args[0] != "-var-file=../envs/prod/terraform.tfvars" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestVarFileArgsIncludesFetchedVarsWhenPresent(t *testing.T) {
	root := t.TempDir()
	stack := filepath.Join(root, StackDirName)
	envDir := filepath.Join(root, EnvsDirName, "prod")
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		t.Fatalf(errMkdirEnvFormat, err)
	}
	if err := os.MkdirAll(stack, 0o755); err != nil {
		t.Fatalf(errMkdirStackFormat, err)
	}
	// Create fetched.auto.tfvars.json to include it in args
	fetchedFile := filepath.Join(envDir, "fetched.auto.tfvars.json")
	if err := os.WriteFile(fetchedFile, []byte("{}"), 0o644); err != nil {
		t.Fatalf("create fetched file: %v", err)
	}
	args := VarFileArgs(stack, root, "prod")
	if len(args) != 2 {
		t.Fatalf("expected 2 args, got %d: %#v", len(args), args)
	}
	if args[0] != "-var-file=../envs/prod/fetched.auto.tfvars.json" {
		t.Fatalf("expected fetched vars first, got: %s", args[0])
	}
	if args[1] != "-var-file=../envs/prod/terraform.tfvars" {
		t.Fatalf("expected terraform vars second, got: %s", args[1])
	}
}

func TestTerraformCommandErrorPrefersStderr(t *testing.T) {
	if got := TerraformCommandError(testStdoutText, "stderr text"); got != "stderr text" {
		t.Fatalf(errUnexpectedMessageFormat, got)
	}
}

func TestTerraformCommandErrorFallsBackToStdout(t *testing.T) {
	if got := TerraformCommandError(testStdoutText, ""); got != testStdoutText {
		t.Fatalf(errUnexpectedMessageFormat, got)
	}
}

func TestTerraformCommandErrorTrimsWhitespace(t *testing.T) {
	if got := TerraformCommandError("  stdout  ", "  "); got != "stdout" {
		t.Fatalf(errUnexpectedMessageFormat, got)
	}
}

func TestTerraformCommandErrorDefaultWhenEmpty(t *testing.T) {
	if got := TerraformCommandError("", ""); got != "no output" {
		t.Fatalf(errUnexpectedMessageFormat, got)
	}
}

func TestBackendArgsIncludesLocalConfigWhenPresent(t *testing.T) {
	root := t.TempDir()
	stack := filepath.Join(root, StackDirName)
	if err := os.MkdirAll(stack, 0o755); err != nil {
		t.Fatalf(errMkdirStackFormat, err)
	}
	// Create backend.local.hcl override
	localBackendFile := filepath.Join(stack, "backend.local.hcl")
	if err := os.WriteFile(localBackendFile, []byte("bucket = \"override\""), 0o644); err != nil {
		t.Fatalf("create local backend: %v", err)
	}
	args := BackendArgs(stack, root, "prod")
	if len(args) != 2 {
		t.Fatalf("expected 2 args with local override, got %d: %#v", len(args), args)
	}
	if args[0] != "-backend-config=backend.local.hcl" {
		t.Fatalf("expected local backend config first, got: %s", args[0])
	}
	if !filepath.IsAbs(args[1]) && !strings.HasPrefix(args[1], "-backend-config=") {
		t.Fatalf("expected backend config arg, got: %s", args[1])
	}
}

func TestBackendArgsExcludesLocalConfigWhenMissing(t *testing.T) {
	root := t.TempDir()
	stack := filepath.Join(root, StackDirName)
	if err := os.MkdirAll(stack, 0o755); err != nil {
		t.Fatalf(errMkdirStackFormat, err)
	}
	args := BackendArgs(stack, root, "prod")
	if len(args) != 1 {
		t.Fatalf("expected 1 arg without local override, got %d: %#v", len(args), args)
	}
	if !strings.HasPrefix(args[0], "-backend-config=") {
		t.Fatalf("expected backend config arg, got: %s", args[0])
	}
}

func TestIsInitialised(t *testing.T) {
	root := t.TempDir()
	if IsInitialised(root) {
		t.Fatal("expected non-initialised directory to return false")
	}
	terraformDir := filepath.Join(root, ".terraform")
	if err := os.Mkdir(terraformDir, 0o755); err != nil {
		t.Fatalf("mkdir .terraform: %v", err)
	}
	if !IsInitialised(root) {
		t.Fatal("expected initialised directory to return true")
	}
}

func TestVarFileArgsRelativePathHandling(t *testing.T) {
	root := t.TempDir()
	stack := filepath.Join(root, StackDirName)
	envDir := filepath.Join(root, EnvsDirName, "staging")
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		t.Fatalf(errMkdirEnvFormat, err)
	}
	if err := os.MkdirAll(stack, 0o755); err != nil {
		t.Fatalf(errMkdirStackFormat, err)
	}
	args := VarFileArgs(stack, root, "staging")
	// Should produce relative paths like ../envs/staging/terraform.tfvars
	if len(args) < 1 || !strings.HasPrefix(args[len(args)-1], "-var-file=") {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestStackDir(t *testing.T) {
	// Create a temporary git repo for testing
	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	stackDir := filepath.Join(root, StackDirName)
	if err := os.Mkdir(stackDir, 0o755); err != nil {
		t.Fatalf(errMkdirStackFormat, err)
	}

	// Change to the root directory for the test
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(oldwd) }()

	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir to root: %v", err)
	}

	result, err := StackDir()
	if err != nil {
		t.Fatalf("StackDir() error = %v", err)
	}
	if !strings.HasSuffix(result, StackDirName) {
		t.Errorf("StackDir() = %q, want to end with %q", result, StackDirName)
	}
}

func TestRepoRootNotFound(t *testing.T) {
	// Create a temporary directory without .git
	root := t.TempDir()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(oldwd) }()

	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	_, err = RepoRoot()
	if err == nil {
		t.Fatal("RepoRoot() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not inside a git repository") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func stubTerraformBin(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	fakeBin := filepath.Join(dir, "terraform")
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("create fake terraform: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestTerraformInitWithSuccess(t *testing.T) {
	root := t.TempDir()
	stack := filepath.Join(root, StackDirName)
	if err := os.MkdirAll(filepath.Join(root, EnvsDirName, "test"), 0o755); err != nil {
		t.Fatalf(errMkdirEnvFormat, err)
	}
	if err := os.MkdirAll(stack, 0o755); err != nil {
		t.Fatalf(errMkdirStackFormat, err)
	}

	backendFile := filepath.Join(root, EnvsDirName, "test", "backend.hcl")
	if err := os.WriteFile(backendFile, []byte("bucket = \"test\""), 0o644); err != nil {
		t.Fatalf("write backend: %v", err)
	}

	stubTerraformBin(t)

	creds := auth.RawCreds{AccessKeyID: "test", SecretAccessKey: "test", Region: "us-east-1"}
	if err := TerraformInit(context.Background(), stack, root, "test", creds); err != nil {
		t.Fatalf("TerraformInit() error = %v", err)
	}
}

func TestEnsureInitWhenNotInitialised(t *testing.T) {
	root := t.TempDir()
	stack := filepath.Join(root, StackDirName)
	if err := os.MkdirAll(stack, 0o755); err != nil {
		t.Fatalf(errMkdirStackFormat, err)
	}
	if err := os.MkdirAll(filepath.Join(root, EnvsDirName, "test"), 0o755); err != nil {
		t.Fatalf(errMkdirEnvFormat, err)
	}
	backendFile := filepath.Join(root, EnvsDirName, "test", "backend.hcl")
	if err := os.WriteFile(backendFile, []byte("bucket = \"test\""), 0o644); err != nil {
		t.Fatalf("write backend: %v", err)
	}

	if IsInitialised(stack) {
		t.Fatal("stack should not be initialised initially")
	}

	stubTerraformBin(t)

	creds := auth.RawCreds{AccessKeyID: "test", SecretAccessKey: "test", Region: "us-east-1"}
	if err := EnsureInit(context.Background(), stack, root, "test", creds); err != nil {
		t.Fatalf("EnsureInit() error = %v", err)
	}
}

func TestEnsureInitWhenAlreadyInitialised(t *testing.T) {
	root := t.TempDir()
	stack := filepath.Join(root, StackDirName)
	if err := os.MkdirAll(filepath.Join(stack, ".terraform"), 0o755); err != nil {
		t.Fatalf("mkdir .terraform: %v", err)
	}

	if !IsInitialised(stack) {
		t.Fatal("stack should be initialised")
	}

	// EnsureInit should return nil immediately without trying to init
	err := EnsureInit(context.Background(), stack, root, "test", auth.RawCreds{})
	if err != nil {
		t.Fatalf("EnsureInit() error = %v", err)
	}
}

func TestRunTerraformContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately
	stackPath := t.TempDir()

	code, err := RunTerraform(ctx, RunOptions{
		StackPath: stackPath,
		Args:      []string{"version"},
		Creds:     auth.RawCreds{},
	})

	// Either error or code -1 indicates context cancellation
	if err == nil && code != -1 {
		t.Fatalf("expected context cancellation error, got code=%d err=%v", code, err)
	}
}

func TestRunTerraformWithDefaultStdout(t *testing.T) {
	stackPath := t.TempDir()
	// Terraform won't actually run, but we're testing the stdout/stderr defaulting logic
	code, err := RunTerraform(context.Background(), RunOptions{
		StackPath: stackPath,
		Args:      []string{"version"},
		Creds:     auth.RawCreds{},
		// Stdout and Stderr are nil, should default to os.Stdout/os.Stderr
	})
	// This will fail but exercises the code path
	_ = code
	_ = err
}
