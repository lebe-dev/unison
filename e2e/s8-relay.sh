#!/usr/bin/env bash
# S8: two devices in different networks exchange text through the relay.
#
# The relay runs on the loopback interface of this machine. The local network
# path is cut with a sandbox profile that blocks the UDP port of the other
# device and mDNS, so only the relay path is left; a second profile blocks every
# outbound UDP datagram, which leaves the WebSocket fallback of the relay.
set -euo pipefail
source "$(dirname "$0")/lib.sh"
e2e_init s8

A=e2e-a
B=e2e-b

for tool in "$E2E_BIN/unison-relay" /usr/bin/sandbox-exec /usr/bin/openssl; do
    if [ ! -x "$tool" ]; then
        echo "missing $tool, run: just e2e-build" >&2
        exit 1
    fi
done

relay_port=$(e2e_free_port)
http_port=$(e2e_free_port)
openssl req -x509 -newkey rsa:2048 -nodes \
    -keyout "$E2E_ROOT/relay.key" -out "$E2E_ROOT/relay.pem" -days 2 \
    -subj "/CN=127.0.0.1" \
    -addext "subjectAltName=IP:127.0.0.1" \
    -addext "basicConstraints=critical,CA:TRUE" >/dev/null 2>&1

# The daemon has no flag for the certificate of a self-hosted relay, so the
# certificate goes into the trust store of the process.
export SSL_CERT_FILE="$E2E_ROOT/relay.pem"

# The relay needs at least 1224 bytes per datagram for the QUIC handshake of the
# devices that travel through it.
"$E2E_BIN/unison-relay" \
    -quic-addr "127.0.0.1:$relay_port" \
    -ws-addr "127.0.0.1:$relay_port" \
    -http-addr "127.0.0.1:$http_port" \
    -tls-cert "$E2E_ROOT/relay.pem" \
    -tls-key "$E2E_ROOT/relay.key" \
    -tokens e2e-token \
    -max-datagram-bytes 1400 >"$E2E_ROOT/relay.console" 2>&1 &
e2e_track $!
deadline=$((SECONDS + 30))
while [ "$SECONDS" -lt "$deadline" ]; do
    if curl -sf "http://127.0.0.1:$http_port/healthz" >/dev/null 2>&1; then
        break
    fi
    sleep 0.1
done
ready=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$http_port/readyz")
e2e_expect_eq "200" "$ready" "relay readiness"

e2e_daemon "$A"
e2e_daemon "$B"
e2e_wait_api "$A"
e2e_wait_api "$B"
e2e_pair "$A" "$B"
e2e_wait_online "$A"
e2e_wait_online "$B"

e2e_cli "$A" relay set "127.0.0.1:$relay_port" --token e2e-token >/dev/null
deadline=$((SECONDS + 30))
while [ "$SECONDS" -lt "$deadline" ]; do
    if e2e_cli "$B" relay 2>/dev/null | grep -q "127.0.0.1:$relay_port"; then
        break
    fi
    sleep 0.2
done
received=$(e2e_cli "$B" relay)
e2e_expect_contains "$received" "127.0.0.1:$relay_port" "relay settings of $B"
e2e_expect_contains "$received" "token stored" "relay token of $B"

# The local network path is cut by the UDP ports the two devices listen on. The
# port comes from the settings, so it survives the restart.
port_a=$(e2e_cli "$A" --json status | python3 -c 'import json, sys; print(json.load(sys.stdin)["addrs"][0].rsplit(":", 1)[1])')
port_b=$(e2e_cli "$B" --json status | python3 -c 'import json, sys; print(json.load(sys.stdin)["addrs"][0].rsplit(":", 1)[1])')
echo "  device ports $port_a and $port_b"

e2e_stop "$A"
e2e_stop "$B"

cat >"$E2E_ROOT/no-lan.sb" <<EOF
(version 1)
(allow default)
(deny network-outbound (remote udp "*:5353"))
(deny network-outbound (remote udp "*:$port_a"))
(deny network-outbound (remote udp "*:$port_b"))
EOF

e2e_sandboxed "$A" "$E2E_ROOT/no-lan.sb"
e2e_sandboxed "$B" "$E2E_ROOT/no-lan.sb"
e2e_wait_api "$A"
e2e_wait_api "$B"

deadline=$((SECONDS + 120))
while [ "$SECONDS" -lt "$deadline" ]; do
    if e2e_cli "$A" devices 2>/dev/null | grep -q "online relay"; then
        break
    fi
    sleep 0.2
done
path=$(e2e_cli "$A" devices)
e2e_expect_contains "$path" "online relay" "path of $B through the relay"
transport=$(e2e_cli "$A" relay)
e2e_expect_contains "$transport" "connected over QUIC" "relay transport of $A"

e2e_copy_text "$A" "$B" "relay text"
relay_ms=$E2E_LAST_MS
echo "  relay text arrived in $relay_ms ms"

e2e_stop "$A"
e2e_stop "$B"

cat >"$E2E_ROOT/no-udp.sb" <<EOF
(version 1)
(allow default)
(deny network-outbound (remote udp "*:*"))
EOF

e2e_sandboxed "$A" "$E2E_ROOT/no-udp.sb"
e2e_sandboxed "$B" "$E2E_ROOT/no-udp.sb"
e2e_wait_api "$A"
e2e_wait_api "$B"

deadline=$((SECONDS + 120))
while [ "$SECONDS" -lt "$deadline" ]; do
    if e2e_cli "$A" devices 2>/dev/null | grep -q "online relay"; then
        break
    fi
    sleep 0.2
done
transport=$(e2e_cli "$A" relay)
e2e_expect_contains "$transport" "connected over WSS" "relay transport without UDP"

e2e_copy_text "$A" "$B" "fallback text"
fallback_ms=$E2E_LAST_MS
echo "  fallback text arrived in $fallback_ms ms"

metrics=$(curl -s "http://127.0.0.1:$http_port/metrics")
registrations=$(printf '%s\n' "$metrics" | awk '/^unison_relay_registrations_total\{status="ok"\}/ { print $2 }')
if [ -z "$registrations" ] || [ "$registrations" -lt 2 ]; then
    echo "the relay reports $registrations successful registrations" >&2
    exit 1
fi

e2e_pass "QUIC relay path in $relay_ms ms, WSS fallback in $fallback_ms ms"
