# Agent Context

**This repo:** `ffreis-platform-cli` — shared Go toolkit for downstream platform CLIs.
Provides Cobra scaffolding, AWS auth/role loading, Terraform subprocess helpers,
audit scanning, output primitives, and destroy orchestration. A library, not a binary.

## Non-obvious facts

- **Consumed via `replace` directive** in downstream repos' `go.mod`. Callers pin to a
  specific GitHub path commit. If the package layout changes (renamed package, moved
  file), all callers break simultaneously.

- **Intentionally minimal.** Genuinely repo-specific commands stay in downstream repos.
  Only cross-cutting concerns belong here. Adding something "because it might be useful"
  is wrong. The **standard infra-repo lifecycle commands** (plan/apply/outputs/nuke/
  version/doctor) are cross-cutting and DO live here — see `pkg/app.RegisterStandardCommands`
  and "Consumer cmd/ layout convention" below.

- **Pre-push hook runs a coverage gate (`COVERAGE_MIN`, currently 75%).** If you add code
  without tests, push will fail locally. CI enforces the same gate.

- **Pre-commit runs:** formatting, module hygiene (`go mod tidy` check), golangci-lint.

- **`pkg/output`** — plain-text only; no ANSI/color. The output layer is intentionally
  simple to remain scriptable.

## Packages

```
pkg/app/        ← root-command scaffolding + standard lifecycle commands (RegisterStandardCommands)
pkg/inventory/  ← ownership/tagging contract definitions
pkg/audit/      ← tagged-resource scanning and classification
pkg/doctor/     ← preflight report types and failure counting
pkg/tfaction/   ← Terraform plan/apply subprocess execution
pkg/nuke/       ← confirmation and destroy orchestration
pkg/auth/       ← non-root downstream auth and role assumption
pkg/tfexec/     ← Terraform path and subprocess helpers
pkg/output/     ← plain-text output primitives
pkg/ui/         ← lipgloss-backed rich presenter (satisfies app.Presenter)
```

## Consumer cmd/ layout convention

Infra repos (`ffreis-flemming-infra`, `ffreis-platform-shared-infra`,
`ffreis-platform-org`, …) used to copy-paste near-identical `plan.go`/`apply.go`/
`outputs.go`/`nuke.go`/`doctor.go`/`version*.go` into their `cmd/` package. Those
skeletons now live here as `pkg/app.RegisterStandardCommands(root, cfg)` and the
individual `New{Plan,Apply,Outputs,Nuke,Doctor}Command(cfg)` / `NewVersionCommand`
builders. Consumers keep only what genuinely varies.

**Standard layout:**

```
cmd/<repo>/main.go   ← thin entrypoint: os.Exit(cmd.Execute())
cmd/root.go          ← NewRoot(...) + AfterAuth that populates a *app.StandardRuntime,
                       then RegisterStandardCommands(root, app.StandardConfig{...})
cmd/<feature>.go     ← ONLY repo-specific commands (e.g. platform-org's `activate`,
                       shared-infra's `sync-artifacts`) and the repo's doctor sections
```

**Wiring `StandardConfig`** (the config surface that covers the three repos' real
variance):

- `DisplayName` / `StackDirName` — header prefix (`"Flemming Infra"`) and tf stack
  dir (`"infra"`; defaults to `tfexec.StackDirName`).
- `Runtime *StandardRuntime` — a pointer the consumer's `AfterAuth` fills each run;
  every standard command reads env/creds/region/account from it.
- `NewOutput` — returns the repo's `app.Presenter`. For rich (lipgloss-styled)
  output, wrap the shared `ui.Presenter` with `ui.NewCommandOutput(presenter, out,
  err)` — it satisfies `app.Presenter` directly. Omit `NewOutput` for the plain-text
  default (`output.CommandOutput`).
- `DoctorSections(ctx, mode)` — the repo supplies its doctor sections; the shared
  command assembles the report, summarises, renders/JSON-emits, and gates. Apply and
  nuke reuse it as their preflight via `DoctorModes.Apply` / `.Nuke`.
- `PostApply(ctx, out, root, stack)` — post-apply verification that differs per repo
  (flemming: shared-dependency check; shared-infra: artifact sync).
- `NukeFallback(...)` / `NukeLong` — SDK cleanup after a failed destroy, and help text.

**Reusing the apply orchestration with extra terraform args.** A repo-specific
command that needs the same doctor-gated apply flow but with extra terraform args
(e.g. flemming's `deliver` command threading a temporary `-var domain_name=...`
override) should call `app.RunApply(ctx, cfg, out, autoApprove, extraArgs)` directly
instead of re-implementing the doctor-preflight-and-gate sequence. `NewApplyCommand`
itself is a thin wrapper around this function with `extraArgs = nil`.

**When a command diverges structurally** (e.g. `platform-org`'s backup-aware `nuke`
with its own confirmation flow), the repo does NOT call `RegisterStandardCommands`.
It registers the builders it wants individually plus its own command:

```go
root.AddCommand(app.NewVersionCommand(bi), app.NewPlanCommand(cfg), app.NewDoctorCommand(cfg))
root.AddCommand(myCustomApplyCmd, myCustomNukeCmd)
```

Consumers pin a tagged release (upstream-first): bump the `replace`/require ref to
the tag, never to an unreleased SHA.

## Build/test

```bash
make lefthook-bootstrap && make lefthook-install
make fmt && make test
```

## Keeping this file current

- **If you discover a fact not reflected here:** add it before finishing your task.
- **If something here is wrong or outdated:** correct it in the same commit as the code change.
- **If you rename a file, command, or concept referenced here:** update the reference.
