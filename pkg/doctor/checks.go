package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	backendEnvCheckKey     = "contract.backend-env"
	backendLocalCheckKey   = "contract.backend-local"
	backendLocalCheckTitle = "backend.local.hcl override"
	fetchedVarsCheckKey    = "contract.fetched-vars"
)

func ParseSimpleAssignments(path string) (map[string]string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is from operator-controlled config
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
			continue
		}
		if idx := strings.Index(trimmed, "="); idx >= 0 {
			key := strings.TrimSpace(trimmed[:idx])
			value := strings.TrimSpace(trimmed[idx+1:])
			value = strings.Trim(value, `"`)
			values[key] = value
		}
	}
	return values, nil
}

func CheckDirExists(key, title, path string, blocking bool) Check {
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		if err == nil {
			err = fmt.Errorf("%s is not a directory", path)
		}
		return Check{
			Key:      key,
			Title:    title,
			Status:   "fail",
			Detail:   err.Error(),
			Hint:     "restore the repository layout before running terraform commands",
			Blocking: blocking,
		}
	}
	return Check{Key: key, Title: title, Status: "ok", Detail: path}
}

func CheckFileExists(key, title, path string, blocking bool) Check {
	if _, err := os.Stat(path); err != nil {
		return Check{
			Key:      key,
			Title:    title,
			Status:   "fail",
			Detail:   err.Error(),
			Hint:     "restore the file from version control or regenerate it before retrying",
			Blocking: blocking,
		}
	}
	return Check{Key: key, Title: title, Status: "ok", Detail: path}
}

func CheckEnvBackendFile(path, env string) Check {
	values, err := ParseSimpleAssignments(path)
	if err != nil {
		return Check{
			Key:      backendEnvCheckKey,
			Title:    "environment backend.hcl is readable",
			Status:   "fail",
			Detail:   err.Error(),
			Hint:     "restore envs/" + env + "/backend.hcl from version control",
			Blocking: true,
		}
	}
	if strings.TrimSpace(values["key"]) == "" {
		return Check{
			Key:      backendEnvCheckKey,
			Title:    "environment backend.hcl contains the state key",
			Status:   "fail",
			Detail:   "missing required key entry",
			Hint:     "regenerate backend configuration from bootstrap or restore the committed file",
			Blocking: true,
		}
	}
	return Check{
		Key:    backendEnvCheckKey,
		Title:  "environment backend.hcl contains the state key",
		Status: "ok",
		Detail: "state key is configured for env " + env,
	}
}

func CheckOptionalBackendLocal(path string) Check {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return Check{
			Key:    backendLocalCheckKey,
			Title:  backendLocalCheckTitle,
			Status: "info",
			Detail: "no local override present",
			Hint:   "this is optional; create it only when overriding backend bucket, lock table, or region",
		}
	}
	values, err := ParseSimpleAssignments(path)
	if err != nil {
		return Check{
			Key:      backendLocalCheckKey,
			Title:    backendLocalCheckTitle,
			Status:   "fail",
			Detail:   err.Error(),
			Hint:     "fix or remove stack/backend.local.hcl before retrying",
			Blocking: true,
		}
	}
	missing := make([]string, 0, 3)
	for _, key := range []string{"bucket", "dynamodb_table", "region"} {
		if strings.TrimSpace(values[key]) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return Check{
			Key:      backendLocalCheckKey,
			Title:    backendLocalCheckTitle,
			Status:   "fail",
			Detail:   "missing required keys: " + strings.Join(missing, ", "),
			Hint:     "either populate all override keys or remove stack/backend.local.hcl",
			Blocking: true,
		}
	}
	return Check{
		Key:    backendLocalCheckKey,
		Title:  backendLocalCheckTitle,
		Status: "ok",
		Detail: "override is complete",
	}
}

func CheckFetchedVars(path, env string) Check {
	data, err := os.ReadFile(path) //nolint:gosec // path is from operator-controlled config
	if err != nil {
		if os.IsNotExist(err) {
			return Check{
				Key:    fetchedVarsCheckKey,
				Title:  "fetched platform config is present",
				Status: "warn",
				Detail: "envs/" + env + "/fetched.auto.tfvars.json is missing",
				Hint:   "run make fetch ENV=" + env + " before apply if this stack depends on bootstrap-exported values",
			}
		}
		return Check{
			Key:      fetchedVarsCheckKey,
			Title:    "fetched platform config is present",
			Status:   "fail",
			Detail:   err.Error(),
			Hint:     "restore or regenerate fetched.auto.tfvars.json before retrying",
			Blocking: true,
		}
	}
	var parsed any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return Check{
			Key:      fetchedVarsCheckKey,
			Title:    "fetched platform config is valid JSON",
			Status:   "fail",
			Detail:   err.Error(),
			Hint:     "re-run make fetch ENV=" + env + " to regenerate the file",
			Blocking: true,
		}
	}
	return Check{
		Key:    fetchedVarsCheckKey,
		Title:  "fetched platform config is valid JSON",
		Status: "ok",
		Detail: filepath.Base(path) + " is readable",
	}
}

func CheckOptionalDir(path string) Check {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return Check{
			Key:    "workspace.terraform-dir",
			Title:  ".terraform workspace exists",
			Status: "ok",
			Detail: "terraform init has already been run in this workspace",
		}
	}
	return Check{
		Key:    "workspace.terraform-dir",
		Title:  ".terraform workspace exists",
		Status: "warn",
		Detail: "terraform has not been initialised yet in this workspace",
		Hint:   "the apply and plan commands will run terraform init automatically when needed",
	}
}
