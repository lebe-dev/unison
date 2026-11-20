Unison @VERSION@ for Linux - test kit
=====================================

This kit runs the tests and the checks of Unison on Ubuntu 24.04 LTS (64-bit).
It is built on a Mac, so the virtual machine needs no Go, Rust, Node or source
code. The daemon and the command line client are in bin/, the test binaries are
in tests/ and the scripts are in scripts/.

Run the scripts from a terminal of the desktop session of your user, not over
ssh: the tests of the clipboard need the display and the tests of the keyring
need the session bus of that user.

1. Copy the kit to the virtual machine

Copy the archive with scp or with the shared folder of the hypervisor:

    scp Unison-@VERSION@-linux-kit-*.tar.gz user@vm:~

Then extract it:

    mkdir ~/unison-kit
    tar xzf Unison-@VERSION@-linux-kit-*.tar.gz -C ~/unison-kit
    cd ~/unison-kit

The archive keeps the permissions of the scripts, so chmod is not needed. Check
it with:

    ls -l scripts

2. Check the kit

    scripts/verify.sh

Every line starts with "ok", "FAIL", "skipped" or "note". The checks of the
installed package print "skipped (no package installed)" while there is no
package on the machine.

3. Run the tests

    scripts/run-tests.sh

The script runs one test binary at a time and writes report/<package>.txt,
report/system.txt and report/summary.txt. At the end it writes the archive
Unison-tests-<machine>-<date>.tar.gz next to the kit and prints its path. A
failed or timed out package makes the exit code 1; the output of that package is
in its report file. A package without tests on Linux is not an error.

The clipboard tests replace the content of the clipboard and the keyring tests
write items into the login keyring. The script asks before it runs them. Answer
"n" to keep the clipboard and the keyring, or skip them with a flag:

    scripts/run-tests.sh --skip-clipboard --skip-keyring

To run one part of the tests, pass a part of the package path:

    scripts/run-tests.sh --package core/node

One package has 10 minutes by default; --timeout takes a Go duration such as 2m
or 30s. The external timeout is one minute longer, so a test that hangs still
prints its stack before it is killed.

4. Check the network between the virtual machine and the Mac

The address of the Mac is shown by "ipconfig getifaddr en0" on the Mac. Start
an echo on the Mac in the repository:

    just build-unisond-host
    bin/unisond --net-echo

Then run on the virtual machine:

    scripts/net-check.sh --peer <address of the Mac>

The script prints the addresses and the default route of the machine, pings the
Mac, sends a UDP probe to it and waits for a probe from it. It also reports the
mDNS listeners, avahi and ufw. The last line says in plain words whether UDP
between the machine and the Mac works. If it does not, the machine is on a NAT
or shared network of the hypervisor: switch it to bridged networking, or use
the manual peer address and the relay.

Bridged networking, one line per hypervisor:

    UTM:        Settings > Network > Network Mode: Bridged, Interface: Wi-Fi
    Parallels:  Devices > Network > Bridged Network > Wi-Fi
    VMware:     Settings > Network Adapter > Bridged
    VirtualBox: Settings > Network > Adapter 1 > Attached to: Bridged Adapter

5. Collect the logs and the state of the machine

    scripts/collect.sh

The script writes Unison-collect-<machine>-<date>.tar.gz next to the kit. It
holds the logs of the daemon, the journal of its user unit, the output of
verify.sh and net-check.sh, the state of the system and the status of the
client. The keys, the database and the blobs of the daemon never go into the
archive. The data directory is ~/.local/share/unison, or
$XDG_DATA_HOME/unison when that variable is set; --data-dir takes another one.
--since keeps the journal and the recent log files of the last minutes:

    scripts/collect.sh --since 60

6. Bring the archives back

Copy Unison-tests-<machine>-<date>.tar.gz and
Unison-collect-<machine>-<date>.tar.gz back to the Mac. If a test failed, the
report of that package is enough to start with.

Manual runs, when a test needs a closer look:

    bin/unisond --version
    bin/unisond --data-dir ./data --log-stderr
    bin/unison status

The daemon of the kit prints its log to the terminal with --log-stderr and also
writes it to <data directory>/logs.
