---
name: python-comments
description: Docstring and inline comment rules for Python. Use when writing Python comments.
---

# Comments and Documentation Skill

**Load `scribe:code-quality` first — its Comments section is the language-agnostic base.**

Guidelines for docstrings and comments in Python.
The goal is **readable code** — a developer should be able to understand what a module does and why, without tracing through the implementation.

---

## Module docstrings

- See `scribe:code-quality`'s Comments section for the file-header rule and its sole-file exception; in Python the file header is the module-level docstring.
- Follows the same triple-quote placement as function docstrings (see below): the opening `"""` alone on its own line, text starting on the next line.

## Function docstrings — Google style

- Use **Google-style** docstrings.
- **Multi-line docstrings start the text on the line after the opening `"""`**, never on the same line:

```python
# BAD — text on the same line as opening quotes
def foo():
    """Create random fold assignments for n_rows.

    Returns an array of length n_rows.
    """

# GOOD — opening quotes alone on first line, text starts on the next line
def foo():
    """
    Create random fold assignments for n_rows.

    Returns an array of length n_rows.
    """
```

- Single-line docstrings (rare — only for trivial helpers) keep everything on one line: `"""Return True if x is positive."""`
- **Never write a one-liner that restates the function name.** `def load_data` does not need `"""Load data."""`.
  Either write a substantive docstring or omit it.
- For any non-trivial function, the docstring must be **multi-line** and explain:
  1. **What** the function does at the domain level — not "processes data" but "stitches together a CBI price index from two sources".
  2. **What** it returns — describe the structure, columns, or shape of the output.
- See `scribe:code-quality`'s Comments section ("Restraint first") for when a function's steps need explaining — decompose into named sub-functions instead of narrating in the docstring.
  Only when that isn't practical does a single inline comment inside the function body remain an accepted exception;
  it does not go in the docstring.
- Include `Args:` when parameters carry domain meaning not obvious from the name (e.g., `std_ratio`, `filter_outliers`, `RSI_stop_date`).
- Include `Returns:` when the return value is a complex structure (DataFrame with specific columns, tuple, dict).
- Omit docstrings only on trivial private helpers where the name and signature are self-explanatory.

### Good vs bad examples

```python
# BAD — restates the function name, tells the reader nothing
def create_CBI_from_SSB_and_RSI(SSB_quarterly, RSI_weekly, RSI_stop_date=None):
    """Create CBI from SSB and RSI data."""

# GOOD — explains the domain purpose and the output structure, without narrating the algorithm
def create_CBI_from_SSB_and_RSI(SSB_quarterly, RSI_weekly, RSI_stop_date=None):
    """
    Stitches together a CBI price index from two different price indices,
    preferring SSB_quarterly before RSI_weekly is sufficiently populated and RSI_weekly for the main period.

    Returns: DataFrame with "date" and "price" columns, plus additional information
        on how the index was created, and a "count" column representative of the
        number of transactions used to compute the index.
    """
```

### Class docstrings

- Document the class's purpose and list key instance variables with their meaning.
- Domain-specific abbreviations are acceptable in variable names when the docstring defines them (e.g., `df_MT` — DataFrame of Matched Transactions).

```python
class LORSIPartitionClass:
    """Logarithmic Repeated Sales Index for a single geographic partition.

    Computes LORSI values across date ranges and BRA (floor area) groups.
    Supports filtering, serialization, and conversion to DataFrames.

    Instance variables:
        LORSI: 3D numpy array (geo × BRA × time) of index values.
        count: 3D numpy array of transaction counts per cell.
        BRA_split: boolean array indicating which BRA groups are active.
    """
```

## Section dividers

- In longer modules (200+ lines), use docstring-style section dividers to separate logical groups:

```python
""" FUNCTIONS FOR DATA LOADING """
```

## Inline comments — narrate the reasoning

Inline comments explain the domain reasoning behind a non-obvious step, so a reader can follow the logic without deciphering the code.

- Use an inline comment on a step that isn't self-evident from the code alone — not on every step.
  A function with 5 logical steps does not need 5 comments;
  it needs one wherever the domain reasoning isn't already obvious from well-named identifiers.
- Write in natural language: "Extract the date where the CBI data will start.
  This is simply the first date in the SSB data."

### Good vs bad examples

```python
# BAD — mechanical, tells you nothing beyond what the code says
min_date = df["date"].min()  # get min date

# GOOD — explains the domain reasoning behind the operation
# Extract the date where the CBI data will start. This is simply the first date in the SSB data.
CBI_start_date = SSB_quarterly["date"].min()
```

```python
# BAD — no comments, reader must reverse-engineer the filtering logic
df = df.dropna(subset=['grunnkrets_number', 'postcode', 'sold_date', 'location', 'BRA-i', 'price_inc_debt'])
df = df[df['price_inc_debt'] != 0]

# GOOD — explains what data quality rules are being enforced and why
# Remove transactions with missing geographic or property data — these cannot be placed in any partition.
df = df.dropna(subset=['grunnkrets_number', 'postcode', 'sold_date', 'location', 'BRA-i', 'price_inc_debt'])
# Exclude zero-price transactions, which represent non-market transfers (gifts, inheritance).
df = df[df['price_inc_debt'] != 0]
```

## Line-wrap style

Raw Python docstrings preserve literal newlines, so tools like `help()`, `pydoc`, and IDE tooltips display sentence-per-line text as short lines rather than reflowing it into one paragraph.
This is a display difference only — the text stays fully readable,
and the addressing/diff-locality benefit holds regardless of how it renders.
See `prose`'s Line-breaks rule for the full rule.

### Good vs bad examples

```python
# BAD — single unbroken line, hides sentence boundaries from diffs and citations
"""
Loads the raw transaction file and filters out zero-price rows. The result feeds directly into the CBI stitching step, and later steps assume the join has already happened.
"""

# GOOD — one sentence per line, with a clause-boundary break inside the second sentence
"""
Loads the raw transaction file and filters out zero-price rows.
The result feeds directly into the CBI stitching step,
and later steps assume the join has already happened.
"""
```
