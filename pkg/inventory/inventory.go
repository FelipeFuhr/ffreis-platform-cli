package inventory

import "sort"

var (
	CoreTagKeys      = []string{"Project", "Environment", "ManagedBy", "Stack"}
	OwnershipTagKeys = []string{"Layer", "TerraformRepo", "TerraformRoot"}
)

type Contract struct {
	DisplayName      string
	StackTag         string
	RepoTag          string
	LayerTag         string
	TerraformRootTag string
}

func (c Contract) SummaryParts() []string {
	parts := make([]string, 0, 4)
	if c.StackTag != "" {
		parts = append(parts, "stack="+c.StackTag)
	}
	if c.RepoTag != "" {
		parts = append(parts, "repo="+c.RepoTag)
	}
	if c.LayerTag != "" {
		parts = append(parts, "layer="+c.LayerTag)
	}
	if c.TerraformRootTag != "" {
		parts = append(parts, "root="+c.TerraformRootTag)
	}
	return parts
}

func MissingTags(tags map[string]string, required []string) []string {
	missing := make([]string, 0, len(required))
	for _, key := range required {
		if tags[key] == "" {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	return missing
}
