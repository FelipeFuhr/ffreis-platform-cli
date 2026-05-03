# Agent Context

**This repo:** `ffreis-platform-cli` — shared Go toolkit for downstream platform CLIs.
Provides Cobra scaffolding, AWS auth/role loading, Terraform subprocess helpers,
audit scanning, output primitives, and destroy orchestration. A library, not a binary.

## Non-obvious facts

- **Consumed via `replace` directive** in downstream repos' `go.mod`. Callers pin to a
  specific GitHub path commit. If the package layout changes (renamed package, moved
  file), all callers break simultaneously.

- **Intentionally minimal.** Repo-specific commands stay in downstream repos. Only
  genuinely cross-cutting concerns belong here. Adding something "because it might be
  useful" is wrong.

- **Pre-push hook runs a coverage gate (35%).** If you add code without tests, push will
  fail locally. CI enforces the same gate.

- **Pre-commit runs:** formatting, module hygiene (`go mod tidy` check), golangci-lint.

- **`pkg/output`** — plain-text only; no ANSI/color. The output layer is intentionally
  simple to remain scriptable.

## Packages

```
pkg/app/        ← root-command and local-command Cobra scaffolding
pkg/inventory/  ← ownership/tagging contract definitions
pkg/audit/      ← tagged-resource scanning and classification
pkg/doctor/     ← preflight report types and failure counting
pkg/tfaction/   ← Terraform plan/apply subprocess execution
pkg/nuke/       ← confirmation and destroy orchestration
pkg/auth/       ← non-root downstream auth and role assumption
pkg/tfexec/     ← Terraform path and subprocess helpers
pkg/output/     ← plain-text output primitives
```

## Build/test

```bash
make lefthook-bootstrap && make lefthook-install
make fmt && make test
```
