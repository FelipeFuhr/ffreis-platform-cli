package tfexec

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVarFileArgsFallsBackWithoutFetchedVars(t *testing.T) {
	root := t.TempDir()
	stack := filepath.Join(root, StackDirName)
	if err := os.MkdirAll(filepath.Join(root, EnvsDirName, "prod"), 0o755); err != nil {
		t.Fatalf("mkdir env: %v", err)
	}
	if err := os.MkdirAll(stack, 0o755); err != nil {
		t.Fatalf("mkdir stack: %v", err)
	}
	args := VarFileArgs(stack, root, "prod")
	if len(args) != 1 || args[0] != "-var-file=../envs/prod/terraform.tfvars" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestTerraformCommandErrorPrefersStderr(t *testing.T) {
	if got := TerraformCommandError("stdout text", "stderr text"); got != "stderr text" {
		t.Fatalf("unexpected message: %q", got)
	}
}
