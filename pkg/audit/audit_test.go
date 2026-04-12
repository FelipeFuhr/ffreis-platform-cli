package audit

import (
	"context"
	"reflect"
	"testing"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi"
	taggingtypes "github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi/types"
	"github.com/ffreis/platform-cli/pkg/inventory"
)

var testContract = inventory.Contract{StackTag: "example", RepoTag: "example-repo", LayerTag: "example-layer", TerraformRootTag: "stack"}

const (
	auditCloudFrontType  = "cloudfront/distribution"
	auditS3BucketARN     = "arn:aws:s3:::my-bucket"
	auditEC2InstanceType = "ec2/instance"
	auditLambdaFuncType  = "lambda/function"
	auditLambdaFuncARN   = "arn:aws:lambda:us-east-1:123456789012:function:my-function"
	auditLogsLogGroup    = "logs/log-group"
	auditIAMRoleARN      = "arn:aws:iam::123456789012:role/my-role"
	auditMyStack         = "my-stack"
	auditOtherStack      = "other-stack"
	auditMissingCoreTags = "missing core tags"
)

func TestClassifyResourceOwned(t *testing.T) {
	resource := ClassifyResource(taggingtypes.ResourceTagMapping{
		ResourceARN: sdkaws.String("arn:aws:s3:::example-bucket"),
		Tags: []taggingtypes.Tag{
			{Key: sdkaws.String("ManagedBy"), Value: sdkaws.String("terraform")},
			{Key: sdkaws.String("Stack"), Value: sdkaws.String("example")},
			{Key: sdkaws.String("Environment"), Value: sdkaws.String("prod")},
		},
	}, testContract, "prod")
	if resource.Status != "OWNED" {
		t.Fatalf("Status = %q, want OWNED", resource.Status)
	}
	if resource.Repo != "example-repo" || resource.Layer != "example-layer" {
		t.Fatalf("unexpected defaults: %+v", resource)
	}
}

func TestBuildReport(t *testing.T) {
	report := BuildReport([]Resource{{Status: "OWNED"}, {Status: "OWNED", Issues: []string{"warn"}}, {Status: "OTHER_MANAGED"}, {Status: "UNOWNED"}})
	if report.Summary.Total != 4 || report.Summary.Owned != 2 || report.Summary.OwnedWarn != 1 || report.Summary.OtherManaged != 1 || report.Summary.Unowned != 1 {
		t.Fatalf("unexpected summary: %+v", report.Summary)
	}
}

func TestScanResourcesSortsResults(t *testing.T) {
	fetch := func(context.Context, *resourcegroupstaggingapi.GetResourcesInput) (*resourcegroupstaggingapi.GetResourcesOutput, error) {
		return &resourcegroupstaggingapi.GetResourcesOutput{ResourceTagMappingList: []taggingtypes.ResourceTagMapping{
			{ResourceARN: sdkaws.String("arn:aws:s3:::bkt-b")},
			{ResourceARN: sdkaws.String("arn:aws:s3:::bkt-a")},
		}}, nil
	}
	resources, err := ScanResources(context.Background(), fetch, testContract, "prod")
	if err != nil {
		t.Fatalf("ScanResources() error = %v", err)
	}
	got := []string{resources[0].Name, resources[1].Name}
	want := []string{"bkt-a", "bkt-b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sorted names = %#v, want %#v", got, want)
	}
}

func TestResourceNameAndTypeFromARN(t *testing.T) {
	if got := ResourceTypeFromARN("arn:aws:cloudfront::123456789012:distribution/ABC"); got != auditCloudFrontType {
		t.Fatalf("ResourceTypeFromARN() = %q", got)
	}
	if got := ResourceNameFromARN("arn:aws:cloudfront::123456789012:distribution/ABC", auditCloudFrontType); got != "ABC" {
		t.Fatalf("ResourceNameFromARN() = %q", got)
	}
}

func TestResourceTypeFromARNTypes(t *testing.T) {
	tests := []struct {
		name string
		arn  string
		want string
	}{
		// S3 buckets
		{"s3 bucket", auditS3BucketARN, "s3"},
		// EC2 resources
		{"ec2 instance", "arn:aws:ec2:us-east-1:123456789012:instance/i-0123456789abcdef0", auditEC2InstanceType},
		{"ec2 volume", "arn:aws:ec2:us-east-1:123456789012:volume/vol-0123456789abcdef0", "ec2/volume"},
		{"ec2 security-group", "arn:aws:ec2:us-east-1:123456789012:security-group/sg-0123456789abcdef0", "ec2/security-group"},
		{"ec2 subnet", "arn:aws:ec2:us-east-1:123456789012:subnet/subnet-0123456789abcdef0", "ec2/subnet"},
		{"ec2 vpc", "arn:aws:ec2:us-east-1:123456789012:vpc/vpc-0123456789abcdef0", "ec2/vpc"},
		// Lambda
		{"lambda function", auditLambdaFuncARN, auditLambdaFuncType},
		// DynamoDB
		{"dynamodb table", "arn:aws:dynamodb:us-east-1:123456789012:table/MyTable", "dynamodb/table"},
		// Logs
		{"logs log-group", "arn:aws:logs:us-east-1:123456789012:log-group:/aws/lambda/function", auditLogsLogGroup},
		// Route53
		{"route53 hostedzone", "arn:aws:route53:::hostedzone/Z123456789ABC", "route53/hostedzone"},
		// CloudFront
		{"cloudfront distribution", "arn:aws:cloudfront::123456789012:distribution/E123456789ABC", auditCloudFrontType},
		// IAM
		{"iam role", auditIAMRoleARN, "iam/role"},
		{"iam policy", "arn:aws:iam::123456789012:policy/my-policy", "iam/policy"},
		// API Gateway
		{"apigateway api", "arn:aws:apigateway:us-east-1::/restapis/abc123", "apigateway/restapis"},
		{"apigatewayv2 api", "arn:aws:apigateway:us-east-1::/apis/abc123/stages/prod", "apigatewayv2/api"},
		// KMS
		{"kms key", "arn:aws:kms:us-east-1:123456789012:key/12345678-1234-1234-1234-123456789012", "kms/key"},
		// ACM
		{"acm certificate", "arn:aws:acm:us-east-1:123456789012:certificate/12345678-1234-1234-1234-123456789012", "acm/certificate"},
		// ElastiCache
		{"elasticache cluster", "arn:aws:elasticache:us-east-1:123456789012:cluster:my-cluster", "elasticache/cluster"},
		// CloudTrail
		{"cloudtrail trail", "arn:aws:cloudtrail:us-east-1:123456789012:trail/my-trail", "cloudtrail/trail"},
		// Edge cases
		{"empty arn", "", "unknown"},
		{"malformed arn", "not-an-arn", "unknown"},
		{"incomplete arn", "arn:aws:ec2", "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResourceTypeFromARN(tt.arn)
			if got != tt.want {
				t.Fatalf("ResourceTypeFromARN(%q) = %q, want %q", tt.arn, got, tt.want)
			}
		})
	}
}

func TestResourceNameFromARNAll(t *testing.T) {
	tests := []struct {
		name         string
		arn          string
		resourceType string
		want         string
	}{
		// S3
		{"s3 bucket", auditS3BucketARN, "s3", "my-bucket"},
		// EC2
		{"ec2 instance", "arn:aws:ec2:us-east-1:123456789012:instance/i-0123456789abcdef0", auditEC2InstanceType, "i-0123456789abcdef0"},
		{"ec2 volume", "arn:aws:ec2:us-east-1:123456789012:volume/vol-0123456789abcdef0", "ec2/volume", "vol-0123456789abcdef0"},
		// Lambda
		{"lambda function", auditLambdaFuncARN, auditLambdaFuncType, "my-function"},
		// DynamoDB
		{"dynamodb table", "arn:aws:dynamodb:us-east-1:123456789012:table/MyTable", "dynamodb/table", "MyTable"},
		// Logs
		{"logs log-group simple", "arn:aws:logs:us-east-1:123456789012:log-group:/aws/lambda/function:*", auditLogsLogGroup, "/aws/lambda/function"},
		{"logs log-group no suffix", "arn:aws:logs:us-east-1:123456789012:log-group:/aws/lambda/function", auditLogsLogGroup, "/aws/lambda/function"},
		// Route53
		{"route53 hostedzone", "arn:aws:route53:::hostedzone/Z123456789ABC", "route53/hostedzone", "Z123456789ABC"},
		// CloudFront
		{"cloudfront distribution", "arn:aws:cloudfront::123456789012:distribution/E123456789ABC", auditCloudFrontType, "E123456789ABC"},
		// IAM
		{"iam role", auditIAMRoleARN, "iam/role", "my-role"},
		{"iam policy", "arn:aws:iam::123456789012:policy/my-policy", "iam/policy", "my-policy"},
		// KMS
		{"kms key", "arn:aws:kms:us-east-1:123456789012:key/12345678-1234-1234-1234-123456789012", "kms/key", "12345678-1234-1234-1234-123456789012"},
		// ElastiCache
		{"elasticache cluster", "arn:aws:elasticache:us-east-1:123456789012:cluster:my-cluster", "elasticache/cluster", "my-cluster"},
		// API Gateway v2
		{"apigatewayv2 api", "arn:aws:apigateway:us-east-1::/apis/abc123/stages/prod", "apigatewayv2/api", "abc123"},
		// Generic with colon separator
		{"generic colon", "arn:aws:service:region:account:type:name", "service/type", "name"},
		// Edge cases
		{"empty arn", "", "s3", ""},
		{"fallback parsing", "arn:aws:iam::123456789012:role/my-role", "unknown/type", "my-role"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResourceNameFromARN(tt.arn, tt.resourceType)
			if got != tt.want {
				t.Fatalf("ResourceNameFromARN(%q, %q) = %q, want %q", tt.arn, tt.resourceType, got, tt.want)
			}
		})
	}
}

func TestMatchedResourceKey(t *testing.T) {
	tests := []struct {
		name     string
		resource Resource
		validate func(string) bool
	}{
		{
			name: "owned resource with arn",
			resource: Resource{
				Status:       "OWNED",
				ARN:          auditS3BucketARN,
				Stack:        auditMyStack,
				Environment:  "prod",
				ResourceType: "s3",
				Name:         "my-bucket",
			},
			validate: func(key string) bool {
				return key == auditS3BucketARN
			},
		},
		{
			name: "other managed resource with arn",
			resource: Resource{
				Status:       "OTHER_MANAGED",
				ARN:          auditLambdaFuncARN,
				Stack:        "",
				Environment:  "prod",
				ResourceType: auditLambdaFuncType,
				Name:         "my-function",
			},
			validate: func(key string) bool {
				return key == auditLambdaFuncARN
			},
		},
		{
			name: "unowned resource without arn",
			resource: Resource{
				Status:       "UNOWNED",
				ARN:          "",
				Stack:        auditMyStack,
				Environment:  "prod",
				ResourceType: auditEC2InstanceType,
				Name:         "i-1234567890abcdef0",
			},
			validate: func(key string) bool {
				return key == auditMyStack+"|prod|"+auditEC2InstanceType+"|i-1234567890abcdef0"
			},
		},
		{
			name: "resource key is lowercase",
			resource: Resource{
				Status:       "OWNED",
				ARN:          "arn:aws:s3:::MY-BUCKET",
				Stack:        "",
				Environment:  "",
				ResourceType: "",
				Name:         "",
			},
			validate: func(key string) bool {
				return key == "arn:aws:s3:::my-bucket"
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchedResourceKey(tt.resource)
			if !tt.validate(got) {
				t.Fatalf("MatchedResourceKey() = %q, validation failed", got)
			}
		})
	}
}

func TestAssignResourceStatus(t *testing.T) {
	contract := inventory.Contract{StackTag: auditMyStack, RepoTag: "my-repo", LayerTag: "api"}
	tests := []struct {
		name         string
		managedBy    string
		stack        string
		environment  string
		env          string
		missingCore  []string
		missingOwner []string
		wantStatus   string
		wantIssues   int
	}{
		{
			name:         "owned resource all tags correct",
			managedBy:    "terraform",
			stack:        auditMyStack,
			environment:  "prod",
			env:          "prod",
			missingCore:  []string{},
			missingOwner: []string{},
			wantStatus:   "OWNED",
			wantIssues:   0,
		},
		{
			name:         "owned resource missing ownership tags",
			managedBy:    "terraform",
			stack:        auditMyStack,
			environment:  "prod",
			env:          "prod",
			missingCore:  []string{},
			missingOwner: []string{"Repo", "Layer"},
			wantStatus:   "OWNED",
			wantIssues:   1,
		},
		{
			name:         "owned resource wrong env",
			managedBy:    "terraform",
			stack:        auditMyStack,
			environment:  "dev",
			env:          "prod",
			missingCore:  []string{},
			missingOwner: []string{},
			wantStatus:   "OTHER_MANAGED",
			wantIssues:   0,
		},
		{
			name:         "other managed with managed by",
			managedBy:    "cloudformation",
			stack:        "",
			environment:  "prod",
			env:          "prod",
			missingCore:  []string{"Stack", "Environment"},
			missingOwner: []string{},
			wantStatus:   "OTHER_MANAGED",
			wantIssues:   1,
		},
		{
			name:         "other managed with stack tag",
			managedBy:    "",
			stack:        auditOtherStack,
			environment:  "prod",
			env:          "prod",
			missingCore:  []string{"ManagedBy"},
			missingOwner: []string{},
			wantStatus:   "OTHER_MANAGED",
			wantIssues:   1,
		},
		{
			name:         "other managed terraform without matching stack",
			managedBy:    "terraform",
			stack:        auditOtherStack,
			environment:  "prod",
			env:          "prod",
			missingCore:  []string{},
			missingOwner: []string{},
			wantStatus:   "OTHER_MANAGED",
			wantIssues:   0,
		},
		{
			name:         "unowned no tags",
			managedBy:    "",
			stack:        "",
			environment:  "prod",
			env:          "prod",
			missingCore:  []string{"ManagedBy", "Stack"},
			missingOwner: []string{},
			wantStatus:   "UNOWNED",
			wantIssues:   1,
		},
		{
			name:         "other managed terraform with missing ownership",
			managedBy:    "terraform",
			stack:        auditOtherStack,
			environment:  "prod",
			env:          "prod",
			missingCore:  []string{},
			missingOwner: []string{"Repo"},
			wantStatus:   "OTHER_MANAGED",
			wantIssues:   1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := Resource{
				ManagedBy:   tt.managedBy,
				Stack:       tt.stack,
				Environment: tt.environment,
			}
			assignResourceStatus(&resource, contract, tt.env, tt.missingCore, tt.missingOwner)
			if resource.Status != tt.wantStatus {
				t.Fatalf("assignResourceStatus() status = %q, want %q", resource.Status, tt.wantStatus)
			}
			if len(resource.Issues) != tt.wantIssues {
				t.Fatalf("assignResourceStatus() issues count = %d, want %d (issues: %v)", len(resource.Issues), tt.wantIssues, resource.Issues)
			}
		})
	}
}

func TestAppendMissingTagIssue(t *testing.T) {
	tests := []struct {
		name        string
		prefix      string
		missing     []string
		wantIssue   bool
		wantContent string
	}{
		{
			name:      "no missing tags",
			prefix:    auditMissingCoreTags,
			missing:   []string{},
			wantIssue: false,
		},
		{
			name:        "single missing tag",
			prefix:      auditMissingCoreTags,
			missing:     []string{"ManagedBy"},
			wantIssue:   true,
			wantContent: auditMissingCoreTags + ": ManagedBy",
		},
		{
			name:        "multiple missing tags",
			prefix:      auditMissingCoreTags,
			missing:     []string{"ManagedBy", "Stack", "Environment"},
			wantIssue:   true,
			wantContent: auditMissingCoreTags + ": ManagedBy, Stack, Environment",
		},
		{
			name:        "ownership tags prefix",
			prefix:      "missing ownership tags",
			missing:     []string{"Repo", "Layer"},
			wantIssue:   true,
			wantContent: "missing ownership tags: Repo, Layer",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := &Resource{}
			appendMissingTagIssue(resource, tt.prefix, tt.missing)
			if tt.wantIssue && len(resource.Issues) == 0 {
				t.Fatalf("appendMissingTagIssue() expected issue but got none")
			}
			if !tt.wantIssue && len(resource.Issues) > 0 {
				t.Fatalf("appendMissingTagIssue() expected no issue but got: %v", resource.Issues)
			}
			if tt.wantIssue && len(resource.Issues) > 0 && resource.Issues[0] != tt.wantContent {
				t.Fatalf("appendMissingTagIssue() issue = %q, want %q", resource.Issues[0], tt.wantContent)
			}
		})
	}
}
