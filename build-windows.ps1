#Requires -Version 5.1
<#
.SYNOPSIS
    Build Aegis and install it to the Go user bin directory on Windows.

.DESCRIPTION
    Compiles ./cmd/aegis, embeds the git version tag, and installs the
    resulting aegis.exe to %USERPROFILE%\go\bin (or %GOPATH%\bin if GOPATH
    is set). Both locations are added to PATH by the standard Go installer,
    so aegis will be available immediately in new terminals without any
    extra configuration.

.EXAMPLE
    .\build-windows.ps1
#>

$ErrorActionPreference = "Stop"

# ── Locate Go ─────────────────────────────────────────────────────────────────
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Error "Go is not installed or not in PATH.`nInstall from: https://go.dev/dl/"
    exit 1
}

Write-Host "Using $(go version)"

# ── Resolve install directory ──────────────────────────────────────────────────
# Prefer $GOPATH\bin if GOPATH is explicitly set; otherwise default to the
# standard %USERPROFILE%\go\bin that the Go installer puts on PATH.
$InstallDir = if ($env:GOPATH) {
    Join-Path $env:GOPATH "bin"
} else {
    Join-Path $env:USERPROFILE "go\bin"
}

if (-not (Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
    Write-Host "Created install directory: $InstallDir"
}

$Dest = Join-Path $InstallDir "aegis.exe"

# ── Resolve version string ─────────────────────────────────────────────────────
$Version = git describe --tags --always --dirty 2>$null
if (-not $Version) { $Version = "dev" }

# ── Build ──────────────────────────────────────────────────────────────────────
Write-Host ""
Write-Host "Building aegis $Version..."

$LDFlags = "-s -w -X github.com/scottymacleod/aegis/internal/cli.Version=$Version"

go build -ldflags $LDFlags -o $Dest ./cmd/aegis
if ($LASTEXITCODE -ne 0) {
    Write-Error "Build failed."
    exit 1
}

# ── Done ───────────────────────────────────────────────────────────────────────
Write-Host ""
Write-Host "Installed: $Dest"
Write-Host "Version:   $Version"
Write-Host ""
Write-Host "Next steps:"
Write-Host "  1. Open a new terminal (or run: `$env:PATH = [System.Environment]::GetEnvironmentVariable('PATH','Machine') + ';' + [System.Environment]::GetEnvironmentVariable('PATH','User'))"
Write-Host "  2. Run: aegis --first-init"
Write-Host "  3. Set: `$env:OPENAI_API_KEY = 'ollama'   (for Ollama; see config for other providers)"
Write-Host "  4. Run: aegis"
