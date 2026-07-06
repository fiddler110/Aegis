#Requires -Version 5.1
<#
.SYNOPSIS
    Build Aegis and set up your shell for first-time use on Windows.

.DESCRIPTION
    Three actions; [3] is opt-in and never implied by "all":
      [1] Compile aegis.exe and install it to your Go bin directory
      [2] Add an aegis-config function to your PowerShell profile so you can
          run "aegis-config" to open the Aegis config file in your editor
      [3] (opt-in) Install the host security scanner tools aegis scan / the
          security_scan tool use (opengrep, trivy, gitleaks, ...) via the
          guided `aegis security install` command for each tool

    The script shows exactly what it will do and asks you to confirm before
    taking any action.

.PARAMETER Action
    Which actions to run without prompting: any of 1, 2, 3, all, or none
    (space-separated, e.g. "all 3"). When omitted the script prompts
    interactively.

.EXAMPLE
    .\build-windows.ps1          # interactive
    .\build-windows.ps1 1        # build only
    .\build-windows.ps1 2        # profile only
    .\build-windows.ps1 3        # security tools only (opt-in)
    .\build-windows.ps1 all      # actions 1 + 2 (never 3 — opt-in)
    .\build-windows.ps1 "all 3"  # actions 1 + 2 + 3
#>
param(
    [string]$Action = ""
)

$ErrorActionPreference = "Stop"

# ─── Colour helpers ────────────────────────────────────────────────────────────
function Write-Header  ($t) { Write-Host "  $t" -ForegroundColor Cyan }
function Write-Item    ($t) { Write-Host "    $t" -ForegroundColor White }
function Write-Detail  ($t) { Write-Host "        $t" -ForegroundColor DarkGray }
function Write-Ok      ($t) { Write-Host "  OK  $t" -ForegroundColor Green }
function Write-Skip    ($t) { Write-Host "  --  $t" -ForegroundColor DarkGray }
function Write-Warn    ($t) { Write-Host "  !!  $t" -ForegroundColor Yellow }
function Write-Divider     { Write-Host ("  " + ("─" * 66)) -ForegroundColor DarkGray }

# Every entry in $SecurityTools has a binary name equal to its scanner name
# (verified against internal/security/method.go), so Get-Command is enough to
# tell whether a tool is already installed before re-running its installer.
# opengrep/kubescape ship no native Windows build and fall back to a WSL
# install (see internal/security/install.go), so they're also checked there.
function Test-ToolInstalled ($Tool) {
    if (Get-Command $Tool -ErrorAction SilentlyContinue) { return $true }
    if (($Tool -eq "opengrep" -or $Tool -eq "kubescape") -and (Get-Command wsl -ErrorAction SilentlyContinue)) {
        wsl -- bash -lc "command -v $Tool" *>$null
        if ($LASTEXITCODE -eq 0) { return $true }
    }
    return $false
}

# ─── Locate Go ─────────────────────────────────────────────────────────────────
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Error "Go is not installed or not in PATH.`nInstall from: https://go.dev/dl/"
    exit 1
}
$GoVer = (go version)

# ─── Resolve binary install location ──────────────────────────────────────────
$InstallDir = if ($env:GOPATH) { Join-Path $env:GOPATH "bin" }
              else              { Join-Path $env:USERPROFILE "go\bin" }
$BinDest = Join-Path $InstallDir "aegis.exe"
$BinExists = Test-Path $BinDest

# ─── Detect stale binary at a different location ──────────────────────────────
# If aegis.exe is already on PATH but NOT at our install destination, we'll
# remove that old copy during action [1] so there is no ambiguity about which
# binary runs after installation.
$ExistingCmd = Get-Command aegis -ErrorAction SilentlyContinue
$ExistingBin = if ($ExistingCmd) { $ExistingCmd.Source } else { $null }
if ($ExistingBin -and ($ExistingBin -ieq $BinDest)) { $ExistingBin = $null }

# ─── Resolve git version ───────────────────────────────────────────────────────
$Version = git describe --tags --always --dirty 2>$null
if (-not $Version) { $Version = "dev" }

# ─── Resolve PowerShell profile ────────────────────────────────────────────────
# CurrentUserCurrentHost ($PROFILE) is the most targeted. We use that over
# AllHosts so it only affects PowerShell, not older Windows PowerShell or pwsh.
$ProfilePath   = $PROFILE.CurrentUserCurrentHost
$ProfileDir    = Split-Path $ProfilePath
$ProfileExists = Test-Path $ProfilePath
$AliasExists   = $ProfileExists -and ((Get-Content $ProfilePath -Raw -ErrorAction SilentlyContinue) -match 'aegis-config')
$ConfigPath    = Join-Path $env:APPDATA "aegis\config.yaml"

# ─── Security scanner tools (opt-in action [3]) ───────────────────────────────
# Mirrors internal/security/method.go's descriptor list — every scanner that
# has a guided host install (zap is container-only and has none, so it's
# handled by container config, not this list).
$SecurityTools = @("opengrep","semgrep","gosec","bandit","brakeman","njsscan","trivy","gitleaks","kubescape","hadolint","grype","dockle","osv-scanner","syft","nmap","nuclei")

# ─── Show plan ─────────────────────────────────────────────────────────────────
Write-Host ""
Write-Divider
Write-Header "Aegis Build Script — Windows"
Write-Divider
Write-Host ""
Write-Host "  The following actions are available:" -ForegroundColor White
Write-Host ""

# Action 1
$BinStatus = if ($BinExists) { "(replaces existing aegis.exe)" } else { "(new install)" }
Write-Item "[1] Build aegis $Version and install binary"
Write-Detail "From : ./cmd/aegis"
Write-Detail "To   : $BinDest  $BinStatus"
Write-Detail "Go   : $GoVer"
if ($ExistingBin) { Write-Detail "Old  : $ExistingBin  (will be removed)" }
Write-Host ""

# Action 2
Write-Item "[2] Add aegis-config function to PowerShell profile"
if ($AliasExists) {
    Write-Detail "Status : aegis-config already present in profile — will skip"
} else {
    $ProfileLabel = if ($ProfileExists) { "exists" } else { "will be created" }
    Write-Detail "Profile: $ProfilePath  ($ProfileLabel)"
    Write-Detail "Config : $ConfigPath"
    Write-Detail "Usage  : aegis-config  →  opens config in your preferred editor (prompted)"
}

Write-Host ""

# Action 3 (opt-in — never implied by "all")
Write-Item "[3] (opt-in) Install security scanner tools"
Write-Detail ("Tools  : " + ($SecurityTools -join ", "))
Write-Detail "Runs   : aegis security install <tool> --yes  for each, using the binary from [1]"
Write-Detail "Skips  : any tool already found on PATH (or in WSL, for opengrep/kubescape) is left alone"
Write-Detail "Note   : best-effort — several tools need Go/pipx/scoop and may fail individually"
Write-Detail "         if that toolchain isn't installed; failures are reported but don't stop"
Write-Detail "         the run. Not included in 'all'."

Write-Host ""
Write-Divider
Write-Host ""

# ─── Prompt (or use supplied argument) ────────────────────────────────────────
$raw = $Action.Trim().ToLower()
if ($raw -eq "") {
    $raw = (Read-Host "  Run which actions? [all / 1 2 3 / none]  (default: all - 3 is opt-in)").Trim().ToLower()
}
if ($raw -eq "") { $raw = "all" }

$RunBuild = $false
$RunAlias = $false
$RunTools = $false

if ($raw -eq "none") {
    Write-Host "  Nothing to do." -ForegroundColor DarkGray; exit 0
}

$parts = $raw -split '\s+'
foreach ($tok in $parts) {
    switch ($tok) {
        "all" { $RunBuild = $true; $RunAlias = $true }
        "1"   { $RunBuild = $true }
        "2"   { $RunAlias = $true }
        "3"   { $RunTools = $true }
    }
}

Write-Host ""

# ─── Action 1 : Build ──────────────────────────────────────────────────────────
if ($RunBuild) {
    Write-Header "[1] Building aegis $Version..."
    if (-not (Test-Path $InstallDir)) {
        New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
    }
    $ldf = "-s -w -X github.com/fiddler110/aegis/internal/cli.Version=$Version"
    go build -ldflags $ldf -o $BinDest ./cmd/aegis
    if ($LASTEXITCODE -ne 0) { Write-Error "Build failed."; exit 1 }

    # Remove any stale binary found at a different PATH location.
    if ($ExistingBin) {
        Write-Host "  Removing old binary: $ExistingBin" -ForegroundColor DarkGray
        Remove-Item $ExistingBin -Force -ErrorAction SilentlyContinue
        if (Test-Path $ExistingBin) {
            Write-Warn "Could not remove $ExistingBin — try running as Administrator"
        } else {
            Write-Ok "Removed:   $ExistingBin"
        }
    }

    Write-Ok "Installed: $BinDest"

    # PATH check
    $userPath    = [System.Environment]::GetEnvironmentVariable("PATH", "User")
    $machinePath = [System.Environment]::GetEnvironmentVariable("PATH", "Machine")
    $allPaths    = ($userPath + ";" + $machinePath) -split ';' | ForEach-Object { $_.TrimEnd('\') }
    if ($allPaths -notcontains $InstallDir.TrimEnd('\')) {
        Write-Warn "$InstallDir is not in your PATH."
        Write-Host "  To fix permanently, add it in:" -ForegroundColor DarkGray
        Write-Host "    System Properties → Advanced → Environment Variables → User PATH" -ForegroundColor DarkGray
    }
    Write-Host ""
}

# ─── Action 2 : aegis-config function ─────────────────────────────────────────
if ($RunAlias) {
    if ($AliasExists) {
        Write-Skip "[2] aegis-config already in profile — nothing to do."
    } else {
        Write-Header "[2] Adding aegis-config to PowerShell profile..."
        Write-Host ""

        # ── Editor selection ───────────────────────────────────────────────────
        Write-Host "    Choose your preferred editor for aegis-config:" -ForegroundColor White
        Write-Host ""

        $EditorChoices = @()
        $edIdx = 1
        $codeAvail = [bool](Get-Command code -ErrorAction SilentlyContinue)
        if ($codeAvail) {
            Write-Detail "[$edIdx] code     — Visual Studio Code"
            $EditorChoices += "code"
            $edIdx++
        }
        Write-Detail "[$edIdx] notepad  — Windows Notepad"
        $EditorChoices += "notepad"
        $edTotal = $edIdx

        Write-Host ""
        $rawSel = (Read-Host "        Select [1-$edTotal]  (default: 1)").Trim()
        if ($rawSel -eq "" -or $rawSel -eq "1") {
            $selIdx = 1
        } elseif ($rawSel -match '^\d+$' -and [int]$rawSel -ge 1 -and [int]$rawSel -le $edTotal) {
            $selIdx = [int]$rawSel
        } else {
            Write-Warn "Invalid selection — using option 1"; $selIdx = 1
        }
        $ChosenEditor = $EditorChoices[$selIdx - 1]

        Write-Host ""
        $alwaysRaw = (Read-Host "        Always use '$ChosenEditor' for aegis-config? [Y/n]").Trim().ToLower()
        $EditorFixed = ($alwaysRaw -eq "" -or $alwaysRaw -eq "y")
        Write-Host ""

        # Build the editor-invocation block embedded in the profile function.
        if ($EditorFixed) {
            $editorBlock = "    $ChosenEditor `$cfg`n"
        } elseif ($ChosenEditor -eq "notepad") {
            $editorBlock = @"
    if (`$env:EDITOR) {
        & `$env:EDITOR `$cfg
    } else {
        notepad `$cfg
    }
"@
        } else {
            # code chosen but not pinned — prefer $env:EDITOR, fall back to code then notepad
            $editorBlock = @"
    if (`$env:EDITOR) {
        & `$env:EDITOR `$cfg
    } elseif (Get-Command code -ErrorAction SilentlyContinue) {
        code `$cfg
    } else {
        notepad `$cfg
    }
"@
        }

        if (-not (Test-Path $ProfileDir)) {
            New-Item -ItemType Directory -Force -Path $ProfileDir | Out-Null
        }

        $block = @"


# ── aegis-config ──────────────────────────────────────────────────────────────
# Opens the Aegis global configuration file in your preferred editor.
# Run 'aegis --first-init' first if the file does not yet exist.
function aegis-config {
    `$cfg = "`$env:APPDATA\aegis\config.yaml"
    if (-not (Test-Path `$cfg)) {
        Write-Warning "Config not found at `$cfg - run: aegis --first-init"
        return
    }
$editorBlock}
"@
        Add-Content -Path $ProfilePath -Value $block -Encoding UTF8
        Write-Ok "Added to: $ProfilePath"
        Write-Detail "Reload now: . `$PROFILE"
    }
    Write-Host ""
}

# ─── Action 3 : Install security scanner tools (opt-in) ──────────────────────
if ($RunTools) {
    Write-Header "[3] Installing security scanner tools..."
    Write-Host ""

    $AegisBin = $null
    if ($RunBuild -and (Test-Path $BinDest)) {
        $AegisBin = $BinDest
    } else {
        $existing = Get-Command aegis -ErrorAction SilentlyContinue
        if ($existing) { $AegisBin = $existing.Source }
    }

    if (-not $AegisBin) {
        Write-Warn "aegis binary not found - run action [1] first (or ensure aegis is on PATH), then re-run with 3."
    } else {
        Write-Detail "Using: $AegisBin"
        Write-Host ""
        $FailedTools = @()
        $SkippedTools = @()
        foreach ($tool in $SecurityTools) {
            Write-Item $tool
            if (Test-ToolInstalled $tool) {
                Write-Skip "$tool already installed - skipping"
                $SkippedTools += $tool
                Write-Host ""
                continue
            }
            & $AegisBin security install $tool --yes
            if ($LASTEXITCODE -eq 0) {
                Write-Ok "$tool installed"
            } else {
                Write-Warn "$tool failed - see output above"
                $FailedTools += $tool
            }
            Write-Host ""
        }
        if ($SkippedTools.Count -gt 0) {
            Write-Detail ("Already installed (skipped): " + ($SkippedTools -join ", "))
        }
        if ($FailedTools.Count -gt 0) {
            Write-Warn ("Some tools failed to install: " + ($FailedTools -join ", "))
            Write-Detail "Often expected - several tools need Go/pipx/scoop, and only matter for"
            Write-Detail "projects that use that language/toolchain."
        } else {
            Write-Ok "All security tools are installed."
        }
        Write-Detail "Run 'aegis security status' to confirm what's available."
    }
    Write-Host ""
}

# ─── Done ──────────────────────────────────────────────────────────────────────
Write-Divider
Write-Host ""
Write-Ok "All done!"
Write-Host ""
Write-Host "  Next steps:" -ForegroundColor White
Write-Host "    aegis --first-init                generate global config (first run only)" -ForegroundColor DarkGray
Write-Host "    aegis --init                      create .aegis/config.yaml project override (optional)" -ForegroundColor DarkGray
Write-Host "    `$env:OPENAI_API_KEY = 'ollama'    required for Ollama; set ANTHROPIC_API_KEY for Claude" -ForegroundColor DarkGray
Write-Host "    ollama pull <model>               pull at least one model  (e.g. ollama pull llama3.2)" -ForegroundColor DarkGray
Write-Host "                                        model: auto in config selects it automatically" -ForegroundColor DarkGray
Write-Host "                                        Aegis starts Ollama itself if it is installed" -ForegroundColor DarkGray
Write-Host "    aegis                             start the TUI" -ForegroundColor DarkGray
Write-Host "    aegis ui                          open the web UI in your browser" -ForegroundColor DarkGray
Write-Host "    aegis security status             check which security scanners are available" -ForegroundColor DarkGray
if ($RunAlias -and -not $AliasExists) {
    Write-Host "    aegis-config                      open the config file  (after '. `$PROFILE')" -ForegroundColor DarkGray
}
if (-not $RunTools) {
    Write-Host "    .\build-windows.ps1 3             install security scanner tools (opt-in, not run yet)" -ForegroundColor DarkGray
}
Write-Host ""
