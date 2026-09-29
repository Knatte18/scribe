---
name: code-quality
description: Strict, clean code guidelines, including comments and docstrings — naming, abstraction, error handling, file organization, and comment content. Use before editing code.
---

# Code Quality Skill

Code and its comments are one craft.
Language-agnostic.
Pairs with `testing` (test structure) and `prose` (writing style for the comments this skill governs the content of).
Per-language skills (e.g. `golang-comments`) apply the Comments section below with language-specific mechanics — placement, docstring syntax, naming idioms.

---

## Validate at boundaries, trust everything internal

- Validate only at trust boundaries — user input, external APIs, another process, another team's code.
  Never re-validate an invariant already guaranteed by the type system, a constructor, or a caller you control.
- Let internal errors propagate.
  Wrap only where the call site acts on the added context, or where crossing an API boundary requires translation — never "just in case."
- Don't handle scenarios that can't happen.
  No fallback branch, no nil check, no default case for a state the code's own guarantees already rule out.
- No backwards-compatibility shims for internal code.
  No feature flags, no `_old`/`_v2` duplication — change the call sites.
  If something is unused, delete it.

## Abstraction discipline (YAGNI)

- Don't extract a helper, interface, or config knob until two real call sites need it,
  or the extraction is required to make the one call site correct.
- Three similar lines beat a premature abstraction.
- Don't design for a requirement that hasn't been asked for.
- No half-finished implementations.
  If scope grows mid-task, finish the current piece before starting the next.

## Naming

- Full, descriptive names.
  No abbreviations, except an established domain term already defined elsewhere in the codebase.
- Encode the operation and its inputs/outputs — not a generic verb.
  A reader should know what data is produced and from what inputs without reading the implementation.
  - Weak: `process_data`, `build_index`, `get_results`, `transform`, `run`, `handle`
  - Strong: `create_pressure_map_from_sensor_readings`, `load_transactions_from_parquet`
- Can't name it precisely on the first attempt?
  It's doing too much — split it.

## Errors and control flow

- Fail loud on programmer errors — a violated invariant, impossible internal state.
  Panic, assert, or raise.
- Fail informative on operational errors — bad input, an unavailable dependency.
  Return or raise something the caller can act on.
- No code path for something the code's own guarantees already prevent.
- Fix a defect at its source; don't band-aid the symptom.
  A NaN or similar invalid result from your own logic usually means an upstream bug — division by zero, an unhandled edge case — not a state to tolerate.
  Trace back and fix the defect;
  don't add a check that special-cases the invalid output as if it were expected.
  This is about bugs in your own logic, not boundary validation of untrusted external input — that's the separate rule above.
- For a non-public-API module, assume the caller uses it correctly.
  Don't build failure handling — or write tests — for every misuse a caller could invent;
  that's the caller's discipline to get right, not this module's job to defend against.
  Reserve defensive handling for genuine public APIs, where the caller is unknown and uncontrolled.

## File and module organization

- Prefer editing an existing file that owns the relevant concern over creating a new one.
- Split a file only when it holds two separate responsibilities.
- One file, one owning concept.

## Comments

A comment — inline or doc/docstring — follows the same discipline as the code around it.
This section covers what a comment may contain;
see `prose` for how to write it — terse, no padding, no empty intensifiers, semantic line breaks.

**Restraint first.**
Default to no comment;
names and structure carry the explanation.
Write one only when the WHY is non-obvious: a hidden constraint, a subtle invariant, a workaround for a specific bug, behavior that would surprise a reader.
Never comment WHAT the code does — refactor instead of narrating `for _, item := range items` as `// loop through items`.
Code that seems to need many comments to explain itself is a signal to decompose it into well-named functions with their own doc comments, not to write a longer one.

**Necessary and sufficient.**
Signature + doc comment must let a reader decide whether a symbol fits their task without opening the file.
Two violations:
- **Insufficient** — omits the purpose/why, or leans on another symbol (see Self-contained, below).
- **Excessive** — tangential remarks, edge-case cataloguing, history, measured numeric deltas, rejected-alternative trails, incident narrative, unrelated commentary.
  If it doesn't help judge fitness, cut it.
  A doc comment past ten or fifteen lines usually means implementation narrative has crept in.

**Self-contained.**
States only what the symbol does and why — never in terms of, compared to, or dependent on another named symbol.
Test: is it correct to a reader with zero knowledge of any other named symbol in the codebase?
"Unlike `X`, this does..." belongs in a package/module doc or design doc, never here — no exception for a well-justified comparison.
No "see also"/"for reference" pointers elsewhere, either.

Two exceptions: self-reference (the doc comment naming its own symbol, per the language's convention) and stdlib/language contracts (a standard-library type, or an interface being satisfied — these don't rename or drift).

Where information belongs:
- What the symbol does, why, and the facts that decide fitness (behavior contract, meaningful failure modes) → the comment.
- How it works internally, including what it calls → the code, never restated.
- Relationships to other symbols, history, wider patterns → a package/module doc or design doc — or nowhere, if not needed to use the symbol.

**No enumerated-consumer lists.**
Naming every current caller or writer of a shared symbol (e.g. "the logger, the scheduler, and the exporter all write it") turns an unrelated change elsewhere into a forced edit here — see `prose`'s "Don't pin a perishable specific".
Name the group instead, unless the specific names are load-bearing to the point being made.

**No end-of-line comments.**
Comments go on their own line, above the code they describe — never at the end of a code line.
An aligned end-of-line comment in a grouped block forces every sibling line to realign (and shows up in the diff) whenever one identifier's length changes;
an above-line comment avoids that.

**File headers — same contract, file scope.**
Header + filename should let a reader decide with high probability whether this is the right file, without opening it.
Same two failure modes: insufficient (doesn't say what's in the file), excessive (padded past what's needed to judge relevance).
Skip it when the file is the only one in its directory/module — nothing to disambiguate from, and the module-level doc already covers it;
a small file that shares its directory with others still needs one, since size isn't what the exception turns on.

**Why:** signature+docstring and file headers meeting this bar are what a mechanical tool (tree-sitter or similar) needs to build a table of contents — per file, per directory — without parsing implementation.
That's how an LLM finds the right file or symbol by reading descriptions, not code.

Prohibited: commented-out code (delete it — version control has the history), edit-history comments ("added in v2"), referencing the current task/ticket/requester (belongs in the commit message), padding.
