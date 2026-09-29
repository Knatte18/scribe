---
name: python-build
description: Build and test commands for Python projects. Use after completing a task.
---

# Build Skill

Build, lint, and test configuration for Python projects.

---

## Build Commands

Run these commands after completing a task to verify correctness:

```bash
ruff check .
pytest
```

**Convention: Writing formatters (when used) run on changed files only, never on the whole project.
This is precautionary since python-build today ships only ruff check (read-only).
Any future ruff format or ruff check --fix must be scoped to changed files.
Whole-project build and test stay whole-project.**

## Failure Handling

- If **ruff fails**: fix the linting violations and retry.
  Do not add `noqa` suppression unless the rule is genuinely inapplicable.
- If **tests fail**: analyze the failure, fix the code or test, and retry.
- If a fix requires changes beyond the current task's scope: stop and report the issue to the user.
- Do **not** skip or disable failing tests.

---

## Import Organization

Organize imports in three tiers, separated by blank lines and labeled with comments:

```python
# Python packages
import datetime
import logging
import os
from pathlib import Path

import numpy as np
import pandas as pd

# Domain packages
from mypackage.timeseries import to_period_index

# Local packages
import project_config
import file_io as fio
```

- Standard library first, then third-party, then local — each group with a section comment.
- Aliased imports are fine for frequently used modules (e.g., `import file_io as fio`).
- No wildcard imports.

## Logging

- Configure logging at the module level using `logging.getLogger(__name__)`.
- Use structured format: `[%(asctime)s] {%(filename)s:%(lineno)d} %(levelname)s - %(message)s`.
- Use appropriate levels: `info` for status, `warning` for recoverable issues, `error` for failures.
- Log messages should be descriptive with context: `"Extracting CBI into cube form..."`, not `"Processing..."`.

## Configuration

- Use a dedicated config module (e.g., `project_config.py`) for constants and paths.
- Load environment variables with `dotenv` and construct paths using `pathlib.Path`.
- Group constants by purpose with section comments: tweakable parameters, file paths, external config.

## Project Configuration

> Customize per project. Specify test paths, ruff config, and virtual environment setup.

### Test discovery

Before running tests, verify the project is testable:

1. **Virtual environment:** Check for `venv/`, `.venv/`, or `env/` directories.
   If present, activate before running commands.
   Check `pyproject.toml` for `[tool.poetry]` (use `poetry run`) or `Pipfile` (use `pipenv run`).
2. **Test configuration:** Check `pyproject.toml` for `[tool.pytest.ini_options]` — it may specify `testpaths`, `addopts`, or custom markers.
   Also check for `pytest.ini`, `setup.cfg` `[tool:pytest]`, or `tox.ini`.
3. **Test directory:** Verify `tests/` (or the configured test path) exists and contains `test_*.py` files.
   If no test files exist, report "No tests found" rather than running pytest on an empty directory.
4. **Ruff configuration:** Check for `ruff.toml`, `.ruff.toml`, or `[tool.ruff]` in `pyproject.toml` for project-specific lint rules.

### Defaults

- Run `ruff check .` from the project root.
- Run `pytest` from the project root (discovers tests automatically).

### Per-project overrides

Specify these when the defaults don't apply:

- Test directory or specific test files
- Ruff configuration file path
- Additional pytest flags (e.g. `-x` for fail-fast, `--cov` for coverage)
- Virtual environment activation command

<!-- Project-specific build configuration goes here -->
