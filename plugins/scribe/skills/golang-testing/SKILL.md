---
name: golang-testing
description: Testing conventions for Go projects. Use when writing tests.
---

# Go Testing Skill

Go-specific testing mechanics on top of `testing`, which this skill does not restate.

---

## Framework and naming

- The standard `testing` package; no testify or other framework.
- `<name>_test.go` beside the code; `TestXxx` functions; subtests named `Scenario` or `Scenario_Detail`.
- External test packages (`package foo_test`) are the default.
  Use `package foo` only to reach a behavior the public surface cannot reach — never to test each unexported helper.
  An unexported value the external test needs is exposed through `export_test.go` in `package foo`.

## Table-driven tests

The pattern for every multi-scenario test, shown for a `go.mod` declaring Go 1.22 or later (earlier versions need `tt := tt` in the loop).

```go
func TestAdd(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		a, b int
		want int
	}{
		{"Positive", 2, 3, 5},
		{"Negative", -1, -2, -3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := foo.Add(tt.a, tt.b); got != tt.want {
				t.Errorf("Add(%d, %d) = %d; want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
```

- The slice is `tests`, each entry `tt`.
- `t.Error` by default; `t.Fatal` only when later assertions depend on this one.
- Messages read `"Func(input) = got; want expected"`.
- Compare structs with `cmp.Diff` from `github.com/google/go-cmp/cmp`, reporting `"Func() mismatch (-want +got):\n%s"`, when go-cmp is already a dependency;
  otherwise use `reflect.DeepEqual`.

## Parallelism and time

- `t.Parallel()` first in every test and subtest, unless it uses `t.Setenv`, `t.Chdir` (Go 1.24 and later) or a mutable fixture shared across tests.
  A test that is not parallel says why in a comment.
- Inject a duration as a field or option, never a package variable: a test that sets one cannot run in parallel.
- Wait on a signal (channel, callback);
  poll with a short tick and a deadline only when none exists, and never use a fixed real-time `time.Sleep`.
- Time-driven code with no injectable duration can run under `testing/synctest` (Go 1.25 and later), where `time.Sleep` runs on a fake clock.
  The clock advances only while every goroutine in the bubble is durably blocked;
  real I/O and mutexes do not count.

## Helpers

- `t.Helper()` first in every helper.
- `t.Cleanup` over `defer`: a parent's `defer` runs before its parallel subtests do.
- `t.TempDir()` for temporary files.

## Project conventions

A project's `CLAUDE.md` states its own strategy — build tags, tiers, hermetic environments — and overrides these defaults: `testdata/` for fixture files, and `//go:build integration` to keep slow tests out of the default `go test ./...`.
