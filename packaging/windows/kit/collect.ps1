<#
.SYNOPSIS
Collects the logs and the state of the system into an archive.

.DESCRIPTION
Packs the daemon logs, the output of verify.ps1, the state of the system, the
startup entry, the firewall rule, the running processes and the status of the
command line client into an archive next to the kit, the path of which is the
last line of the output.

The keys, the database and the blobs of the daemon never go into the archive:
only the logs are read from the data directory.

.PARAMETER DataDir
Data directory of the daemon, %LOCALAPPDATA%\Unison by default.

.PARAMETER Since
Keeps only the log lines and the log files of the last minutes, the whole log
by default.

.EXAMPLE
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\collect.ps1

.EXAMPLE
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\collect.ps1 -Since 60
#>
[CmdletBinding()]
param(
    [string] $DataDir,
    [int] $Since = 0
)

$ErrorActionPreference = 'Continue'
Set-StrictMode -Version Latest

$kit = Split-Path -Parent $PSScriptRoot
$version = (Get-Content -Raw (Join-Path $kit 'VERSION')).Trim()
$computer = $env:COMPUTERNAME
if (-not $computer) {
    $computer = [Environment]::MachineName
}
$webView2Guid = '{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}'

if (-not $DataDir) {
    $DataDir = Join-Path $env:LOCALAPPDATA 'Unison'
}

function Get-RegistryValue([string] $path, [string] $name) {
    $item = Get-ItemProperty -Path $path -Name $name -ErrorAction SilentlyContinue
    if (-not $item) {
        return 'unknown'
    }
    return [string] $item.$name
}

function Get-ScreenScale {
    try {
        Add-Type -Namespace Unison -Name Dpi -MemberDefinition `
            '[DllImport("user32.dll")] public static extern uint GetDpiForSystem();' -ErrorAction Stop
        $dpi = [Unison.Dpi]::GetDpiForSystem()
        $percent = [int] [Math]::Round($dpi / 96 * 100)
        return "$percent% ($dpi dpi)"
    }
    catch {
        $logPixels = Get-RegistryValue 'HKCU:\Control Panel\Desktop' 'LogPixels'
        return "unknown (LogPixels $logPixels)"
    }
}

function Get-WebView2Version {
    $paths = @(
        "HKLM:\SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\$webView2Guid",
        "HKLM:\SOFTWARE\Microsoft\EdgeUpdate\Clients\$webView2Guid",
        "HKCU:\SOFTWARE\Microsoft\EdgeUpdate\Clients\$webView2Guid"
    )
    foreach ($path in $paths) {
        $found = Get-RegistryValue $path 'pv'
        if ($found -ne 'unknown') {
            return $found
        }
    }
    return 'not installed'
}

function Get-NetworkState {
    $profiles = @()
    try {
        $profiles = @(Get-NetConnectionProfile -ErrorAction Stop)
    }
    catch {
        return 'unknown'
    }
    if ($profiles.Count -eq 0) {
        return 'none'
    }
    $items = @($profiles | ForEach-Object { "$($_.InterfaceAlias)=$($_.NetworkCategory)" })
    return $items -join '; '
}

function Get-SystemText {
    $versionKey = 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion'
    $lines = @(
        ("ProductName: " + (Get-RegistryValue $versionKey 'ProductName')),
        ("DisplayVersion: " + (Get-RegistryValue $versionKey 'DisplayVersion')),
        ("CurrentBuild: " + (Get-RegistryValue $versionKey 'CurrentBuild')),
        ("UBR: " + (Get-RegistryValue $versionKey 'UBR')),
        ("Architecture: " + $env:PROCESSOR_ARCHITECTURE),
        ("Locale: " + (Get-Culture).Name),
        ("UI language: " + (Get-UICulture).Name),
        ("Screen scale: " + (Get-ScreenScale)),
        ("Network profiles: " + (Get-NetworkState)),
        ("WebView2: " + (Get-WebView2Version)),
        ("Kit version: $version"),
        ("Computer: $computer"),
        ("Data directory: $DataDir"),
        ('Report time: ' + (Get-Date -Format 'yyyy-MM-dd HH:mm:ss zzz'))
    )
    return ($lines -join [Environment]::NewLine) + [Environment]::NewLine
}

function Write-Text([string] $path, [string] $text) {
    $parent = Split-Path -Parent $path
    if (-not (Test-Path $parent)) {
        New-Item -ItemType Directory -Force -Path $parent | Out-Null
    }
    [System.IO.File]::WriteAllText($path, $text, (New-Object System.Text.UTF8Encoding($false)))
}

function Invoke-Capture([string] $path, [scriptblock] $command) {
    $text = @()
    try {
        $text = @(& $command 2>&1 | ForEach-Object { "$_" })
    }
    catch {
        $text = @("failed: $_")
    }
    Write-Text $path (($text -join [Environment]::NewLine) + [Environment]::NewLine)
}

function Copy-Logs([string] $from, [string] $to, [datetimeoffset] $cutoff) {
    if (-not (Test-Path $from)) {
        return 0
    }
    New-Item -ItemType Directory -Force -Path $to | Out-Null
    $count = 0
    foreach ($file in @(Get-ChildItem -Path $from -File -ErrorAction SilentlyContinue)) {
        if ($cutoff -ne [datetimeoffset]::MinValue -and $file.LastWriteTime -lt $cutoff.LocalDateTime) {
            continue
        }
        $lines = New-Object System.Collections.Generic.List[string]
        foreach ($line in [System.IO.File]::ReadLines($file.FullName)) {
            if ($cutoff -ne [datetimeoffset]::MinValue -and $line -match '"time":"([^"]+)"') {
                try {
                    if ([datetimeoffset]::Parse($Matches[1]) -lt $cutoff) {
                        continue
                    }
                }
                catch {
                }
            }
            $lines.Add($line)
        }
        Write-Text (Join-Path $to $file.Name) (($lines -join [Environment]::NewLine) + [Environment]::NewLine)
        $count++
    }
    return $count
}

Add-Type -AssemblyName System.IO.Compression.FileSystem

$work = Join-Path ([System.IO.Path]::GetTempPath()) ("unison-collect-" + [System.IO.Path]::GetRandomFileName())
New-Item -ItemType Directory -Force -Path $work | Out-Null

$cutoff = [datetimeoffset]::MinValue
if ($Since -gt 0) {
    $cutoff = [datetimeoffset]::Now.AddMinutes(-$Since)
}

Write-Text (Join-Path $work 'system.txt') (Get-SystemText)

$verify = Join-Path $PSScriptRoot 'verify.ps1'
if (Test-Path $verify) {
    $output = @()
    try {
        # A child process keeps the exit code of verify.ps1 away from this script.
        $output = @(& powershell.exe -NoProfile -ExecutionPolicy Bypass -File $verify 2>&1 |
            ForEach-Object { "$_" })
        $output += "verify exit code: $LASTEXITCODE"
    }
    catch {
        $output = @("failed: $_")
    }
    Write-Text (Join-Path $work 'verify.txt') (($output -join [Environment]::NewLine) + [Environment]::NewLine)
}
else {
    Write-Text (Join-Path $work 'verify.txt') 'verify.ps1 is missing next to collect.ps1'
}

$runPath = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
Invoke-Capture (Join-Path $work 'run.txt') {
    "value: " + (Get-RegistryValue $runPath 'Unison')
    reg query 'HKCU\Software\Microsoft\Windows\CurrentVersion\Run' /v Unison
}

Invoke-Capture (Join-Path $work 'install.txt') {
    "ProgramFiles: $env:ProgramFiles"
    foreach ($key in @(
            'HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall',
            'HKLM:\Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall',
            'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall')) {
        $entries = @(Get-ChildItem -Path $key -ErrorAction SilentlyContinue)
        foreach ($entry in $entries) {
            $item = Get-ItemProperty -Path $entry.PSPath -ErrorAction SilentlyContinue
            if (-not $item -or $item.PSObject.Properties.Name -notcontains 'DisplayName') {
                continue
            }
            if ($item.DisplayName -notlike 'Unison*') {
                continue
            }
            $location = 'unknown'
            if ($item.PSObject.Properties.Name -contains 'InstallLocation') {
                $location = [string] $item.InstallLocation
            }
            "$($item.DisplayName) in $location ($key)"
        }
    }
}

Invoke-Capture (Join-Path $work 'firewall.txt') {
    netsh advfirewall firewall show rule name="Unison daemon" verbose
}

Invoke-Capture (Join-Path $work 'processes.txt') {
    $processes = @(Get-CimInstance Win32_Process -Filter "Name = 'unisond.exe' OR Name = 'Unison.exe'" -ErrorAction SilentlyContinue)
    if ($processes.Count -eq 0) {
        'no unisond.exe or Unison.exe process'
    }
    foreach ($process in $processes) {
        "$($process.Name) pid $($process.ProcessId) started $($process.CreationDate)"
    }
}

$cli = Join-Path $kit 'bin/unison.exe'
if (Test-Path $cli) {
    Invoke-Capture (Join-Path $work 'cli.txt') { & $cli status }
}
else {
    Write-Text (Join-Path $work 'cli.txt') 'unison.exe is missing in bin'
}

$logs = Join-Path $DataDir 'logs'
$logDir = Join-Path $work 'logs'
$logCount = Copy-Logs $logs $logDir $cutoff
if ($logCount -eq 0) {
    Write-Text (Join-Path $work 'logs.txt') "no logs in $logs"
}

$archive = Join-Path (Split-Path -Parent $kit) "Unison-collect-$computer-$(Get-Date -Format 'yyyyMMdd-HHmm').zip"
if (Test-Path $archive) {
    Remove-Item -Force $archive
}
[System.IO.Compression.ZipFile]::CreateFromDirectory($work, $archive)
Remove-Item -Recurse -Force $work

Write-Host "collect: $logCount log files from $logs"
Write-Host $archive
