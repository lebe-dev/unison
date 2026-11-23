Unison @VERSION@ for Windows - test kit
=======================================

This kit installs Unison on Windows 10 22H2 or Windows 11 (64-bit) and writes
the report of the tests. The files are built on a Mac and are not signed, so
Windows shows an unknown publisher and SmartScreen warns about them. That is
expected.

The kit does not need Go, Rust, Node or any source code. The daemon and the
command line client are in bin\, the installer is in setup\, the test binaries
are in tests\ and the scripts are in scripts\.

1. Unblock the files

Windows marks files that come from another computer. Unblock the archive before
you extract it:

    Unblock-File .\Unison-*-windows-kit-*.zip

Then open the archive with "Extract All" and unblock the extracted files. Run
these commands in the folder of the kit:

    Get-ChildItem -Recurse | Unblock-File

2. Install Unison

    .\setup\Unison-@VERSION@-setup.exe

Allow the elevation of the installer: it asks once and installs for all users
into C:\Program Files\Unison by default; the installer asks for the directory and
allows another one. SmartScreen may show "Windows protected your PC":
click "More info" and then "Run anyway". The installer adds the Windows Firewall
rule of the daemon and the startup entry of the current user.

3. Check the installation

    powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\verify.ps1

Every line starts with "ok" (passed), "FAIL" (failed) or "note" (needs you).
The script prints the checks that you have to do by hand at the end: log out
and in again, copy text between two devices, the firewall dialog and the
uninstall.

4. Run the tests

    powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\run-tests.ps1

The script runs one package at a time and writes report\<package>.txt,
report\system.txt and report\summary.txt. At the end it writes the archive
Unison-tests-<computer>-<date>.zip into the folder above the kit and prints its
path. A failed or timed out test makes the exit code 1, and the output of that
package is in its report file.

Windows Firewall may show a dialog for a test binary that listens on the
network. "Cancel" is fine: the tests use the loopback and work without the
permission.

The clipboard tests replace the content of the clipboard. The script asks before
it runs them; answer "n" to keep the clipboard, or use -SkipClipboard. To run
one part of the tests, pass the path of the package:

    powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\run-tests.ps1 -Package core\node -SkipClipboard

One package has 10 minutes by default; -Timeout takes a Go duration such as 2m
or 30s.

5. Collect the logs and the state of the system

    powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\collect.ps1

The script writes Unison-collect-<computer>-<date>.zip next to the kit. It holds
the daemon logs from %LOCALAPPDATA%\Unison\logs, the output of verify.ps1, the
state of the system, the startup entry, the firewall rule, the running processes
and the status of the client. The keys, the database and the blobs of the daemon
never go into the archive. -Since keeps the last minutes only:

    powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\collect.ps1 -Since 60

6. Return the archives

Copy Unison-tests-<computer>-<date>.zip and Unison-collect-<computer>-<date>.zip
back to the Mac.

7. Check the access of a second user (optional)

The daemon serves the local API on a pipe of its own user. A second user of the
machine must not be able to connect to it. Log in as that user, open the kit and
run:

    powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\check-pipe.ps1

The script prints "ok" for a pipe that refuses the user and "FAIL" for a pipe
that accepts the connection. Add the output to the report.

Manual runs, if a test needs a closer look:

    .\bin\unisond.exe --version
    .\bin\unisond.exe --data-dir .\data --log-stderr
    .\bin\unison.exe status

The daemon of bin\unisond.exe writes to the console. The daemon of the installed
app runs without a console window and writes to
%LOCALAPPDATA%\Unison\logs\unisond.log.
