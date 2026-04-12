package audit

import (
	"context"
	"sort"
	"strings"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi"
	taggingtypes "github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi/types"
	"github.com/ffreis/platform-cli/pkg/inventory"
)

type PageFetcher func(context.Context, *resourcegroupstaggingapi.GetResourcesInput) (*resourcegroupstaggingapi.GetResourcesOutput, error)

type Resource struct {
	Status       string            `json:"status"`
	ResourceType string            `json:"resource_type"`
	Name         string            `json:"name"`
	ARN          string            `json:"arn"`
	Stack        string            `json:"stack"`
	ManagedBy    string            `json:"managed_by"`
	Environment  string            `json:"environment"`
	Repo         string            `json:"repo"`
	Layer        string            `json:"layer"`
	Issues       []string          `json:"issues,omitempty"`
	Tags         map[string]string `json:"tags,omitempty"`
}

type Summary struct {
	Owned            int `json:"owned"`
	OwnedWarn        int `json:"owned_warn"`
	OtherManaged     int `json:"other_managed"`
	OtherManagedWarn int `json:"other_managed_warn"`
	Unowned          int `json:"unowned"`
	Total            int `json:"total"`
}

type Report struct {
	Owned        []Resource `json:"owned"`
	OtherManaged []Resource `json:"other_managed"`
	Unowned      []Resource `json:"unowned"`
	Summary      Summary    `json:"summary"`
}

func ScanResources(ctx context.Context, fetch PageFetcher, contract inventory.Contract, env string) ([]Resource, error) {
	var results []Resource
	var nextToken *string
	for {
		out, err := fetch(ctx, &resourcegroupstaggingapi.GetResourcesInput{
			ResourcesPerPage: sdkaws.Int32(100),
			PaginationToken:  nextToken,
		})
		if err != nil {
			return nil, err
		}
		for _, mapping := range out.ResourceTagMappingList {
			results = append(results, ClassifyResource(mapping, contract, env))
		}
		if sdkaws.ToString(out.PaginationToken) == "" {
			break
		}
		nextToken = out.PaginationToken
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Status != results[j].Status {
			return results[i].Status < results[j].Status
		}
		if results[i].ResourceType != results[j].ResourceType {
			return results[i].ResourceType < results[j].ResourceType
		}
		return results[i].Name < results[j].Name
	})
	return results, nil
}

func ClassifyResource(mapping taggingtypes.ResourceTagMapping, contract inventory.Contract, env string) Resource {
	resource := newClassifiedResource(mapping)
	missingCore := inventory.MissingTags(resource.Tags, inventory.CoreTagKeys)
	missingOwnership := inventory.MissingTags(resource.Tags, inventory.OwnershipTagKeys)
	assignResourceStatus(&resource, contract, env, missingCore, missingOwnership)
	applyOwnedResourceDefaults(&resource, contract)
	return resource
}

func newClassifiedResource(mapping taggingtypes.ResourceTagMapping) Resource {
	tags := make(map[string]string, len(mapping.Tags))
	for _, tag := range mapping.Tags {
		tags[sdkaws.ToString(tag.Key)] = sdkaws.ToString(tag.Value)
	}
	resourceARN := sdkaws.ToString(mapping.ResourceARN)
	resourceType := ResourceTypeFromARN(resourceARN)
	return Resource{
		ResourceType: resourceType,
		ARN:          resourceARN,
		Name:         ResourceNameFromARN(resourceARN, resourceType),
		Stack:        tags["Stack"],
		ManagedBy:    tags["ManagedBy"],
		Environment:  tags["Environment"],
		Repo:         tags["TerraformRepo"],
		Layer:        tags["Layer"],
		Tags:         tags,
	}
}

func assignResourceStatus(resource *Resource, contract inventory.Contract, env string, missingCore, missingOwnership []string) {
	if resource.ManagedBy == "terraform" && resource.Stack == contract.StackTag && resource.Environment == env {
		resource.Status = "OWNED"
		appendMissingTagIssue(resource, "missing ownership tags", missingOwnership)
		return
	}
	if resource.ManagedBy != "" || resource.Stack != "" {
		resource.Status = "OTHER_MANAGED"
		appendMissingTagIssue(resource, "missing core tags", missingCore)
		if resource.ManagedBy == "terraform" {
			appendMissingTagIssue(resource, "missing ownership tags", missingOwnership)
		}
		return
	}
	resource.Status = "UNOWNED"
	appendMissingTagIssue(resource, "missing core tags", missingCore)
}

func appendMissingTagIssue(resource *Resource, prefix string, missing []string) {
	if len(missing) == 0 {
		return
	}
	resource.Issues = append(resource.Issues, prefix+": "+strings.Join(missing, ", "))
}

func applyOwnedResourceDefaults(resource *Resource, contract inventory.Contract) {
	if resource.Status != "OWNED" {
		return
	}
	if resource.Repo == "" {
		resource.Repo = contract.RepoTag
	}
	if resource.Layer == "" {
		resource.Layer = contract.LayerTag
	}
}

func BuildReport(resources []Resource) Report {
	report := Report{}
	for _, resource := range resources {
		report.Summary.Total++
		switch resource.Status {
		case "OWNED":
			report.Owned = append(report.Owned, resource)
			report.Summary.Owned++
			if len(resource.Issues) > 0 {
				report.Summary.OwnedWarn++
			}
		case "OTHER_MANAGED":
			report.OtherManaged = append(report.OtherManaged, resource)
			report.Summary.OtherManaged++
			if len(resource.Issues) > 0 {
				report.Summary.OtherManagedWarn++
			}
		default:
			report.Unowned = append(report.Unowned, resource)
			report.Summary.Unowned++
		}
	}
	return report
}

func ResourceTypeFromARN(arn string) string {
	if arn == "" {
		return "unknown"
	}
	if strings.HasPrefix(arn, "arn:aws:s3:::") {
		return "s3"
	}
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) < 6 {
		return "unknown"
	}
	service := parts[2]
	resource := strings.TrimPrefix(parts[5], "/")

	switch {
	case service == "logs" && strings.HasPrefix(resource, "log-group:"):
		return "logs/log-group"
	case service == "route53" && strings.HasPrefix(resource, "hostedzone/"):
		return "route53/hostedzone"
	case service == "cloudfront" && strings.HasPrefix(resource, "distribution/"):
		return "cloudfront/distribution"
	case service == "apigateway" && strings.HasPrefix(resource, "apis/"):
		return "apigatewayv2/api"
	case service == "iam" && strings.HasPrefix(resource, "role/"):
		return "iam/role"
	case service == "lambda" && strings.HasPrefix(resource, "function:"):
		return "lambda/function"
	case strings.Contains(resource, "/"):
		return service + "/" + strings.Split(resource, "/")[0]
	case strings.Contains(resource, ":"):
		return service + "/" + strings.Split(resource, ":")[0]
	default:
		return service + "/" + resource
	}
}

func ResourceNameFromARN(arn, resourceType string) string {
	if arn == "" {
		return ""
	}
	switch resourceType {
	case "s3":
		return strings.TrimPrefix(arn, "arn:aws:s3:::")
	case "logs/log-group":
		parts := strings.SplitN(arn, ":log-group:", 2)
		if len(parts) == 2 {
			return strings.TrimSuffix(parts[1], ":*")
		}
	case "route53/hostedzone":
		parts := strings.Split(arn, "hostedzone/")
		return parts[len(parts)-1]
	case "cloudfront/distribution":
		parts := strings.Split(arn, "distribution/")
		return parts[len(parts)-1]
	case "apigatewayv2/api":
		parts := strings.Split(arn, "/apis/")
		if len(parts) == 2 {
			return strings.Split(parts[1], "/")[0]
		}
	}
	trimmed := arn
	if idx := strings.LastIndexAny(trimmed, "/:"); idx >= 0 && idx+1 < len(trimmed) {
		return trimmed[idx+1:]
	}
	return trimmed
}

func MatchedResourceKey(resource Resource) string {
	if resource.ARN != "" {
		return strings.ToLower(resource.ARN)
	}
	return strings.ToLower(resource.Stack + "|" + resource.Environment + "|" + resource.ResourceType + "|" + resource.Name)
}
