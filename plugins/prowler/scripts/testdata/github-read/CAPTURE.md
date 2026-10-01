# CAPTURE.md — live-capture record for the `github-read.sh` fixtures

Private repository names are deliberately redacted throughout this file, because this testdata ships with the plugin: every private target is referred to as `<private-repo>`, and any private path appearing inside a captured command or response body is genericized the same way.
The public target used below is `Knatte18/quarry`, a public repository, named literally per the Shared Decision that public repositories may be named.

## Capture 1: `gh api` contents call against a missing path, with the raw Accept header

- **Command:** `gh api "repos/Knatte18/quarry/contents/does-not-exist-nope.txt" -H "Accept: application/vnd.github.raw"`
- **Exit status:** `1`
- **Stdout:** `{"message":"Not Found","documentation_url":"https://docs.github.com/rest/repos/contents#get-repository-content","status":"404"}`
- **Stderr:** `gh: Not Found (HTTP 404)`

This is the pinned 404 envelope shape: a `message`, a `documentation_url`, and a `status` field carrying the code as a JSON string, plus a `gh`-printed stderr line carrying a parenthesised `(HTTP <code>)` fragment.
`bodies/error-404.json` and `bodies/error-404.stderr` are this capture's stdout and stderr verbatim.

## Capture 2: the type probe against a real file

- **Command:** `gh api "repos/Knatte18/quarry/contents/.gitignore" --jq 'if type=="array" then "dir" else .type end'`
- **Exit status:** `0`
- **Stdout:** `file`
- **Unfiltered response** (used to build the fixture): a JSON object with `name`, `path`, `sha`, `size`, `url`, `html_url`, `git_url`, `download_url`, `type: "file"`, base64 `content`, `encoding: "base64"`, and `_links`.

`bodies/probe-file.json` is this capture's unfiltered response body verbatim (`.gitignore` was chosen over a larger file in the same repository to keep the checked-in fixture small; the shape is identical for any file).

## Capture 3: the type probe against a real directory

- **Command:** `gh api "repos/Knatte18/quarry/contents/bench" --jq 'if type=="array" then "dir" else .type end'`
- **Exit status:** `0`
- **Stdout:** `dir`
- **Unfiltered response:** a JSON array of one object (`bench` contains exactly one entry, the `loomyard-eval` subdirectory), each with `type: "dir"`.

`bodies/probe-dir.json` is this capture's unfiltered response body verbatim.
The jq expression `if type=="array" then "dir" else .type end` answers `dir` for the array shape and the object's own `type` field otherwise, confirmed against both captures 2 and 3 above.

## Capture 4: the content fetch with the raw media-type header, against the same real file

- **Command:** `gh api "repos/Knatte18/quarry/contents/.gitignore" -H "Accept: application/vnd.github.raw"`
- **Exit status:** `0`
- **Stdout:** the file's raw text content, byte-identical to the file at that path (no JSON envelope, no base64) — confirms the raw-media-type response body is the content itself, not the contents-API object.

## Capture 5 (conditional, not observed): type probe against a symlink entry

Attempted against `Knatte18/quarry`'s full recursive git tree (`gh api "repos/Knatte18/quarry/git/trees/main?recursive=1" --jq '.tree[] | select(.mode=="120000" or .mode=="160000") | .path'`) and, on a best-effort basis, against both reachable private repositories (`<private-repo>`, `<private-repo>`) the same way.
No entry with git mode `120000` (symlink) exists in any of the three repositories reachable with this box's credentials.
**Not observed.** `bodies/probe-symlink.json` is derived from `bodies/probe-file.json`'s observed shape with `type` changed to `"symlink"`, `name`/`path` changed to a placeholder symlink name, and `content` changed to a placeholder base64 payload — the shape is otherwise identical to the observed file-probe response, per the disposition that a contents-API object response carries the same field set regardless of `type`.

## Capture 6 (conditional, not observed): type probe against a submodule entry

Attempted the same way as Capture 5, selecting git mode `160000` (commit/submodule).
No submodule exists in any of the three reachable repositories.
**Not observed.** `bodies/probe-submodule.json` is derived from `bodies/probe-file.json`'s observed shape with `type` changed to `"submodule"`, `name`/`path` changed to a placeholder module name, `content`/`encoding` dropped, `download_url` set to `null`, and a `submodule_git_url` field added — matching the documented real shape of a submodule contents-API entry, which carries no blob content.

## Capture 7 (conditional, not observed): default-media-type contents call against a file above roughly one megabyte

Attempted by ranking every blob's `size` field (descending) across the recursive git tree of `Knatte18/quarry` and both reachable private repositories.
The largest blob found across all three repositories is under 500 KB (`<private-repo>`'s largest file, a PDF receipt, at 479678 bytes).
**Not observed — the contents-API ceiling was not exercised.** No fixture is built for this case; the limitation is carried in the skill/README documentation as described (per the discussion), not as an observation, per the disposition for this capture.

## Capture 8 (best-effort, unresolved): `curl` against a symlink path on `raw.githubusercontent.com`

Not attempted against a live symlink path, because no symlink target exists in any repository reachable with this box's credentials (see Capture 5).
**Unresolved.** Nothing in this plan assumes what `raw.githubusercontent.com` does with a symlink path; batch 3's conditional caveat sentence about the raw path's symlink behaviour must be written as unresolved rather than as either "the limitation stands" or "the limitation is empty in practice."

For context (not a fixture, and not this capture's question), a plain successful raw fetch and a plain raw 404 were both exercised against `Knatte18/quarry` to confirm the general raw-host response shape:

- `curl -s -f -L --connect-timeout 5 --max-time 30 -o out -w '%{http_code}\n' "https://raw.githubusercontent.com/Knatte18/quarry/HEAD/.gitignore"` → HTTP `200`, exit `0`, body byte-identical to the file.
- `curl -s -L --connect-timeout 5 --max-time 30 -o out -w '%{http_code}\n' "https://raw.githubusercontent.com/Knatte18/quarry/HEAD/does-not-exist-nope.txt"` → HTTP `404`, exit `0` (without `-f`), body `404: Not Found` — this is exactly why `-f` is load-bearing: without it, curl's own exit status alone would not signal the failure.

## Derived fixtures (not live-captured, by design)

Two fixtures are explicitly **not** live-captured:

- `bodies/error-401.json`: a 401 cannot be produced without revoking the very authentication every other capture in this file depends on.
  Derived from Capture 1's pinned envelope shape (`message`, `documentation_url`, `status`) with `status: "401"` and a `message` worded the way GitHub's REST API documents an unauthenticated response ("Requires authentication").
- `bodies/error-403.json`: a 403 is a rate-limit or access-denied condition that must not be deliberately triggered.
  Derived from Capture 1's pinned envelope shape with `status: "403"` and a `message` worded the way GitHub's REST API documents a rate-limited response ("API rate limit exceeded for this token.").

Two further fixtures are derived from the same pinned envelope shape, with the `status` field removed rather than changed, because they exist to exercise the second and third status-extraction sources rather than the first:

- `bodies/error-nostatus.json`: Capture 1's envelope with the `status` field dropped, on one physical line.
- `bodies/error-multiline.json`: the same no-`status` envelope, pretty-printed across four physical lines, so a scenario can assert the generic diagnosis still collapses to exactly one physical stderr line.

## Note for card 11 (status-extraction ordering)

Card 11's status-extraction order — body first, then stderr, then the generic form — has no capture above that disagrees with the plan's description, so no divergence comment was needed in `github-read.sh`.
