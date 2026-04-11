package doctor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	errWriteFile         = "write file: %v"
	errUnexpectedOK      = "unexpected ok result: %+v"
	errUnexpectedMissing = "unexpected missing result: %+v"
)

func TestParseSimpleAssignments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backend.hcl")
	if err := os.WriteFile(path, []byte("bucket = \"one\"\n# ignore\ndynamodb_table = \"locks\"\n"), 0o644); err != nil {
		t.Fatalf(errWriteFile, err)
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
		t.Fatalf(errWriteFile, err)
	}
	incomplete := CheckOptionalBackendLocal(path)
	if incomplete.Status != "fail" {
		t.Fatalf("incomplete status = %q, want fail", incomplete.Status)
	}
}

func TestCheckDirExists(t *testing.T) {
	dir := t.TempDir()
	ok := CheckDirExists("dir", "directory exists", dir, true)
	if ok.Status != "ok" || ok.Detail != dir {
		t.Fatalf(errUnexpectedOK, ok)
	}

	filePath := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatalf(errWriteFile, err)
	}
	notDir := CheckDirExists("dir", "directory exists", filePath, true)
	if notDir.Status != "fail" || !strings.Contains(notDir.Detail, "not a directory") {
		t.Fatalf("unexpected non-dir result: %+v", notDir)
	}
}

func TestCheckFileExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.hcl")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf(errWriteFile, err)
	}
	ok := CheckFileExists("file", "config file", path, true)
	if ok.Status != "ok" || ok.Detail != path {
		t.Fatalf(errUnexpectedOK, ok)
	}

	missing := CheckFileExists("file", "config file", filepath.Join(dir, "missing.hcl"), true)
	if missing.Status != "fail" || missing.Hint == "" || !missing.Blocking {
		t.Fatalf(errUnexpectedMissing, missing)
	}
}

func TestCheckEnvBackendFile(t *testing.T) {
	dir := t.TempDir()
	missing := CheckEnvBackendFile(filepath.Join(dir, "missing.hcl"), "prod")
	if missing.Status != "fail" || missing.Key != backendEnvCheckKey || !missing.Blocking {
		t.Fatalf(errUnexpectedMissing, missing)
	}

	missingKeyPath := filepath.Join(dir, "backend-missing-key.hcl")
	if err := os.WriteFile(missingKeyPath, []byte("bucket = \"one\"\n"), 0o644); err != nil {
		t.Fatalf(errWriteFile, err)
	}
	missingKey := CheckEnvBackendFile(missingKeyPath, "prod")
	if missingKey.Status != "fail" || !strings.Contains(missingKey.Detail, "missing required key entry") {
		t.Fatalf("unexpected missing-key result: %+v", missingKey)
	}

	okPath := filepath.Join(dir, "backend-ok.hcl")
	if err := os.WriteFile(okPath, []byte("key = \"state.tfstate\"\n"), 0o644); err != nil {
		t.Fatalf(errWriteFile, err)
	}
	ok := CheckEnvBackendFile(okPath, "prod")
	if ok.Status != "ok" || !strings.Contains(ok.Detail, "prod") {
		t.Fatalf(errUnexpectedOK, ok)
	}
}

func TestCheckFetchedVars(t *testing.T) {
	dir := t.TempDir()
	missing := CheckFetchedVars(filepath.Join(dir, "missing.json"), "prod")
	if missing.Status != "warn" || missing.Key != fetchedVarsCheckKey {
		t.Fatalf(errUnexpectedMissing, missing)
	}

	invalidPath := filepath.Join(dir, "invalid.json")
	if err := os.WriteFile(invalidPath, []byte("{"), 0o644); err != nil {
		t.Fatalf(errWriteFile, err)
	}
	invalid := CheckFetchedVars(invalidPath, "prod")
	if invalid.Status != "fail" || !invalid.Blocking {
		t.Fatalf("unexpected invalid result: %+v", invalid)
	}

	okPath := filepath.Join(dir, "fetched.auto.tfvars.json")
	data, err := json.Marshal(map[string]string{"bucket": "one"})
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	if err := os.WriteFile(okPath, data, 0o644); err != nil {
		t.Fatalf(errWriteFile, err)
	}
	ok := CheckFetchedVars(okPath, "prod")
	if ok.Status != "ok" || !strings.Contains(ok.Detail, "fetched.auto.tfvars.json") {
		t.Fatalf(errUnexpectedOK, ok)
	}
}

func TestCheckOptionalDir(t *testing.T) {
	dir := t.TempDir()
	warn := CheckOptionalDir(filepath.Join(dir, ".terraform"))
	if warn.Status != "warn" {
		t.Fatalf("unexpected warn result: %+v", warn)
	}

	terraformDir := filepath.Join(dir, ".terraform")
	if err := os.Mkdir(terraformDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	ok := CheckOptionalDir(terraformDir)
	if ok.Status != "ok" {
		t.Fatalf(errUnexpectedOK, ok)
	}
}
