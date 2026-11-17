# Relay

The relay connects devices that are not in the same local network. It forwards opaque datagrams
between the members of a mailbox, so the devices still run their usual QUIC connection with mutual
TLS and the post-quantum key exchange inside it. The relay never sees keys or plaintext, and it
keeps nothing on disk.

The relay is optional and self-hosted: run it on a server with a public address, and the devices of
one Personal group share it.

## Settings

The relay access belongs to the Personal group: an address and one access token. Both are stored
once and spread to the other Personal devices with the group data, so every device of the group uses
the same relay.

The address is a host and a port, with an optional `https://` or `wss://` scheme and port 443 when
the port is missing:

    relay.example.com:443
    https://relay.example.com

The token is a secret: the daemon stores it sealed, never logs it and never returns it. Clients only
learn whether a token is stored. Turning the relay off clears both values for the group.

```sh
unison relay                                          # address, token, state, files rule
unison relay set relay.example.com:443 --token <token>
unison relay off
```

The desktop app and the Android app have the same settings in their relay section.

## Files over the relay

Files are a special case: by default a file stays in the history and travels over the relay only
when you download it, so a large file does not fill the connection of a relay by itself. The switch
for files in the relay section of the apps and `unison relay set --files-on-request` change that
rule. It is a setting of each device, not of the group.

## When the relay is used

The local network always has priority:

1. The device tries the known local addresses and the devices that discovery reports.
2. If a peer stays unreachable for a few seconds and the network policy allows relay traffic, the
   device opens the relay path.
3. As soon as the peer answers in the local network, the device opens a new connection over it and
   closes the relay one. The relay is never used for a peer that is reachable directly.

The network policy allows the relay per network condition, see [Network policies](policy.md). The
path of every peer is visible in the status: `lan` or `relay`.

## Mailboxes

Both devices of a pair derive the same mailbox from their shared rendezvous key and the current UTC
day:

    mailbox = BLAKE3(rendezvous_key || day)[:16]

Two devices that paired directly agree on the rendezvous key during pairing. Two Personal devices
that know each other through the group derive it from the relay token of the group and both device
IDs, so the device IDs alone do not reveal the mailboxes of a pair.

The id changes every day and reveals nothing about the pair, so the relay cannot follow a device
across days. Around midnight UTC a device registers the mailbox of the next day as well, so a pair
stays reachable across the boundary.

## Run a relay

The repository builds a container image with the server and a small health check program. There is
no shell in the image, the process runs as uid 10001, and the root file system can stay read-only.

```sh
just relay-image              # unison-relay:<version>
```

The image needs three files in `deploy/relay/secrets`: `cert.pem`, `key.pem` and `tokens.txt`. Then
either run the compose file or the container directly:

```sh
just relay-run
docker compose -f deploy/relay/docker-compose.yml up -d
```

The compose file and the operator notes are in `deploy/relay`.

| Port | Protocol | Purpose |
|------|----------|---------|
| 443 | UDP | QUIC, the primary transport |
| 443 | TCP | WebSocket over TLS, the fallback for networks that block UDP, path `/relay` |
| 8080 | TCP | `GET /healthz`, `GET /readyz` and `GET /metrics`, plain HTTP, loopback only |

Devices address the relay by URL, for example `https://relay.example.com`, and use the same port
for both transports so that a client can switch from QUIC to WebSocket without changing the
address.

Only one instance can serve a group: the mailboxes live in memory, so a second replica and a
rolling restart both drop the connections of the devices.

The flags and environment variables of the server are listed by:

```sh
docker run --rm unison-relay:<version> -h
```

### TLS

The relay terminates TLS itself, so the certificate and the key are mounted into the container. A
publicly trusted certificate is recommended. The files have to be readable by uid 10001. The relay
does not ask for client certificates: the devices authenticate each other inside the encrypted
datagrams.

### Access tokens

Every group has one token, and the server keeps only its SHA-256 in memory:

```sh
openssl rand -hex 32 > deploy/relay/secrets/tokens.txt
chmod 0444 deploy/relay/secrets/tokens.txt
```

The file holds one token per line; empty lines and lines starting with `#` are ignored.
`UNISON_RELAY_TOKENS` takes a comma separated list instead, which leaves the tokens in the
environment of the container.

A token allows 200 datagrams per second with a burst of 400, and 1 GiB of traffic per 24 hours.
Above the rate the relay drops datagrams, above the quota it closes the connection.

### Datagram size

A relay datagram travels in a single QUIC packet, so the relay caps its payload
(`max_datagram_bytes`). The QUIC connection between the devices runs inside those datagrams, and a
QUIC handshake needs packets of at least 1200 bytes, which is 1224 bytes of relay payload after the
frame headers.

Keep the default of 1400 bytes. A relay configured below 1224 bytes cannot carry a handshake, so
the devices refuse the relay path and log a warning.

### Network placement

QUIC is UDP, and an ordinary HTTP ingress or reverse proxy cannot route it: those accept TCP and
work on the request path. Publish the UDP port on the host that runs the relay, or put a layer 4
load balancer or a firewall rule in front of it that forwards UDP and TCP on the same port without
touching the packets. The WebSocket fallback is ordinary TLS over TCP and passes an ingress that
keeps WebSocket upgrades enabled.

### Health and metrics

```sh
curl -s http://127.0.0.1:8080/healthz
curl -s http://127.0.0.1:8080/metrics
```

`/healthz` reports that the process runs, `/readyz` that it accepts connections, and `/metrics`
serves the Prometheus text format: connections, registrations, live mailboxes, datagrams and bytes
per transport, uptime and dropped datagrams by reason. The labels carry no mailbox ids, tokens or
addresses.

## Reconnects

The client reconnects with exponential backoff after a failure, registers its mailboxes again and
reports the transport in use. A relay connection is also replaced at UTC midnight, when the
mailboxes of the day change.

A session between two devices survives a short relay outage: the packets are retransmitted once the
relay connection is back. When the relay stays away longer than the grace period, the session ends
and is opened again when the relay returns.
