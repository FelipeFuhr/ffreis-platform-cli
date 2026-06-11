BASH_SHELL := /usr/bin/env bash
SHELL := $(BASH_SHELL)

BINARY     := platform-cli
BUILD_DIR  := ./bin
MODULE     := github.com/ffreis/platform-cli
CMD_PKG    := ./cmd/$(BINARY)
CMD_DIR    := cmd/$(BINARY)
GO_VERSION := $(shell sed -n 's/^go //p' go.mod | head -n1)
MODULE_TOOLCHAIN := $(shell sed -n 's/^toolchain //p' go.mod | head -n1)
GO_TOOLCHAIN ?= $(if $(MODULE_TOOLCHAIN),$(MODULE_TOOLCHAIN),go$(GO_VERSION).0)

GOFMT         ?= gofmt
GOLANGCI_LINT_VERSION ?= v2.4.0
GITLEAKS      ?= gitleaks
COVERAGE_MIN      ?= 75
COVERAGE_PACKAGES ?= ./...

LEFTHOOK_VERSION ?= 1.7.10

MUTATION_PACKAGES ?= ./pkg/...
MUTATION_THRESHOLD ?= 60
LEFTHOOK_DIR     ?= $(CURDIR)/.bin
LEFTHOOK_BIN     ?= $(LEFTHOOK_DIR)/lefthook
LOCAL_GOLANGCI_LINT ?= $(LEFTHOOK_DIR)/golangci-lint
LOCAL_GOVULNCHECK   ?= $(LEFTHOOK_DIR)/govulncheck
GOLANGCI_LINT       ?= $(LOCAL_GOLANGCI_LINT)
GOVULNCHECK         ?= $(LOCAL_GOVULNCHECK)

# Build flags: embed version info from git at compile time.
GIT_COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
GIT_TAG     := $(shell git describe --tags --exact-match 2>/dev/null || echo "dev")
BUILD_TIME  := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS     := -ldflags "-X $(MODULE)/cmd.version=$(GIT_TAG) \
                          -X $(MODULE)/cmd.commit=$(GIT_COMMIT) \
                          -X $(MODULE)/cmd.buildTime=$(BUILD_TIME)"

.PHONY: all build clean test test-verbose test-integration test-integration-verbose test-race fmt fmt-check lint tidy \
        validate plan mutation-test \
	coverage-gate smoke-check secrets-scan-staged quality-gates hook-generated-drift \
	bootstrap-hook-tools \
	ensure-golangci-lint \
	ensure-govulncheck \
        lefthook-bootstrap lefthook-install lefthook-run lefthook \
        run-init run-init-dry run-nuke run-nuke-dry nuke-all

all: tidy build

## build: compile the binary into ./bin/
build:
	@if [ ! -d "$(CMD_DIR)" ]; then \
		echo "No CLI command package found at $(CMD_DIR); skipping build."; \
		exit 0; \
	fi
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY) $(CMD_PKG)
	@echo "built $(BUILD_DIR)/$(BINARY)"

## clean: remove build artefacts
clean:
	rm -rf $(BUILD_DIR)

## test: run all tests
test:
	go test ./...

## test-verbose: run all tests with verbose output
test-verbose:
	go test -v ./...

## test-integration: run tests including integration-tagged suites
test-integration:
	go test -tags=integration ./...

## test-integration-verbose: run integration-tagged suites with verbose output
test-integration-verbose:
	go test -tags=integration -v ./...

## fmt: format all Go source files
fmt:
	$(GOFMT) -w .

## fmt-check: fail if Go files are not gofmt-formatted
fmt-check:
	@./scripts/hooks/check_required_tools.sh $(GOFMT)
	@out="$$(find . -type f -name '*.go' -not -path './vendor/*' -not -path './.git/*' -print0 | xargs -0 -r $(GOFMT) -l)"; \
	if [ -n "$$out" ]; then \
		echo "Unformatted Go files:"; \
		echo "$$out"; \
		echo "Run: make fmt"; \
		exit 1; \
	fi

## ensure-golangci-lint: rebuild repo-local golangci-lint if missing or on the wrong major version
ensure-golangci-lint:
	@if [ "$(GOLANGCI_LINT)" = "$(LOCAL_GOLANGCI_LINT)" ]; then \
		mkdir -p $(LEFTHOOK_DIR); \
		if [ ! -x "$(LOCAL_GOLANGCI_LINT)" ] \
			|| ! "$(LOCAL_GOLANGCI_LINT)" version 2>/dev/null | grep -Eq 'golangci-lint has version (v)?2\.' \
			|| ! go version -m "$(LOCAL_GOLANGCI_LINT)" 2>/dev/null | grep -Eq '^[[:space:]]+go[[:space:]]+$(GO_TOOLCHAIN)$$'; then \
			echo "Installing golangci-lint $(GOLANGCI_LINT_VERSION) with $(GO_TOOLCHAIN) into $(LEFTHOOK_DIR)"; \
			GOTOOLCHAIN=$(GO_TOOLCHAIN) GOBIN=$(LEFTHOOK_DIR) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION); \
		fi; \
	fi

## lint: run golangci-lint
lint: ensure-golangci-lint
	@command -v $(GOLANGCI_LINT) >/dev/null 2>&1 || (echo "Missing tool: $(GOLANGCI_LINT). Run: make lefthook-bootstrap" && exit 1)
	@$(GOLANGCI_LINT) version 2>/dev/null | grep -Eq 'golangci-lint has version (v)?2\.' || (echo "golangci-lint v2 is required. Install with: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)" && exit 1)
	$(GOLANGCI_LINT) run ./...

## validate: static analysis and compilation check (go vet + build)
validate:
	go vet ./...
	go build -o /dev/null ./...

## plan: not applicable — use 'make validate' or 'make quality-gates' for Go repos
plan:
	@echo "INFO: 'plan' is Terraform-specific and does not apply to Go repos."
	@echo "      To verify compilation: make validate"
	@echo "      For full quality gates: make quality-gates"

## tidy: resolve and pin all dependencies, update go.sum
tidy:
	go mod tidy

## test-race: run tests with race detector
test-race:
	go test -race ./...

## coverage-gate: run tests with coverage; fail if below COVERAGE_MIN
coverage-gate:
	@COVERAGE_MIN="$(COVERAGE_MIN)" COVERAGE_PACKAGES="$(COVERAGE_PACKAGES)" ./scripts/hooks/check_coverage_gate.sh

## smoke-check: build binary and verify --help exits cleanly
smoke-check:
	@set -euo pipefail; \
	if [ ! -d "$(CMD_DIR)" ]; then \
		echo "No CLI command package found at $(CMD_DIR); skipping smoke check."; \
		exit 0; \
	fi; \
	tmp_bin="$$(mktemp)"; \
	trap 'rm -f "$$tmp_bin"' EXIT; \
	go build $(LDFLAGS) -o "$$tmp_bin" $(CMD_PKG) && "$$tmp_bin" --help >/dev/null

## secrets-scan-staged: scan staged diff for secrets
secrets-scan-staged:
	@command -v $(GITLEAKS) >/dev/null 2>&1 || (echo "Missing tool: $(GITLEAKS). Install: https://github.com/gitleaks/gitleaks#installing" && exit 1)
	$(GITLEAKS) protect --staged --redact

## ensure-govulncheck: rebuild repo-local govulncheck if it was built with the wrong Go toolchain
ensure-govulncheck:
	@mkdir -p $(LEFTHOOK_DIR)
	@if [ ! -x "$(LOCAL_GOVULNCHECK)" ] || ! go version -m "$(LOCAL_GOVULNCHECK)" 2>/dev/null | grep -Eq '^[[:space:]]+go[[:space:]]+$(GO_TOOLCHAIN)$$'; then \
		echo "Installing govulncheck with $(GO_TOOLCHAIN)"; \
		GOTOOLCHAIN=$(GO_TOOLCHAIN) GOBIN=$(LEFTHOOK_DIR) go install golang.org/x/vuln/cmd/govulncheck@latest; \
	fi

## quality-gates: strict pre-push checks (tests + race + coverage + vulncheck + smoke)
quality-gates: ensure-govulncheck
	@command -v $(GOVULNCHECK) >/dev/null 2>&1 || (echo "Missing tool: $(GOVULNCHECK). Run: make lefthook-bootstrap" && exit 1)
	$(MAKE) test
	$(MAKE) test-integration
	$(MAKE) test-race
	$(MAKE) coverage-gate
	$(GOVULNCHECK) ./...
	$(MAKE) smoke-check

## hook-generated-drift: run generate target if present and fail on uncommitted changes
hook-generated-drift:
	@set -euo pipefail; \
	if $(MAKE) -n generate >/dev/null 2>&1; then \
		$(MAKE) generate; \
		if ! git diff --quiet -- .; then \
			echo "Generated files are out of date. Run 'make generate' and commit updates."; \
			git status --short; \
			exit 1; \
		fi; \
	else \
		echo "No 'generate' target found; skipping generated drift check."; \
	fi

# ---------------------------------------------------------------------------
# Local run targets — require ORG, PROFILE, and ROOT_EMAIL to be set.
# Example:
#   make run-init ORG=acme PROFILE=bootstrap ROOT_EMAIL=root@acme.example.com
# ---------------------------------------------------------------------------

## run-init: execute `init` against real AWS
run-init:
ifndef ORG
	$(error ORG is required, e.g. make run-init ORG=acme PROFILE=bootstrap ROOT_EMAIL=root@acme.example.com)
endif
ifndef PROFILE
	$(error PROFILE is required)
endif
ifndef ROOT_EMAIL
	$(error ROOT_EMAIL is required)
endif
	go run $(CMD_PKG) init \
		--org=$(ORG) \
		--profile=$(PROFILE) \
		--root-email=$(ROOT_EMAIL)

## run-init-dry: dry-run `init` — no AWS calls made
run-init-dry:
ifndef ORG
	$(error ORG is required)
endif
ifndef PROFILE
	$(error PROFILE is required)
endif
ifndef ROOT_EMAIL
	$(error ROOT_EMAIL is required)
endif
	go run $(CMD_PKG) init \
		--org=$(ORG) \
		--profile=$(PROFILE) \
		--root-email=$(ROOT_EMAIL) \
		--dry-run

## run-audit: run `audit` against real AWS
run-audit:
ifndef ORG
	$(error ORG is required, e.g. make run-audit ORG=ffreis PROFILE=bootstrap)
endif
ifndef PROFILE
	$(error PROFILE is required)
endif
	go run $(CMD_PKG) audit \
		--org=$(ORG) \
		--profile=$(PROFILE)

## run-nuke: destroy all Layer 0 bootstrap resources (irreversible!)
run-nuke:
ifndef ORG
	$(error ORG is required, e.g. make run-nuke ORG=acme PROFILE=bootstrap)
endif
ifndef PROFILE
	$(error PROFILE is required)
endif
	go run $(CMD_PKG) nuke \
		--org=$(ORG) \
		--profile=$(PROFILE)

## run-nuke-dry: dry-run nuke — show what would be deleted without making AWS calls
run-nuke-dry:
ifndef ORG
	$(error ORG is required)
endif
ifndef PROFILE
	$(error PROFILE is required)
endif
	go run $(CMD_PKG) nuke \
		--org=$(ORG) \
		--profile=$(PROFILE) \
		--dry-run

# ---------------------------------------------------------------------------
# Orchestration — nuke all platform stacks in reverse dependency order.
#
# Assumes the sibling platform repos are checked out alongside this repo:
#   ../ffreis-platform-atlantis
#   ../ffreis-platform-project-template
#   ../ffreis-platform-github-oidc
#   ../ffreis-platform-org
#
# Usage:
#   make nuke-all ORG=ffreis PROFILE=bootstrap ENV=prod
# ---------------------------------------------------------------------------

## nuke-all: destroy ALL platform infrastructure in safe reverse order (irreversible!)
nuke-all:
ifndef ORG
	$(error ORG is required, e.g. make nuke-all ORG=ffreis PROFILE=bootstrap ENV=prod)
endif
ifndef PROFILE
	$(error PROFILE is required)
endif
ifndef ENV
	$(error ENV is required)
endif
	@echo "============================================================"
	@echo "  PLATFORM NUKE — ORG=$(ORG)  ENV=$(ENV)"
	@echo "  Destroying all infrastructure in reverse dependency order."
	@echo "============================================================"
	@read -p "Type 'nuke-all-$(ORG)' to confirm: " -r; \
	if [ "$$REPLY" != "nuke-all-$(ORG)" ]; then \
		echo "Cancelled."; \
		exit 1; \
	fi
	@echo "--- [1/5] Destroying Atlantis ---"
	$(MAKE) -C ../ffreis-platform-atlantis nuke ENV=$(ENV)
	@echo "--- [2/5] Destroying project-template ---"
	$(MAKE) -C ../ffreis-platform-project-template nuke ENV=$(ENV)
	@echo "--- [3/5] Destroying github-oidc ---"
	$(MAKE) -C ../ffreis-platform-github-oidc nuke ENV=$(ENV)
	@echo "--- [4/5] Destroying org stack ---"
	$(MAKE) -C ../ffreis-platform-org nuke ENV=$(ENV)
	@echo "--- [5/5] Destroying bootstrap Layer 0 ---"
	$(MAKE) run-nuke ORG=$(ORG) PROFILE=$(PROFILE)
	@echo "============================================================"
	@echo "  Nuke complete. All platform infrastructure has been removed."
	@echo "============================================================"

## run-audit-json: run `audit` and output JSON
run-audit-json:
ifndef ORG
	$(error ORG is required)
endif
ifndef PROFILE
	$(error PROFILE is required)
endif
	go run $(CMD_PKG) audit \
		--org=$(ORG) \
		--profile=$(PROFILE) \
		--json

## bootstrap-hook-tools: install repo-local Go hook tooling into ./.bin
bootstrap-hook-tools: $(LOCAL_GOLANGCI_LINT) $(LOCAL_GOVULNCHECK)
	@echo "Installed repo-local Go hook tools into $(LEFTHOOK_DIR)"

$(LOCAL_GOLANGCI_LINT):
	@mkdir -p $(LEFTHOOK_DIR)
	GOBIN=$(LEFTHOOK_DIR) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

$(LOCAL_GOVULNCHECK):
	@mkdir -p $(LEFTHOOK_DIR)
	GOTOOLCHAIN=$(GO_TOOLCHAIN) GOBIN=$(LEFTHOOK_DIR) go install golang.org/x/vuln/cmd/govulncheck@latest

## lefthook-bootstrap: download lefthook binary into ./.bin
lefthook-bootstrap: bootstrap-hook-tools
	LEFTHOOK_VERSION="$(LEFTHOOK_VERSION)" BIN_DIR="$(LEFTHOOK_DIR)" bash ./scripts/bootstrap_lefthook.sh
	@echo "Optional external dependency still required for staged secret scans: $(GITLEAKS)"

## lefthook-install: install git hooks (runs bootstrap first)
lefthook-install: lefthook-bootstrap
	@if [ -x "$(LEFTHOOK_BIN)" ] && [ -x ".git/hooks/pre-commit" ] && [ -x ".git/hooks/pre-push" ] && [ -x ".git/hooks/commit-msg" ]; then \
		echo "lefthook hooks already installed"; \
		exit 0; \
	fi
	LEFTHOOK="$(LEFTHOOK_BIN)" "$(LEFTHOOK_BIN)" install

## lefthook-run: run all hooks locally
lefthook-run: lefthook-bootstrap
	LEFTHOOK="$(LEFTHOOK_BIN)" "$(LEFTHOOK_BIN)" run pre-commit
	@tmp_msg="$$(mktemp)"; \
	echo "chore(hooks): validate commit-msg hook" > "$$tmp_msg"; \
	LEFTHOOK="$(LEFTHOOK_BIN)" "$(LEFTHOOK_BIN)" run commit-msg -- "$$tmp_msg"; \
	rm -f "$$tmp_msg"
	LEFTHOOK="$(LEFTHOOK_BIN)" "$(LEFTHOOK_BIN)" run pre-push

## lefthook: install hooks and run them
lefthook: lefthook-bootstrap lefthook-install lefthook-run

## mutation-test: run mutation testing with gremlins (slow — intended for CI/weekly)
mutation-test: ## Run mutation testing with gremlins (slow — CI only)
	@which gremlins >/dev/null 2>&1 || go install github.com/go-gremlins/gremlins/cmd/gremlins@latest
	gremlins unleash --threshold-efficacy $(MUTATION_THRESHOLD) $(MUTATION_PACKAGES)

## help: list documented targets
help:
	@grep -E '^## ' Makefile | sed 's/## /  /'

PLATFORM_STANDARDS_SHA ?= 3c787edb4e96ddea2e86b2add2c32139685e8db7  # v1.2.1
PLATFORM_STANDARDS_RAW ?= https://raw.githubusercontent.com/FelipeFuhr/ffreis-platform-standards

install-act: ## Download pinned act binary into .bin/
	@mkdir -p scripts
	@curl -fsSL "$(PLATFORM_STANDARDS_RAW)/$(PLATFORM_STANDARDS_SHA)/scripts/install_act.sh" \
		-o scripts/install_act.sh && chmod +x scripts/install_act.sh
	@bash ./scripts/install_act.sh

ci-local: ## Run workflows locally via act (GH Actions quota fallback). Args via ARGS=...
	@mkdir -p scripts
	@curl -fsSL "https://raw.githubusercontent.com/FelipeFuhr/ffreis-platform-ci-local/v1.0.0/scripts/run-ci-local.sh" \
		-o scripts/run-ci-local.sh && chmod +x scripts/run-ci-local.sh
	@CI_LOCAL_FINDINGS_REF=v1.0.0 PATH="$(CURDIR)/.bin:$(PATH)" bash ./scripts/run-ci-local.sh $(ARGS)
