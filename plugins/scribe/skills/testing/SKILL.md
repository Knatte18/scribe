---
name: testing
description: Language-agnostic testing principles. Use when writing or reviewing tests.
---

# Testing Skill

Language-specific skills (e.g. `golang-testing`) build framework mechanics on top of these without restating them.

---

## Coverage

- **Test behavior at the public surface.**
  Assert observable outcomes at a module's exported API, CLI or file contract — not internal state, call sequence or each helper.
  A correct refactor of the internals never breaks these tests.
- **One test per behavior.**
  A new test must cover a behavior no existing test covers;
  otherwise the case is a row or an assertion in the test that already covers it.
  A bug fix adds a regression row or assertion;
  a review finding adds a case only for a coverage gap, and otherwise corrects the wrong assertion.
  Many items of one kind (error codes, commands, rules) share one table-driven test, not one test each.
  A redundant test is a defect exactly as a missing one is.
- **Cover every path in its behavior's test.**
  Cover error paths at trust boundaries (invalid input, missing data, an unavailable dependency), edge cases (empty, nil, boundary values, concurrent access) and negative cases (no side effect on a rejected input), as rows or assertions in the test of that behavior.
  Not inputs the type system rules out, nor internal misuse — see `code-quality`'s trust-the-caller rule.
- **Existing tests keep passing** under the project's full suite, or the subset its own rules prescribe.

## Assertions

- Assert the specific value, shape and side effect; "something came back" is not a test.
- Prefer exact equality to substring or truthiness checks: `result == "valid"`, not `"valid" in result`.

## Determinism, isolation and speed

- **No wall-clock, random or real-time dependence.**
  Seed or freeze clocks and randomness;
  a production interval, timeout or retry delay is injectable, and the test shortens it instead of waiting it out.
  A test that passes only most of the time is a bug, not an acceptable flake.
- **Fake the slow boundaries.**
  Behavior testable against a fake or in-memory fixture is tested that way;
  a real subprocess, database, network or external service is driven only by the few tests that pin that boundary itself.
  Even those use a local, controlled instance, never live external state.
- **Independent and parallel.**
  A test passes alone and in any order, shares no mutable state, and is safe to run concurrently with the others unless it touches process-global state, which it names.
- **Leave nothing behind.**
  Teardown removes every file, socket and process the test created.

## TDD discipline

When TDD is specified:

1. **RED** — write the test, or the new row, first and see it fail.
2. **GREEN** — write the minimum implementation that passes.
3. **REFACTOR** — clean up with tests green.

A test never seen failing confirms the implementation instead of specifying the behavior.

## Mocking

- Prefer fakes, stubs or in-memory implementations to a mocking framework.
- Use the real in-process collaborator;
  fake your own interface only where it stands in for a slow boundary, and never assert on calls into your own code.
- Mock only an external dependency you don't control.
- Use the right term: a *mock* asserts on how it was called, a *fake* is a lightweight working implementation, a *stub* returns fixed data.

## Naming

Test names describe behavior and read as a sentence of what's expected, without the word "test" beyond the framework's required prefix or suffix.
