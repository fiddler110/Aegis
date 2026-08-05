#Requires -Version 5.1
<#
.SYNOPSIS
    Build Aegis and set up your shell for first-time use on Windows.

.DESCRIPTION
    Four actions; [3] and [4] are opt-in and never implied by "all":
      [1] Compile aegis.exe and install it to your Go bin directory
      [2] Add an aegis-config function to your PowerShell profile so you can
          run "aegis-config" to open the Aegis config file in your editor
      [3] (opt-in, RECOMMENDED) Build the container scanner images and fill the
          vulnerability-database cache volume, so `aegis scan` / the security_scan
          tool run pinned, verified, confined scanners instead of whatever host
          binaries happen to be installed
      [4] (opt-in, fallback) Install the host security scanner binaries one at a
          time via the guided `aegis security install` command — for machines with
          no container runtime, plus dockle, which no scanner image can carry

    [3] and [4] are alternatives, not a sequence. Prefer [3]: host binaries are
    unpinned and unconfined, so two machines silently scan with different rule
    sets, and under `default_method: auto` Aegis prefers the container anyway.

    The script shows exactly what it will do and asks you to confirm before
    taking any action.

.PARAMETER Action
    Which actions to run without prompting: any of 1, 2, 3, 4, all, or none
    (space-separated, e.g. "all 3"). When omitted the script prompts
    interactively.

.PARAMETER WslDistro
    For action [3] only, and only when no Windows-side container runtime is
    available: the WSL distro to look inside for docker/podman. Defaults to
    probing every registered distro. Match this to security.wsl_distro in your
    Aegis config if you have set one.

.EXAMPLE
    .\build-windows.ps1          # interactive
    .\build-windows.ps1 1        # build only
    .\build-windows.ps1 2        # profile only
    .\build-windows.ps1 3        # container scanner images only (opt-in)
    .\build-windows.ps1 4        # host scanner binaries only (opt-in)
    .\build-windows.ps1 all      # actions 1 + 2 (never 3/4 — opt-in)
    .\build-windows.ps1 "all 3"  # actions 1 + 2 + 3 (recommended)
#>
param(
    [string]$Action = "",
    [string]$WslDistro = ""
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
# Hoisted out of action [1]: action [3]'s WSL fallback cross-compiles a Linux
# binary with the same stamp, and may run without [1] having been selected.
$ldf = "-s -w -X github.com/fiddler110/aegis/internal/cli.Version=$Version"

# ─── Resolve PowerShell profile ────────────────────────────────────────────────
# CurrentUserCurrentHost ($PROFILE) is the most targeted. We use that over
# AllHosts so it only affects PowerShell, not older Windows PowerShell or pwsh.
$ProfilePath   = $PROFILE.CurrentUserCurrentHost
$ProfileDir    = Split-Path $ProfilePath
$ProfileExists = Test-Path $ProfilePath
$AliasExists   = $ProfileExists -and ((Get-Content $ProfilePath -Raw -ErrorAction SilentlyContinue) -match 'aegis-config')
$ConfigPath    = Join-Path $env:APPDATA "aegis\config.yaml"

# ─── Container scanner images (opt-in action [3]) ─────────────────────────────
# Only docker and podman can BUILD the images: the build shells out to
# "<runtime> build -f Containerfile".
#
# The runtime is passed to build-image EXPLICITLY rather than left to
# auto-detection. Aegis's Windows priority is [podman, docker, wslc], so wslc no
# longer wins by default — but DetectBest returns the first *available* engine,
# not the one that built anything, and a locally-built image lives only in the
# storage of the engine that built it. On a machine with both installed, being
# explicit is what keeps the build, the pin and the cache volumes on one engine.
function Get-BuildRuntime {
    foreach ($rt in @("docker","podman")) {
        if (Get-Command $rt -ErrorAction SilentlyContinue) {
            & $rt info *>$null
            if ($LASTEXITCODE -eq 0) { return @{ Name = $rt; Up = $true } }
            return @{ Name = $rt; Up = $false }
        }
    }
    return @{ Name = $null; Up = $false }
}
$RuntimeInfo    = Get-BuildRuntime
$ContainerRt    = if ($RuntimeInfo.Up) { $RuntimeInfo.Name } else { $null }
$RtPresentDown  = if (-not $RuntimeInfo.Up) { $RuntimeInfo.Name } else { $null }

# ── WSL fallback probe ─────────────────────────────────────────────────────────
# Docker Desktop and Podman Desktop both put their client on the Windows PATH, so
# reaching this means neither is installed (or WSL integration is off). A distro
# may still have its own engine. Note the caveat this raises, which the script
# states rather than hides: a locally-built image lives only in the storage of the
# engine that built it, so an image built inside WSL is reachable by an Aegis
# running INSIDE that distro — not by aegis.exe on Windows.
function Find-WslRuntime ($Distro) {
    if (-not (Get-Command wsl -ErrorAction SilentlyContinue)) { return $null }
    $distros = @()
    if ($Distro) {
        $distros = @($Distro)
    } else {
        # wsl -l -q emits UTF-16LE; decode it rather than filtering nulls by hand.
        $prevEnc = [Console]::OutputEncoding
        try {
            [Console]::OutputEncoding = [System.Text.Encoding]::Unicode
            $distros = (wsl -l -q) | ForEach-Object { $_.Trim() } | Where-Object { $_ }
        } catch {
            return $null
        } finally {
            [Console]::OutputEncoding = $prevEnc
        }
    }
    foreach ($d in $distros) {
        foreach ($rt in @("docker","podman")) {
            wsl -d $d -- sh -lc "command -v $rt >/dev/null 2>&1 && $rt info >/dev/null 2>&1" *>$null
            if ($LASTEXITCODE -eq 0) { return @{ Distro = $d; Runtime = $rt } }
        }
    }
    return $null
}
$WslRuntime = $null
if (-not $ContainerRt) { $WslRuntime = Find-WslRuntime $WslDistro }

# ─── Security scanner tools (opt-in action [4]) ───────────────────────────────
# Mirrors internal/security/method.go's descriptor list — every scanner that has
# a guided host install. zap has none (it keeps its own official image, invoked
# directly), so it is not here.
#
# dockle comes first because it is the one tool no container image can carry: it
# inspects images through the container engine socket, which is effectively host
# root. Everything after it is also available via action [3], pinned and
# confined — install those on the host only if you have no container runtime.
$SecurityToolsHostOnly = @("dockle")
$SecurityToolsAlsoContainerized = @("opengrep","gosec","bandit","brakeman","njsscan","trivy","gitleaks","trufflehog","kubescape","hadolint","grype","osv-scanner","syft","nmap","nuclei")
$SecurityTools = $SecurityToolsHostOnly + $SecurityToolsAlsoContainerized

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
Write-Item "[3] (opt-in, recommended) Build the container scanner images"
Write-Detail "Runs   : aegis security build-image  (+ update-db), using the binary from [1]"
Write-Detail "Asks   : which profile - core (static scanners) or full (+ Python/Ruby/Go/"
Write-Detail "         network), and whether to also build the netscanner image"
if ($ContainerRt) {
    Write-Detail "Runtime: $ContainerRt  (detected - the image is pinned to the engine that builds it)"
} elseif ($RtPresentDown) {
    Write-Detail "Runtime: $RtPresentDown found but not responding - start it first, then re-run with 3"
    Write-Detail "         (launch Docker Desktop, or run 'podman machine start')"
} elseif ($WslRuntime) {
    Write-Detail "Runtime: none on Windows, but $($WslRuntime.Runtime) is running inside WSL distro"
    Write-Detail "         '$($WslRuntime.Distro)'. The script can cross-compile a Linux aegis and"
    Write-Detail "         build there - but see the warning it prints: the image lives in that"
    Write-Detail "         distro's engine storage, so only an Aegis run inside WSL can use it."
} else {
    Write-Detail "Runtime: NONE FOUND - install Docker Desktop or Podman Desktop (or enable WSL"
    Write-Detail "         integration if you already have one), or use action [4] instead"
}
Write-Detail "Note   : the build is long and large (core ~1GB, full ~2-3GB, netscanner ~570MB)."
Write-Detail "         Vulnerability databases are NOT in the image - update-db fills a shared"
Write-Detail "         volume so scans don't re-download ~1.2GB each time. The pin is written"
Write-Detail "         machine-wide to %AppData%\aegis\config.yaml, not to this repo."

Write-Host ""

# Action 4 (opt-in — never implied by "all")
Write-Item "[4] (opt-in, fallback) Install host security scanner binaries"
Write-Detail ("Tools  : " + ($SecurityTools -join ", "))
Write-Detail "Runs   : aegis security install <tool> --yes  for each, using the binary from [1]"
Write-Detail "Skips  : any tool already found on PATH (or in WSL, for opengrep/kubescape) is left alone"
Write-Detail "Note   : prefer [3]. Host binaries are unpinned and unconfined, so two machines can"
Write-Detail "         silently scan with different rule sets, and under default_method: auto a"
Write-Detail "         tool the image carries resolves to the container regardless. dockle is the"
Write-Detail "         exception - it needs the container engine socket, so no scanner image can"
Write-Detail "         carry it. Best-effort: several tools need Go/pipx/scoop and may fail"
Write-Detail "         individually; failures are reported but don't stop the run."

Write-Host ""
Write-Divider
Write-Host ""

# ─── Prompt (or use supplied argument) ────────────────────────────────────────
$raw = $Action.Trim().ToLower()
if ($raw -eq "") {
    $raw = (Read-Host "  Run which actions? [all / 1 2 3 4 / none]  (default: all - 3 and 4 are opt-in)").Trim().ToLower()
}
if ($raw -eq "") { $raw = "all" }

$RunBuild  = $false
$RunAlias  = $false
$RunImages = $false
$RunTools  = $false

if ($raw -eq "none") {
    Write-Host "  Nothing to do." -ForegroundColor DarkGray; exit 0
}

$parts = $raw -split '\s+'
foreach ($tok in $parts) {
    switch ($tok) {
        "all" { $RunBuild = $true; $RunAlias = $true }
        "1"   { $RunBuild = $true }
        "2"   { $RunAlias = $true }
        "3"   { $RunImages = $true }
        "4"   { $RunTools = $true }
    }
}

Write-Host ""

# ─── Action 1 : Build ──────────────────────────────────────────────────────────
if ($RunBuild) {
    Write-Header "[1] Building aegis $Version..."
    if (-not (Test-Path $InstallDir)) {
        New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
    }
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

# ─── Resolve the aegis binary for actions [3] and [4] ────────────────────────
function Resolve-AegisBin {
    if ($RunBuild -and (Test-Path $BinDest)) { return $BinDest }
    $existing = Get-Command aegis -ErrorAction SilentlyContinue
    if ($existing) { return $existing.Source }
    return $null
}

# Ask once for the profile and whether to also build the netscanner. Shared by
# the Windows-runtime and WSL-fallback paths so the two can't drift.
function Read-ImagePlan {
    Write-Host "    Which multiscanner profile?" -ForegroundColor White
    Write-Host ""
    Write-Detail "[1] full  - every filesystem scanner: adds Python (bandit/njsscan), Ruby"
    Write-Detail "            (brakeman), Go (gosec + a pinned Go toolchain) and network"
    Write-Detail "            (nmap/nuclei) on top of core. ~2-3GB, long first build."
    Write-Detail "[2] core  - static binaries only: trivy, gitleaks, trufflehog, syft,"
    Write-Detail "            osv-scanner, grype, kubescape, hadolint, opengrep. ~1GB."
    Write-Host ""
    # Not named $profile: that is a PowerShell automatic variable ($PROFILE),
    # which this script also uses for action [2].
    $sel = (Read-Host "        Select [1-2]  (default: 1)").Trim()
    $prof = if ($sel -eq "2") { "core" } else { "full" }

    Write-Host ""
    Write-Detail "The netscanner is a second, smaller image (~570MB): nmap, nuclei, and"
    Write-Detail "image-reference scanning with trivy/grype. It runs with network ON and"
    Write-Detail "your workspace never mounted - that mount posture is why it is separate."
    $ns = (Read-Host "        Also build the netscanner image? [y/N]").Trim().ToLower()
    Write-Host ""
    return @{ Profile = $prof; Netscanner = ($ns -eq "y") }
}

# ─── Action 3 : Build the container scanner images (opt-in) ──────────────────
if ($RunImages) {
    Write-Header "[3] Building container scanner images..."
    Write-Host ""

    $AegisBin = Resolve-AegisBin

    if (-not $AegisBin) {
        Write-Warn "aegis binary not found - run action [1] first (or ensure aegis is on PATH), then re-run with 3."
    } elseif ($ContainerRt) {
        Write-Detail "Using: $AegisBin"
        Write-Detail "Runtime: $ContainerRt"
        Write-Host ""
        $plan = Read-ImagePlan
        $imageFailures = @()

        # build-image verifies the result itself (each scanner is probed AND run
        # against a fixture with planted findings), then pins the image ID into
        # the user config. A --version probe alone cannot catch a tool that exits
        # clean reporting zero because it never loaded its data, which is how two
        # scanners previously shipped broken - so --skip-verify is never passed.
        Write-Header "  build-image --profile $($plan.Profile)"
        & $AegisBin security build-image --profile $plan.Profile --runtime $ContainerRt
        if ($LASTEXITCODE -eq 0) {
            Write-Ok "multiscanner image built, verified and pinned"
        } else {
            Write-Warn "multiscanner build failed - see output above"
            $imageFailures += "build-image"
        }
        Write-Host ""

        # update-db runs a container from the image just built, so it is only
        # worth attempting if that succeeded. It is the one container run given
        # network access, and it mounts no workspace.
        if ($imageFailures.Count -eq 0) {
            Write-Header "  update-db  (fills the shared vulnerability-database volume)"
            Write-Detail "Needs network. Scans run with --network none and read this volume, so"
            Write-Detail "without it the database-backed scanners are refused rather than silently"
            Write-Detail "reporting zero findings."
            & $AegisBin security update-db
            if ($LASTEXITCODE -eq 0) {
                Write-Ok "vulnerability databases populated"
            } else {
                Write-Warn "update-db failed - re-run 'aegis security update-db' once network is available"
                $imageFailures += "update-db"
            }
            Write-Host ""
        }

        if ($plan.Netscanner) {
            Write-Header "  build-image --netscanner"
            Write-Detail "Its verification needs network - proving trivy/grype load a database"
            Write-Detail "against a known-vulnerable image is the whole point. No update-db needed."
            & $AegisBin security build-image --netscanner --runtime $ContainerRt
            if ($LASTEXITCODE -eq 0) {
                Write-Ok "netscanner image built, verified and pinned"
            } else {
                Write-Warn "netscanner build failed - see output above"
                $imageFailures += "build-image --netscanner"
            }
            Write-Host ""
        }

        if ($imageFailures.Count -gt 0) {
            Write-Warn ("Some image steps failed: " + ($imageFailures -join ", "))
        } else {
            Write-Ok "Container scanners are ready."
        }
        Write-Detail "Run 'aegis security status' to see which scanners resolve to the images,"
        Write-Detail "and how old each vulnerability database is."
    } elseif ($RtPresentDown) {
        Write-Warn "$RtPresentDown is installed but not responding to '$RtPresentDown info'."
        Write-Detail "Launch Docker Desktop, or run 'podman machine start', then re-run with 3."
    } elseif ($WslRuntime) {
        # The honest version of the WSL fallback. It works, but it produces an
        # image only an Aegis running inside that distro can use, so the choice
        # is stated up front rather than discovered later via "image not found".
        Write-Warn "No container runtime on Windows, but $($WslRuntime.Runtime) is running inside WSL distro '$($WslRuntime.Distro)'."
        Write-Host ""
        Write-Detail "A locally-built image exists ONLY in the storage of the engine that built it."
        Write-Detail "Building inside WSL therefore gives you scanners for an Aegis run INSIDE that"
        Write-Detail "distro - aegis.exe on Windows will not find the image, and the pin would be"
        Write-Detail "written to the WSL user config, not %AppData%\aegis\config.yaml."
        Write-Detail ""
        Write-Detail "If you want aegis.exe on Windows to use the images instead, stop here and turn"
        Write-Detail "on Docker Desktop's or Podman Desktop's WSL integration (which puts the client"
        Write-Detail "on the Windows PATH), then re-run with 3."
        Write-Host ""
        $go = (Read-Host "        Cross-compile a Linux aegis and build inside '$($WslRuntime.Distro)'? [y/N]").Trim().ToLower()
        if ($go -ne "y") {
            Write-Skip "Skipped the WSL build."
        } else {
            Write-Host ""
            $plan = Read-ImagePlan

            # Cross-compiled from Windows rather than built inside the distro:
            # Aegis is pure Go (modernc SQLite, no cgo), so this needs no Go
            # toolchain in WSL at all.
            $linuxBin = Join-Path $env:TEMP "aegis-linux-build"
            Write-Header "  cross-compiling aegis for linux/amd64"
            $env:GOOS = "linux"; $env:GOARCH = "amd64"; $env:CGO_ENABLED = "0"
            try {
                go build -ldflags $ldf -o $linuxBin ./cmd/aegis
            } finally {
                Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue
            }
            if ($LASTEXITCODE -ne 0) {
                Write-Warn "Cross-compile failed - cannot run the WSL build."
            } else {
                $d = $WslRuntime.Distro
                $wslSrc = (wsl -d $d -- wslpath -a ($linuxBin -replace '\\','/')).Trim()
                wsl -d $d -- sh -lc "mkdir -p ~/.local/bin && cp '$wslSrc' ~/.local/bin/aegis && chmod +x ~/.local/bin/aegis"
                if ($LASTEXITCODE -ne 0) {
                    Write-Warn "Could not stage the Linux binary inside '$d'."
                } else {
                    Write-Ok "Staged: ~/.local/bin/aegis inside '$d'"
                    Write-Host ""
                    $nsArg = if ($plan.Netscanner) { "yes" } else { "no" }
                    $script = @"
set -e
~/.local/bin/aegis security build-image --profile $($plan.Profile) --runtime $($WslRuntime.Runtime)
~/.local/bin/aegis security update-db
if [ "$nsArg" = "yes" ]; then
  ~/.local/bin/aegis security build-image --netscanner --runtime $($WslRuntime.Runtime)
fi
~/.local/bin/aegis security status
"@
                    wsl -d $d -- sh -lc $script
                    if ($LASTEXITCODE -eq 0) {
                        Write-Ok "Container scanners are ready inside '$d'."
                    } else {
                        Write-Warn "The WSL build reported a failure - see output above."
                    }
                    Write-Detail "Use them by running aegis from inside '$d' (wsl -d $d -- ~/.local/bin/aegis)."
                }
                Remove-Item $linuxBin -Force -ErrorAction SilentlyContinue
            }
        }
    } else {
        Write-Warn "No container runtime found - the images need docker or podman to build."
        Write-Detail "Install Docker Desktop or Podman Desktop (and enable WSL integration), or"
        Write-Detail "use action [4] to install host scanner binaries instead."
    }
    Write-Host ""
}

# ─── Action 4 : Install host security scanner binaries (opt-in) ──────────────
if ($RunTools) {
    Write-Header "[4] Installing host security scanner binaries..."
    Write-Host ""

    $AegisBin = Resolve-AegisBin

    if (-not $AegisBin) {
        Write-Warn "aegis binary not found - run action [1] first (or ensure aegis is on PATH), then re-run with 4."
    } else {
        if ($ContainerRt -and -not $RunImages) {
            Write-Warn "$ContainerRt is available - action [3] would give you pinned, verified,"
            Write-Detail "confined scanners instead. Continuing with host installs as requested."
            Write-Host ""
        }
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
            Write-Detail "Action [3] avoids this entirely for every tool except dockle."
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
if (-not $RunImages) {
    Write-Host "    .\build-windows.ps1 3             build the container scanner images (opt-in, not run yet)" -ForegroundColor DarkGray
}
if (-not $RunTools) {
    Write-Host "    .\build-windows.ps1 4             install host scanner binaries (opt-in, not run yet)" -ForegroundColor DarkGray
}
if ($RunImages) {
    Write-Host "    aegis security update-db          re-run periodically - a stale database" -ForegroundColor DarkGray
    Write-Host "                                        under-reports rather than failing" -ForegroundColor DarkGray
}
Write-Host ""
