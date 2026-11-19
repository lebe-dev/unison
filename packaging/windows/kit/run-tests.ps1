<#
.SYNOPSIS
Runs the Go test binaries of the kit and writes the report archive.

.DESCRIPTION
Runs every package of tests below tests\, one binary at a time, and writes the
output to report\<package>.txt. The report also holds the state of the system
and a summary, and it is packed into an archive next to the kit, the path of
which is the last line of the output.

The clipboard tests replace the content of the clipboard, so the script asks
before it runs them.

.PARAMETER Package
Runs only the packages whose path contains this text.

.PARAMETER Timeout
Timeout of one package, 10m by default, in the Go duration format.

.PARAMETER SkipClipboard
Skips the clipboard tests without asking.

.EXAMPLE
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\run-tests.ps1

.EXAMPLE
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\run-tests.ps1 -Package core `
    -Timeout 2m -SkipClipboard
#>
[CmdletBinding()]
param(
    [string] $Package,
    [string] $Timeout = '10m',
    [switch] $SkipClipboard
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$kit = Split-Path -Parent $PSScriptRoot
$testsDir = Join-Path $kit 'tests'
$reportDir = Join-Path $kit 'report'
$version = (Get-Content -Raw (Join-Path $kit 'VERSION')).Trim()
$computer = $env:COMPUTERNAME
if (-not $computer) {
    $computer = [Environment]::MachineName
}
$webView2Guid = '{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}'

function ConvertTo-Milliseconds([string] $value) {
    if ($value -notmatch '^([0-9]+)(ms|s|m|h)$') {
        throw "run-tests: the timeout $value is not a Go duration like 10m"
    }
    $count = [int] $Matches[1]
    $unit = $Matches[2]
    switch ($unit) {
        'ms' { return $count }
        's' { return $count * 1000 }
        'm' { return $count * 60000 }
        'h' { return $count * 3600000 }
    }
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

function Get-PackageName([string] $exe) {
    $relative = $exe.Substring($testsDir.Length + 1)
    $directory = Split-Path -Parent $relative
    return $directory.Replace('\', '/')
}

function Invoke-TestBinary([string] $path, [string] $duration, [int] $timeoutMs) {
    $startInfo = New-Object System.Diagnostics.ProcessStartInfo
    $startInfo.FileName = $path
    $startInfo.Arguments = "-test.v -test.count=1 -test.timeout $duration"
    $startInfo.WorkingDirectory = Split-Path -Parent $path
    $startInfo.UseShellExecute = $false
    $startInfo.CreateNoWindow = $true
    $startInfo.RedirectStandardOutput = $true
    $startInfo.RedirectStandardError = $true

    $process = [System.Diagnostics.Process]::Start($startInfo)
    $out = $process.StandardOutput.ReadToEndAsync()
    $err = $process.StandardError.ReadToEndAsync()

    # The binary enforces the Go timeout itself; the extra minute lets it print
    # the panic and the stack before the process is ended here.
    $killed = $false
    $limit = $timeoutMs + 60000
    if (-not $process.WaitForExit($limit)) {
        $killed = $true
        try { $process.Kill() } catch { }
    }
    $process.WaitForExit()
    $output = $out.Result + $err.Result
    $code = $process.ExitCode
    $process.Dispose()

    return @{
        Output    = $output
        ExitCode  = $code
        TimedOut  = ($killed -or $output -match 'panic: test timed out')
    }
}

Add-Type -AssemblyName System.IO.Compression.FileSystem

$timeoutMs = ConvertTo-Milliseconds $Timeout

if (-not (Test-Path $testsDir)) {
    throw "run-tests: $testsDir is missing"
}

$binaries = @(Get-ChildItem -Path $testsDir -Recurse -Filter '*.test.exe' | Sort-Object FullName)
if ($Package) {
    $binaries = @($binaries | Where-Object { (Get-PackageName $_.FullName) -like "*$Package*" })
}
if ($binaries.Count -eq 0) {
    throw "run-tests: no test binaries match $Package"
}

$clipboardName = 'platform/windows/clipboard'
$runClipboard = $true
if ($SkipClipboard) {
    $runClipboard = $false
}
else {
    $clipboardBinaries = @($binaries | Where-Object { (Get-PackageName $_.FullName) -eq $clipboardName })
    if ($clipboardBinaries.Count -gt 0) {
        $answer = Read-Host 'The clipboard tests replace the content of the clipboard. Run them? [y/N]'
        $runClipboard = $answer -match '^(y|yes)$'
    }
}

if (Test-Path $reportDir) {
    Remove-Item -Recurse -Force $reportDir
}
New-Item -ItemType Directory -Force -Path $reportDir | Out-Null
Write-Text (Join-Path $reportDir 'system.txt') (Get-SystemText)

$summary = New-Object System.Collections.Generic.List[string]
$failed = 0
$timedOut = 0
$passed = 0
$skipped = 0

foreach ($binary in $binaries) {
    $name = Get-PackageName $binary.FullName
    if ($name -eq $clipboardName -and -not $runClipboard) {
        Write-Host "skipped $name"
        $summary.Add(('{0,-8} {1}' -f 'skipped', $name))
        $skipped++
        continue
    }

    Write-Host "running $name"
    $started = Get-Date
    $result = Invoke-TestBinary $binary.FullName $Timeout $timeoutMs
    $elapsed = (Get-Date) - $started
    $seconds = [Math]::Round($elapsed.TotalSeconds, 1)

    Write-Text (Join-Path $reportDir "$name.txt") $result.Output

    if ($result.TimedOut) {
        $status = 'timeout'
        $timedOut++
    }
    elseif ($result.ExitCode -ne 0) {
        $status = 'FAIL'
        $failed++
    }
    else {
        $status = 'ok'
        $passed++
    }

    Write-Host "  $status after $seconds s"
    $summary.Add(('{0,-8} {1} ({2} s, exit code {3})' -f $status, $name, $seconds, $result.ExitCode))
}

$total = $binaries.Count
$summary.Add('')
$summary.Add("total $total, ok $passed, FAIL $failed, timeout $timedOut, skipped $skipped")
Write-Text (Join-Path $reportDir 'summary.txt') (($summary -join [Environment]::NewLine) + [Environment]::NewLine)

$archive = Join-Path (Split-Path -Parent $kit) "Unison-tests-$computer-$(Get-Date -Format 'yyyyMMdd-HHmm').zip"
if (Test-Path $archive) {
    Remove-Item -Force $archive
}
[System.IO.Compression.ZipFile]::CreateFromDirectory($reportDir, $archive)

Write-Host ''
Write-Host "run-tests: ok $passed, FAIL $failed, timeout $timedOut, skipped $skipped of $total"
Write-Host $archive
if ($failed + $timedOut -gt 0) {
    exit 1
}
