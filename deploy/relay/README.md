# Relay

Container image and compose file for `unison-relay`, the self-hosted relay that forwards
encrypted datagrams between paired devices when they cannot reach each other over the local
network. The relay keeps mailboxes in memory and writes nothing to disk.

The image holds two static binaries: the server and a small probe that the container health check
runs. There is no shell and no package manager, the process runs as uid/gid 10001, and the root
file system can stay read-only.

## Build

Build from the repository root, because the build needs the Go sources and the proto packages:

```sh
just relay-image              # unison-relay:<version>, version from VERSION
```

The recipe is a wrapper around:

```sh
docker build --build-arg VERSION="$(cat VERSION)" \
    -f deploy/relay/Dockerfile -t unison-relay:"$(cat VERSION)" .
```

The version is compiled into the binary, printed by `unison-relay -version` and reported in the
`unison_relay_info` metric. For a server of another architecture build the matching platform:

```sh
docker buildx build --platform linux/amd64 --build-arg VERSION="$(cat VERSION)" \
    -f deploy/relay/Dockerfile -t unison-relay:"$(cat VERSION)" --load .
```

## Run

The compose file expects three files in `deploy/relay/secrets`: `cert.pem`, `key.pem` and
`tokens.txt` (see below). Then:

```sh
just relay-run                 # build and run in the foreground, stop with Ctrl-C
```

or in the background with the same file:

```sh
docker compose -f deploy/relay/docker-compose.yml up -d
docker compose -f deploy/relay/docker-compose.yml logs -f
docker compose -f deploy/relay/docker-compose.yml down
```

Compose reads these variables from the environment:

| Variable | Default | Meaning |
|----------|---------|---------|
| `RELAY_IMAGE` | `unison-relay:local` | Image name and tag |
| `RELAY_VERSION` | `dev` | Version passed to the build |
| `RELAY_PORT` | `443` | Host port for UDP and TCP |
| `RELAY_HTTP_PORT` | `8080` | Host port of health and metrics, bound to `127.0.0.1` |
| `RELAY_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |
| `RELAY_TOKENS` | empty | Tokens as a comma separated list instead of a file |

Without compose:

```sh
docker run -d --name unison-relay --restart unless-stopped \
    -p 443:443/udp -p 443:443/tcp -p 127.0.0.1:8080:8080 \
    -v "$PWD/deploy/relay/secrets:/etc/unison-relay:ro" \
    -e UNISON_RELAY_TLS_CERT=/etc/unison-relay/cert.pem \
    -e UNISON_RELAY_TLS_KEY=/etc/unison-relay/key.pem \
    -e UNISON_RELAY_TOKENS_FILE=/etc/unison-relay/tokens.txt \
    --read-only --cap-drop ALL --security-opt no-new-privileges \
    unison-relay:"$(cat VERSION)"
```

Change the host side of `-p` when another service already listens on 443. The container ports stay
the same.

## Ports

| Port | Protocol | Purpose |
|------|----------|---------|
| 443 | UDP | QUIC transport, the primary path. ALPN `unison-relay/1` |
| 443 | TCP | WebSocket over TLS, the fallback for networks that block or throttle UDP. Path `/relay`, subprotocol `unison-relay/1` |
| 8080 | TCP | `GET /healthz`, `GET /readyz` and `GET /metrics`, plain HTTP |

Devices reach the relay by URL. Use the same port for both transports so a client can switch from
QUIC to WebSocket without changing the address, for example `https://relay.example.com` on 443.

Everything the relay can do is configured with flags and environment variables; the flags are
printed by `docker run --rm unison-relay:"$(cat VERSION)" -h`. The ones an operator usually needs:

| Variable | Default | Meaning |
|----------|---------|---------|
| `UNISON_RELAY_QUIC_ADDR` | `:443` | UDP listen address |
| `UNISON_RELAY_WS_ADDR` | `:443` | TCP listen address of the WebSocket fallback |
| `UNISON_RELAY_HTTP_ADDR` | `:8080` | Health and metrics address |
| `UNISON_RELAY_TLS_CERT` | | PEM certificate, optionally with its chain |
| `UNISON_RELAY_TLS_KEY` | | PEM private key of the certificate |
| `UNISON_RELAY_TOKENS` | | Tokens as a comma separated list |
| `UNISON_RELAY_TOKENS_FILE` | | File with one token per line |
| `UNISON_RELAY_RATE` | `200` | Datagrams per second per token |
| `UNISON_RELAY_RATE_BURST` | `400` | Burst of the datagram rate |
| `UNISON_RELAY_QUOTA_BYTES` | `1073741824` | Traffic quota per token and window, in bytes |
| `UNISON_RELAY_QUOTA_WINDOW` | `24h` | Length of the quota window |
| `UNISON_RELAY_MAX_CONNECTIONS` | `4096` | Concurrent connections |

## TLS

The relay terminates TLS itself, so the certificate and the key are mounted into the container
instead of being kept in the image. A publicly trusted certificate is recommended; the devices
connect to the relay URL like to any HTTPS server.

The files must be readable by uid 10001, which owns the container process. Make the certificate
world readable and give the key to the container user, or run the container with a matching
`--user`.

For a throwaway test certificate:

```sh
mkdir -p deploy/relay/secrets
openssl req -x509 -newkey rsa:2048 -nodes -days 30 \
    -keyout deploy/relay/secrets/key.pem -out deploy/relay/secrets/cert.pem \
    -subj "/CN=relay.local"
chmod 0444 deploy/relay/secrets/cert.pem deploy/relay/secrets/key.pem
```

The server certificate is the only TLS material involved: the relay does not verify client
certificates, because devices authenticate each other end to end inside the encrypted datagrams.

## Access tokens

Every device registers with an access token, and the relay keeps only SHA-256 hashes of them in
memory, never the tokens themselves. One token per Personal group is enough. Generate one and put
it into the tokens file:

```sh
openssl rand -hex 32 > deploy/relay/secrets/tokens.txt
chmod 0444 deploy/relay/secrets/tokens.txt
```

The file holds one token per line; empty lines and lines starting with `#` are ignored. The
alternative is `UNISON_RELAY_TOKENS` with a comma separated list, which is handy for a quick local
run but leaves the tokens in the container environment. The rate limit and the traffic quota apply
to the whole token, not to a single connection, and exceeding the quota closes the connection.

## Health and metrics

`/healthz` reports that the process runs, `/readyz` that it accepts connections, and `/metrics`
serves Prometheus text format. The endpoints are plain HTTP, so keep the port on the loopback
interface or behind the monitoring network.

```sh
curl -s http://127.0.0.1:8080/healthz
curl -s http://127.0.0.1:8080/metrics
```

A Prometheus scrape configuration:

```yaml
scrape_configs:
  - job_name: unison-relay
    static_configs:
      - targets: ["relay.example.com:8080"]
```

The counters cover connections, registrations, live mailboxes, datagrams and bytes per transport,
uptime and dropped datagrams by reason. Labels never carry mailbox ids, tokens or client addresses.

## Why UDP does not pass a normal HTTP ingress

Most ingress and reverse proxies (nginx with a TLS server block, Traefik, an HTTP load balancer of
a cloud provider, a Kubernetes Ingress object) are HTTP servers: they accept TCP, terminate TLS and
route by host and path. A QUIC client sends UDP datagrams to the same port number, but nothing in
such a proxy listens for UDP or understands QUIC, so those packets are dropped. Publishing the
container port in compose or with `docker run -p` forwards UDP only to the local host; it does not
get the traffic through an HTTP proxy in front of it.

A relay that has to serve QUIC therefore needs one of these:

- the UDP port published on the host that runs the relay, with DNS pointing at that host and no
  HTTP proxy in between;
- a layer 4 load balancer or a firewall DNAT rule that forwards UDP and TCP with the same port
  number and leaves the payload untouched. QUIC encrypts everything, connection ids included, so
  a proxy that buffers, rewrites or splits packets breaks the handshake.

The WebSocket fallback is the other way around: it is ordinary TLS over TCP, so an ingress can pass
it through by SNI without touching the bytes, or terminate TLS and forward to the relay with a new
TLS connection. HTTP/2 is not used, and the ingress must keep WebSocket upgrades enabled. The relay
sends a WebSocket ping every 30 seconds, so keep the idle timeout of the ingress above that, or the
connection is closed under the devices.

A setup where UDP is blocked and only the WebSocket path works is the reason both transports share
one port: clients try QUIC first and fall back to the same address over TCP.

## Secrets

`deploy/relay/secrets` holds the private key and the access tokens and is not part of the
repository. Keep the directory outside the checkout when the files live somewhere else, and mount
that path instead. The container needs nothing else: there is no state, no database and no cache,
so restarting it only drops the live mailboxes and the counters. Run a single instance; mailboxes
are kept in memory, so neither a second replica nor a rolling restart keeps existing connections.
