package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSimpleAssignments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backend.hcl")
	if err := os.WriteFile(path, []byte("bucket = \"one\"\n# ignore\ndynamodb_table = \"locks\"\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	values, err := ParseSimpleAssignments(path)
	if err != nil {
		t.Fatalf("ParseSimpleAssignments() error = %v", err)
	}
	if values["bucket"] != "one" || values["dynamodb_table"] != "locks" {
		t.Fatalf("unexpected values: %#v", values)
	}
}

func TestCheckOptionalBackendLocal(t *testing.T) {
	missing := CheckOptionalBackendLocal(filepath.Join(t.TempDir(), "backend.local.hcl"))
	if missing.Status != "info" {
		t.Fatalf("missing status = %q, want info", missing.Status)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "backend.local.hcl")
	if err := os.WriteFile(path, []byte("bucket=\"x\"\nregion=\"us-east-1\"\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	incomplete := CheckOptionalBackendLocal(path)
	if incomplete.Status != "fail" {
		t.Fatalf("incomplete status = %q, want fail", incomplete.Status)
	}
}
