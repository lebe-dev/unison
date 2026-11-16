# Devices and pairing

Unison trusts devices, not accounts. A paired device is stored with its public keys, a display
name, its platform, a role and the time it was last reachable. Everything else follows from that
record: an unknown device is refused during the connection handshake, before any data is exchanged.

## Roles

The role is chosen when a device is paired.

- **Personal** - the devices of one person. They share the clipboard, the history, the cached
  payloads and the group discovery key. Personal devices are introduced to each other, so pairing a
  new device with one of them is enough.
- **Friends** - devices of other people. A Friends device is paired explicitly with every device it
  works with and never learns the group key. It can offer you files, and every offer waits for your
  decision.

The role is enforced for every message that arrives, so a Friends device cannot read the history even
when it asks. The role of a device does not change later: to move a device to the other role, revoke
it and pair it again.

## Pairing

Two devices are paired from a pairing window. The window is open for three minutes, can be
extended, and is the only time a device accepts a pairing connection: outside it the pairing
listener does not exist.

**Short code.** Both devices open the window, one of them picks the other in the list of devices
that are open for pairing, and both screens show the same six digits and four emoji. The exchange
starts with a fresh key exchange, and the code is computed from it, so a device between the two
gets a different code on each side and the users see the difference. A device is stored only after
both users confirmed the code.

**QR code.** The window of one device also shows a code for the camera of the other one:

- The code carries the device ID, the fingerprint of the pairing certificate, a random one time
  token, the addresses of the pairing listener and the role to propose.
- The scanning device connects to one of the addresses and accepts only the certificate with the
  fingerprint of the code, so a device between the two cannot take part.
- It sends the token with a request signed by its identity keys. The showing device checks the
  token, the signature, the role and its own records, and then asks its user.
- The scanning device does not ask: reading the code is its confirmation.

The token belongs to one window, is accepted once and is replaced after a completed pairing. A
refused or failed exchange gives it back, and a closed window accepts no connection at all. The code
carries the token, so it appears on the screen of the device that owns the window and is never
written to a log.

The command line prints the code of its window next to the short code flow, and the Add device
screen of the app shows the same code. See [Command line](cli.md).

On Android the code can be read by the Scan a code button of the Add screen or by the system
camera: the camera opens the `unison://pair` link in Unison, which starts the pairing at once.

The command line lists the devices that are open for pairing, one line per device, named by the
port of its window. A device has no name and no fingerprint before the pairing, and one device is
listed once even when it is reachable over IPv4 and IPv6.

Re-pairing a known device keeps the single record. A revoked device cannot be paired again.

## Renaming

The names in the device list are local to the device that shows them. Renaming a paired device
changes how it is called here and nowhere else; the device keeps its own name:

```sh
unison devices rename <id> "Desk phone"
unison devices rename self "Workstation"
```

The second form renames this device. The new name is stored, and the paired devices learn it in the
handshake, so it takes a moment and a session with that device: the others show the old name until
they connect. Setting the name of this device in the settings of the app does the same.

## Manual addresses

Discovery is the usual way to find a device, but it can be unavailable: some networks block
multicast, and a program started from a terminal may not be allowed to announce itself. A paired
device can then be reached by an address that you enter:

```sh
unison status                        # prints the addresses of this device
unison devices address <id> <host:port>
```

The address is stored with the device, tried before the discovered and cached ones, and the command
waits a few seconds for the connection; the daemon keeps trying after that. It survives a restart,
so two devices are reachable again after both sides restarted. An IPv6 address is written in
brackets, for example `[fe80::1%en0]:47123`.

## Group discovery

The Personal devices of one person recognise each other by a tag in their local network
announcement. The tag is derived from a discovery key that only the group knows, so other devices
see an anonymous service. The key is created at the first pairing and shared with every new
Personal device.

The tag changes every ten minutes, and a device accepts the tags of the previous and the next ten
minutes, so clock differences do not matter. The announcement carries no name, device ID or role.

## Searching for Friends devices

A Friends device never learns the group key, so it cannot use the announcement of the group. Two
Friends devices find each other only while both users turned the search on:

```sh
unison friends search
unison friends search --off
```

The search lasts fifteen minutes and ends by itself; asking for it again extends the running one. A
device that restarts keeps the search until it ends. The desktop app has the same switch in
Settings.

While the search runs, the device announces itself on a separate service and looks for the
announcements of the other devices. Both carry one tag per pair, derived from the rendezvous key
that only the two devices of the pair know. Nobody else can compute the tag or tell that the device
is online.

Turning the search off withdraws the announcement, stops the lookups and refuses new sessions with
Friends devices. A session that is already open stays until it ends.

The Android app has no switch for the search yet, so a Friends device cannot open a session with
Android: the pairing works and the device is listed, but nothing is exchanged.

## The confirmation flag

A device can hold the items that arrive instead of applying them. Such an item goes to the history,
waiting for you, and is applied only when you say so; see
[Clipboard and history](clipboard.md#holding-items-until-you-apply-them). The flag belongs to the
device that receives, so it covers everything that arrives there, not just the items of one device.

Turn it on for this device in the settings, or from another Personal device:

```sh
unison devices confirm self on
unison devices confirm <id> on
```

The second form sends a signed request to that device, which changes its own flag; the device has
to be online. The request also sets the local policy for that device: its items wait for you here,
while the device holds what it receives. The switch in the device card is that local policy.

A device reports its own flag when a session starts, so this device keeps a copy of what the other
one announced and shows it under the switch, as a line of its own. The line appears after the
device connects or reconnects, and only the device itself changes it.

## Revocation

Revoking a device is irreversible. The device stays in the record as revoked and is never trusted
again:

- every connection to it is closed immediately;
- its address cache, its introduction and its relay rendezvous key are dropped;
- the transport refuses it during the handshake, so it cannot connect even with the right keys;
- pairing it again is refused, so the same keys cannot be introduced a second time.

The revocation is signed by the device that revokes and carries the time it was signed. It spreads
over the sessions between Personal devices: a device that applies it cuts the revoked device off as
well. Any Personal device may revoke any device of the group. A revoked device disappears from the
device list of the others.

A device that is offline learns about the revocation only when it meets another Personal device.
Until then it does not know that it is cut off, but it also cannot connect: the other devices
refuse it.

Revoking a Personal device rotates the discovery key of the group. The new key spreads the same
way, and the revoked device no longer matches the tag of the group because it never receives the
new key. The previous key is still accepted for a transition period after a rotation, so devices
that have not received the new key yet can still be found during that time.

## Leaving the group

A device can revoke itself. This is the normal way to remove the app: the device sends the signed
revocation to the Personal devices it can reach and then forgets the group, that is its peers,
their introductions, the group key and the relay access. Devices that were offline keep their
records until they meet another Personal device, which brings them the revocation.

From the command line both cases are one command, and both ask for a confirmation first:

```sh
unison devices revoke <id>
unison devices revoke self
```
