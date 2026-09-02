#!/bin/bash
set -u -o pipefail

# Read-only Debian 13 amd64 validation for VPN's LND SQLite boundary.
# Run as root on a disposable, freshly installed candidate. The default live
# mode verifies configuration, exact database inventory, ownership, modes, and
# LND's authoritative wallet state. The stopped mode additionally requires a
# process-free LND service and runs SQLite integrity checks on temporary copies
# without opening the source databases.

if [[ ${EUID} -ne 0 ]]; then
    echo "ERROR: run as root on the disposable Debian fixture" >&2
    exit 2
fi

profile=${1:-}
mode=${2:-live}
case "${profile}" in
    mainnet) lnd_network=mainnet ;;
    testnet4) lnd_network=testnet4 ;;
    public-signet) lnd_network=signet ;;
    *)
        echo "Usage: $0 <mainnet|testnet4|public-signet> [live|stopped]" >&2
        exit 2
        ;;
esac
case "${mode}" in
    live|stopped) ;;
    *)
        echo "Usage: $0 <mainnet|testnet4|public-signet> [live|stopped]" >&2
        exit 2
        ;;
esac

passes=0
failures=0

pass() {
    passes=$((passes + 1))
    echo "PASS $1"
}

fail() {
    failures=$((failures + 1))
    echo "FAIL $1: $2"
}

assert_equal() {
    local label=$1
    local got=$2
    local want=$3
    if [[ ${got} == "${want}" ]]; then
        pass "${label}"
    else
        fail "${label}" "got '${got}', want '${want}'"
    fi
}

assert_exact_config_line() {
    local label=$1
    local line=$2
    local count
    count=$(grep -Fxc -- "${line}" /etc/lnd/lnd.conf 2>/dev/null || true)
    assert_equal "${label}" "${count}" 1
}

assert_private_file() {
    local label=$1
    local file=$2
    if [[ -L ${file} || ! -f ${file} ]]; then
        fail "${label}" "${file} is missing, symlinked, or not regular"
        return
    fi
    assert_equal "${label} owner" "$(stat -c '%U:%G' -- "${file}")" "lnd:lnd"
    assert_equal "${label} mode" "$(stat -c '%a' -- "${file}")" 600
    assert_equal "${label} link count" "$(stat -c '%h' -- "${file}")" 1
}

lnd_conf=/etc/lnd/lnd.conf
assert_exact_config_line "CFG SQLite backend" "db.backend=sqlite"
assert_exact_config_line "CFG native SQL" "db.use-native-sql=true"
assert_exact_config_line "CFG disk threshold" \
    "healthcheck.diskspace.diskrequired=0.10"
assert_exact_config_line "CFG disk attempts" \
    "healthcheck.diskspace.attempts=2"
assert_exact_config_line "CFG disk interval" \
    "healthcheck.diskspace.interval=12h"

for forbidden in 'db.bolt.' 'db.sqlite.' 'skip-native-sql-migration'; do
    if grep -Fq -- "${forbidden}" "${lnd_conf}" 2>/dev/null; then
        fail "CFG excludes ${forbidden}" "unexpected setting exists"
    else
        pass "CFG excludes ${forbidden}"
    fi
done

assert_equal "UNIT user" \
    "$(systemctl show -p User --value lnd.service 2>/dev/null)" lnd
assert_equal "UNIT group" \
    "$(systemctl show -p Group --value lnd.service 2>/dev/null)" lnd
assert_equal "UNIT private umask" \
    "$(systemctl show -p UMask --value lnd.service 2>/dev/null)" 0077

chain_dir=/var/lib/lnd/data/chain/bitcoin/${lnd_network}
graph_dir=/var/lib/lnd/data/graph/${lnd_network}
tower_dir=/var/lib/lnd/data/watchtower/bitcoin/${lnd_network}
declare -a databases
databases=(
    "${chain_dir}/chain.sqlite"
    "${graph_dir}/channel.sqlite"
    "${graph_dir}/lnd.sqlite"
    "${tower_dir}/watchtower.sqlite"
)

for database in "${databases[@]}"; do
    assert_private_file "DB $(basename "${database}")" "${database}"
    for suffix in -wal -shm; do
        companion=${database}${suffix}
        if [[ -e ${companion} || -L ${companion} ]]; then
            assert_private_file "DB companion $(basename "${companion}")" \
                "${companion}"
        else
            pass "DB companion $(basename "${companion}") absent"
        fi
    done
done

expected_inventory=$(printf '%s\n' "${databases[@]}" | sort)
actual_inventory=$(find /var/lib/lnd/data -xdev \
    \( -type f -o -type l \) -name '*.sqlite' \
    -printf '%p\n' 2>/dev/null | sort)
assert_equal "DB exact SQLite inventory" "${actual_inventory}" \
    "${expected_inventory}"

declare -a forbidden_databases
forbidden_databases=(
    "${chain_dir}/wallet.db"
    "${chain_dir}/macaroons.db"
    "${graph_dir}/channel.db"
    "${graph_dir}/sphinxreplay.db"
    "${graph_dir}/wtclient.db"
    "${tower_dir}/watchtower.db"
)
for database in "${forbidden_databases[@]}"; do
    if [[ ! -e ${database} && ! -L ${database} ]]; then
        pass "DB no legacy $(basename "${database}")"
    else
        fail "DB no legacy $(basename "${database}")" \
            "unexpected ${database} exists"
    fi
done

if [[ ${mode} == live ]]; then
    state_json=$(/usr/local/bin/lncli \
        --rpcserver=127.0.0.1:10009 \
        --tlscertpath=/var/lib/lnd/tls.cert \
        state 2>/dev/null || true)
    wallet_state=$(python3 -c '
import json, sys
try:
    print(json.load(sys.stdin).get("state", ""))
except Exception:
    print("")
' <<<"${state_json}")
    case "${wallet_state}" in
        NON_EXISTING)
            pass "RPC wallet does not exist"
            ;;
        LOCKED|UNLOCKED|RPC_ACTIVE|SERVER_ACTIVE)
            pass "RPC wallet exists (${wallet_state})"
            ;;
        *)
            fail "RPC wallet fact is known" \
                "State/GetState returned '${wallet_state:-unavailable}'"
            ;;
    esac
else
    active_state=$(systemctl show -p ActiveState --value lnd.service 2>/dev/null)
    main_pid=$(systemctl show -p MainPID --value lnd.service 2>/dev/null)
    control_pid=$(systemctl show -p ControlPID --value lnd.service 2>/dev/null)
    case "${active_state}" in
        inactive|failed) pass "STOP LND inactive" ;;
        *) fail "STOP LND inactive" "ActiveState=${active_state}" ;;
    esac
    assert_equal "STOP no main process" "${main_pid}" 0
    assert_equal "STOP no control process" "${control_pid}" 0

    check_dir=$(mktemp -d /tmp/vpn-lnd-sqlite-check.XXXXXX 2>/dev/null || true)
    if [[ -z ${check_dir} || ! -d ${check_dir} ]]; then
        fail "DB prepare integrity checks" "could not create temporary directory"
        check_dir=
    else
        pass "DB prepare integrity checks"
    fi

    declare -a check_databases
    check_databases=()
    if [[ -n ${check_dir} ]]; then
        for database in "${databases[@]}"; do
            check_database=${check_dir}/$(basename "${database}")
            if cp --preserve=mode,timestamps -- "${database}" \
                "${check_database}" 2>/dev/null; then
                check_databases+=("${check_database}")
            else
                fail "DB copy $(basename "${database}")" \
                    "could not create stopped-state validation copy"
            fi
            for suffix in -wal -shm; do
                companion=${database}${suffix}
                if [[ -f ${companion} && ! -L ${companion} ]]; then
                    if ! cp --preserve=mode,timestamps -- "${companion}" \
                        "${check_database}${suffix}" 2>/dev/null; then
                        fail "DB copy $(basename "${companion}")" \
                            "could not copy stopped-state companion"
                    fi
                fi
            done
        done
    fi

    integrity_status=1
    integrity_output=
    if [[ ${#check_databases[@]} -eq ${#databases[@]} ]]; then
        integrity_output=$(python3 - "${check_databases[@]}" <<'PY'
import sqlite3
import sys
import urllib.parse

failed = False
for path in sys.argv[1:]:
    uri = "file:" + urllib.parse.quote(path) + "?mode=ro"
    try:
        connection = sqlite3.connect(uri, uri=True)
        connection.execute("PRAGMA query_only=ON")
        rows = [row[0] for row in connection.execute("PRAGMA integrity_check")]
        foreign = list(connection.execute("PRAGMA foreign_key_check"))
        connection.close()
        if rows == ["ok"] and not foreign:
            print("PASS " + path)
        else:
            print("FAIL " + path + " integrity=" + repr(rows) +
                  " foreign_keys=" + repr(foreign))
            failed = True
    except Exception as exc:
        print("FAIL " + path + " error=" + str(exc))
        failed = True
sys.exit(1 if failed else 0)
PY
        )
        integrity_status=$?
    fi
    while IFS= read -r line; do
        [[ -n ${line} ]] && echo "SQLITE ${line}"
    done <<<"${integrity_output}"
    if [[ ${integrity_status} -eq 0 ]]; then
        pass "DB all integrity checks"
    else
        fail "DB all integrity checks" "one or more checks failed"
    fi
    if [[ -n ${check_dir} ]]; then
        rm -rf -- "${check_dir}"
    fi
fi

echo "SUMMARY pass=${passes} fail=${failures}"
if (( failures > 0 )); then
    exit 1
fi
