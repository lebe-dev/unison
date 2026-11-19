<#
.SYNOPSIS
Checks the Windows installer and the installed application.

.DESCRIPTION
Checks the installer of the build, the subsystem of the daemon and, when Unison
is installed, the installed files, the startup entry, the firewall rule and the
running daemon. The script works in the repository and in the test kit: the
version comes from the VERSION file next to the script and the installer from
the setup directory of the kit or from the build directory of the repository.

The line "ok" is a passed check, "FAIL" a failed one and "note" a check that
needs the user.

.PARAMETER Installer
Path to the installer, the artifact of the build by default.

.PARAMETER InstallDir
Installation directory, %ProgramFiles%\Unison by default.

.PARAMETER Version
Version of the build, the VERSION file next to the script by default.

.EXAMPLE
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\verify.ps1
#>
[CmdletBinding()]
param(
    [string] $Installer,
    [string] $InstallDir,
    [string] $Version
)

$ErrorActionPreference = 'Continue'
Set-StrictMode -Version Latest

# The script lives in packaging\windows of the repository or in scripts\ of the
# kit, so the VERSION file is one or two directories above it.
$root = $PSScriptRoot
for ($i = 0; $i -lt 3; $i++) {
    if (Test-Path (Join-Path $root 'VERSION')) {
        break
    }
    $root = Split-Path -Parent $root
}
if (-not $Version) {
    $Version = (Get-Content -Raw (Join-Path $root 'VERSION')).Trim()
}

# Names the installer and the app share; the NSIS hooks use the same ones.
$productName = 'Unison'
$runKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
$firewallRule = 'Unison daemon'

if (-not $Installer) {
    $setup = Join-Path $root 'setup'
    if (Test-Path $setup) {
        $found = @(Get-ChildItem -Path $setup -Filter '*-setup.exe' | Sort-Object LastWriteTime)
        if ($found.Count -gt 0) {
            $Installer = $found[$found.Count - 1].FullName
        }
    }
}
if (-not $Installer) {
    $Installer = Join-Path $root "packaging/windows/build/$productName-$Version-setup.exe"
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

function Get-RegistryValue([string] $path, [string] $name) {
    $item = Get-ItemProperty -Path $path -Name $name -ErrorAction SilentlyContinue
    if (-not $item) {
        return $null
    }
    return [string] $item.$name
}

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

# Operating system.
$versionKey = 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion'
$displayVersion = Get-RegistryValue $versionKey 'DisplayVersion'
$buildText = Get-RegistryValue $versionKey 'CurrentBuild'
$build = 0
if ($buildText) {
    $build = [int] $buildText
}
if ([Environment]::OSVersion.Version.Major -ge 10 -and $build -ge 10240) {
    Pass "Windows $displayVersion, build $build"
}
else {
    Fail "Windows $displayVersion, build $build, Unison needs Windows 10 or later"
}

$profiles = @()
try {
    $profiles = @(Get-NetConnectionProfile -ErrorAction Stop)
}
catch {
    Note 'the network profiles cannot be read here'
}
if ($profiles.Count -eq 0) {
    Note 'no network profile, connect to a network'
}
else {
    $private = @($profiles | Where-Object { $_.NetworkCategory -eq 'Private' })
    if ($private.Count -gt 0) {
        Pass "private network profile $($private[0].InterfaceAlias)"
    }
    else {
        $categories = @($profiles | ForEach-Object { "$($_.InterfaceAlias)=$($_.NetworkCategory)" })
        Note "no private network profile ($($categories -join ', ')), the firewall rule of the daemon does not apply"
    }
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

$sidecars = Join-Path $root 'desktop/src-tauri/binaries'
if (Test-Path $sidecars) {
    $hostTriple = $null
    if (Get-Command rustc -ErrorAction SilentlyContinue) {
        $hostTriple = (& rustc -vV | Select-String -Pattern '^host: ').ToString().Split(' ')[1].Trim()
    }
    if ($hostTriple) {
        $sidecar = Join-Path $sidecars "unisond-$hostTriple.exe"
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
}
else {
    Note 'the daemon sidecar is not built here, the check is skipped'
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

    $entry = Get-RegistryValue $runKey $productName
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
            $ruleProfiles = ($rules | ForEach-Object { $_.Profile }) -join ','
            if ($ruleProfiles -match 'Public') {
                Note "firewall rule also covers the public profile: $ruleProfiles"
            }
            else {
                Pass "firewall rule covers $ruleProfiles"
            }
        }
    }
    catch {
        Note "the firewall rules cannot be read here, check netsh advfirewall firewall show rule name=`"$firewallRule`""
    }
}

$pipes = @(Get-ChildItem '\\.\pipe\' -ErrorAction SilentlyContinue |
    Where-Object { $_.Name -like 'unison-*' })
if ($pipes.Count -gt 0) {
    Pass "daemon answers on $($pipes[0].Name)"
}
else {
    Note 'the daemon does not answer, start it or log in again'
}

# The daemon belongs to the session of the user that runs it.
$processes = @(Get-CimInstance Win32_Process -Filter "Name = 'unisond.exe'" -ErrorAction SilentlyContinue)
if ($processes.Count -eq 0) {
    Note 'unisond.exe does not run'
}
else {
    $current = [System.Security.Principal.WindowsIdentity]::GetCurrent().Name
    $owners = @()
    foreach ($process in $processes) {
        $owner = Invoke-CimMethod -InputObject $process -MethodName GetOwner -ErrorAction SilentlyContinue
        if ($owner -and $owner.User) {
            $owners += "$($owner.Domain)\$($owner.User)"
        }
    }
    if ($owners.Count -eq 0) {
        Note 'the owner of unisond.exe cannot be read here'
    }
    elseif ($owners -contains $current) {
        Pass "unisond.exe runs for $current"
    }
    else {
        Fail "unisond.exe runs for $($owners -join ', '), not for $current"
    }
}

Write-Host
if ($failures -gt 0) {
    Write-Host "verify: $failures checks failed"
    exit 1
}

@'
verify: check these by hand on the target machine:
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
