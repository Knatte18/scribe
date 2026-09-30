#!/usr/bin/env bash
# Put the current scribe source into use on this machine: mirror each plugin in this repo into the
# Claude Code plugin cache directory it is installed in, regardless of version.
# `claude plugin update` is version-gated and skips an unchanged version, so it cannot deploy an
# in-place edit; this can. Run from anywhere; install a plugin once first
# (`claude plugin marketplace add Knatte18/scribe`, `claude plugin install scribe@scribe`).
# POSIX twin of update-plugins.ps1.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
manifest="$repo_root/.claude-plugin/marketplace.json"
installed="$HOME/.claude/plugins/installed_plugins.json"
marketplace="$(jq -r '.name' "$manifest")"

jq -c '.plugins[]' "$manifest" | while read -r plugin; do
    name="$(jq -r '.name' <<<"$plugin")"
    source_dir="$repo_root/$(jq -r '.source' <<<"$plugin")"
    key="$name@$marketplace"
    target="$(jq -r --arg k "$key" '.plugins[$k][0].installPath // empty' "$installed" 2>/dev/null || true)"

    if [[ -z "$target" || ! -d "$target" ]]; then
        echo "Skipped (not installed): $key -- run 'claude plugin install $key' first."
        continue
    fi

    rsync -a --delete "$source_dir/" "$target/"
    echo "Synced: $key -> $target"
done

echo ""
echo "Done. Run /reload-plugins in open sessions; new sessions load it on start."
