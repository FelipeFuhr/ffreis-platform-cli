package nuke

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	sharedaudit "github.com/ffreis/platform-cli/pkg/audit"
)

type fakeStateS3Client struct {
	listOutputs []*s3.ListObjectVersionsOutput
	listErrs    []error
	deleteCalls []string
	deleteErr   error
	listIndex   int
}

func (f *fakeStateS3Client) ListObjectVersions(_ context.Context, _ *s3.ListObjectVersionsInput, _ ...func(*s3.Options)) (*s3.ListObjectVersionsOutput, error) {
	if f.listIndex < len(f.listErrs) && f.listErrs[f.listIndex] != nil {
		err := f.listErrs[f.listIndex]
		f.listIndex++
		return nil, err
	}
	if f.listIndex >= len(f.listOutputs) {
		return &s3.ListObjectVersionsOutput{}, nil
	}
	out := f.listOutputs[f.listIndex]
	f.listIndex++
	return out, nil
}

func (f *fakeStateS3Client) GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	return nil, nil
}

func (f *fakeStateS3Client) DeleteObject(_ context.Context, in *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	f.deleteCalls = append(f.deleteCalls, awsString(in.Key)+"@"+awsString(in.VersionId))
	if f.deleteErr != nil {
		return nil, f.deleteErr
	}
	return &s3.DeleteObjectOutput{}, nil
}

func (f *fakeStateS3Client) HeadBucket(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	return &s3.HeadBucketOutput{}, nil
}

func awsString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

const (
	resourceTypeLambdaFunction = "lambda/function"
	defaultStateKey            = "state.tfstate"
	resourceTypeDynamoDBTable  = "dynamodb/table"
	errorDoesNotExist          = "does not exist"
	errorNotFound              = "not found"
)

func TestOwnedResourcesForFallbackSortsByPriority(t *testing.T) {
	resources := []sharedaudit.Resource{
		{Status: "OWNED", ResourceType: resourceTypeLambdaFunction, Name: "zeta"},
		{Status: "OWNED", ResourceType: "s3", Name: "bucket"},
		{Status: "OTHER_MANAGED", ResourceType: resourceTypeIAMRole, Name: "skip"},
		{Status: "OWNED", ResourceType: resourceTypeIAMRole, Name: "role"},
	}
	owned := ownedResourcesForFallback(resources)
	if len(owned) != 3 {
		t.Fatalf("len = %d, want 3", len(owned))
	}
	if owned[0].ResourceType != resourceTypeLambdaFunction || owned[1].ResourceType != resourceTypeIAMRole || owned[2].ResourceType != "s3" {
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
		return map[string]string{"bucket": "default", "dynamodb_table": "locks", "key": defaultStateKey}, nil
	}
	cfg, err := loadBackendStateConfigForNuke(root, stack, "prod", parse)
	if err != nil {
		t.Fatalf("loadBackendStateConfigForNuke() error = %v", err)
	}
	if cfg.BucketName != "default" || cfg.TableName != "locks" || cfg.StateKey != defaultStateKey {
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
		{resourceTypeLambdaFunction, 10},
		{resourceTypeDynamoDBTable, 10},
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
		{resourceTypeIAMRole, "iam", resourceTypeIAMRole},
		{"s3", "s3", "s3"},
		{resourceTypeDynamoDBTable, "dynamodb", resourceTypeDynamoDBTable},
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
		{errorDoesNotExist, errors.New(errorDoesNotExist), purgeFailureGone},
		{errorNotFound, errors.New(errorNotFound), purgeFailureGone},
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
		{errorNotFound, errors.New(errorNotFound), true},
		{errorDoesNotExist, errors.New(errorDoesNotExist), true},
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
		{"generic not found error", errors.New(errorNotFound), true},
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
		{"", "dynamodb", resourceTypeDynamoDBTable, "users", "AWS::DynamoDB::Table", "users"},
		{"arn:aws:lambda:us-east-1:123456789:function:my-func", "lambda", resourceTypeLambdaFunction, "my-func", "AWS::Lambda::Function", "my-func"},
		{"", "iam", resourceTypeIAMRole, "service-role", "AWS::IAM::Role", "service-role"},
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
		{defaultStateKey, defaultStateKey, true},
		{defaultStateKey, "other.tfstate", false},
		{defaultStateKey, "", true},
		{"", "", true},
		{"", defaultStateKey, false},
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

func TestDeleteBucketVersionsPage(t *testing.T) {
	client := &fakeStateS3Client{}
	key := defaultStateKey
	otherKey := "other.tfstate"
	versionID1 := "v1"
	versionID2 := "v2"
	deleted, err := deleteBucketVersionsPage(context.Background(), client, "bucket", key, []types.ObjectVersion{
		{Key: &key, VersionId: &versionID1},
		{Key: &otherKey, VersionId: &versionID2},
	}, 0)
	if err != nil {
		t.Fatalf("deleteBucketVersionsPage() error = %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	if len(client.deleteCalls) != 1 || client.deleteCalls[0] != defaultStateKey+"@v1" {
		t.Fatalf("unexpected delete calls: %#v", client.deleteCalls)
	}
}

func TestDeleteBucketDeleteMarkersPageError(t *testing.T) {
	client := &fakeStateS3Client{deleteErr: errors.New("delete failed")}
	key := defaultStateKey
	versionID := "m1"
	deleted, err := deleteBucketDeleteMarkersPage(context.Background(), client, "bucket", key, []types.DeleteMarkerEntry{{Key: &key, VersionId: &versionID}}, 2)
	if err == nil {
		t.Fatal("expected delete error")
	}
	if deleted != 2 {
		t.Fatalf("deleted = %d, want 2", deleted)
	}
}

func TestWalkObjectVersionPages(t *testing.T) {
	key := defaultStateKey
	versionID1 := "v1"
	versionID2 := "v2"
	truncated := true
	client := &fakeStateS3Client{listOutputs: []*s3.ListObjectVersionsOutput{
		{Versions: []types.ObjectVersion{{Key: &key, VersionId: &versionID1}}, IsTruncated: &truncated, NextKeyMarker: &key, NextVersionIdMarker: &versionID1},
		{Versions: []types.ObjectVersion{{Key: &key, VersionId: &versionID2}}},
	}}
	visits := 0
	err := walkObjectVersionPages(context.Background(), client, "bucket", key, func(out *s3.ListObjectVersionsOutput) error {
		visits += len(out.Versions)
		return nil
	})
	if err != nil {
		t.Fatalf("walkObjectVersionPages() error = %v", err)
	}
	if visits != 2 {
		t.Fatalf("visits = %d, want 2", visits)
	}
}
