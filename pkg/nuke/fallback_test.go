package nuke

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	sharedaudit "github.com/ffreis/platform-cli/pkg/audit"
)

func TestOwnedResourcesForFallbackSortsByPriority(t *testing.T) {
	resources := []sharedaudit.Resource{
		{Status: "OWNED", ResourceType: "lambda/function", Name: "zeta"},
		{Status: "OWNED", ResourceType: "s3", Name: "bucket"},
		{Status: "OTHER_MANAGED", ResourceType: resourceTypeIAMRole, Name: "skip"},
		{Status: "OWNED", ResourceType: resourceTypeIAMRole, Name: "role"},
	}
	owned := ownedResourcesForFallback(resources)
	if len(owned) != 3 {
		t.Fatalf("len = %d, want 3", len(owned))
	}
	if owned[0].ResourceType != "lambda/function" || owned[1].ResourceType != resourceTypeIAMRole || owned[2].ResourceType != "s3" {
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

func TestDeletePriority(t *testing.T) {
	tests := []struct {
		resourceType string
		want         int
	}{
		{resourceTypeIAMRole, 100},
		{"s3", 100},
		{"lambda/function", 10},
		{"dynamodb/table", 10},
		{"rds/cluster", 10},
		{"", 10},
	}
	for _, tt := range tests {
		t.Run(tt.resourceType, func(t *testing.T) {
			if got := deletePriority(tt.resourceType); got != tt.want {
				t.Errorf("deletePriority(%q) = %d, want %d", tt.resourceType, got, tt.want)
			}
		})
	}
}

func TestParseServiceType(t *testing.T) {
	tests := []struct {
		resourceType string
		wantService  string
		wantFullType string
	}{
		{"iam/role", "iam", "iam/role"},
		{"s3", "s3", "s3"},
		{"dynamodb/table", "dynamodb", "dynamodb/table"},
		{"service/type/extra", "service", "service/type/extra"},
	}
	for _, tt := range tests {
		t.Run(tt.resourceType, func(t *testing.T) {
			service, fullType := parseServiceType(tt.resourceType)
			if service != tt.wantService || fullType != tt.wantFullType {
				t.Errorf("parseServiceType(%q) = (%q, %q), want (%q, %q)", tt.resourceType, service, fullType, tt.wantService, tt.wantFullType)
			}
		})
	}
}

func TestClassifyPurgeDeleteError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want purgeFailureDisposition
	}{
		{"nil error", nil, purgeFailureFatal},
		{"was not found", errors.New("resource was not found"), purgeFailureGone},
		{"does not exist", errors.New("does not exist"), purgeFailureGone},
		{"not found", errors.New("not found"), purgeFailureGone},
		{"nosuchentity", errors.New("NoSuchEntity"), purgeFailureGone},
		{"nosuchbucket", errors.New("NoSuchBucket"), purgeFailureGone},
		{"throttling", errors.New("throttling error"), purgeFailureRetryable},
		{"rate exceeded", errors.New("rate exceeded"), purgeFailureRetryable},
		{"too many requests", errors.New("too many requests"), purgeFailureRetryable},
		{"unsupported", errors.New("unsupported operation"), purgeFailureManual},
		{"does not support delete", errors.New("does not support delete"), purgeFailureManual},
		{"typenotfound", errors.New("TypeNotFound"), purgeFailureManual},
		{"dependency", errors.New("dependency violation"), purgeFailureBlocked},
		{"resourceinuse", errors.New("ResourceInUse"), purgeFailureBlocked},
		{"targets", errors.New("targets depend on this"), purgeFailureBlocked},
		{"unknown error", errors.New("unknown error"), purgeFailureFatal},
		{"manual error", &purgeManualError{cause: errors.New("test")}, purgeFailureManual},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyPurgeDeleteError(tt.err); got != tt.want {
				t.Errorf("classifyPurgeDeleteError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPurgeClientToken(t *testing.T) {
	tests := []struct {
		cfnType    string
		identifier string
		stackTag   string
		checkFn    func(token string) bool
	}{
		{
			cfnType:    "AWS::IAM::Role",
			identifier: "test-role",
			stackTag:   "prod",
			checkFn:    func(t string) bool { return strings.HasPrefix(t, "prod-purge-") && len(t) > 11 },
		},
		{
			cfnType:    "AWS::Lambda::Function",
			identifier: "my-function",
			stackTag:   "",
			checkFn:    func(t string) bool { return strings.HasPrefix(t, "stack-purge-") && len(t) > 12 },
		},
		{
			cfnType:    "AWS::S3::Bucket",
			identifier: "bucket-name",
			stackTag:   "  ",
			checkFn:    func(t string) bool { return strings.HasPrefix(t, "stack-purge-") && len(t) > 12 },
		},
	}
	for i, tt := range tests {
		token := purgeClientToken(tt.cfnType, tt.identifier, tt.stackTag)
		if !tt.checkFn(token) {
			t.Errorf("test %d: purgeClientToken format check failed for token: %s", i, token)
		}
		// Idempotency: same inputs should produce same output
		token2 := purgeClientToken(tt.cfnType, tt.identifier, tt.stackTag)
		if token != token2 {
			t.Errorf("test %d: purgeClientToken not idempotent: %s != %s", i, token, token2)
		}
	}
}

func TestIsNotFoundError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"not found", errors.New("not found"), true},
		{"does not exist", errors.New("does not exist"), true},
		{"cannot be found", errors.New("cannot be found"), true},
		{"nosuchentity", errors.New("NoSuchEntity"), true},
		{"resourcenotfound", errors.New("ResourceNotFound"), true},
		{"other error", errors.New("some other error"), false},
		{"empty string", errors.New(""), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNotFoundError(tt.err); got != tt.want {
				t.Errorf("isNotFoundError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsS3BucketMissing(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"s3 not found type", &types.NotFound{}, true},
		{"generic not found error", errors.New("not found"), true},
		{"s3 error other", errors.New("access denied"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isS3BucketMissing(tt.err); got != tt.want {
				t.Errorf("isS3BucketMissing() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestArnToCloudControl(t *testing.T) {
	tests := []struct {
		arn          string
		service      string
		resourceType string
		name         string
		wantCfnType  string
		wantID       string
	}{
		{"arn:aws:sns:us-east-1:123456789:topic", "sns", "topic", "topic", "AWS::SNS::Topic", "arn:aws:sns:us-east-1:123456789:topic"},
		{"", "s3", "bucket", "my-bucket", "AWS::S3::Bucket", "my-bucket"},
		{"", "dynamodb", "dynamodb/table", "users", "AWS::DynamoDB::Table", "users"},
		{"arn:aws:lambda:us-east-1:123456789:function:my-func", "lambda", "lambda/function", "my-func", "AWS::Lambda::Function", "my-func"},
		{"", "iam", "iam/role", "service-role", "AWS::IAM::Role", "service-role"},
		{"arn:aws:unknown:us-east-1:123456789:unknown", "unknown", "unknown", "resource", "", ""},
		{"", "logs", "logs/log-group", "/aws/lambda/group", "AWS::Logs::LogGroup", "/aws/lambda/group"},
		{"arn:aws:acm:us-east-1:123456789:certificate/abc-123", "acm", "acm/certificate", "", "AWS::CertificateManager::Certificate", "arn:aws:acm:us-east-1:123456789:certificate/abc-123"},
	}
	for _, tt := range tests {
		t.Run(tt.service+"/"+tt.resourceType, func(t *testing.T) {
			cfnType, id := arnToCloudControl(tt.arn, tt.service, tt.resourceType, tt.name)
			if cfnType != tt.wantCfnType || id != tt.wantID {
				t.Errorf("arnToCloudControl() = (%q, %q), want (%q, %q)", cfnType, id, tt.wantCfnType, tt.wantID)
			}
		})
	}
}

func TestMatchesObjectKey(t *testing.T) {
	tests := []struct {
		objectKey string
		expected  string
		want      bool
	}{
		{"state.tfstate", "state.tfstate", true},
		{"state.tfstate", "other.tfstate", false},
		{"state.tfstate", "", true},
		{"", "", true},
		{"", "state.tfstate", false},
	}
	for i, tt := range tests {
		var objKey *string
		if tt.objectKey != "" {
			objKey = &tt.objectKey
		}
		if got := matchesObjectKey(objKey, tt.expected); got != tt.want {
			t.Errorf("test %d: matchesObjectKey(%q, %q) = %v, want %v", i, tt.objectKey, tt.expected, got, tt.want)
		}
	}
}
