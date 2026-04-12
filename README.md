# ffreis-platform-cli

Shared Go toolkit for downstream platform Terraform CLIs.

This module is intended for repos such as:

- `ffreis-platform-shared-infra`
- `ffreis-flemming-infra`
- `ffreis-website-infra`

It provides reusable building blocks for:

- Cobra root-command construction for downstream Terraform CLIs
- Standard local commands such as `version`
- AWS profile loading and `platform-admin` role assumption
- Terraform subprocess execution helpers
- Plain-text command output helpers

The module is intentionally small. Repo-specific commands and validation rules
stay in each downstream repo.

Development hygiene:

- local hooks are managed with `lefthook`
- install toolchain locally with `make lefthook-bootstrap`
- install hooks with `make lefthook-install`
- CI also runs the configured `pre-commit` and `pre-push` hook suites
- `pre-commit` runs formatting, module hygiene, and `golangci-lint`
- `pre-push` runs `go vet`, tests, `govulncheck`, and a 35% coverage gate
- staged secret scans still require `gitleaks` to be available locally

Current package layout:

- `pkg/app` for common root-command and local-command scaffolding
- `pkg/inventory` for shared ownership and tagging contract definitions
- `pkg/audit` for generic tagged-resource scan and classification helpers
- `pkg/doctor` for shared preflight report types and failure-counting helpers
- `pkg/tfaction` for shared Terraform plan/apply execution helpers
- `pkg/nuke` for shared confirmation and destroy orchestration helpers
- `pkg/auth` for non-root downstream auth and role assumption
- `pkg/tfexec` for Terraform path and subprocess helpers
- `pkg/output` for shared plain-text output primitives