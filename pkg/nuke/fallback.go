package nuke

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/cloudcontrol"
	cctypes "github.com/aws/aws-sdk-go-v2/service/cloudcontrol/types"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	sharedaudit "github.com/ffreis/platform-cli/pkg/audit"
	sharedoutput "github.com/ffreis/platform-cli/pkg/output"
	"github.com/ffreis/platform-cli/pkg/tfexec"
)

type cloudControlAPI interface {
	DeleteResource(context.Context, *cloudcontrol.DeleteResourceInput, ...func(*cloudcontrol.Options)) (*cloudcontrol.DeleteResourceOutput, error)
	GetResourceRequestStatus(context.Context, *cloudcontrol.GetResourceRequestStatusInput, ...func(*cloudcontrol.Options)) (*cloudcontrol.GetResourceRequestStatusOutput, error)
}

type purgeFailureDisposition int

const (
	purgeFailureFatal purgeFailureDisposition = iota
	purgeFailureGone
	purgeFailureManual
	purgeFailureBlocked
	purgeFailureRetryable
)

const (
	resourceTypeIAMRole  = "iam/role"
	purgeErrorFormat     = "%s %s: %v"
	purgeStateFileFormat = "state-%06d.tfstate"
)

type purgeManualError struct {
	cause error
	hint  string
}

func (e *purgeManualError) Error() string {
	if e.hint != "" {
		return e.cause.Error() + " (" + e.hint + ")"
	}
	return e.cause.Error()
}

func (e *purgeManualError) Unwrap() error { return e.cause }

type fallbackSummary struct {
	Deleted int
	Gone    int
	Blocked int
	Manual  int
	Failed  int
}

type backendStateConfig struct {
	BucketName string
	TableName  string
	StateKey   string
}

type backendResetSummary struct {
	BucketName            string
	TableName             string
	StateKey              string
	DeletedStateVersions  int
	DeletedDeleteMarkers  int
	DeletedLockEntries    int
	RemovedLocalTerraform bool
	BackupDir             string
}

type stateS3API interface {
	ListObjectVersions(context.Context, *s3.ListObjectVersionsInput, ...func(*s3.Options)) (*s3.ListObjectVersionsOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	HeadBucket(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
}

type stateDynamoAPI interface {
	DescribeTable(context.Context, *dynamodb.DescribeTableInput, ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error)
	Scan(context.Context, *dynamodb.ScanInput, ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error)
	DeleteItem(context.Context, *dynamodb.DeleteItemInput, ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
}

type Reporter interface {
	Status(kind, label, detail string)
	Summary(title string, parts ...string)
	Blank()
}

type ScanResourcesFunc func(context.Context) ([]sharedaudit.Resource, error)
type ParseAssignmentsFunc func(string) (map[string]string, error)

type FallbackOptions struct {
	AWSConfig        sdkaws.Config
	Reporter         Reporter
	ScanResources    ScanResourcesFunc
	ParseAssignments ParseAssignmentsFunc
	CountPart        func(label string, value int) string
	Root             string
	Stack            string
	Env              string
	StackTag         string
}

var (
	newCloudControlClient = func(cfg sdkaws.Config) cloudControlAPI { return cloudcontrol.NewFromConfig(cfg) }
	newIAMDeleteClient    = iam.NewFromConfig
	newS3DeleteClient     = s3.NewFromConfig
	newDynamoDeleteClient = dynamodb.NewFromConfig
	purgeAfter            = time.After
)

func RunFallbackCleanup(ctx context.Context, cause error, opts FallbackOptions) error {
	if opts.Reporter == nil {
		return fmt.Errorf("fallback reporter is required")
	}
	if opts.ScanResources == nil {
		return fmt.Errorf("fallback scan function is required")
	}
	if opts.ParseAssignments == nil {
		return fmt.Errorf("fallback parse function is required")
	}

	opts.Reporter.Status("warn", "fallback", fmt.Sprintf("terraform destroy could not complete cleanly; attempting SDK fallback cleanup (%v)", cause))

	resources, err := opts.ScanResources(ctx)
	if err != nil {
		return fmt.Errorf("scan owned resources for fallback: %w", err)
	}
	owned := ownedResourcesForFallback(resources)
	if len(owned) > 0 {
		summary, err := runManagedSDKFallbackNuke(ctx, opts.AWSConfig, opts.Reporter, owned)
		opts.Reporter.Summary(
			"AWS Fallback Cleanup",
			sharedoutput.CountPartWithFn(opts.CountPart, "deleted", summary.Deleted),
			sharedoutput.CountPartWithFn(opts.CountPart, "gone", summary.Gone),
			sharedoutput.CountPartWithFn(opts.CountPart, "blocked", summary.Blocked),
			sharedoutput.CountPartWithFn(opts.CountPart, "manual", summary.Manual),
			sharedoutput.CountPartWithFn(opts.CountPart, "failed", summary.Failed),
		)
		if err != nil {
			return err
		}
	}

	remaining, err := opts.ScanResources(ctx)
	if err != nil {
		return fmt.Errorf("verify owned resources after fallback: %w", err)
	}
	owned = ownedResourcesForFallback(remaining)
	if len(owned) > 0 {
		names := make([]string, 0, len(owned))
		for _, resource := range owned {
			names = append(names, resource.ResourceType+" "+resource.Name)
		}
		return fmt.Errorf("fallback cleanup left %d owned resource(s): %s", len(owned), strings.Join(names, ", "))
	}

	backupDir := filepath.Join(opts.Root, ".backups", "nuke", opts.Env, time.Now().UTC().Format("20060102T150405Z"), opts.StackTag)
	resetSummary, err := resetBackendStateForNuke(ctx, opts, backupDir)
	if err != nil {
		return fmt.Errorf("reset terraform backend state: %w", err)
	}

	opts.Reporter.Status("ok", "reset", fmt.Sprintf("deleted %d state object version(s) and %d delete marker(s) for %s", resetSummary.DeletedStateVersions, resetSummary.DeletedDeleteMarkers, resetSummary.StateKey))
	opts.Reporter.Status("ok", "reset", fmt.Sprintf("deleted %d lock row(s) from %s", resetSummary.DeletedLockEntries, resetSummary.TableName))
	if resetSummary.RemovedLocalTerraform {
		opts.Reporter.Status("ok", "reset", "removed local .terraform cache so the next init starts clean")
	}
	if resetSummary.BackupDir != "" {
		opts.Reporter.Status("info", "backup", "backend state backup written to "+resetSummary.BackupDir)
	}
	opts.Reporter.Blank()
	opts.Reporter.Status("ok", "ok", "AWS fallback cleanup complete; terraform backend reset")
	return nil
}

func ownedResourcesForFallback(resources []sharedaudit.Resource) []sharedaudit.Resource {
	owned := make([]sharedaudit.Resource, 0)
	for _, resource := range resources {
		if resource.Status == "OWNED" {
			owned = append(owned, resource)
		}
	}
	sort.Slice(owned, func(i, j int) bool {
		left := deletePriority(owned[i].ResourceType)
		right := deletePriority(owned[j].ResourceType)
		if left != right {
			return left < right
		}
		if owned[i].ResourceType != owned[j].ResourceType {
			return owned[i].ResourceType < owned[j].ResourceType
		}
		return owned[i].Name < owned[j].Name
	})
	return owned
}

func deletePriority(resourceType string) int {
	switch resourceType {
	case resourceTypeIAMRole, "s3":
		return 100
	default:
		return 10
	}
}

func runManagedSDKFallbackNuke(ctx context.Context, awsCfg sdkaws.Config, out Reporter, resources []sharedaudit.Resource) (fallbackSummary, error) {
	summary := fallbackSummary{}
	cc := newCloudControlClient(awsCfg)
	var errs []string

	for _, resource := range resources {
		out.Status("info", "cleanup", fmt.Sprintf("deleting %s %s", resource.ResourceType, resource.Name))
		err := deleteManagedResourceWithFallback(ctx, awsCfg, cc, resource)
		switch classifyPurgeDeleteError(err) {
		case purgeFailureGone:
			summary.Gone++
			out.Status("muted", "skip", fmt.Sprintf("%s %s already absent", resource.ResourceType, resource.Name))
		case purgeFailureManual:
			summary.Manual++
			errs = append(errs, formatPurgeError(resource, err))
			out.Status("warn", "skip", fmt.Sprintf("%s %s requires manual cleanup", resource.ResourceType, resource.Name))
		case purgeFailureBlocked:
			summary.Blocked++
			errs = append(errs, formatPurgeError(resource, err))
			out.Status("warn", "wait", fmt.Sprintf("%s %s is blocked by dependent resources", resource.ResourceType, resource.Name))
		default:
			if err != nil {
				summary.Failed++
				errs = append(errs, formatPurgeError(resource, err))
				out.Status("error", "fail", fmt.Sprintf("delete %s %s: %v", resource.ResourceType, resource.Name, err))
			} else {
				summary.Deleted++
				out.Status("ok", "ok", fmt.Sprintf("deleted %s %s", resource.ResourceType, resource.Name))
			}
		}
	}

	if len(errs) > 0 {
		return summary, fmt.Errorf("AWS fallback cleanup incomplete (%d blocked, %d manual, %d failed)", summary.Blocked, summary.Manual, summary.Failed)
	}
	return summary, nil
}

func deleteManagedResourceWithFallback(ctx context.Context, awsCfg sdkaws.Config, cc cloudControlAPI, resource sharedaudit.Resource) error {
	if handled, err := deleteResourceNatively(ctx, awsCfg, resource); handled {
		return err
	}

	service, fullType := parseServiceType(resource.ResourceType)
	cfnType, identifier := arnToCloudControl(resource.ARN, service, fullType, resource.Name)
	if cfnType == "" || identifier == "" {
		return &purgeManualError{cause: fmt.Errorf("no delete strategy for %s", resource.ResourceType), hint: "delete this resource manually or extend the fallback mapping"}
	}

	resp, err := deleteResourceWithRetry(ctx, cc, &cloudcontrol.DeleteResourceInput{
		TypeName:    sdkaws.String(cfnType),
		Identifier:  sdkaws.String(identifier),
		ClientToken: sdkaws.String(purgeClientToken(cfnType, identifier, resource.Stack)),
	})
	if err != nil {
		return err
	}
	return waitForDelete(ctx, cc, sdkaws.ToString(resp.ProgressEvent.RequestToken))
}

func deleteResourceNatively(ctx context.Context, awsCfg sdkaws.Config, resource sharedaudit.Resource) (bool, error) {
	switch resource.ResourceType {
	case resourceTypeIAMRole:
		return true, forceDeleteIAMRole(ctx, awsCfg, resource.Name)
	case "s3":
		return true, forceDeleteS3Bucket(ctx, awsCfg, resource.Name)
	default:
		return false, nil
	}
}

func forceDeleteIAMRole(ctx context.Context, awsCfg sdkaws.Config, roleName string) error {
	client := newIAMDeleteClient(awsCfg)
	if err := deleteInlineRolePolicies(ctx, client, roleName); err != nil {
		return err
	}
	if err := detachAttachedRolePolicies(ctx, client, roleName); err != nil {
		return err
	}
	_, err := client.DeleteRole(ctx, &iam.DeleteRoleInput{RoleName: sdkaws.String(roleName)})
	if err != nil && isNotFoundError(err) {
		return nil
	}
	return err
}

func forceDeleteS3Bucket(ctx context.Context, awsCfg sdkaws.Config, bucket string) error {
	client := newS3DeleteClient(awsCfg)
	if _, _, err := deleteMatchingBucketVersions(ctx, client, bucket, ""); err != nil {
		return err
	}
	_, err := client.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: sdkaws.String(bucket)})
	if err != nil && isS3BucketMissing(err) {
		return nil
	}
	return err
}

func deleteInlineRolePolicies(ctx context.Context, client *iam.Client, roleName string) error {
	var marker *string
	for {
		out, err := client.ListRolePolicies(ctx, &iam.ListRolePoliciesInput{RoleName: sdkaws.String(roleName), Marker: marker})
		if err != nil {
			if isNotFoundError(err) {
				return nil
			}
			return err
		}
		for _, policyName := range out.PolicyNames {
			if err := deleteInlineRolePolicy(ctx, client, roleName, policyName); err != nil {
				return err
			}
		}
		if !out.IsTruncated {
			return nil
		}
		marker = out.Marker
	}
}

func deleteInlineRolePolicy(ctx context.Context, client *iam.Client, roleName, policyName string) error {
	_, err := client.DeleteRolePolicy(ctx, &iam.DeleteRolePolicyInput{RoleName: sdkaws.String(roleName), PolicyName: sdkaws.String(policyName)})
	if err != nil && !isNotFoundError(err) {
		return err
	}
	return nil
}

func detachAttachedRolePolicies(ctx context.Context, client *iam.Client, roleName string) error {
	var marker *string
	for {
		out, err := client.ListAttachedRolePolicies(ctx, &iam.ListAttachedRolePoliciesInput{RoleName: sdkaws.String(roleName), Marker: marker})
		if err != nil {
			if isNotFoundError(err) {
				return nil
			}
			return err
		}
		for _, policy := range out.AttachedPolicies {
			if err := detachAttachedRolePolicy(ctx, client, roleName, policy.PolicyArn); err != nil {
				return err
			}
		}
		if !out.IsTruncated {
			return nil
		}
		marker = out.Marker
	}
}

func detachAttachedRolePolicy(ctx context.Context, client *iam.Client, roleName string, policyArn *string) error {
	_, err := client.DetachRolePolicy(ctx, &iam.DetachRolePolicyInput{RoleName: sdkaws.String(roleName), PolicyArn: policyArn})
	if err != nil && !isNotFoundError(err) {
		return err
	}
	return nil
}

func deleteMatchingBucketVersions(ctx context.Context, client stateS3API, bucket, key string) (int, int, error) {
	deletedVersions := 0
	deletedMarkers := 0
	err := walkObjectVersionPages(ctx, client, bucket, key, func(out *s3.ListObjectVersionsOutput) error {
		var err error
		deletedVersions, err = deleteBucketVersionsPage(ctx, client, bucket, key, out.Versions, deletedVersions)
		if err != nil {
			return err
		}
		deletedMarkers, err = deleteBucketDeleteMarkersPage(ctx, client, bucket, key, out.DeleteMarkers, deletedMarkers)
		return err
	})
	return deletedVersions, deletedMarkers, err
}

func deleteBucketVersionsPage(ctx context.Context, client stateS3API, bucket, key string, versions []s3types.ObjectVersion, deleted int) (int, error) {
	for _, version := range versions {
		if !matchesObjectKey(version.Key, key) {
			continue
		}
		if err := deleteBucketObjectVersion(ctx, client, bucket, version.Key, version.VersionId); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

func deleteBucketDeleteMarkersPage(ctx context.Context, client stateS3API, bucket, key string, markers []s3types.DeleteMarkerEntry, deleted int) (int, error) {
	for _, marker := range markers {
		if !matchesObjectKey(marker.Key, key) {
			continue
		}
		if err := deleteBucketObjectVersion(ctx, client, bucket, marker.Key, marker.VersionId); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

func walkObjectVersionPages(ctx context.Context, client stateS3API, bucket, prefix string, visit func(*s3.ListObjectVersionsOutput) error) error {
	var keyMarker, versionMarker *string
	for {
		out, err := client.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{Bucket: sdkaws.String(bucket), Prefix: sdkaws.String(prefix), KeyMarker: keyMarker, VersionIdMarker: versionMarker})
		if err != nil {
			if isS3BucketMissing(err) {
				return nil
			}
			return err
		}
		if err := visit(out); err != nil {
			return err
		}
		if !sdkaws.ToBool(out.IsTruncated) {
			return nil
		}
		keyMarker = out.NextKeyMarker
		versionMarker = out.NextVersionIdMarker
	}
}

func matchesObjectKey(objectKey *string, expected string) bool {
	if expected == "" {
		return true
	}
	return sdkaws.ToString(objectKey) == expected
}

func deleteBucketObjectVersion(ctx context.Context, client stateS3API, bucket string, key, versionID *string) error {
	_, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: sdkaws.String(bucket), Key: key, VersionId: versionID})
	if err != nil && !isNotFoundError(err) {
		return err
	}
	return nil
}

func parseServiceType(resourceType string) (service, fullType string) {
	parts := strings.SplitN(resourceType, "/", 2)
	if len(parts) == 1 {
		return resourceType, resourceType
	}
	return parts[0], resourceType
}

type cloudControlMapping struct {
	cfnType    string
	identifier func(name, arn string) string
}

func cloudControlServiceMapping(service string) (cloudControlMapping, bool) {
	mapping, ok := map[string]cloudControlMapping{
		"sns": {cfnType: "AWS::SNS::Topic", identifier: cloudControlARNIdentifier},
		"s3":  {cfnType: "AWS::S3::Bucket", identifier: cloudControlNameIdentifier},
	}[service]
	return mapping, ok
}

func cloudControlTypedMapping(service, resourceType string) (cloudControlMapping, bool) {
	mapping, ok := map[string]cloudControlMapping{
		"dynamodb|dynamodb/table":            {cfnType: "AWS::DynamoDB::Table", identifier: cloudControlNameIdentifier},
		"lambda|lambda/function":             {cfnType: "AWS::Lambda::Function", identifier: cloudControlNameIdentifier},
		"logs|logs/log-group":                {cfnType: "AWS::Logs::LogGroup", identifier: cloudControlNameIdentifier},
		"apigatewayv2|apigatewayv2/api":      {cfnType: "AWS::ApiGatewayV2::Api", identifier: func(name, arn string) string { return apigatewayID(name, arn) }},
		"cloudtrail|cloudtrail/trail":        {cfnType: "AWS::CloudTrail::Trail", identifier: cloudControlNameIdentifier},
		"acm|acm/certificate":                {cfnType: "AWS::CertificateManager::Certificate", identifier: cloudControlARNIdentifier},
		"cloudfront|cloudfront/distribution": {cfnType: "AWS::CloudFront::Distribution", identifier: func(name, arn string) string { return cloudfrontDistributionID(name, arn) }},
		"route53|route53/hostedzone":         {cfnType: "AWS::Route53::HostedZone", identifier: func(name, arn string) string { return route53HostedZoneID(name, arn) }},
		"kms|kms/key":                        {cfnType: "AWS::KMS::Key", identifier: cloudControlARNIdentifier},
	}[service+"|"+resourceType]
	return mapping, ok
}

func arnToCloudControl(arn, service, resourceType, name string) (string, string) {
	if mapping, ok := cloudControlServiceMapping(service); ok {
		return mapping.cfnType, mapping.identifier(name, arn)
	}
	if mapping, ok := cloudControlTypedMapping(service, resourceType); ok {
		return mapping.cfnType, mapping.identifier(name, arn)
	}
	if service == "iam" && resourceType == resourceTypeIAMRole {
		return "AWS::IAM::Role", name
	}
	return "", ""
}

func cloudControlNameIdentifier(name, _ string) string {
	return name
}

func cloudControlARNIdentifier(_, arn string) string {
	return arn
}

func apigatewayID(name, arn string) string {
	if name != "" {
		return name
	}
	parts := strings.Split(arn, "/apis/")
	if len(parts) == 2 {
		return strings.Split(parts[1], "/")[0]
	}
	return ""
}

func cloudfrontDistributionID(name, arn string) string {
	if name != "" {
		return name
	}
	parts := strings.Split(arn, "distribution/")
	if len(parts) == 2 {
		return parts[1]
	}
	return ""
}

func route53HostedZoneID(name, arn string) string {
	if name != "" && strings.HasPrefix(name, "Z") {
		return name
	}
	parts := strings.Split(arn, "hostedzone/")
	if len(parts) == 2 {
		return parts[1]
	}
	return name
}

func waitForDelete(ctx context.Context, cc cloudControlAPI, token string) error {
	for {
		out, err := cc.GetResourceRequestStatus(ctx, &cloudcontrol.GetResourceRequestStatusInput{RequestToken: sdkaws.String(token)})
		if err != nil {
			if classifyPurgeDeleteError(err) == purgeFailureRetryable {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-purgeAfter(2 * time.Second):
					continue
				}
			}
			return fmt.Errorf("polling delete status: %w", err)
		}
		switch out.ProgressEvent.OperationStatus {
		case cctypes.OperationStatusSuccess:
			return nil
		case cctypes.OperationStatusFailed, cctypes.OperationStatusCancelComplete:
			return fmt.Errorf("delete failed: %s", sdkaws.ToString(out.ProgressEvent.StatusMessage))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-purgeAfter(2 * time.Second):
		}
	}
}

func deleteResourceWithRetry(ctx context.Context, cc cloudControlAPI, input *cloudcontrol.DeleteResourceInput) (*cloudcontrol.DeleteResourceOutput, error) {
	backoff := time.Second
	for attempt := 1; ; attempt++ {
		resp, err := cc.DeleteResource(ctx, input)
		if err == nil {
			return resp, nil
		}
		if classifyPurgeDeleteError(err) != purgeFailureRetryable || attempt >= 5 {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-purgeAfter(backoff):
		}
		if backoff < 8*time.Second {
			backoff *= 2
		}
	}
}

func classifyPurgeDeleteError(err error) purgeFailureDisposition {
	if err == nil {
		return purgeFailureFatal
	}
	var manual *purgeManualError
	if errors.As(err, &manual) {
		return purgeFailureManual
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "was not found"), strings.Contains(msg, "does not exist"), strings.Contains(msg, "not found"), strings.Contains(msg, "nosuchentity"), strings.Contains(msg, "nosuchbucket"):
		return purgeFailureGone
	case strings.Contains(msg, "throttling"), strings.Contains(msg, "rate exceeded"), strings.Contains(msg, "too many requests"):
		return purgeFailureRetryable
	case strings.Contains(msg, "unsupported"), strings.Contains(msg, "does not support delete"), strings.Contains(msg, "typenotfound"):
		return purgeFailureManual
	case strings.Contains(msg, "dependency"), strings.Contains(msg, "resourceinuse"), strings.Contains(msg, "targets"):
		return purgeFailureBlocked
	default:
		return purgeFailureFatal
	}
}

func purgeClientToken(cfnType, identifier, stackTag string) string {
	if strings.TrimSpace(stackTag) == "" {
		stackTag = "stack"
	}
	sum := sha256.Sum256([]byte(cfnType + "|" + identifier))
	return stackTag + "-purge-" + base64.RawStdEncoding.EncodeToString(sum[:])
}

func loadBackendStateConfigForNuke(root, stack, env string, parseAssignments ParseAssignmentsFunc) (backendStateConfig, error) {
	envCfg, err := parseAssignments(filepath.Join(root, tfexec.EnvsDirName, env, "backend.hcl"))
	if err != nil {
		return backendStateConfig{}, err
	}
	cfg := backendStateConfig{
		BucketName: strings.TrimSpace(envCfg["bucket"]),
		TableName:  strings.TrimSpace(envCfg["dynamodb_table"]),
		StateKey:   strings.TrimSpace(envCfg["key"]),
	}
	localPath := filepath.Join(stack, "backend.local.hcl")
	if _, err := os.Stat(localPath); err == nil {
		localCfg, err := parseAssignments(localPath)
		if err != nil {
			return backendStateConfig{}, err
		}
		if strings.TrimSpace(localCfg["bucket"]) != "" {
			cfg.BucketName = strings.TrimSpace(localCfg["bucket"])
		}
		if strings.TrimSpace(localCfg["dynamodb_table"]) != "" {
			cfg.TableName = strings.TrimSpace(localCfg["dynamodb_table"])
		}
	}
	if cfg.BucketName == "" || cfg.TableName == "" || cfg.StateKey == "" {
		return backendStateConfig{}, fmt.Errorf("backend config incomplete: bucket=%q table=%q key=%q", cfg.BucketName, cfg.TableName, cfg.StateKey)
	}
	return cfg, nil
}

func resetBackendStateForNuke(ctx context.Context, opts FallbackOptions, backupDir string) (backendResetSummary, error) {
	cfg, err := loadBackendStateConfigForNuke(opts.Root, opts.Stack, opts.Env, opts.ParseAssignments)
	if err != nil {
		return backendResetSummary{}, err
	}
	s3Client := newS3DeleteClient(opts.AWSConfig)
	dynamoClient := newDynamoDeleteClient(opts.AWSConfig)
	summary := backendResetSummary{BucketName: cfg.BucketName, TableName: cfg.TableName, StateKey: cfg.StateKey, BackupDir: backupDir}

	if err := backupBackendState(ctx, s3Client, dynamoClient, cfg, backupDir, &summary); err != nil {
		return backendResetSummary{}, err
	}
	summary.DeletedStateVersions, summary.DeletedDeleteMarkers, err = deleteMatchingBucketVersions(ctx, s3Client, cfg.BucketName, cfg.StateKey)
	if err != nil {
		return backendResetSummary{}, err
	}
	if err := deleteLockEntries(ctx, dynamoClient, cfg.TableName, cfg.StateKey); err != nil {
		return backendResetSummary{}, err
	}
	if err := removeTerraformCache(filepath.Join(opts.Stack, ".terraform"), &summary); err != nil {
		return backendResetSummary{}, err
	}
	return summary, nil
}

func backupStateVersions(ctx context.Context, client stateS3API, bucket, key, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	index := 0
	manifest := []map[string]any{}
	if err := walkObjectVersionPages(ctx, client, bucket, key, func(out *s3.ListObjectVersionsOutput) error {
		for _, version := range out.Versions {
			if sdkaws.ToString(version.Key) != key {
				continue
			}
			index++
			entry, err := backupStateVersion(ctx, client, bucket, key, dir, index, version)
			if err != nil {
				return err
			}
			manifest = append(manifest, entry)
		}
		for _, marker := range out.DeleteMarkers {
			if sdkaws.ToString(marker.Key) == key {
				manifest = append(manifest, deleteMarkerManifestEntry(key, marker))
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return writeJSONFile(filepath.Join(dir, "manifest.json"), manifest)
}

func downloadBucketVersion(ctx context.Context, client stateS3API, bucket, key, versionID, target string) (retErr error) {
	out, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: sdkaws.String(bucket), Key: sdkaws.String(key), VersionId: sdkaws.String(versionID)})
	if err != nil {
		return err
	}
	defer func() { _ = out.Body.Close() }()
	file, err := os.Create(target)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := file.Close(); cerr != nil && retErr == nil {
			retErr = cerr
		}
	}()
	_, err = io.Copy(file, out.Body)
	return err
}

func backupLockEntries(ctx context.Context, client stateDynamoAPI, table, key, target string) ([]map[string]dbtypes.AttributeValue, error) {
	items, err := scanLockEntries(ctx, client, table, key)
	if err != nil {
		return nil, err
	}
	serializable, err := marshalLockEntries(items)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return nil, err
	}
	if err := writeJSONFile(target, serializable); err != nil {
		return nil, err
	}
	return items, nil
}

func writeJSONFile(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") || strings.Contains(msg, "does not exist") || strings.Contains(msg, "cannot be found") || strings.Contains(msg, "nosuchentity") || strings.Contains(msg, "resourcenotfound")
}

func isS3BucketMissing(err error) bool {
	if err == nil {
		return false
	}
	var notFound *s3types.NotFound
	if errors.As(err, &notFound) {
		return true
	}
	return isNotFoundError(err)
}

func formatPurgeError(resource sharedaudit.Resource, err error) string {
	return fmt.Sprintf(purgeErrorFormat, resource.ResourceType, resource.Name, err)
}

func backupBackendState(ctx context.Context, s3Client stateS3API, dynamoClient stateDynamoAPI, cfg backendStateConfig, backupDir string, summary *backendResetSummary) error {
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return err
	}
	if err := backupStateVersions(ctx, s3Client, cfg.BucketName, cfg.StateKey, filepath.Join(backupDir, "s3")); err != nil {
		return err
	}
	lockItems, err := backupLockEntries(ctx, dynamoClient, cfg.TableName, cfg.StateKey, filepath.Join(backupDir, "dynamodb", "locks.json"))
	if err != nil {
		return err
	}
	summary.DeletedLockEntries = len(lockItems)
	return nil
}

func deleteLockEntries(ctx context.Context, client stateDynamoAPI, table, key string) error {
	items, err := scanLockEntries(ctx, client, table, key)
	if err != nil {
		return err
	}
	for _, item := range items {
		lockID, ok := item["LockID"].(*dbtypes.AttributeValueMemberS)
		if !ok {
			continue
		}
		_, err := client.DeleteItem(ctx, &dynamodb.DeleteItemInput{TableName: sdkaws.String(table), Key: map[string]dbtypes.AttributeValue{"LockID": &dbtypes.AttributeValueMemberS{Value: lockID.Value}}})
		if err != nil && !isNotFoundError(err) {
			return err
		}
	}
	return nil
}

func removeTerraformCache(path string, summary *backendResetSummary) error {
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	summary.RemovedLocalTerraform = true
	return nil
}

func backupStateVersion(ctx context.Context, client stateS3API, bucket, key, dir string, index int, version s3types.ObjectVersion) (map[string]any, error) {
	name := fmt.Sprintf(purgeStateFileFormat, index)
	if err := downloadBucketVersion(ctx, client, bucket, key, sdkaws.ToString(version.VersionId), filepath.Join(dir, name)); err != nil {
		return nil, err
	}
	return map[string]any{"file": name, "key": key, "version_id": sdkaws.ToString(version.VersionId), "is_latest": sdkaws.ToBool(version.IsLatest)}, nil
}

func deleteMarkerManifestEntry(key string, marker s3types.DeleteMarkerEntry) map[string]any {
	return map[string]any{"delete_marker": true, "key": key, "version_id": sdkaws.ToString(marker.VersionId), "is_latest": sdkaws.ToBool(marker.IsLatest)}
}

func scanLockEntries(ctx context.Context, client stateDynamoAPI, table, key string) ([]map[string]dbtypes.AttributeValue, error) {
	if err := ensureLockTableExists(ctx, client, table); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	items := make([]map[string]dbtypes.AttributeValue, 0)
	var startKey map[string]dbtypes.AttributeValue
	for {
		out, err := client.Scan(ctx, &dynamodb.ScanInput{TableName: sdkaws.String(table), ExclusiveStartKey: startKey})
		if err != nil {
			return nil, err
		}
		for _, item := range out.Items {
			if lockEntryMatches(item, key) {
				items = append(items, item)
			}
		}
		if len(out.LastEvaluatedKey) == 0 {
			return items, nil
		}
		startKey = out.LastEvaluatedKey
	}
}

func ensureLockTableExists(ctx context.Context, client stateDynamoAPI, table string) error {
	_, err := client.DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: sdkaws.String(table)})
	if err == nil {
		return nil
	}
	var notFound *dbtypes.ResourceNotFoundException
	if errors.As(err, &notFound) {
		return os.ErrNotExist
	}
	return err
}

func lockEntryMatches(item map[string]dbtypes.AttributeValue, key string) bool {
	member, ok := item["LockID"].(*dbtypes.AttributeValueMemberS)
	return ok && member.Value == key
}

func marshalLockEntries(items []map[string]dbtypes.AttributeValue) ([]map[string]any, error) {
	serializable := make([]map[string]any, 0, len(items))
	for _, item := range items {
		var decoded map[string]any
		if err := attributevalue.UnmarshalMap(item, &decoded); err != nil {
			return nil, err
		}
		serializable = append(serializable, decoded)
	}
	return serializable, nil
}
