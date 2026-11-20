# Relay

[Русский](relay.ru.md) | English

The relay connects devices that are not in one local network. It forwards opaque datagrams between
the members of a mailbox, so the two devices still run their own QUIC connection with mutual TLS and
the hybrid key exchange inside the relay datagrams. The relay sees neither keys nor plaintext, keeps
nothing on disk and learns no device identity.

Two parts live in the repository:

| Part | Path | Binary |
|------|------|--------|
| Client | `core/relay` | part of `unisond` and of the mobile core |
| Server | `internal/relay`, `cmd/unison-relay` | `unison-relay` |

The client is a library, not a process: the node opens one relay connection per device and carries
the mailboxes of all its pairs over it.

## Client

`core/relay` turns the relay into a virtual packet connection, so the transport does not know that
its packets travel through a relay.

```
peer session
  core/transport      QUIC over a net.PacketConn
    core/relay.Conn   one mailbox, one pair
      Dialer          one connection to the relay, many mailboxes
        quicCarrier   QUIC datagrams, primary
        wsCarrier     WebSocket over TLS, fallback
```

| Constant | Value |
|----------|-------|
| ALPN and subprotocol | `unison-relay/1` |
| WebSocket path | `/relay` |
| Default port | 443 |
| Dial timeout | 3 s |
| Reconnect delay | 1 s, doubling to 30 s |
| Queue per pair | 256 datagrams |
| Rollover window | 30 minutes before and after UTC midnight |
| Mailbox id | 16 bytes |

### Mailboxes

Both devices of a pair derive the same mailbox from their shared rendezvous key and the UTC day:

```
mailbox = BLAKE3(rendezvous_key || uint64_be(day))[:16]
```

A pair that paired directly agreed on the rendezvous key during pairing. Two Personal devices that
know each other through the group derive it from the group relay token and both device IDs, so the
device IDs alone do not reveal the mailbox. The id changes every day and reveals nothing about the
pair, so the relay cannot follow a device across days.

`Mailboxes` returns the current day first, then the next day within the last 30 minutes of the day
and the previous day within the first 30 minutes, so a peer whose clock is a little behind still
reaches the pair. A connection that takes updates follows the set at these moments in place; one to
an older relay is dropped at UTC midnight and registers the new set without backoff.

### Connection

`Dialer.Run` keeps one connection to the relay:

1. collect the mailboxes of every open pair, cut at the mailbox limit of the relay;
2. dial QUIC and fall back to WebSocket when the QUIC attempt fails within 3 s;
3. send `Register` with the token, the mailbox ids and the protocol version, read
   `RegisterResult` with the limits and the relay version;
4. serve datagrams until the carrier ends, and keep the mailbox set up to date with `Update`;
5. reconnect immediately for a failed update, a rollover or a mailbox set change on an older relay,
   and with the backoff otherwise.

`Register` and `RegisterResult` carry the protocol version from `core/protocol`. The client logs a
relay with a newer version, because the client has to be updated, and a relay with an older one,
because the relay has to be updated. A message that does not carry the field comes from a relay of
version `1`, which is the version that predates the field, so the client reads it as `1`. A
mismatch does not close the connection: the versioned fields stay additive, and both sides continue
with the parts they understand.

The limit is 8 until the first registration reports the real one. The set is cut so that the
current day mailbox of every pair comes before the extra mailboxes around midnight, and an earlier
pair before a later one. `Open` fails with `ErrMailboxLimit` for a pair that finds no room once
the set is full; the node logs it and tries again with the backoff, so the pair gets a mailbox when
another one closes.

`RegisterResult` reports in `mailbox_updates` whether the relay takes `Update`. When a pair opens or
closes, or the mailboxes of the day change, the client sends `Update` with a new sequence number and
the whole set. It travels like a datagram, so the client sends it again every 250 ms until
`UpdateResult` with that number arrives, and replaces the connection when no answer arrives within
5 s or the relay refuses the update. While an update travels, datagrams of both the old and the
new set are delivered. Without `mailbox_updates` the client replaces the connection for every
change, as relays before the update did require.

The WebSocket preference is sticky across reconnects until `NetworkChanged` resets it, so a network
that blocks UDP does not retry QUIC on every attempt.

`Conn` implements `net.PacketConn` for one pair. Its address has network `relay` and a name of the
form `relay:N`, which is how the node reports a peer path. The relay carries no peer address, so
local and remote addresses are the same. A write while the relay is down is dropped and succeeds,
like a packet on a broken link; QUIC retransmits it. The inbound queue is bounded and the excess is
dropped.

### Packet size

A relay datagram travels in one QUIC packet of the relay connection, which is 1280 bytes, so one
relay frame carries at most 1240 bytes. A frame adds 24 bytes of headers for an empty payload.

The inner QUIC connection has to fit into that, and a handshake needs 1200 bytes. `PacketSize`
returns the largest inner packet that fits; it returns zero when the datagrams are too small, and
the node then refuses the relay path and logs a warning. With the default `max_datagram_bytes` of
1400 the inner packet size is 1216, and path MTU discovery is off for that connection.

### When the node uses the relay

The local network always wins:

1. the node tries the manual, the discovered and the cached addresses;
2. when a peer stays offline for `RelayDelay` (3 s) and the network policy allows the relay, one
   goroutine per pair opens the relay path;
3. while the pair is online over the relay, the node keeps dialing the local addresses, with a
   delay that doubles from 1 s to 30 s and starts over after a network change;
4. as soon as a local connection is established, it takes over and the relay path is closed.

The relay client also stops when the group turns the relay off, and links that are on the relay path
are closed at that moment.

A session survives a relay outage for `RelayGrace` (15 s): QUIC retransmits the packets once the
relay is back. A longer outage ends the session, which is opened again when the relay returns.

The settings belong to the Personal group: an address and one access token, stored sealed and
spread with the group data. The address is a host and a port with an optional `https://` or `wss://`
scheme. Nothing returns the token after it is stored; clients only learn whether one is set.

## Server

`internal/relay` is the server. It has no state on disk and no identities: a client authenticates
with an access token, and a mailbox is a set of connections that share an id.

| Package file | Responsibility |
|--------------|----------------|
| `server.go` | the server, capacity, registration, forwarding, peer write loop |
| `quic.go` | the UDP listener, per connection registration, datagram loop |
| `ws.go` | the TLS listener, the WebSocket upgrade, pings |
| `registry.go` | the in-memory mailboxes |
| `limits.go` | token hashes, rate and quota |
| `frame.go` | framing and control frames |
| `http.go` | `/healthz`, `/readyz`, `/metrics` |
| `metrics.go` | counters and their Prometheus rendering |

### Registration

The first message of a connection is `Register` with the token, the mailbox ids and the protocol
version. The server compares the SHA-256 of the presented token with every configured hash in
constant time, checks that there are at most `max_mailboxes` ids of 16 bytes each, and joins the
mailboxes. It answers with `RegisterResult`, which carries the limits block and the protocol
version, then closes the registration stream or ends the WebSocket registration phase. A client
that does not send the version is read as version `1`. The server logs a client with a newer
version, because the client has to be updated, and a client with an older one, because the server
has to be updated; both continue.

| Status | Meaning |
|--------|---------|
| `OK` | registered |
| `UNAUTHORIZED` | unknown or empty token |
| `QUOTA_EXCEEDED` | the token used up its traffic |
| `INVALID_ARGUMENT` | malformed registration or mailbox id |
| `UNAVAILABLE` | the server is at its capacity |
| `DATAGRAMS_REQUIRED` | the transport cannot carry unreliable messages |
| `SHUTTING_DOWN` | the server stops |

On QUIC the codes map to application error codes `0x01` to `0x06`; on WebSocket they map to close
codes `4000 + status`.

### Updates

A registered connection replaces its mailbox set with `Update`, which carries a sequence number and
the whole new set. It arrives like a datagram, on the datagram path of QUIC or as a WebSocket
message, and counts against the rate and the quota of the token. The server checks the set like a
registration, leaves the mailboxes that are not in it, joins the new ones and answers with
`UpdateResult` and the same sequence number. A refused update, `INVALID_ARGUMENT` or `UNAVAILABLE`,
leaves the previous set and the connection in place. An update with the number of the last applied
one gets the same answer again, for a client whose answer was lost, and an older number is ignored.
`Config.NoMailboxUpdates` turns updates off; it has no flag and exists for the compatibility tests
of the clients.

### Mailboxes

A mailbox exists while at least one connection is a member of it. Four members fit into one
mailbox; a fifth join evicts the oldest member, so a reconnecting client wins over a stale
connection. `max_mailboxes_total` bounds the registry.

A datagram is forwarded byte for byte to every other member of its mailbox. The server checks that
the sender is a member, that the payload is not empty or oversized, and that the token has rate and
quota left. It never rewrites or inspects a payload.

| Drop reason | Meaning |
|-------------|---------|
| `malformed` | not a datagram frame, or a mailbox id that is not 16 bytes |
| `too_large` | a payload above `max_datagram_bytes` |
| `not_member` | the sender did not join the mailbox |
| `rate_limit` | the token is above its rate |
| `no_peer` | the sender is alone in the mailbox |
| `backpressure` | the target queue is full |
| `write` | the write to a target failed |

A datagram above the rate is dropped and the connection stays. A token that runs out of quota is
closed with `QUOTA_EXCEEDED`. Only accepted datagrams count as traffic.

### Limits

| Flag | Environment | Default |
|------|-------------|---------|
| `-quic-addr` | `UNISON_RELAY_QUIC_ADDR` | `:8443`, `:443` in the image |
| `-ws-addr` | `UNISON_RELAY_WS_ADDR` | `:8443`, `:443` in the image |
| `-http-addr` | `UNISON_RELAY_HTTP_ADDR` | `:8080` |
| `-tls-cert`, `-tls-key` | `UNISON_RELAY_TLS_CERT`, `UNISON_RELAY_TLS_KEY` | required |
| `-tokens` | `UNISON_RELAY_TOKENS` | none |
| `-tokens-file` | `UNISON_RELAY_TOKENS_FILE` | none |
| `-log-level` | `UNISON_RELAY_LOG_LEVEL` | `info` |
| `-max-mailboxes` | `UNISON_RELAY_MAX_MAILBOXES` | 32 per connection |
| `-max-datagram-bytes` | `UNISON_RELAY_MAX_DATAGRAM_BYTES` | 1400 |
| `-rate`, `-rate-burst` | `UNISON_RELAY_RATE`, `UNISON_RELAY_RATE_BURST` | 200 per second, burst 400 |
| `-quota-bytes`, `-quota-window` | `UNISON_RELAY_QUOTA_BYTES`, `UNISON_RELAY_QUOTA_WINDOW` | 1 GiB per 24 hours |
| `-max-connections` | `UNISON_RELAY_MAX_CONNECTIONS` | 4096 |
| `-max-mailboxes-total` | `UNISON_RELAY_MAX_MAILBOXES_TOTAL` | 100000 |
| `-ping-interval` | `UNISON_RELAY_PING_INTERVAL` | 30 s |
| `-register-timeout` | `UNISON_RELAY_REGISTER_TIMEOUT` | 10 s |

The environment supplies the defaults and the flags override them. Tokens come from a file with one
token per line, or from a comma separated variable; the file allows comments and empty lines. The
server keeps only the SHA-256 of every token.

### Health and metrics

`/healthz` reports the process, `/readyz` reports that new connections are accepted, and `/metrics`
renders the Prometheus text format over plain HTTP, for a sidecar scrape.

The metrics cover connections and registrations per transport, mailbox updates per outcome, live
mailboxes, datagrams and bytes per direction, uptime and dropped datagrams per reason. The labels never carry mailboxes, tokens,
addresses or device IDs. Logs are JSON on stderr and never carry payloads, tokens or mailbox ids.

### Lifecycle

`Start` loads the certificate, the key and the tokens, then binds QUIC, WebSocket and HTTP. `Close`
marks the server closed so `/readyz` fails, sends `Close` to every peer, leaves the mailboxes, and
closes the listeners and the sockets. `cmd/unison-relay` maps `SIGINT` and `SIGTERM` to that.

Only one instance can serve a group: the mailboxes live in memory, so a second replica keeps a
disjoint registry and a rolling restart drops every connection.

## Deployment

`deploy/relay` holds the image and a compose file. The image is built from `scratch`: two static
binaries, no shell, no package manager, and the process runs as uid 10001, so the root file system
can stay read-only.

| Port | Protocol | Purpose |
|------|----------|---------|
| 443 | UDP | QUIC, the primary transport |
| 443 | TCP | WebSocket over TLS, the fallback for networks that block UDP |
| 8080 | TCP | health and metrics, plain HTTP, loopback only |

QUIC is UDP and an ordinary HTTP ingress or reverse proxy cannot route it: publish the UDP port on
the host that runs the relay, or put a layer 4 load balancer in front of it. The WebSocket fallback
is ordinary TLS over TCP and passes an ingress that keeps upgrades enabled.

The compose file mounts `deploy/relay/secrets` read-only with `cert.pem`, `key.pem` and
`tokens.txt`, drops every capability and sets `no-new-privileges`. The files have to be readable by
uid 10001.

## Security properties

What the relay can observe: client addresses, TLS SNI and timing, the presented token, the mailbox
ids a connection joins, and datagram sizes and rates. It can tell that two connections share a
mailbox on one day and that devices share a token.

What it cannot observe: payload plaintext, keys, device identities, and the link between a pair
across days. The inner connection keeps its own TLS 1.3, hybrid key exchange, certificate
verification and session authentication proof, so the relay is a transport that sees ciphertext.

Without a mailbox id, a client cannot receive the traffic of a pair even with a valid token, and a
mailbox id is a 128 bit hash that cannot be enumerated.
