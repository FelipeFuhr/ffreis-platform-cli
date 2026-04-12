package inventory

import (
	"reflect"
	"testing"
)

func TestSummaryParts(t *testing.T) {
	got := Contract{StackTag: "stack", RepoTag: "repo", LayerTag: "layer", TerraformRootTag: "root"}.SummaryParts()
	want := []string{"stack=stack", "repo=repo", "layer=layer", "root=root"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SummaryParts() = %#v, want %#v", got, want)
	}
}

func TestMissingTagsSorted(t *testing.T) {
	got := MissingTags(map[string]string{"Project": "x", "Stack": "y"}, []string{"Stack", "ManagedBy", "Project", "Environment"})
	want := []string{"Environment", "ManagedBy"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MissingTags() = %#v, want %#v", got, want)
	}
}
