# install-windows.ps1
# Installs the latest Pando release for Windows to the user's PATH
# Usage: iex (irm https://raw.githubusercontent.com/digiogithub/pando/main/scripts/install-windows.ps1)
#   or:  .\install-windows.ps1 [-Version v0.311.0]

param(
    [string]$Version = ""
)

$ErrorActionPreference = "Stop"

$REPO     = "digiogithub/pando"
$BINARY   = "pando.exe"
$INSTALL_DIR = Join-Path $env:LOCALAPPDATA "Programs\pando"

# ── helpers ────────────────────────────────────────────────────────────────────

function Write-Step([string]$msg) { Write-Host "`n>> $msg" -ForegroundColor Cyan }
function Write-Ok([string]$msg)   { Write-Host "   OK  $msg" -ForegroundColor Green }
function Write-Warn([string]$msg) { Write-Host "   WARN $msg" -ForegroundColor Yellow }
function Write-Fail([string]$msg) { Write-Host "   ERR  $msg" -ForegroundColor Red }

# ── detect architecture ────────────────────────────────────────────────────────

Write-Step "Detecting architecture..."
$arch = $env:PROCESSOR_ARCHITECTURE
switch ($arch) {
    "AMD64"  { $zipArch = "x64"   }
    "ARM64"  { $zipArch = "arm64" }
    default  {
        Write-Fail "Unsupported architecture: $arch"
        exit 1
    }
}
Write-Ok "Architecture: $arch -> using zip suffix '$zipArch'"

# ── check existing installation ────────────────────────────────────────────────

Write-Step "Checking existing Pando installation..."
$oldVersion = $null
$pandoPath  = $null

try {
    $pandoPath = (Get-Command pando -ErrorAction SilentlyContinue).Source
} catch { }

if ($pandoPath) {
    try {
        $rawVer = & pando --version 2>&1 | Select-Object -First 1
        # Strip "+dirty" suffix and leading "pando " or "v" prefix
        $oldVersion = ($rawVer -replace '\+dirty', '').Trim()
        Write-Ok "Found existing installation: $oldVersion  ($pandoPath)"
    } catch {
        Write-Warn "Found binary at '$pandoPath' but could not read its version."
    }
} else {
    Write-Ok "No existing Pando installation found."
}

# ── resolve target version ─────────────────────────────────────────────────────

Write-Step "Resolving target version..."
if ($Version -eq "") {
    try {
        $apiUrl = "https://api.github.com/repos/$REPO/releases/latest"
        $rel    = Invoke-RestMethod -Uri $apiUrl -Headers @{ "User-Agent" = "pando-installer" }
        $Version = $rel.tag_name
    } catch {
        Write-Fail "Could not fetch latest release from GitHub: $_"
        exit 1
    }
}
Write-Ok "Target version: $Version"

# Skip if already at the correct version
$cleanOld = $oldVersion -replace '^pando\s+', '' -replace '^v', ''
$cleanNew = $Version    -replace '^v', ''
if ($cleanOld -eq $cleanNew) {
    Write-Host "`nPando $Version is already installed. Nothing to do." -ForegroundColor Green
    exit 0
}

# ── download ───────────────────────────────────────────────────────────────────

$zipName    = "pando-windows-$zipArch.zip"
$downloadUrl = "https://github.com/$REPO/releases/download/$Version/$zipName"
$tmpDir      = Join-Path $env:TEMP "pando-install-$PID"
$zipPath     = Join-Path $tmpDir $zipName

Write-Step "Downloading $zipName..."
Write-Host "   URL: $downloadUrl"

New-Item -ItemType Directory -Force -Path $tmpDir | Out-Null
try {
    Invoke-WebRequest -Uri $downloadUrl -OutFile $zipPath -UseBasicParsing
} catch {
    Write-Fail "Download failed: $_"
    Remove-Item -Recurse -Force $tmpDir -ErrorAction SilentlyContinue
    exit 1
}
Write-Ok "Download complete."

# ── extract ────────────────────────────────────────────────────────────────────

Write-Step "Extracting archive..."
$extractDir = Join-Path $tmpDir "extracted"
Expand-Archive -Path $zipPath -DestinationPath $extractDir -Force

# Find the binary (archive contains architecture-specific file name)
$expectedExe = "pando-windows-$zipArch.exe"
$exeFile = Get-ChildItem -Recurse -Filter $expectedExe -Path $extractDir |
           Select-Object -First 1

if (-not $exeFile) {
    # Fallback for possible packaging layout/name variations
    $exeFile = Get-ChildItem -Recurse -Filter "pando*.exe" -Path $extractDir |
               Select-Object -First 1
}

if (-not $exeFile) {
    Write-Fail "Pando executable not found inside $zipName"
    Remove-Item -Recurse -Force $tmpDir -ErrorAction SilentlyContinue
    exit 1
}
Write-Ok "Found binary: $($exeFile.FullName)"

# ── install ────────────────────────────────────────────────────────────────────

Write-Step "Installing to $INSTALL_DIR..."
if (-not (Test-Path $INSTALL_DIR)) {
    New-Item -ItemType Directory -Force -Path $INSTALL_DIR | Out-Null
}

$destBin = Join-Path $INSTALL_DIR $BINARY

# If the binary is currently running it will be locked; warn but continue
try {
    Copy-Item -Force $exeFile.FullName $destBin
} catch {
    Write-Fail "Could not copy binary (is Pando running?): $_"
    Remove-Item -Recurse -Force $tmpDir -ErrorAction SilentlyContinue
    exit 1
}
Write-Ok "Binary installed: $destBin"

# ── add to user PATH if not already present ────────────────────────────────────

Write-Step "Checking PATH..."
$userPath = [Environment]::GetEnvironmentVariable("PATH", "User")
$pathEntries = $userPath -split ";" | Where-Object { $_ -ne "" }

if ($pathEntries -notcontains $INSTALL_DIR) {
    Write-Warn "$INSTALL_DIR is not in your PATH. Adding it now..."

    $newPath = ($pathEntries + $INSTALL_DIR) -join ";"
    [Environment]::SetEnvironmentVariable("PATH", $newPath, "User")

    # Also update the current session
    $env:PATH = "$env:PATH;$INSTALL_DIR"

    Write-Ok "Added $INSTALL_DIR to user PATH."
    Write-Warn "Restart your terminal (or run: `$env:PATH = [Environment]::GetEnvironmentVariable('PATH','User')`) to use pando."
} else {
    Write-Ok "$INSTALL_DIR is already in PATH."
}

# ── verify ─────────────────────────────────────────────────────────────────────

Write-Step "Verifying installation..."
try {
    $newRaw = & "$destBin" --version 2>&1 | Select-Object -First 1
    $newVersion = ($newRaw -replace '\+dirty', '').Trim()
    Write-Ok "Installed version: $newVersion"
} catch {
    Write-Warn "Installed but could not run pando --version: $_"
    $newVersion = $Version
}

# ── cleanup ────────────────────────────────────────────────────────────────────

Remove-Item -Recurse -Force $tmpDir -ErrorAction SilentlyContinue

# ── summary ────────────────────────────────────────────────────────────────────

Write-Host ""
Write-Host "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" -ForegroundColor DarkGray
Write-Host " Pando installed successfully!" -ForegroundColor Green
Write-Host "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" -ForegroundColor DarkGray
if ($oldVersion) {
    Write-Host "  Previous version : $oldVersion" -ForegroundColor DarkGray
} else {
    Write-Host "  Previous version : (none)" -ForegroundColor DarkGray
}
Write-Host "  New version      : $newVersion" -ForegroundColor White
Write-Host "  Install path     : $destBin" -ForegroundColor White
Write-Host "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" -ForegroundColor DarkGray
Write-Host ""
