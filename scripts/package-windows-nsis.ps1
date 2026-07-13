#
# Package threev as a Windows .exe installer using NSIS.
#
# This script:
# - Ensures NSIS is installed via chocolatey
# - Builds a Windows amd64 binary via `wails build -platform windows/amd64 -nsis`
# - Renames the generated .exe to a predictable versioned name
#
# Code signing: The installer is completely unsigned.
# Windows SmartScreen will warn users about the unsigned executable.
#
# Prerequisites: Windows, wails CLI, NSIS (installed via choco)
# Invoke from repo root: .\scripts\package-windows-nsis.ps1
#

$ErrorActionPreference = "Stop"

$RepoRoot = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$BuildDir = Join-Path $RepoRoot "build"
$BinDir = Join-Path $BuildDir "bin"
$WailsJson = Join-Path $RepoRoot "wails.json"

# Extract version from wails.json
try {
  $WailsConfig = Get-Content $WailsJson | ConvertFrom-Json
  $Version = $WailsConfig.info.productVersion
  if (-not $Version) {
    Write-Error "productVersion not found in $WailsJson"
    exit 1
  }
} catch {
  Write-Error "Failed to read $WailsJson : $_"
  exit 1
}

$InstallerName = "threev-${Version}-windows-amd64-installer.exe"
$InstallerPath = Join-Path $BinDir $InstallerName

Write-Host "Installing NSIS if not present..."
try {
  # Check if nsis is already installed
  $NsisPath = Get-Command makensis -ErrorAction SilentlyContinue
  if (-not $NsisPath) {
    choco install nsis -y
    if ($LASTEXITCODE -ne 0) {
      Write-Error "choco install nsis failed with exit code $LASTEXITCODE"
      exit 1
    }

    # choco install doesn't update this already-running PowerShell process's
    # $env:PATH automatically - and in practice, the nsis/nsis.install
    # Chocolatey package doesn't register itself in the machine/user PATH
    # registry values at all either, only reporting "Deployed to
    # 'C:\Program Files (x86)\NSIS'" in its own install log. Append (never
    # replace/reassign wholesale) that fixed, well-known install location to
    # the CURRENT $env:PATH - replacing $env:PATH outright from the
    # Machine/User registry values would silently drop anything added to
    # this process's PATH by earlier steps that isn't itself registry-backed
    # (e.g. actions/setup-go's own GOPATH/bin addition, which is where the
    # `wails` CLI installed two steps above actually lives - confirmed by a
    # real CI failure where `wails` became "not recognized" right after such
    # a wholesale reassignment).
    $NsisInstallDir = "C:\Program Files (x86)\NSIS"
    if ((Test-Path $NsisInstallDir) -and ($env:PATH -notlike "*${NsisInstallDir}*")) {
      $env:PATH = "$env:PATH;$NsisInstallDir"
    }

    $NsisPath = Get-Command makensis -ErrorAction SilentlyContinue
    if (-not $NsisPath) {
      Write-Error "makensis still not found on PATH after choco install and appending $NsisInstallDir"
      exit 1
    }
  }
} catch {
  Write-Error "Failed to install NSIS: $_"
  exit 1
}

Write-Host "Building threev (Windows amd64)..."
& wails build -platform windows/amd64 -nsis -clean
if ($LASTEXITCODE -ne 0) {
  Write-Error "wails build failed with exit code $LASTEXITCODE"
  exit 1
}

# Find the generated installer exe in build/bin
# Wails names it as: threev-windows-amd64-installer.exe
# We'll glob for the most recent .exe to be safe
$ExeFiles = @(Get-ChildItem -Path $BinDir -Filter "*-installer.exe" -ErrorAction SilentlyContinue |
              Sort-Object LastWriteTime -Descending)

if ($ExeFiles.Count -eq 0) {
  Write-Error "No installer .exe found in $BinDir after build"
  exit 1
}

$GeneratedExe = $ExeFiles[0].FullName
Write-Host "Found generated installer: $GeneratedExe"

# Rename to predictable name if different
if ($GeneratedExe -ne $InstallerPath) {
  Move-Item -Path $GeneratedExe -Destination $InstallerPath -Force
  Write-Host "Renamed to: $InstallerPath"
}

if (-not (Test-Path $InstallerPath)) {
  Write-Error "Failed to create $InstallerPath"
  exit 1
}

Write-Host "Successfully created: $InstallerPath"
Write-Host "Version: $Version"
