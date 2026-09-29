---
name: testing
description: Language-agnostic testing principles. Use when writing or reviewing tests.
---

# Testing Skill

Universal testing principles.
Language-specific skills (e.g. `golang-testing`) build framework mechanics on top of these — naming conventions, table-driven patterns, helper idioms — without restating them.

---

## Coverage

- **Test behavior, not implementation.**
  Verify observable outcomes, not internal state or call sequence.
  A correct refactor of the internals should never break these tests.
- **Every path, not just the happy one.**
  At minimum, cover:
  - **Error paths** — invalid input, missing data, unauthorized access, an unavailable dependency, at trust boundaries.
    Not internal misuse of a well-typed, well-documented module — see `code-quality`'s trust-the-caller rule.
    A test for an input the type system already rules out (wrong type, a non-nullable made null) is testing something the compiler already prevents, not a real path.
  - **Edge cases** — empty collections, nil/undefined, boundary values, the smallest and largest valid input, concurrent access.
  - **Negative cases** — what should NOT happen (no side effect fires, no data leaks, no write occurs on a rejected input).
- **All existing tests must keep passing.**
  Run the full suite, not just the tests you touched.
- **No shallow assertions.**
  "Something came back" is not a test.
  Assert the specific value, shape, and side effects.
  - Weak: `assert result is not None`
  - Strong: `assert result.status == "active" and result.created_at > start_time`

## Determinism and isolation

- **No wall-clock or random dependence** unless the test explicitly seeds/freezes it.
  A test that passes only "most of the time" or only near a particular time of day is a bug in the test, not an acceptable flake.
- **No network or filesystem dependence** beyond a controlled, hermetic fixture.
  If the code under test talks to the outside world, that boundary gets faked or stubbed for the test (see Mocking below) — the test itself never depends on live external state.
- **No execution-order dependence.**
  Any test must pass alone and in any order relative to the others.
  A test that only passes because an earlier test left behind some state is testing the suite's incidental ordering, not the code.
- **No shared mutable state across tests.**
  Each test sets up and tears down its own fixtures;
  two tests must be able to run concurrently without interfering.

## TDD discipline

When TDD is specified:

1. **RED** — write the test first, run it, and confirm it fails.
   If it passes immediately, the test isn't testing what you think it is.
2. **GREEN** — write the minimum implementation that makes it pass.
   No more.
3. **REFACTOR** — clean up, keeping tests green throughout.

Skipping RED verification (writing the implementation before ever seeing the test fail) produces a test that confirms the implementation rather than specifying the behavior — it will pass even if you later reintroduce the bug it was meant to catch.

## Assertions

- **Strict equality over loose containment.**
  Prefer exact equality checks to substring/contains checks.
  - Weak: `assert "valid" in result`
  - Strong: `assert result == "valid"`
- Never assert truthiness alone (`assert result`) — assert the specific expected value.

## Mocking

- **Last resort.**
  Prefer fakes, stubs, or in-memory implementations over a mocking framework.
- **Never mock your own code.**
  Mock only an external dependency you don't control — a third-party service, a system clock, an environment you can't run in CI.
- **Prefer record/replay** over hand-written mocks for network traffic, where the tooling supports it.
- **Terminology matters** — use the right term so a reader knows what a test double actually does:
  - *Mock* — a test double that also asserts on how it was called (call count, arguments), usually via a mocking framework.
  - *Fake* — a lightweight working implementation (e.g. an in-memory database).
  - *Stub* — returns fixed data without real logic or call-pattern assertions.

## Naming

- Test names describe **behavior**, not implementation — the name should read as a sentence stating what's expected.
- Don't include the word "test" in the name beyond the framework's required prefix/suffix convention (e.g. `test_` in Python, `Test` in Go).
