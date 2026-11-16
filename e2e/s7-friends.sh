#!/usr/bin/env bash
# S7: a file offered by a Friends device waits for the decision of the receiver
# and is written only after the receiver accepts it.
set -euo pipefail
source "$(dirname "$0")/lib.sh"
e2e_init s7

C=e2e-c
D=e2e-d

e2e_daemon "$C"
e2e_daemon "$D"
e2e_wait_api "$C"
e2e_wait_api "$D"
e2e_pair "$C" "$D" friends

# Friends devices meet only while the temporary search is on, on both of them.
e2e_cli "$C" friends search >/dev/null
e2e_cli "$D" friends search >/dev/null
e2e_wait_online "$C" 1 online 90
e2e_wait_online "$D" 1 online 90

e2e_set_downloads "$D" "$E2E_ROOT/downloads"
downloads="$E2E_ROOT/downloads"

request_id() {
    e2e_cli "$D" requests | awk 'NR == 2 { print $1 }'
}

# An accepted offer arrives with the same content.
first="$E2E_ROOT/report.bin"
head -c 524288 /dev/urandom >"$first"
want=$(e2e_sha256 "$first")
e2e_cli "$C" send "$first" --to "$D"
deadline=$((SECONDS + 30)) id=""
while [ "$SECONDS" -lt "$deadline" ]; do
    id=$(request_id)
    [ -n "$id" ] && break
    sleep 0.1
done
if [ -z "$id" ]; then
    echo "the request did not arrive on $D" >&2
    exit 1
fi
echo "  request $id"
if [ -n "$(ls -A "$downloads")" ]; then
    echo "a file arrived before the decision" >&2
    ls -l "$downloads" >&2
    exit 1
fi

e2e_cli "$D" requests accept "$id" >/dev/null
deadline=$((SECONDS + 60))
while [ "$SECONDS" -lt "$deadline" ]; do
    [ -f "$downloads/report.bin" ] && break
    sleep 0.1
done
if [ ! -f "$downloads/report.bin" ]; then
    echo "the accepted file did not arrive in the downloads directory" >&2
    ls -l "$downloads" >&2
    exit 1
fi
got=$(e2e_sha256 "$downloads/report.bin")
e2e_expect_eq "$want" "$got" "checksum of the accepted file"

# A declined offer leaves nothing and tells the sender.
second="$E2E_ROOT/notes.txt"
printf 'notes of the day' >"$second"
e2e_cli "$C" send "$second" --to "$D"
deadline=$((SECONDS + 30)) id=""
while [ "$SECONDS" -lt "$deadline" ]; do
    id=$(request_id)
    [ -n "$id" ] && break
    sleep 0.1
done
if [ -z "$id" ]; then
    echo "the second request did not arrive on $D" >&2
    exit 1
fi
e2e_cli "$D" requests decline "$id" >/dev/null
deadline=$((SECONDS + 30)) state=""
while [ "$SECONDS" -lt "$deadline" ]; do
    state=$(e2e_cli "$C" transfers | awk 'NR == 2 { print $4 }')
    [ "$state" = "declined" ] && break
    sleep 0.1
done
e2e_expect_eq "declined" "$state" "state of the declined offer on $C"
if [ -f "$downloads/notes.txt" ]; then
    echo "the declined file was written" >&2
    exit 1
fi

# The clipboard of a Friends device does not cross the role matrix.
printf 'friends text' | e2e_cli "$C" clip set >/dev/null
sleep 1
leaked=$(e2e_cli "$D" clip get 2>/dev/null || true)
if [ "$leaked" = "friends text" ]; then
    echo "the text of a Friends device reached $D" >&2
    exit 1
fi

e2e_pass "request, accept and decline of Friends offers"
