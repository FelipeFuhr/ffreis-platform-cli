# ffreis-platform-cli

<!-- ffreis-badges:start -->
[![CI](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/FelipeFuhr/ffreis-badges/main/badges/ffreis-platform-cli/ci.json)](https://github.com/FelipeFuhr/ffreis-platform-cli/actions) [![License](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/FelipeFuhr/ffreis-badges/main/badges/ffreis-platform-cli/license.json)](https://github.com/FelipeFuhr/ffreis-platform-cli/blob/main/LICENSE)
<!-- ffreis-badges:end -->

Shared Go toolkit for downstream platform Terraform CLIs.

This module is intended for private platform infrastructure repos that wrap Terraform with a Go CLI.

It provides reusable building blocks for:

- Cobra root-command construction for downstream Terraform CLIs
- The standard infra-repo lifecycle command set — `plan`, `apply`, `outputs`,
  `nuke`, `version`, `doctor` — wired in one call via
  `app.RegisterStandardCommands(root, cfg)`, with per-repo variance captured by
  `app.StandardConfig`
- AWS profile loading and `platform-admin` role assumption
- Terraform subprocess execution helpers
- Plain-text command output helpers
- A lipgloss-backed rich terminal presenter (`ui.CommandOutput`) satisfying
  `app.Presenter`, for consumers that want styled output instead of the plain
  default
- `app.RunApply` — the doctor-gated apply orchestration used by `apply`,
  exported so a repo-specific command needing extra terraform args (e.g. a
  one-time `-var` override) can reuse it instead of re-implementing it

The module is intentionally small. Repo-specific commands and validation rules
stay in each downstream repo.

Development hygiene:

- local hooks are managed with `lefthook`
- install toolchain locally with `make lefthook-bootstrap`
- install hooks with `make lefthook-install`
- CI also runs the configured `pre-commit` and `pre-push` hook suites
- `pre-commit` runs formatting, module hygiene, and `golangci-lint`
- `pre-push` runs `go vet`, tests, `govulncheck`, and a 75% coverage gate
- staged secret scans still require `gitleaks` to be available locally

Current package layout:

- `pkg/app` for root-command scaffolding and the standard lifecycle command set
- `pkg/inventory` for shared ownership and tagging contract definitions
- `pkg/audit` for generic tagged-resource scan and classification helpers
- `pkg/doctor` for shared preflight report types and failure-counting helpers
- `pkg/tfaction` for shared Terraform plan/apply execution helpers
- `pkg/nuke` for shared confirmation and destroy orchestration helpers
- `pkg/auth` for non-root downstream auth and role assumption
- `pkg/tfexec` for Terraform path and subprocess helpers
- `pkg/output` for shared plain-text output primitives
- `pkg/ui` for the shared lipgloss-backed rich presenter and its `CommandOutput`
  adapter (satisfies `app.Presenter`)
