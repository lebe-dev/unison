#!/usr/bin/env bash
# Shows how the virtual machine is connected and whether the Mac is reachable.
set -euo pipefail

kit="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
peer=""
port=47811
inbound_wait=45

usage() {
    cat <<'EOF'
usage: scripts/net-check.sh [--peer <address of the Mac>] [--port <number>]

  --peer <address>  address of the Mac, e.g. 192.168.1.20
  --port <number>   port of the echo, 47811 by default

Without --peer the script only describes the machine. With --peer it pings the
Mac, sends a UDP probe to bin/unisond --net-echo running on the Mac and waits
for a probe from the Mac.
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --peer)
            [ $# -ge 2 ] || { echo "net-check: --peer needs a value" >&2; exit 2; }
            peer="$2"
            shift 2
            ;;
        --peer=*)
            peer="${1#--peer=}"
            shift
            ;;
        --port)
            [ $# -ge 2 ] || { echo "net-check: --port needs a value" >&2; exit 2; }
            port="$2"
            shift 2
            ;;
        --port=*)
            port="${1#--port=}"
            shift
            ;;
        -h | --help)
            usage
            exit 0
            ;;
        *)
            echo "net-check: unknown option $1" >&2
            usage >&2
            exit 2
            ;;
    esac
done

if ! command -v ip >/dev/null 2>&1; then
    echo "net-check: ip is missing, install iproute2" >&2
    exit 1
fi

# hint_for returns a note about a known address range of a hypervisor.
hint_for() {
    local address="$1"
    case "$address" in
        10.0.2.*) echo "10.0.2.x is the user mode network of QEMU and UTM" ;;
        192.168.64.*) echo "192.168.64.x is the shared network of the macOS hypervisor" ;;
        10.211.55.*) echo "10.211.55.x is the shared network of Parallels" ;;
        *) echo "" ;;
    esac
}

# local_address returns the address the machine uses to reach the peer.
local_address() {
    local address
    address="$(ip route get "$peer" 2>/dev/null | awk '{for (i = 1; i <= NF; i++) if ($i == "src") print $(i + 1)}' | head -n 1)"
    if [ -n "$address" ]; then
        printf '%s\n' "$address"
        return 0
    fi
    while IFS= read -r address; do
        case "$address" in
            127.*) continue ;;
        esac
        printf '%s\n' "$address"
        return 0
    done < <(ip -o -4 addr show | awk '{print $4}' | cut -d/ -f1)
    return 1
}

echo "net-check: addresses"
while IFS= read -r line; do
    echo "  $line"
done < <(ip -o -4 addr show | awk '{print $2, $4}')
while IFS= read -r line; do
    echo "  default route: $line"
done < <(ip route show default)

while IFS= read -r line; do
    address="${line%%/*}"
    hint="$(hint_for "$address")"
    if [ -n "$hint" ]; then
        echo "net-check: hint: $hint"
    fi
done < <(ip -o -4 addr show | awk '{print $4}')

echo
echo "net-check: multicast"
if command -v ss >/dev/null 2>&1; then
    listeners="$(ss -ulpn 2>/dev/null || true)"
    case "$listeners" in
        *:5353*) echo "  5353: a listener is present" ;;
        *) echo "  5353: no listener, mDNS is off or disabled" ;;
    esac
else
    echo "  5353: ss is missing"
fi
if command -v systemctl >/dev/null 2>&1; then
    echo "  avahi-daemon: $(systemctl is-active avahi-daemon 2>/dev/null || true)"
else
    echo "  avahi-daemon: systemctl is missing"
fi
avahi_browse=0
mdns_seen=0
if command -v avahi-browse >/dev/null 2>&1; then
    avahi_browse=1
    services="$(timeout 8 avahi-browse -art 2>/dev/null || true)"
    case "$services" in
        *_unison._udp*)
            mdns_seen=1
            echo "  avahi-browse: a _unison._udp service is visible"
            ;;
        *)
            echo "  avahi-browse: no _unison._udp service is visible"
            ;;
    esac
else
    echo "  avahi-browse: not installed, the services of the Mac are not listed"
fi
if [ "$avahi_browse" -eq 1 ] && [ "$mdns_seen" -eq 0 ]; then
    echo "  note: a service is only visible while the daemon of the Mac runs"
fi

echo
echo "net-check: firewall"
if command -v ufw >/dev/null 2>&1; then
    status="$(ufw status 2>&1 || true)"
    while IFS= read -r line; do
        echo "  $line"
    done <<< "$status"
    case "$status" in
        *"need to be root"* | *"ERROR"*)
            echo "  ufw: the state needs root, run sudo ufw status yourself"
            ;;
    esac
else
    echo "  ufw is not installed"
fi

udp_ok=0
inbound_ok=0
if [ -z "$peer" ]; then
    echo
    echo "net-check: run this script with --peer <address of the Mac> to test the path to the Mac"
else
    echo
    echo "net-check: path to $peer"
    route="$(ip route get "$peer" 2>/dev/null || true)"
    if [ -n "$route" ]; then
        echo "  $route"
        case "$route" in
            *" via "*) echo "  the Mac is on another subnet, traffic goes through the gateway" ;;
            *) echo "  the Mac is on the same subnet" ;;
        esac
    else
        echo "  no route to $peer"
    fi

    if command -v ping >/dev/null 2>&1; then
        if ping -c 3 -W 2 "$peer" >/dev/null 2>&1; then
            echo "  ping: the Mac answers"
        else
            echo "  ping: the Mac does not answer, ICMP may be blocked"
        fi
    else
        echo "  ping is missing"
    fi

    echo "  udp: probe the echo on the Mac"
    if [ -x "$kit/bin/unisond" ]; then
        set +e
        "$kit/bin/unisond" --net-echo-peer "$peer:$port" --net-echo-wait 5s 2>&1 | sed 's/^/  /'
        probe_code="${PIPESTATUS[0]}"
        set -e
        if [ "$probe_code" -eq 0 ]; then
            udp_ok=1
        fi
    else
        echo "  bin/unisond is missing"
    fi

    echo "  udp: wait for a probe from the Mac"
    echo "  start this on the Mac in the repository:"
    echo "    bin/unisond --net-echo-peer $(local_address 2>/dev/null || echo "<address of this machine>"):$port"
    echo "  waiting up to $inbound_wait s, press Ctrl-C to stop"
    if [ ! -x "$kit/bin/unisond" ]; then
        echo "  bin/unisond is missing, the incoming probe is not checked"
        inbound_wait=0
    fi
    if [ "$inbound_wait" -gt 0 ]; then
        inbound_log="$(mktemp)"
        "$kit/bin/unisond" --net-echo --net-echo-listen "0.0.0.0:$port" \
            --net-echo-wait "${inbound_wait}s" >"$inbound_log" 2>&1 &
        inbound_pid=$!
        cleanup() { kill "$inbound_pid" 2>/dev/null || true; }
        trap cleanup EXIT
        set +e
        wait "$inbound_pid"
        inbound_code=$?
        set -e
        trap - EXIT
        sed 's/^/  /' "$inbound_log"
        rm -f "$inbound_log"
        if [ "$inbound_code" -eq 0 ]; then
            inbound_ok=1
        fi
    fi
fi

echo
if [ "$mdns_seen" -eq 1 ]; then
    echo "result: multicast between this VM and the Mac works"
elif [ "$avahi_browse" -eq 0 ]; then
    echo "result: avahi-browse is missing, multicast is not proven, install avahi-utils"
else
    echo "result: multicast between this VM and the Mac does not work, use bridged networking or the manual peer address"
fi
if [ -n "$peer" ]; then
    if [ "$udp_ok" -eq 1 ] && [ "$inbound_ok" -eq 1 ]; then
        echo "result: UDP between this VM and the Mac works in both directions"
    elif [ "$udp_ok" -eq 1 ]; then
        echo "result: UDP from this VM to the Mac works, the opposite direction is not proven"
    elif [ "$inbound_ok" -eq 1 ]; then
        echo "result: UDP from the Mac to this VM works, the opposite direction is not proven"
    else
        echo "result: UDP between this VM and the Mac does not work, use bridged networking or the manual peer address"
    fi
fi
