---
name: distill-subagent
description: Judgment rule for wrapping expensive web/repo reads in a cheap distillation subagent — used by the prowler and github-repo-explorer skills
---

This is a helper skill: it holds only the judgment rule for whether to wrap an expensive read in a subagent, not any fetch/read commands of its own.

**When NOT to wrap:** an already-small, isolated worker (e.g. a dedicated research subagent reading a handful of sources) reads the content inline itself — there is no larger context to protect.

**When to wrap:** a general-purpose or long-lived thread,
or any expensive tier, dispatches a cheap-tier subagent (Agent tool, the cheap distillation tier, currently Haiku) to do the read and return only a distilled answer, keeping the raw content out of the caller's context.

**Batch related sources into one dispatch:** give every source needed for one batch of related questions to a single subagent dispatch — dispatch overhead is paid once, not per source.

**Split only when answers must stay unmixed:** dispatch multiple subagents only when sources genuinely need independent, unmixed answers, and always in parallel (one message) — never a sequential loop.

**The subagent's contract:** it returns ONLY its distilled answer, never the raw fetched content.

**The caller's contract:** never dump raw content to the user, and never carry it into your own context as a substitute for the distilled answer.
