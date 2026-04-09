package nuke

import (
	"path/filepath"
	"testing"

	sharedaudit "github.com/ffreis/platform-cli/pkg/audit"
)

func TestOwnedResourcesForFallbackSortsByPriority(t *testing.T) {
	resources := []sharedaudit.Resource{
		{Status: "OWNED", ResourceType: "lambda/function", Name: "zeta"},
		{Status: "OWNED", ResourceType: "s3", Name: "bucket"},
		{Status: "OTHER_MANAGED", ResourceType: "iam/role", Name: "skip"},
		{Status: "OWNED", ResourceType: "iam/role", Name: "role"},
	}
	owned := ownedResourcesForFallback(resources)
	if len(owned) != 3 {
		t.Fatalf("len = %d, want 3", len(owned))
	}
	if owned[0].ResourceType != "lambda/function" || owned[1].ResourceType != "iam/role" || owned[2].ResourceType != "s3" {
		t.Fatalf("unexpected order: %#v", owned)
	}
}

func TestLoadBackendStateConfigForNukeUsesLocalOverrideWhenPresent(t *testing.T) {
	root := t.TempDir()
	stack := filepath.Join(root, "stack")
	parse := func(path string) (map[string]string, error) {
		if filepath.Base(path) == "backend.local.hcl" {
			return map[string]string{"bucket": "override", "dynamodb_table": "locks-local"}, nil
		}
		return map[string]string{"bucket": "default", "dynamodb_table": "locks", "key": "state.tfstate"}, nil
	}
	cfg, err := loadBackendStateConfigForNuke(root, stack, "prod", parse)
	if err != nil {
		t.Fatalf("loadBackendStateConfigForNuke() error = %v", err)
	}
	if cfg.BucketName != "default" || cfg.TableName != "locks" || cfg.StateKey != "state.tfstate" {
		t.Fatalf("unexpected config without local file: %+v", cfg)
	}
}
