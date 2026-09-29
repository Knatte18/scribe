---
name: prose
description: Precision and terseness rules for every piece of text an agent writes — chat replies, markdown files, code comments, docstrings. Always active.
---

# Prose Skill

Rules for how text is written, independent of where it ends up — including markdown formatting, folded in below rather than split into its own skill since markdown is most of what gets written.
`code-quality`'s Comments section builds on the general rules here rather than restating them;
it owns comment-specific content on top.

---

## Get to the point

State the point first.
No throat-clearing, no restating what's about to be said before saying it.

## Eliminate empty intensifiers

Words that add emphasis without meaning: "actually", "really", "genuinely", "truly", "completely", "totally", "fully", "definitely", "certainly", "absolutely", "just", "simply", "merely".
Test: remove the word.
If the sentence means the same thing, delete it.

## No padding

Don't restate a point already made.
Don't narrate what's already visible in context — a heading, an example, the surrounding code.
Don't catalogue edge cases or history the reader doesn't need for the point at hand.
If a sentence adds no new information, cut it.
Apply a per-sentence cut test: would the reader act differently if this sentence were missing? If not, cut it.

## Say it once

State a rule, fact, or conclusion in the one place it belongs.
Don't repeat it in a summary, a "why this matters" aside, or a closing recap — reference the original instead of restating it.

## Don't pin a perishable specific

Name the source, not the snapshot.
A detail a reader could instead derive, and that changes independently of the prose around it, goes stale silently — an unrelated change elsewhere becomes a forced edit here, and nothing signals when the edit was missed.

Three forms of the same mistake:

- **A tally** — a count, a runtime, a file or test total.
  Write "the recipe's rows", never "the recipe's fourteen rows"; write "every module but `x`", never "thirteen of fourteen"; and in code derive the number (`len(wantTable)`) instead of repeating a literal.
- **An enumerated consumer list** — naming every current caller, writer, or implementer of a shared symbol when the point doesn't depend on which ones currently do.
- **A name cited descriptively** — referring to a function or file by what it does rather than as a stable identifier, so a rename falsifies the sentence with nothing to catch it.

Write the derivation instead: point at the command that produces the number, the directory that holds the files, or the subsystem group rather than its current members.
When a name is load-bearing, cite it as a stable identifier in backticks so a rename search finds it.

Two exceptions:

- **A closed set whose size is the contract** — an enum a test pins shut. Even that is stated once, at its definition site.
- **A measurement report** — a benchmark table, a results document, a captured baseline. Such a document records one specific run at a point in time and is not claiming to stay true; going stale when a new run happens is what makes it useful.

## Line breaks

Applies to any multi-line prose written into a file — markdown paragraphs and list items, and multi-line code comments/docstrings alike.
Not chat replies, which aren't diffed.

**Semantic line breaks, never fixed-column hard-wrap.**
Fixed-column wrapping breaks mid-phrase,
so a single-word edit touches every wrapped line in the paragraph instead of just the changed words.
Write one sentence per line;
inside a long sentence, also break at an internal independent-clause boundary — a comma before a coordinating conjunction ("but"/"and"/"or"), or a semicolon, where what follows has its own subject and verb.
A comma followed by a coordinating conjunction that joins a list item or a compound predicate does not trigger a break.
When sentence-ending punctuation is ambiguous — a period inside a URL, or an abbreviation like "e.g." or "etc." — do not force a break there;
readability wins over mechanical rule compliance in that edge case.
Use a plain newline.
Never trailing double-spaces or a backslash — those force a real `<br>`.

Table cells and blockquotes stay on one line — this rule doesn't apply inside them.

## Markdown

Applies whenever the output is a `.md` file or a markdown-formatted reply.

- **Headings structure the document, not decorate it.**
  Use them to separate genuinely distinct sections.
  Don't skip levels (no `#` straight to `###`).
  Don't build heading scaffolding — `## Summary`, `## Details`, `## Conclusion` — around content short enough to just say.
- **Fenced YAML for metadata in generated files.**
  Use fenced ` ```yaml ` blocks for metadata in generated `.md` files — status files, review reports, registry entries, any machine-written markdown.
  `---` frontmatter is reserved for skill definitions (`SKILL.md`) and plugin manifests, which the platform parses.
  Never use frontmatter for human-facing metadata in a generated file — previewers hide it.

## Applies everywhere text is produced

Not a chat-reply rule.
Governs every piece of text an agent writes: conversational responses, markdown files, code comments, docstrings.
Load this skill first, before any skill that writes user-facing text.
