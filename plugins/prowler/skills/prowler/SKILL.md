---
name: prowler
description: Fetch blocked/restricted web pages and answer questions about their content
argument-hint: "<url> [url2...] [question]"
---

Use this skill only when the built-in `WebFetch` tool fails or returns unusable content (bot-blocked sites, paywalls, JS-rendered pages, Reddit posts).
It drives a real headless browser plus Readability extraction as a fallback, so it can read pages `WebFetch` cannot.

Raw output is often long and noisy (a fetched Reddit thread especially can carry dozens of comments), so before deciding whether to read it directly or wrap the fetch in a subagent, load the `distill-subagent` skill by name (`prowler:distill-subagent`) via the Skill tool and apply its rule.

Steps:

1. Resolve the wrapper's absolute path now, while `${CLAUDE_SKILL_DIR}` is still set — a dispatched subagent won't have it: `RUN_SH="$(cd "${CLAUDE_SKILL_DIR}/../../scripts" && pwd)/run.sh"`.
2. Per fetch: run `path=$(bash "$RUN_SH" <url1> [url2...])`, checking the exit code (report stderr and stop on failure).
   Read `$path`.
   Answer the question, or give a 3-5 sentence summary if none was asked — say explicitly if the content doesn't support an answer, never guess.
   Delete the file: `bash -c 'rm -f "<path>"'`.
3. If wrapping: dispatch one cheap-tier subagent (currently Haiku, `model: haiku`) running step 2 with every URL for this batch passed to the same `run.sh` call.
   Dispatch more than one subagent only when sources need independent, unmixed answers — in that case, all of them in one message (parallel), never a sequential loop.
   Each subagent returns only its distilled answer, never the raw content.
4. Relay each answer, or synthesize across several.
   Never dump raw fetched content to the user, or carry it into your own context as a substitute for the above.
