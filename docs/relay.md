# Relay

[Русский](relay.ru.md) | English

The relay connects devices that are not in the same local network. It forwards opaque datagrams
between the members of a mailbox, so the devices still run their usual QUIC connection with mutual
TLS and the post-quantum key exchange inside it. The relay sees neither keys nor plaintext, learns
no device identity and keeps nothing on disk.

The relay is optional and self-hosted: run it on a server with a public address, and the devices of
one Personal group share it. The client is part of the daemon and of the mobile core, the server is
`unison-relay` in this repository.

## Settings

The relay access belongs to the Personal group: an address and one access token. Both are stored
sealed and spread to the other Personal devices with the group data, so every device of the group
uses the same relay.

The address is a host and a port, with an optional `https://` or `wss://` scheme. The port defaults
to 443, and the address takes no path, query, user or fragment:

    relay.example.com:443
    https://relay.example.com

The token is a secret: the daemon stores it, never logs it and never returns it. Clients only learn
whether a token is stored.

```sh
unison relay                                          # address, token, state, files rule
unison relay set relay.example.com:443 --token <token>
unison relay off
```

`unison relay` also reports the transport of a registered connection: QUIC or WebSocket. The
desktop app and the Android app have the same settings in their relay section. Turning the relay off
clears the address and the token for the group. The relay section refreshes the state of the
connection in the background; the address, the token and the files rule stay as typed until they
are saved or the relay is turned off.

### Check the connection

The relay client connects only when a peer needs the relay, so "waiting" alone does not tell a
working relay from a broken one. **Check connection** in the relay section of the desktop and
Android apps tries both transports right away with the address and the token in the form, before
they are saved; an empty token field uses the stored token. It registers a throwaway mailbox,
disconnects and stores nothing.

The result has one line per transport: the time to connect and register, or the reason it failed:

| Reason | What to look at |
|--------|-----------------|
| The host name does not resolve | The address, DNS |
| No answer in time | The host, the firewall; for QUIC, a network or a host that drops UDP |
| Connection refused | Nothing listens on the port |
| Certificate | The certificate is not publicly trusted or does not match the host |
| Not a relay | Another service on the address, or a proxy that does not forward `/relay` |
| Busy or unavailable | The relay is full or restarting, or the proxy gets no answer from it |
| Token not accepted | The token is missing from the tokens file of the relay |
| Quota | The token used up its traffic quota for the window |

A QUIC failure with a working WebSocket is a working relay: the devices fall back to WebSocket,
which is slower. The technical error is shown under each failure.

## Files over the relay

Files are a special case: by default a file stays in the history and travels over the relay only
when you download it, so a large file does not fill the connection of a relay by itself. The switch
for files in the relay section of the apps and `unison relay set --files-on-request` change that
rule; `--files-on-request=false` allows files over the relay. It is a setting of each device, not of
the group.

The relay limit of the network policy also applies: an item above 1 MiB is not fetched eagerly over
the relay, whatever the network condition. Files stay in the history either way; see
[Network policies](policy.md). The same rules hold a file offered with `unison send`: over the relay
it waits in `Requests` until you accept it.

## When the relay is used

The local network always has priority:

1. The device tries the saved addresses of the peer, including a manually entered one, and the
   addresses that discovery reports.
2. If the peer stays offline for 3 seconds and the network policy allows relay traffic on the
   current network, the device opens the relay path.
3. While the peer is reachable over the relay, the device keeps dialing the local addresses.
4. As soon as the peer answers in the local network, the new connection takes over and the relay
   path closes. The relay is never used for a peer that is reachable directly.

The network policy allows the relay on an unmetered and on a metered network, and blocks it on a
roaming network and when the data saver is on. The path of every peer is visible in the status:
`lan` or `relay`.

The device tries QUIC first. When UDP does not get through within 3 seconds, it falls back to
WebSocket over TLS on the same address. The fallback stays in use for the following connections
until the network changes, so a network that blocks UDP does not retry QUIC on every attempt.

## Mailboxes

Both devices of a pair derive the same mailbox from their shared rendezvous key and the current UTC
day, where `day` counts the days since the Unix epoch:

    mailbox = BLAKE3(rendezvous_key || uint64_be(day))[:16]

Two devices that paired directly agree on the rendezvous key during pairing. Two Personal devices
that know each other through the group derive it from the relay token of the group and both device
IDs, so the device IDs alone do not reveal the mailboxes of a pair.

The id changes every day and reveals nothing about the pair, so the relay cannot follow a device
across days. In the last 30 minutes before midnight UTC a device registers the mailbox of the next
day as well, and it keeps the mailbox of the previous day for 30 minutes after midnight, so a pair
stays reachable across the boundary even when the clocks of the two devices differ a little. A
relay of this version changes the mailboxes of a live connection; an older relay gets a new
connection with the new set.

## What the relay can see

The relay observes the client addresses, the TLS server name and timing, the token a connection
presents, the mailbox ids it joins, and the size and rate of the datagrams. It can tell that two
connections share a mailbox on one day and that devices share a token.

It cannot observe the payload, the keys, the device identities, or the link between a pair across
days. The inner connection keeps its own TLS 1.3, hybrid key exchange, certificate verification and
session authentication proof, so the relay is a transport that sees ciphertext. See
[Security](security.md).

## Run a relay

The repository builds a container image with the server and a small health check program. The image
is built from scratch: there is no shell and no package manager, the process runs as uid 10001, and
the root file system can stay read-only.

```sh
just relay-image              # unison-relay:<version>
```

The default compose file mounts three files from `deploy/relay/secrets`: `cert.pem`, `key.pem` and
`tokens.txt`. Then either run the compose file or the container directly:

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

Only one instance can serve a group: the mailboxes live in memory, so a second replica keeps a
disjoint set of mailboxes, and a rolling restart drops the connections of the devices.

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
environment of the container. The server reads the tokens at start, so restart the container after
changing them.

A token allows 200 datagrams per second with a burst of 400, and 1 GiB of traffic per 24-hour
window. The rate and the quota belong to the token, not to one connection, so all devices of the
group share them. Above the rate the relay drops datagrams and keeps the connection; above the
quota it closes the connection. Only datagrams that pass the checks count as traffic.

### Datagram size

A relay datagram travels in a single QUIC packet, so the relay caps its payload
(`max_datagram_bytes`). The QUIC connection between the devices runs inside those datagrams, and a
QUIC handshake needs packets of at least 1200 bytes, which is 1224 bytes of relay payload after the
frame headers.

Keep the default of 1400 bytes. It leaves a 1216-byte packet for the inner connection and turns its
path MTU discovery off. A value above 1240 bytes does not increase the inner packet, since one QUIC
packet of the relay connection holds no more than that. A relay whose limit is below 1224 bytes
cannot carry the handshake, so the devices refuse the relay path and log a warning.

### Network placement

QUIC is UDP, and an ordinary HTTP ingress or reverse proxy cannot route it: those accept TCP and
work on the request path. Publish the UDP port on the host that runs the relay, or put a layer 4
load balancer or a firewall rule in front of it that forwards UDP and TCP on the same port without
touching the packets. The WebSocket fallback is ordinary TLS over TCP and passes an ingress that
keeps WebSocket upgrades enabled. The relay pings an idle WebSocket every 30 seconds, so keep the
idle timeout of the ingress above that.

### Behind a reverse proxy

When UDP is not available, the relay can run behind a proxy that terminates TLS. The compose file
`deploy/relay/docker-compose.proxy.yml` publishes only the WebSocket port and the health port on the
loopback interface:

```sh
RELAY_IMAGE=unison-relay:<version> RELAY_WS_PORT=8443 \
    docker compose -f deploy/relay/docker-compose.proxy.yml up -d
```

The proxy forwards `/relay` to `https://127.0.0.1:8443` with the WebSocket upgrade headers, and the
devices use the domain of the proxy without a path. A self-signed certificate between the proxy and
the relay is enough, because the devices trust the certificate of the proxy.

### Health and metrics

```sh
curl -s http://127.0.0.1:8080/healthz
curl -s http://127.0.0.1:8080/readyz
curl -s http://127.0.0.1:8080/metrics
```

`/healthz` reports that the process runs, `/readyz` that it accepts connections, and `/metrics`
serves the Prometheus text format: connections, registrations, live mailboxes, datagrams and bytes
per transport, uptime and dropped datagrams by reason. The labels carry no mailbox ids, tokens or
addresses.

### Server settings

| Flag | Environment | Default |
|------|-------------|---------|
| `-quic-addr` | `UNISON_RELAY_QUIC_ADDR` | `:8443`, `:443` in the image |
| `-ws-addr` | `UNISON_RELAY_WS_ADDR` | `:8443`, `:443` in the image |
| `-http-addr` | `UNISON_RELAY_HTTP_ADDR` | `:8080` |
| `-tls-cert`, `-tls-key` | `UNISON_RELAY_TLS_CERT`, `UNISON_RELAY_TLS_KEY` | required |
| `-tokens`, `-tokens-file` | `UNISON_RELAY_TOKENS`, `UNISON_RELAY_TOKENS_FILE` | none |
| `-log-level` | `UNISON_RELAY_LOG_LEVEL` | `info` |
| `-max-datagram-bytes` | `UNISON_RELAY_MAX_DATAGRAM_BYTES` | 1400 |
| `-rate`, `-rate-burst` | `UNISON_RELAY_RATE`, `UNISON_RELAY_RATE_BURST` | 200 per second, burst 400 |
| `-quota-bytes`, `-quota-window` | `UNISON_RELAY_QUOTA_BYTES`, `UNISON_RELAY_QUOTA_WINDOW` | 1 GiB, 24h |
| `-max-mailboxes` | `UNISON_RELAY_MAX_MAILBOXES` | 32 per connection |
| `-max-mailboxes-total` | `UNISON_RELAY_MAX_MAILBOXES_TOTAL` | 100000 |
| `-max-connections` | `UNISON_RELAY_MAX_CONNECTIONS` | 4096 |
| `-ping-interval` | `UNISON_RELAY_PING_INTERVAL` | 30s |
| `-register-timeout` | `UNISON_RELAY_REGISTER_TIMEOUT` | 10s |

One connection carries the mailboxes of every pair that uses the relay, up to two per pair around
midnight. Raise `-max-mailboxes` when many devices of a group are away from the local network at
the same time; the largest value is 256. When a device needs more mailboxes than the relay allows,
the pairs it opened first keep theirs, and the device logs that the others wait for a free one.

The environment supplies the defaults, and the flags override them. The flags are listed by:

```sh
docker run --rm unison-relay:<version> -h
```

## Reconnects

The client reconnects with exponential backoff after a failure: 1 second, doubling to 30 seconds,
and registers its mailboxes again. When a peer starts or stops using the relay, or the mailboxes of
the day change at UTC midnight, the client changes the mailboxes of its live connection, so the
sessions with the other peers on the relay are not interrupted. A relay of an older version cannot
do that: the client then replaces the connection, at once for a change of the mailbox set and at
UTC midnight for the new day.

A session between two devices survives a short relay outage for 15 seconds: the packets are
retransmitted once the relay connection is back. When the relay stays away longer, the session ends
and is opened again when the relay returns.
