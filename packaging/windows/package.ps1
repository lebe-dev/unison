<#
.SYNOPSIS
Builds the Windows installer with the daemon inside it.

.DESCRIPTION
Builds unisond.exe for the host architecture, puts it next to the app as an
external binary of the bundle and builds the NSIS installer. The result is
copied to packaging/windows/build.

The installer installs the app for all users, starts the daemon at the login of
the user that runs it and allows the daemon in the Windows Firewall on the
private and domain networks.

Environment:
  UNISON_VERSION                  version of the artifacts, the VERSION file when unset
  UNISON_WINDOWS_CERT_THUMBPRINT  thumbprint of an Authenticode certificate
  UNISON_WINDOWS_TIMESTAMP_URL    timestamp server, http://timestamp.digicert.com by default

Without the thumbprint the installer and the daemon stay unsigned, so Windows
shows an unknown publisher and SmartScreen warns about the download.

.PARAMETER SkipDaemon
Reuses the daemon binary of the previous build.

.EXAMPLE
just package-windows
#>
[CmdletBinding()]
param(
    [switch] $SkipDaemon
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$root = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$version = if ($env:UNISON_VERSION) {
    $env:UNISON_VERSION.Trim()
} else {
    (Get-Content -Raw (Join-Path $root 'VERSION')).Trim()
}
$sidecars = Join-Path $root 'desktop/src-tauri/binaries'
$build = Join-Path $root 'packaging/windows/build'

if ($env:OS -ne 'Windows_NT') {
    throw 'package-windows: the installer can only be built on Windows'
}

foreach ($tool in 'go', 'rustc', 'cargo', 'yarn') {
    if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) {
        throw "package-windows: $tool is missing"
    }
}

# The sidecar of the bundle carries the target triple of the build.
$triple = (& rustc -vV | Select-String -Pattern '^host: ').ToString().Split(' ')[1].Trim()
$arch = switch -Wildcard ($triple) {
    'x86_64-*' { 'amd64' }
    'aarch64-*' { 'arm64' }
    default { throw "package-windows: unknown host architecture $triple" }
}

# ---------------------------------------------------------------------------
# Daemon
# ---------------------------------------------------------------------------

$sidecar = Join-Path $sidecars "unisond-$triple.exe"
if (-not $SkipDaemon) {
    New-Item -ItemType Directory -Force -Path $sidecars | Out-Null
    Write-Host "package-windows: building unisond for windows/$arch"
    # The GUI subsystem keeps the console window away when the startup entry
    # runs the daemon at login.
    $ldflags = "-s -w -H windowsgui -X main.version=$version"
    $ldflags = "$ldflags -X github.com/lebe-dev/unison/internal/observability.buildDSN=$env:SENTRY_DSN"
    $ldflags = "$ldflags -X github.com/lebe-dev/unison/internal/observability.buildEnvironment=release"
    $env:CGO_ENABLED = '0'
    Push-Location $root
    try {
        & go build -trimpath -ldflags $ldflags -o $sidecar ./cmd/unisond
        if ($LASTEXITCODE -ne 0) {
            throw 'package-windows: the daemon build failed'
        }
    }
    finally {
        Pop-Location
    }
}

if (-not (Test-Path $sidecar)) {
    throw "package-windows: $sidecar is missing"
}
Write-Host "package-windows: daemon $sidecar"

# ---------------------------------------------------------------------------
# App and installer
# ---------------------------------------------------------------------------

$config = @{ version = $version }
$thumbprint = $env:UNISON_WINDOWS_CERT_THUMBPRINT
if ($thumbprint) {
    $timestamp = $env:UNISON_WINDOWS_TIMESTAMP_URL
    if (-not $timestamp) {
        $timestamp = 'http://timestamp.digicert.com'
    }
    $config.bundle = @{
        windows = @{
            certificateThumbprint = $thumbprint
            timestampUrl          = $timestamp
        }
    }
}
else {
    Write-Warning 'package-windows: UNISON_WINDOWS_CERT_THUMBPRINT is not set, building unsigned'
}

$json = $config | ConvertTo-Json -Depth 5 -Compress
Write-Host 'package-windows: building the app and the installer'
Push-Location (Join-Path $root 'desktop')
try {
    & yarn tauri build --bundles nsis --config $json
    if ($LASTEXITCODE -ne 0) {
        throw 'package-windows: the build failed'
    }
}
finally {
    Pop-Location
}

# ---------------------------------------------------------------------------
# Artifact
# ---------------------------------------------------------------------------

$bundles = Join-Path $root 'desktop/src-tauri/target/release/bundle/nsis'
$installer = Get-ChildItem -Path $bundles -Filter '*-setup.exe' -ErrorAction SilentlyContinue |
    Sort-Object LastWriteTime | Select-Object -Last 1
if (-not $installer) {
    throw "package-windows: no installer in $bundles"
}

New-Item -ItemType Directory -Force -Path $build | Out-Null
$artifact = Join-Path $build "Unison-$version-setup.exe"
Copy-Item $installer.FullName $artifact -Force

$hash = (Get-FileHash -Algorithm SHA256 $artifact).Hash.ToLowerInvariant()
Write-Host "package-windows: $artifact"
Write-Host "package-windows: sha256 $hash"
if (-not $thumbprint) {
    Write-Host 'package-windows: the artifacts are unsigned, see the signing section of the documentation'
}
