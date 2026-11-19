<#
.SYNOPSIS
Checks that the pipes of the daemon refuse the current user.

.DESCRIPTION
The daemon serves the local API on a named pipe that grants access to its own
user only. Run this script as a second user of the machine: every pipe of Unison
must refuse the connection with access denied. A pipe that accepts the
connection is a failure, and the report of the run says who ran the script.

The script needs no sources and no Go: it connects to the pipe with .NET.

.PARAMETER TimeoutMs
Time to wait for one connection, 1000 milliseconds by default.

.EXAMPLE
runas /user:other "powershell -NoProfile -ExecutionPolicy Bypass -File scripts\check-pipe.ps1"
#>
[CmdletBinding()]
param(
    [int] $TimeoutMs = 1000
)

$ErrorActionPreference = 'Continue'
Set-StrictMode -Version Latest

$user = [System.Security.Principal.WindowsIdentity]::GetCurrent().Name
Write-Host "user  $user"

$pipes = @(Get-ChildItem '\\.\pipe\' -ErrorAction SilentlyContinue |
    Where-Object { $_.Name -like 'unison-*' })
if ($pipes.Count -eq 0) {
    Write-Host 'note  no Unison pipe is visible, start the daemon of another user first'
    exit 1
}

$failures = 0
foreach ($pipe in $pipes) {
    $stream = $null
    try {
        $stream = [System.IO.Pipes.NamedPipeClientStream]::new(
            '.', $pipe.Name, [System.IO.Pipes.PipeDirection]::InOut)
        $stream.Connect($TimeoutMs)
        Write-Host "FAIL  $($pipe.Name) accepts a connection from $user"
        $failures++
    }
    catch [System.UnauthorizedAccessException] {
        Write-Host "ok    $($pipe.Name) refuses $user"
    }
    catch {
        Write-Host "note  $($pipe.Name) $($_.Exception.GetType().Name): $($_.Exception.Message)"
    }
    finally {
        if ($stream) {
            $stream.Dispose()
        }
    }
}

Write-Host
if ($failures -gt 0) {
    Write-Host "check-pipe: $failures pipes are open to other users"
    exit 1
}
Write-Host 'check-pipe: every pipe of the daemon refuses this user'
