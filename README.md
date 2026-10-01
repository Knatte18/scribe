# scribe

Shared writing and code conventions for Claude Code sessions.
The `scribe` plugin holds the rules for how an agent writes — prose, chat replies, code and its comments, tests, Go, Python and C# mechanics, handoff documents — so every project and tool that relies on them reads one copy instead of keeping its own.

## Install

```
/plugin marketplace add Knatte18/scribe
/plugin install scribe@scribe
```

Install it once per machine at user level;
it then applies in every session.

The marketplace also carries `prowler`, which fetches blocked or JS-rendered web pages as readable markdown and searches code across repos (`/plugin install prowler@scribe`).
It builds its Go binary on first run; see [plugins/prowler/README.md](plugins/prowler/README.md).

## Deploying an edit

Run `./update-plugins.sh` (`update-plugins.ps1` on Windows) after changing a plugin: it mirrors each plugin's source into the cache directory that plugin is installed in.
The version stays fixed at `1.1.0` and is never bumped: the cache path contains the version, so a bump moves the plugin to a new directory, and `update-plugins` deploys without one.
See [plugins/scribe/skills/INDEX.md](plugins/scribe/skills/INDEX.md) for the skills.

## Repo-specific conventions

Nothing in the `scribe` plugin knows about a particular repository or tool.
A project's own conventions — test tiers, build tags, tools it has or hasn't adopted, where its task state lives — belong in that project's `CLAUDE.md`, or in a thin plugin of its own layered on top of scribe, and they override scribe's defaults.
