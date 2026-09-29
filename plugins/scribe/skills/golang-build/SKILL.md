---
name: golang-build
description: Build and test commands for Go. Use after completing a task.
---

# Go Build Skill

Build and test configuration for Go projects.
A project's own `CLAUDE.md` overrides any default here — test tiers, build tags, or a tool the project hasn't adopted.

---

## Build commands

Run these after completing a task, to verify correctness:

```bash
goimports -w <changed-files>
go vet ./...
go build ./...
go test ./...
golangci-lint run
```

**Convention:** the writing formatter (`goimports -w`) runs on changed files only, never the whole project.
Build, test, and the read-only lint stay whole-project.

## Failure handling

- **Build fails** — analyze the error, fix the issue, retry.
- **Tests fail** — analyze the failure, fix the code or the test, retry.
- **A fix needs changes beyond the current task's scope** — stop and report it.
- Never skip or disable a failing test.
- **`golangci-lint` unavailable because the network is restricted** — take the fallback under Tool installation instead of stopping.

---

## Tool installation

Required before running the build workflow:

- **goimports** — organizes and formats imports.
  Install: `go install golang.org/x/tools/cmd/goimports@latest`
- **golangci-lint** — linter aggregator.
  Install: `go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest`

Detect each tool with a bare `PATH` check first, then a `GOPATH/bin` fallback — `go install` places binaries in `$(go env GOPATH)/bin`, which is not guaranteed to be on `PATH`:

```bash
command -v goimports >/dev/null 2>&1 || test -x "$(go env GOPATH)/bin/goimports"
command -v golangci-lint >/dev/null 2>&1 || test -x "$(go env GOPATH)/bin/golangci-lint"
```

When a tool resolves only via the fallback, invoke it by that full path for the rest of the workflow.

Only when both checks fail for a tool:
- **Missing goimports** — report it with its install command, then stop.
- **Missing golangci-lint** — report it and attempt its install command.
  - If the install succeeds, continue with it.
  - If the install fails because an external VCS host is unreachable (output like "Repository not found", "dial tcp", "no such host", "i/o timeout", "unable to fetch"), don't stop: run `goimports -w <changed-files>` + `go vet ./...` in place of `golangci-lint run`, and note in the summary that the lint was skipped for a network-restricted sandbox.
  - Any other install failure — a compile error, a bad module path, an authentication failure — stops and reports.

Never skip a step silently.
A project that hasn't adopted one or both tools says so in its own `CLAUDE.md`, and that overrides this section.

---

## Project configuration

### Test discovery

Before running tests, confirm the project is testable:

1. Look for `*_test.go` files.
   If none exist, report "No test files found" rather than running `go test` on an empty package.
2. Test files live in the same directory as the code they test;
   a package with at least one `*_test.go` file is testable.

### Defaults

- Build all packages in the current working directory and subdirectories.
- Run all tests found in the project.

### Per-project overrides

A project states these in its own `CLAUDE.md` when the defaults don't apply:

- Specific package paths to build or test.
- Build flags (`-tags`, `-ldflags`).
- Test flags (`-race`, `-cover`).
- Test tiers or build tags that gate slower tests.
