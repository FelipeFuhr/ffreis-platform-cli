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
	if got := ResourceTypeFromARN("arn:aws:cloudfront::123456789012:distribution/ABC"); got != "cloudfront/distribution" {
		t.Fatalf("ResourceTypeFromARN() = %q", got)
	}
	if got := ResourceNameFromARN("arn:aws:cloudfront::123456789012:distribution/ABC", "cloudfront/distribution"); got != "ABC" {
		t.Fatalf("ResourceNameFromARN() = %q", got)
	}
}
