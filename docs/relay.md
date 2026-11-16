# Relay

The relay connects devices that are not in the same local network. It forwards opaque datagrams
between the members of a mailbox, so the devices still run their usual QUIC connection with mutual
TLS and the post-quantum key exchange inside it. The relay never sees keys or plaintext.

## Settings

The relay access belongs to the Personal group: an address and one access token. Both are stored
once and spread to the other Personal devices with the group data, so every device of the group uses
the same relay.

The address is a host and a port, with an optional `https://` or `wss://` scheme:

    relay.example.com:443
    https://relay.example.com

The token is a secret: the daemon stores it sealed, never logs it and never returns it. Clients only
learn whether a token is stored. Turning the relay off clears both values for the group.

Ways to set it:

    unison relay set relay.example.com:443 --token <token> [--files-on-request]
    unison relay off
    unison relay                  # address, token presence and the transport in use

The desktop settings screen and the Android settings screen do the same over the local API
(`GetRelay`, `SetRelay`, `RelayOff`). A change made on one device reaches the others over the
sessions they already have, so a device that is only reachable through the relay picks the change up
when it meets the group again.

Files are a special case: by default a file travels over the relay only when the user asks for it,
so file items stay in the history until they are downloaded. The rule is the
`policy.relay.files_on_request` setting; the relay section of the settings screens and
`relay set --files-on-request` change it.

## When the relay is used

The local network always has priority:

1. The device tries the known local addresses and the devices that discovery reports.
2. If a peer stays unreachable for a few seconds and the network policy allows relay traffic, the
   device opens the relay path.
3. As soon as the peer answers in the local network, the device opens a new connection over it and
   closes the relay one. The relay is never used for a peer that is reachable directly.

The path of every peer is visible in the device status: `lan` or `relay`.

## Mailboxes

Both devices of a pair derive the same mailbox from their shared rendezvous key and the current
UTC day:

    mailbox = BLAKE3(rendezvous_key || day)[:16]

The id changes every day and reveals nothing about the pair, so the relay cannot follow a device
across days. Around midnight UTC a device registers the mailbox of the next day as well, so a pair
stays reachable across the boundary.

## Transports

The client connects over QUIC on UDP. If UDP does not get through within the dial timeout, it falls
back to WebSocket over TLS on the same port and stays on it until the network changes. The relay
must serve both transports on one port.

## Datagram size

A relay datagram travels in a single QUIC packet, so the relay caps its payload
(`max_datagram_bytes`). The QUIC connection between the devices runs inside those datagrams, and a
QUIC handshake needs packets of at least 1200 bytes, which is 1224 bytes of relay payload after the
frame headers.

A relay configured with a payload below 1224 bytes cannot carry a QUIC handshake, so devices would
refuse the relay path and log a warning. The default is 1400 bytes: the client then uses packets of
1216 bytes and keeps path MTU discovery off, so nothing exceeds the relay limit.

## Reconnects

The client reconnects with exponential backoff after a failure, registers its mailboxes again and
reports the transport in use. A relay connection is also replaced at UTC midnight, when the
mailboxes of the day change.

A session between two devices survives a short relay outage: the packets are retransmitted once the
relay connection is back. When the relay stays away longer than the grace period, the session ends
and is opened again when the relay returns.
