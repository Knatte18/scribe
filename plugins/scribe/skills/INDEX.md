# Scribe Skills

| Skill | Description |
| --- | --- |
| [prose](prose/SKILL.md) | Precision and terseness rules for every piece of text an agent writes — chat replies, markdown files, code comments, docstrings. Always active. |
| [conversation](conversation/SKILL.md) | Interaction rules for chat replies — tone, reply length, user choices, file/shell conventions. Always active. Builds on `prose`. |
| [code-quality](code-quality/SKILL.md) | Strict, clean code guidelines, including comments and docstrings — naming, abstraction, error handling, file organization, and comment content. Use before editing code. |
| [testing](testing/SKILL.md) | Language-agnostic testing principles. Use when writing or reviewing tests. |
| [golang-comments](golang-comments/SKILL.md) | Godoc and inline comment mechanics for Go. Use when writing or reviewing Go comments. |
| [golang-build](golang-build/SKILL.md) | Build and test commands for Go. Use after completing a task. |
| [golang-testing](golang-testing/SKILL.md) | Testing conventions for Go projects. Use when writing tests. |
| [handoff](handoff/SKILL.md) | Write a handoff document so a fresh session can continue this conversation's work. |

`prose` and `conversation` are always active by default: `hooks/hooks.json` ships a `SessionStart` hook that asks the agent to load `scribe:conversation`, which builds on `scribe:prose`, once at the start of every session.
The hook can only ask, not force-load;
a tool that writes its own agent prompts should also name the skills it wants loaded.
