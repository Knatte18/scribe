# Put the current scribe source into use on this machine: mirror each plugin in this repo into the
# Claude Code plugin cache directory it is installed in, regardless of version.
# `claude plugin update` is version-gated and skips an unchanged version, so it cannot deploy an
# in-place edit; this can. Install a plugin once first
# (`claude plugin marketplace add Knatte18/scribe`, `claude plugin install scribe@scribe`).
# Windows twin of update-plugins.sh.
$ErrorActionPreference = 'Stop'

$repoRoot = $PSScriptRoot
$manifest = Get-Content (Join-Path $repoRoot '.claude-plugin/marketplace.json') -Raw | ConvertFrom-Json
$installedPath = Join-Path $HOME '.claude/plugins/installed_plugins.json'
$installed = if (Test-Path $installedPath) { Get-Content $installedPath -Raw | ConvertFrom-Json } else { $null }

foreach ($plugin in $manifest.plugins) {
    $key = "$($plugin.name)@$($manifest.name)"
    $sourceDir = Join-Path $repoRoot $plugin.source
    $entry = if ($installed) { $installed.plugins.$key } else { $null }
    $target = if ($entry) { $entry[0].installPath } else { $null }

    if (-not $target -or -not (Test-Path $target)) {
        Write-Host "Skipped (not installed): $key -- run 'claude plugin install $key' first."
        continue
    }

    # /MIR mirrors, deleting anything in the target the source no longer has.
    robocopy $sourceDir $target /MIR /NFL /NDL /NJH /NJS /NP | Out-Null
    if ($LASTEXITCODE -ge 8) { throw "robocopy failed for $key (exit $LASTEXITCODE)" }
    Write-Host "Synced: $key -> $target"
}

Write-Host ""
Write-Host "Done. Run /reload-plugins in open sessions; new sessions load it on start."
