# Command line

The command line client talks to the daemon through its local endpoint. It is built from the
sources with `just build` and lands in `bin/unison`; the app bundle of macOS contains the daemon
only. The client reaches the daemon of the app without any option, because both use the same
endpoint.

```
usage: unison [--endpoint path] [--json] <command> [arguments]
```

| Option | Meaning |
|--------|---------|
| `--endpoint path` | endpoint of the daemon, by default `$UNISON_LOCAL_API` or the socket of the platform |
| `--json` | print the answer as JSON instead of the readable form |
| `--version` | print the version and exit |

Write the options before the command. The flags of a command may stand before, after or between its
arguments, so `history fetch <id> -o <path>` and `history fetch -o <path> <id>` are the same.

The exit codes are 0 for a command that ran to the end, 1 for a failure and 2 when the daemon is
not reachable.

A device may be named by its name, a unique prefix of its name, a unique prefix of its fingerprint
or a unique prefix of its device ID. `unison status` and `unison devices` print the fingerprint.

## status

```sh
unison status
```

```
daemon      0.1.0 running
device      MacBook (W5MF-NWIT-4C6K-P7KR)
address     192.168.1.24:60190
network     online, unmetered
clipboard   on
peers       2 online, 0 offline
```

`address` is what another device enters with `devices address` when discovery does not work.
`network` lists the conditions that select the row of the network policy. `clipboard` is the pause
state, `friends` appears while the search for Friends devices runs, and `relay` appears when the
group uses a relay.

## devices

```sh
unison devices                       # the same as unison devices ls
unison devices address <id> <host:port> --wait 10s
unison devices confirm <id|self> on
unison devices confirm <id|self> off
unison devices revoke <id|self> [--yes]
```

```
FINGERPRINT          NAME     ROLE      PLATFORM  STATE
W5MF-NWIT-4C6K-P7KR  MacBook  personal  macos     online lan
QK3A-7T2M-VX5E-J9BD  Phone    personal  android   offline
```

`STATE` is `online lan`, `online relay` or `offline`.

`devices address` stores an address of a paired device and waits for the connection; `--wait 0s`
returns at once. `devices confirm self on` makes this device hold the items that arrive for you,
and `devices confirm <id> on` asks another device to do that; see
[Devices and pairing](devices.md#the-confirmation-flag). `devices revoke` asks for a confirmation
unless `--yes` is given, and `devices revoke self` leaves the group.

Renaming a device is not available yet: the daemon does not serve the call and answers with
`unimplemented`.

## pair

```sh
unison pair                          # propose the Personal role
unison pair --role friends
unison pair --qr <code>              # join the device that shows this code
```

The command opens the pairing window, prints the QR code of the window, lists the devices that are
open for pairing and asks which one to start with. The short code and four emoji of the exchange
appear on the screen, and the pairing completes after `y` on both devices. With `--qr` the command
joins the device that shows the code instead, without discovery.

## clip

```sh
unison clip get                      # the last item of this device
unison clip get --from <id>          # the last item of one device
unison clip set                      # standard input to the clipboard
unison clip set -
```

`clip get` prints the text as it is, without a newline. Text above 64 KiB is not carried by the
local API, so an error is reported instead. `clip set` announces the text to the online Personal
devices and reports its size.

## history

```sh
unison history                       # the same as unison history ls
unison history --type image --limit 5
unison history fetch <id> -o <path>
unison history apply <id>
```

```
ID                                    TYPE   FROM                 CREATED   SIZE   SUMMARY
01a1e3a1-f89f-79db-bda9-e2d9c0990d3a  text   W5MF-NWIT-4C6K-P7KR  12:13:25  15 B   hello from docs
```

The list holds 20 items by default and at most 200. `--type` takes `text`, `image` or `file`. An
item that came as metadata only is marked as not downloaded. `history fetch` downloads the payloads
of one item; with `-o` it writes them to a file, or to a directory when the item has several, and
without it the payloads stay in the cache. `history apply` writes the item to the clipboard of this
device, which is how a held item is taken. The reference is a unique prefix of the item id.

## send

```sh
unison send <path> --to <id>
unison send <path> --to <id> --to <other id>
unison send - --name report.txt --to <id>
```

`send` offers the file to the named devices. A Personal device receives it at once; a Friends
device answers with a request that waits for the user there, see `requests`. The command returns
after the offer, not after the transfer. `-` reads standard input, and `--name` sets the name of the
payload.

## transfers

```sh
unison transfers                     # the same as unison transfers ls
unison transfers cancel <id>
```

The file transfers of this device, newest first, with the direction, the device, the state, the
files and the bytes that moved so far. The state is `pending` while an offer waits for your answer,
then `offered`, `transferring`, and one of `done`, `declined`, `canceled` or `failed`. `transfers
cancel` stops one of them; the other side sees the cancellation as well. The reference is a unique
prefix of the transfer id.

## requests

```sh
unison requests                      # the same as unison requests ls
unison requests accept <id>
unison requests decline <id>
```

The incoming offers that wait for a decision, with the device, the files, the size and the time the
request expires. An unanswered request expires after ten minutes. The reference is a unique prefix
of the request id.

## pause

```sh
unison pause                         # print the state
unison pause 5m
unison pause 1h
unison pause on                      # until it is turned off
unison pause off
```

## friends

```sh
unison friends search
unison friends search --off
```

Turns the temporary search for Friends devices on for fifteen minutes or off. Both devices of a
pair have to turn it on, see [Devices and pairing](devices.md#searching-for-friends-devices).

## policy

```sh
unison policy
```

```
CONDITION   TEXT           IMAGE          FILES     RELAY
unmetered   auto 10.0 MiB  auto 10.0 MiB  metadata  yes
metered     auto 1.0 MiB   auto 1.0 MiB   metadata  yes
roaming     auto 1.0 MiB   metadata       metadata  no
data saver  auto 1.0 MiB   metadata       metadata  no
relay limit 1.0 MiB
```

The table is described under [Network policies](policy.md).

## settings

```sh
unison settings                    # all settings, with the history lifetimes
unison settings ttl                # the lifetimes only
unison settings ttl text 30d       # keep text for 30 days
unison settings ttl files 48h
unison settings ttl reset          # the three lifetimes back to the defaults
```

```
KIND    LIFETIME
text    7d
images  3d
files   1d
```

The lifetime belongs to the device that captured the item; see
[Clipboard and history](clipboard.md#history). Days and hours are accepted, from one second up to
365 days.

## relay

```sh
unison relay                         # the same as unison relay status
unison relay set relay.example.com:443 --token <token>
unison relay set relay.example.com:443 --token <token> --files-on-request
unison relay off
```

The address and the token belong to the Personal group and reach the other devices by themselves;
the token is never printed. `relay` shows the address, whether a token is stored, the state of the
connection and the rule for files. The rest is under [Relay](relay.md).

## JSON

With `--json` every command prints the answer of the daemon as protobuf JSON, with the default
values of the fields. The progress lines of `pair` go to stderr in that mode and the pairing events
go to stdout as one JSON object per line, so the output stays machine readable.

## The daemon

The daemon is built with `just build-unisond-host` and lands in `bin/unisond`; the macOS app runs
the one inside its bundle. A daemon started by hand takes:

| Flag | Meaning |
|------|---------|
| `--data-dir <path>` | the data directory of this instance |
| `--endpoint <path>` | the endpoint of the local API |
| `--clipboard system\|none` | watch the clipboard, or run without one |
| `--name <name>` | the name announced to the other devices |
| `--log-level debug\|info\|warn\|error` | how much to log, `info` by default |
| `--log-stderr` | copy the log to standard error as well |
| `--version` | print the version and exit |

