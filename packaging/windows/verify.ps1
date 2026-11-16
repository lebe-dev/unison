<#
.SYNOPSIS
Checks the Windows installer and the installed application.

.DESCRIPTION
Checks the artifacts of package-windows. When Unison is installed, it also
checks the installed files, the startup entry, the firewall rule and the running
daemon, and prints the checks that need the user.

.PARAMETER Installer
Path to the installer, the artifact of package-windows by default.

.PARAMETER InstallDir
Installation directory, %ProgramFiles%\Unison by default.

.EXAMPLE
just verify-windows
#>
[CmdletBinding()]
param(
    [string] $Installer,
    [string] $InstallDir
)

$ErrorActionPreference = 'Continue'
Set-StrictMode -Version Latest

$root = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$version = (Get-Content -Raw (Join-Path $root 'VERSION')).Trim()

# Names the installer and the app share; the NSIS hooks use the same ones.
$productName = 'Unison'
$runKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
$firewallRule = 'Unison daemon'

if (-not $Installer) {
    $Installer = Join-Path $root "packaging/windows/build/$productName-$version-setup.exe"
}
if (-not $InstallDir) {
    $InstallDir = Join-Path $env:ProgramFiles $productName
}

$failures = 0

function Pass([string] $message) { Write-Host "ok    $message" }
function Fail([string] $message) {
    Write-Host "FAIL  $message"
    $script:failures++
}
function Note([string] $message) { Write-Host "note  $message" }

function Get-PeSubsystem([string] $path) {
    # The optional header holds the subsystem 68 bytes after its start; 2 is the
    # GUI subsystem, which opens no console window.
    $bytes = [System.IO.File]::ReadAllBytes($path)
    $pe = [BitConverter]::ToInt32($bytes, 0x3C)
    $magic = [BitConverter]::ToUInt16($bytes, $pe + 24)
    if ($magic -ne 0x10B -and $magic -ne 0x20B) {
        return 0
    }
    return [BitConverter]::ToUInt16($bytes, $pe + 24 + 68)
}

# Artifacts.
if (Test-Path $Installer) {
    $size = [Math]::Round((Get-Item $Installer).Length / 1MB, 1)
    Pass "installer $Installer ($size MiB)"
    $signature = Get-AuthenticodeSignature $Installer
    if ($signature.Status -eq 'Valid') {
        Pass "installer signature $($signature.SignerCertificate.Subject)"
    }
    else {
        Note "installer signature $($signature.Status), unsigned or not trusted"
    }
}
else {
    Fail "installer $Installer"
}

$hostTriple = $null
if (Get-Command rustc -ErrorAction SilentlyContinue) {
    $hostTriple = (& rustc -vV | Select-String -Pattern '^host: ').ToString().Split(' ')[1].Trim()
}
if ($hostTriple) {
    $sidecar = Join-Path $root "desktop/src-tauri/binaries/unisond-$hostTriple.exe"
    if (Test-Path $sidecar) {
        $subsystem = Get-PeSubsystem $sidecar
        if ($subsystem -eq 2) {
            Pass 'daemon uses the GUI subsystem'
        }
        else {
            Fail "daemon subsystem is $subsystem, expected 2"
        }
    }
    else {
        Fail "daemon sidecar $sidecar"
    }
}
else {
    Note 'rustc is missing, the check of the daemon sidecar is skipped'
}

# Installed state.
$app = Join-Path $InstallDir "$productName.exe"
$daemon = Join-Path $InstallDir 'unisond.exe'
if (-not (Test-Path $app)) {
    Note "the app is not installed, run $Installer first"
}
else {
    Pass "installation directory $InstallDir"
    if (Test-Path $daemon) {
        Pass 'daemon unisond.exe in the installation directory'
    }
    else {
        Fail 'daemon unisond.exe in the installation directory'
    }

    $entry = $null
    $values = Get-ItemProperty -Path $runKey -ErrorAction SilentlyContinue
    if ($values -and $values.PSObject.Properties.Name -contains $productName) {
        $entry = $values.$productName
    }
    $expected = "`"$daemon`""
    if ($entry -eq $expected) {
        Pass "startup entry $entry"
    }
    else {
        Fail "startup entry is $entry, expected $expected"
    }

    try {
        $rules = @(Get-NetFirewallRule -DisplayName $firewallRule -ErrorAction Stop |
            Where-Object { $_.Direction -eq 'Inbound' -and $_.Enabled -eq 'True' })
        if ($rules.Count -eq 0) {
            Fail "firewall rule $firewallRule"
        }
        else {
            $programs = @($rules | Get-NetFirewallApplicationFilter | ForEach-Object { $_.Program })
            if ($programs -icontains $daemon) {
                Pass "firewall rule $firewallRule for $daemon"
            }
            else {
                Fail "firewall rule points at $($programs -join ', '), expected $daemon"
            }
            $profiles = ($rules | ForEach-Object { $_.Profile }) -join ','
            if ($profiles -match 'Public') {
                Note "firewall rule also covers the public profile: $profiles"
            }
            else {
                Pass "firewall rule covers $profiles"
            }
        }
    }
    catch {
        Note "the firewall rules cannot be read here, check netsh advfirewall firewall show rule name=`"$firewallRule`""
    }

    $pipes = @(Get-ChildItem '\\.\pipe\' -ErrorAction SilentlyContinue |
        Where-Object { $_.Name -like 'unison-*' })
    if ($pipes.Count -gt 0) {
        Pass "daemon answers on $($pipes[0].Name)"
    }
    else {
        Note 'the daemon does not answer, start it or log in again'
    }
}

Write-Host
if ($failures -gt 0) {
    Write-Host "verify-windows: $failures checks failed"
    exit 1
}

@'
verify-windows: check these by hand on the target machine:
  1. install the setup file and allow the elevation of the installer
  2. log out and in again: unisond.exe runs without the app, no console window
     appears and the paired devices are online
  3. copy text on one device and paste it on the other one
  4. work on a private network: the Windows Firewall shows no dialog for
     unisond.exe, and the public profile stays blocked
  5. uninstall from Settings > Apps: the installation directory, the startup
     entry and the firewall rule are gone
  6. log out and in again after the uninstall: nothing of Unison starts
'@
