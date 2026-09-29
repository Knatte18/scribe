---
name: python-testing
description: Testing conventions for Python projects. Use when writing tests.
---

# Testing Skill

Testing conventions for Python projects using pytest.

---

## General Principles

See `scribe:testing` for language-agnostic rules (assertion strictness, mock discipline, naming).

---

## Framework: pytest

### Naming

- Test files: `test_<module>.py`
- Test functions: `test_<behavior_description>()`
- No test classes unless grouping is needed for shared fixtures.

### Fixtures over setup

- Use `@pytest.fixture` for shared setup.
  Avoid `setup_method` / `teardown_method`.
- Prefer function-scoped fixtures (the default).
  Use broader scopes (`session`, `module`) only when setup is expensive.
- Use `tmp_path` for temporary files instead of manual cleanup.

### Parametrize for variants

- Use `@pytest.mark.parametrize` when testing the same logic with different inputs.
- Keep parameter lists readable — one tuple per line.

```python
@pytest.mark.parametrize("input_value, expected", [
    (0, "zero"),
    (1, "one"),
    (-1, "negative"),
])
def test_classify_number(input_value, expected):
    assert classify(input_value) == expected
```

### Assertions

- Use plain `assert` statements.
  No assertion libraries needed.
- Compare exact values: `assert result == expected`, not `assert result` or `assert expected in result`.
- For floating point: `assert result == pytest.approx(expected)`.
- For exceptions: `with pytest.raises(ValueError, match="specific message")`.

### DataFrame assertions

- For pandas DataFrames, use `pd.testing.assert_frame_equal(result, expected)`.
- For numpy arrays, use `np.testing.assert_array_almost_equal(result, expected)`.
- When comparing DataFrame subsets, be explicit about columns and index: `assert list(df.columns) == [...]`.

### Data-heavy tests

- For tests that need real-looking data, construct small representative DataFrames inline rather than loading from files.
- When integration tests require data loading, use `pytest.mark.skip` with a descriptive reason if the data source is unavailable.
- Sample large datasets for speed: `df.sample(N).reset_index(drop=True)`.

### Project layout

- Tests live in a `tests/` directory at the project root, mirroring the source structure.
- Each source module `src/foo/bar.py` has a corresponding `tests/foo/test_bar.py`.

### conftest.py discovery

- Root `conftest.py` (in `tests/` or project root): shared fixtures available to all tests.
- Subdirectory `conftest.py` files: scoped fixtures for that directory tree.
- Before writing new fixtures, check existing `conftest.py` files for reusable fixtures — avoid duplication.

### Conventions to specify per project

- Test directory path (if not `tests/`)
- Fixture sharing strategy (conftest.py locations)
- Integration test markers and how to run them separately
- Coverage requirements (if any)

<!-- Project-specific testing configuration goes here -->
