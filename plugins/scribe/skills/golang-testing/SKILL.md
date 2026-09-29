---
name: golang-testing
description: Testing conventions for Go projects. Use when writing tests.
---

# Go Testing Skill

Go-specific testing conventions.
See `testing` for language-agnostic rules — assertion strictness, mock discipline, determinism, naming.

---

## Framework: standard `testing` package

Go's built-in `testing` package is the standard.
No external frameworks like testify.

---

## Naming conventions

**Test files:** named `<name>_test.go`, in the same directory as the code they test.

**Test functions:** named `TestXxx`, uppercase first letter after `Test`.
`Xxx` describes what's being tested.

**Subtests:** use an underscore as a logical separator: `TestFoo_ScenarioName`.
This is the one permitted exception to Go's usual no-underscores naming convention.

**Example:**

```go
func TestUserValidation(t *testing.T) {
	t.Run("ValidEmail", func(t *testing.T) {
		// test valid email
	})
	t.Run("InvalidEmail_Empty", func(t *testing.T) {
		// test empty email
	})
}
```

---

## Table-driven tests

The standard pattern for any test with multiple scenarios.

- Declare a slice named `tests` holding every case.
- Name each entry `tt` — not `tc`, not `case`.
- Call `t.Run(tt.name, ...)` per entry, as a subtest.
- Default to `t.Error` (continues testing); use `t.Fatal` only when later assertions depend on the current one succeeding.
- Error message format: `"Func(input) = got; want expected"` — actual before expected.

**Example:**

```go
func TestAdd(t *testing.T) {
	tests := []struct {
		name string
		a, b int
		want int
	}{
		{"positive", 2, 3, 5},
		{"negative", -1, -2, -3},
		{"zero", 0, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Add(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("Add(%d, %d) = %d; want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
```

---

## Test helpers

- Call `t.Helper()` as the first line of any helper function, so a failure reports the calling test's line, not the helper's.
- Prefer `t.Cleanup(f)` over manual `defer` for teardown — registered functions run after the test, LIFO.
- Use `t.TempDir()` for a temporary directory that's cleaned up automatically.

**Example:**

```go
func TestFileWriter(t *testing.T) {
	tmpDir := t.TempDir()
	file := filepath.Join(tmpDir, "output.txt")
	// test writes to file
}

func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
```

---

## Struct comparison

For complex structs, use `cmp.Diff` from `github.com/google/go-cmp/cmp` rather than `reflect.DeepEqual` — it gives a human-readable diff of what differs.
That module is a dependency of the project under test, not of this skill file itself; import it in tests as needed.

**Example:**

```go
import "github.com/google/go-cmp/cmp"

func TestUserStruct(t *testing.T) {
	got := parseUser("John Doe")
	want := &User{Name: "John Doe", Email: ""}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("parseUser() mismatch (-want +got):\n%s", diff)
	}
}
```

---

## Package naming

**Same-package tests** (`package foo`) can access unexported identifiers — useful for testing internal behavior.

**External tests** (`package foo_test`) exercise only the public API — preferred for library packages, since they verify what an external caller actually sees.

Choose same-package for low-level unit tests of internals;
external for integration tests and library packages.

---

## Project conventions

Defaults below; a project states its own test strategy — extra build tags, tier rules, hermetic-environment requirements — in its `CLAUDE.md`, and that overrides them.

- **Test directory:** `*_test.go` alongside the code they test — standard Go layout.
- **Fixture strategy:** `testdata/` subdirectories for fixture files (JSON, YAML, etc.), loaded explicitly in tests.
- **Integration test markers:** `//go:build integration` build tags to exclude integration tests from the fast unit run;
  `go test -tags=integration ./...` runs them separately.
