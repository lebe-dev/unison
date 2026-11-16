# Trust and devices

Unison trusts devices, not accounts. A paired device is stored with its public keys, a display
name, its platform, a role and the time it was last reachable.

## Roles

- **Personal** - the devices of one person. They share the clipboard, the history, the blobs and
  the group discovery key. Personal devices are introduced to each other, so pairing a new device
  with one of them is enough.
- **Friends** - devices of other people. A Friends device is paired explicitly with every device
  it works with and never learns the group key.

The roles decide what a device may do; the core checks them for every message it accepts.

## File transfers

A device offers a file with `unison send`, and the receiver downloads it and verifies every chunk. A
Personal device receives the offer at once. An offer of a Friends device waits for a decision:

```sh
unison requests
unison requests accept <id>
unison requests decline <id>
```

Nothing of a pending request is written to the disk. An unanswered request expires after ten
minutes, and one device cannot pile up more than eight requests of another device.

## Pairing

Two devices are paired from a pairing window. The window is open for three minutes, can be
extended, and is the only time a device accepts a pairing connection: outside it the pairing
listener does not exist.

**Short code.** Both devices open the window, one of them picks the other in the list of devices
that are open for pairing, and both screens show the same six digits and four emoji. A device is
stored only after both users confirmed the code.

**QR code.** The window of one device also has a code for the camera of another one:

- The code carries the device ID, the fingerprint of the pairing certificate, a random one time
  token, the addresses of the pairing listener and the role to propose.
- The scanning device connects to one of the addresses and accepts only the certificate with the
  fingerprint of the code, so a device between the two cannot take part.
- It sends the token with a request signed by its identity keys. The showing device checks the
  token, the signature, the role and the trust store, and then asks its user.
- The scanning device does not ask: reading the code is its confirmation.

The token belongs to one window, is accepted once and is replaced after a completed pairing. A
refused or failed exchange gives it back, and a closed window accepts no connection at all. The
code carries the token, so it is shown on the screen of the device that owns the window and never
written to a log.

`unison pair` prints the code of the window in the terminal next to the short code flow, and the
Add device screen of the app shows the same code.

## Group discovery

The Personal devices of one person recognise each other by a tag in their local network
announcement. The tag is derived from a discovery key that only the group knows, so other devices
see an anonymous service. The key is created at the first pairing and shared with every new
Personal device.

## Revocation

Revoking a device is irreversible. The device stays in the trust store as a revoked record and is
never trusted again:

- every connection to it is closed immediately;
- its address cache, its introduction and its relay rendezvous key are dropped;
- the transport refuses it during the handshake, so it cannot connect even with the right keys;
- pairing it again is refused, so the same keys cannot be introduced a second time.

The revocation is signed by the device that revokes and carries the time it was signed. It spreads
over the sessions between Personal devices: a device that applies it cuts the revoked device off as
well. Any Personal device may revoke any device of the group.

A device that is offline learns about the revocation only when it meets another Personal device.
Until then it does not know that it is cut off, but it still cannot connect: the other devices
refuse it.

## Discovery key rotation

Revoking a Personal device rotates the discovery key of the group. The new key spreads over the
group the same way, and the revoked device no longer matches the tag of the group because it never
receives the new key.

The previous key is still accepted for a transition period after a rotation. Devices that have not
received the new key yet can still be found during that time, which is why a rotation does not
break the group while it is being distributed.

## Leaving the group

A device can revoke itself. This is the normal way to remove the app: the device sends the signed
revocation to the Personal devices it can reach and then forgets the group, that is its peers,
their introductions, the group key and the relay access. Devices that were offline keep their
records until they meet another Personal device, which brings them the revocation.

From the command line both cases are one command:

```sh
unison devices revoke <id>
unison devices revoke self
```
