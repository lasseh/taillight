#!/usr/bin/env bash
#
# test-filters.sh -- Feed sample messages through rsyslog validation
# Usage: make test  (or bash tests/test-filters.sh)
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
TEST_MESSAGES="$SCRIPT_DIR/test-messages.txt"
RSYSLOGD="rsyslogd"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

pass=0
fail=0
skip=0

# Check for rsyslogd availability
HAVE_RSYSLOGD=false
if command -v $RSYSLOGD >/dev/null 2>&1; then
    HAVE_RSYSLOGD=true
fi

echo "=== rsyslog Filter Tests ==="
echo ""

# Use WORK_DIR instead of TMPDIR to avoid clobbering the standard POSIX variable
WORK_DIR=$(mktemp -d)
trap 'rm -rf "$WORK_DIR"' EXIT

# --- Test 1: Configuration syntax validation ---
echo -n "Test 1: Configuration syntax validation... "
if [ "$HAVE_RSYSLOGD" = false ]; then
    echo -e "${YELLOW}SKIP${NC} (rsyslogd not found)"
    (( skip += 1 ))
else
    cp "$PROJECT_DIR/rsyslog.conf" "$WORK_DIR/"
    cp -r "$PROJECT_DIR/conf.d" "$WORK_DIR/"
    cp -r "$PROJECT_DIR/filters" "$WORK_DIR/"

    # Rewrite paths to use work directory
    if [[ "$OSTYPE" == "darwin"* ]]; then
        find "$WORK_DIR" -name "*.conf" -exec sed -i '' "s|/etc/rsyslog.d/|${WORK_DIR}/|g" {} +
    else
        find "$WORK_DIR" -name "*.conf" -exec sed -i "s|/etc/rsyslog.d/|${WORK_DIR}/|g" {} +
    fi

    if $RSYSLOGD -N1 -f "$WORK_DIR/rsyslog.conf" > /dev/null 2>&1; then
        echo -e "${GREEN}PASS${NC}"
        (( pass += 1 ))
    else
        echo -e "${RED}FAIL${NC}"
        echo "  rsyslogd -N1 reported errors:"
        $RSYSLOGD -N1 -f "$WORK_DIR/rsyslog.conf" 2>&1 | head -20
        (( fail += 1 ))
    fi
fi

# --- Test 2: All filter files parse individually ---
echo -n "Test 2: Individual filter file syntax... "
if [ "$HAVE_RSYSLOGD" = false ]; then
    echo -e "${YELLOW}SKIP${NC} (rsyslogd not found)"
    (( skip += 1 ))
else
    filter_errors=0
    for f in "$PROJECT_DIR/filters/"*.conf; do
        # Wrap in minimal rsyslog config with all modules/templates used by filters
        cat > "$WORK_DIR/test-single.conf" <<CONFEOF
module(load="imtcp")
module(load="mmpstrucdata")
module(load="omprog")
template(name="CommitFormat" type="string" string="%msg%\n")
template(name="OxidizedJSON" type="string" string="%msg%\n")
template(name="ResinTemplate" type="string" string="%msg%\n")
template(name="NetworkDeviceFormat" type="string" string="%msg%\n")
input(type="imtcp" port="15140" ruleset="test_rs")
ruleset(name="test_rs") {
    action(type="mmpstrucdata")
$(cat "$f")
}
CONFEOF
        if ! $RSYSLOGD -N1 -f "$WORK_DIR/test-single.conf" > /dev/null 2>&1; then
            echo ""
            echo -e "  ${RED}FAIL${NC}: $(basename "$f")"
            (( filter_errors += 1 ))
        fi
    done

    if [ $filter_errors -eq 0 ]; then
        echo -e "${GREEN}PASS${NC} ($(ls "$PROJECT_DIR/filters/"*.conf | wc -l | tr -d ' ') files)"
        (( pass += 1 ))
    else
        echo -e "${RED}FAIL${NC} ($filter_errors files with errors)"
        (( fail += 1 ))
    fi
fi

# --- Test 3: Test messages file exists and has content ---
echo -n "Test 3: Test message fixtures exist... "
if [ -f "$TEST_MESSAGES" ] && [ -s "$TEST_MESSAGES" ]; then
    msg_count=$(grep -c '^<' "$TEST_MESSAGES" || true)
    echo -e "${GREEN}PASS${NC} ($msg_count messages)"
    (( pass += 1 ))
else
    echo -e "${RED}FAIL${NC} (missing or empty: $TEST_MESSAGES)"
    (( fail += 1 ))
fi

# --- Test 4: No orphaned include references ---
echo -n "Test 4: All included files exist... "
grep -rh 'include.*file=' "$PROJECT_DIR/rsyslog.conf" "$PROJECT_DIR/conf.d/" 2>/dev/null | \
    grep -oE 'file="[^"]+"' | \
    sed 's/file="//;s/"$//' > "$WORK_DIR/includes.txt" || true

include_errors=0
while read -r inc; do
    resolved="${inc/\/etc\/rsyslog.d\//$PROJECT_DIR/}"
    if [ ! -f "$resolved" ]; then
        echo ""
        echo -e "  ${RED}MISSING${NC}: $inc -> $resolved"
        (( include_errors += 1 ))
    fi
done < "$WORK_DIR/includes.txt"

if [ $include_errors -eq 0 ]; then
    inc_count=$(wc -l < "$WORK_DIR/includes.txt" | tr -d ' ')
    echo -e "${GREEN}PASS${NC} ($inc_count includes resolved)"
    (( pass += 1 ))
else
    echo -e "${RED}FAIL${NC} ($include_errors missing files)"
    (( fail += 1 ))
fi

# --- Test 5: Behavioral filter validation ---
echo -n "Test 5: Behavioral filter validation... "
if [ "$HAVE_RSYSLOGD" = false ]; then
    echo -e "${YELLOW}SKIP${NC} (rsyslogd not found)"
    (( skip += 1 ))
elif ! command -v nc >/dev/null 2>&1; then
    echo -e "${YELLOW}SKIP${NC} (nc not found)"
    (( skip += 1 ))
else
    TEST_PORT=15514
    OUTPUT_FILE="$WORK_DIR/behavioral-output.log"
    PID_FILE="$WORK_DIR/rsyslog-test.pid"

    # Build test config -- includes pure filter files only
    # Excludes 06-ui-commit-trigger.conf (has omprog/omfwd actions requiring external services)
    cat > "$WORK_DIR/behavioral.conf" <<CONFEOF
global(maxMessageSize="64k")
module(load="imudp")
module(load="mmpstrucdata")
template(name="TestOutput" type="string"
    string="KEPT: %msgid% | %programname% | %msg:2:\$%\n")
input(type="imudp" port="$TEST_PORT" ruleset="test_behavioral")

ruleset(name="test_behavioral") {
    action(type="mmpstrucdata")
$(cat "$PROJECT_DIR/filters/05-by-msgid.conf")
$(cat "$PROJECT_DIR/filters/10-by-programname.conf")
$(cat "$PROJECT_DIR/filters/30-by-facility.conf")
$(cat "$PROJECT_DIR/filters/40-by-severity.conf")
    action(type="omfile"
        file="$OUTPUT_FILE"
        template="TestOutput"
        flushOnTXEnd="on"
    )
}
CONFEOF

    # Start rsyslog in background
    $RSYSLOGD -n -f "$WORK_DIR/behavioral.conf" -i "$PID_FILE" >/dev/null 2>&1 &
    RSYSLOG_PID=$!
    sleep 2

    # Check if rsyslog started
    if ! kill -0 $RSYSLOG_PID 2>/dev/null; then
        echo -e "${YELLOW}SKIP${NC} (rsyslogd failed to start -- may need root for UDP listener)"
        (( skip += 1 ))
    else
        # Send test messages
        while IFS= read -r line; do
            [[ "$line" =~ ^#.*$ || -z "$line" ]] && continue
            echo "$line" | nc -u -w1 127.0.0.1 $TEST_PORT 2>/dev/null || true
        done < "$TEST_MESSAGES"
        sleep 2

        # Stop rsyslog
        kill $RSYSLOG_PID 2>/dev/null || true
        wait $RSYSLOG_PID 2>/dev/null || true

        if [ ! -f "$OUTPUT_FILE" ]; then
            echo -e "${YELLOW}SKIP${NC} (no output -- rsyslog may not have processed messages)"
            (( skip += 1 ))
        else
            behavioral_errors=0

            # Messages that SHOULD be dropped (noise)
            for msgid in CHASSISD_BLOWERS_SPEED RPD_SCHED_CALLBACK RPD_SCHED_MODULE_INFO \
                         RT_FLOW_SESSION_CREATE RT_FLOW_SESSION_CLOSE \
                         SNMPD_TRAP_COLD_START SNMPD_AUTH_FAILURE; do
                if grep -q "KEPT: ${msgid} " "$OUTPUT_FILE"; then
                    echo -e "\n  ${RED}LEAK${NC}: $msgid was not filtered (should be dropped)"
                    (( behavioral_errors += 1 ))
                fi
            done

            # Messages that MUST be kept (important)
            for msgid in RT_FLOW_SESSION_DENY RPD_BGP_NEIGHBOR_STATE_CHANGED \
                         KERNEL_PANIC LICENSE_EXPIRING UI_COMMIT; do
                if ! grep -q "KEPT: ${msgid} " "$OUTPUT_FILE"; then
                    echo -e "\n  ${RED}MISSING${NC}: $msgid was filtered (should be kept)"
                    (( behavioral_errors += 1 ))
                fi
            done

            # Noise rules that match message text drop it only from lines with
            # programname "-". An sshd or mgd event quoting the text is kept.
            # TestOutput cuts the first character of msg, so each text below
            # starts past the first character of its noise line.
            kept_from() {
                awk -v prog="| $1 | " -v text="$2" \
                    'index($0, prog) && index($0, text) { found = 1 } END { exit !found }' "$OUTPUT_FILE"
            }
            for text in "ifd_set_eff_bandwidth" \
                        "compute_install_sched_params" \
                        "update_sched_policy_for_ifd" \
                        "media speed change" \
                        "nh_ucast_process_static_mac"; do
                if kept_from - "$text"; then
                    echo -e "\n  ${RED}LEAK${NC}: programname \"-\" line with \"$text\" was not filtered (should be dropped)"
                    (( behavioral_errors += 1 ))
                fi
                for prog in sshd mgd; do
                    if ! kept_from "$prog" "$text"; then
                        echo -e "\n  ${RED}MISSING${NC}: $prog line quoting \"$text\" was filtered (should be kept)"
                        (( behavioral_errors += 1 ))
                    fi
                done
            done

            if [ $behavioral_errors -eq 0 ]; then
                kept_count=$(wc -l < "$OUTPUT_FILE" | tr -d ' ')
                echo -e "${GREEN}PASS${NC} ($kept_count messages survived filters)"
                (( pass += 1 ))
            else
                echo -e "${RED}FAIL${NC} ($behavioral_errors errors)"
                (( fail += 1 ))
            fi
        fi
    fi
fi

# Print one section of a ruleset in 20-ruleset.conf, from its "# --- <name>"
# header to the next "# ---" header, so tests run the shipped drop rules
# rather than a copy of them.
ruleset_section() {
    awk -v rs="$1" -v hdr="# --- $2" '
        index($0, "ruleset(name=\"" rs "\"") { inrs = 1 }
        inrs && index($0, hdr) { inblk = 1; next }
        inblk && /# ---/ { exit }
        inblk { print }
    ' "$PROJECT_DIR/conf.d/20-ruleset.conf"
}

# --- Test 6: Header drop rules in both feed rulesets ---
echo -n "Test 6: Oversized hostname dropped on both rulesets... "
if [ "$HAVE_RSYSLOGD" = false ]; then
    echo -e "${YELLOW}SKIP${NC} (rsyslogd not found)"
    (( skip += 1 ))
elif ! command -v nc >/dev/null 2>&1; then
    echo -e "${YELLOW}SKIP${NC} (nc not found)"
    (( skip += 1 ))
else
    NET_PORT=15515
    SRV_PORT=15516
    cat > "$WORK_DIR/header.conf" <<CONFEOF
global(maxMessageSize="64k")
module(load="imudp")
template(name="HeaderOutput" type="string" string="KEPT: %msg%\n")
input(type="imudp" port="$NET_PORT" ruleset="test_network_devices")
input(type="imudp" port="$SRV_PORT" ruleset="test_server_logs")

ruleset(name="test_network_devices") {
$(ruleset_section network_devices "Phase 0.5")
    action(type="omfile" file="$WORK_DIR/header-network_devices.log"
        template="HeaderOutput" flushOnTXEnd="on")
}

ruleset(name="test_server_logs") {
$(ruleset_section server_logs "Phase 0.5")
    action(type="omfile" file="$WORK_DIR/header-server_logs.log"
        template="HeaderOutput" flushOnTXEnd="on")
}
CONFEOF

    $RSYSLOGD -n -f "$WORK_DIR/header.conf" -i "$WORK_DIR/rsyslog-header.pid" >/dev/null 2>&1 &
    HEADER_PID=$!
    sleep 2

    if ! kill -0 $HEADER_PID 2>/dev/null; then
        echo -e "${YELLOW}SKIP${NC} (rsyslogd failed to start -- may need root for UDP listener)"
        (( skip += 1 ))
    else
        # RFC 5424 caps HOSTNAME at 255 characters: 255 is kept, 256 dropped.
        host255=$(printf 'h%.0s' $(seq 1 255))
        for port in $NET_PORT $SRV_PORT; do
            echo "<13>1 2024-11-28T12:03:00.000Z ${host255} app 1 HOSTLEN - hostname-255" | nc -u -w1 127.0.0.1 $port 2>/dev/null || true
            echo "<13>1 2024-11-28T12:03:00.000Z ${host255}h app 1 HOSTLEN - hostname-256" | nc -u -w1 127.0.0.1 $port 2>/dev/null || true
        done
        sleep 2

        kill $HEADER_PID 2>/dev/null || true
        wait $HEADER_PID 2>/dev/null || true

        header_errors=0
        for rs in network_devices server_logs; do
            out="$WORK_DIR/header-$rs.log"
            if ! grep -q "hostname-255" "$out" 2>/dev/null; then
                echo -e "\n  ${RED}MISSING${NC}: $rs dropped a 255-byte hostname (should be kept)"
                (( header_errors += 1 ))
            fi
            if grep -q "hostname-256" "$out" 2>/dev/null; then
                echo -e "\n  ${RED}LEAK${NC}: $rs kept a 256-byte hostname (should be dropped)"
                (( header_errors += 1 ))
            fi
        done

        if [ $header_errors -eq 0 ]; then
            echo -e "${GREEN}PASS${NC}"
            (( pass += 1 ))
        else
            echo -e "${RED}FAIL${NC} ($header_errors errors)"
            (( fail += 1 ))
        fi
    fi
fi

# --- Test 7: Hostnames containing * in both feed rulesets ---
echo -n "Test 7: Hostname containing * dropped on both rulesets... "
if [ "$HAVE_RSYSLOGD" = false ]; then
    echo -e "${YELLOW}SKIP${NC} (rsyslogd not found)"
    (( skip += 1 ))
elif ! command -v nc >/dev/null 2>&1; then
    echo -e "${YELLOW}SKIP${NC} (nc not found)"
    (( skip += 1 ))
else
    NET_PORT=15517
    SRV_PORT=15518
    # network_devices drops these after its critical capture, so its test
    # ruleset runs the critical section too, with a stand-in output_critical.
    cat > "$WORK_DIR/star.conf" <<CONFEOF
global(maxMessageSize="64k")
module(load="imudp")
template(name="StarOutput" type="string" string="KEPT: %msg%\n")
input(type="imudp" port="$NET_PORT" ruleset="test_network_devices")
input(type="imudp" port="$SRV_PORT" ruleset="test_server_logs")

ruleset(name="output_critical") {
    action(type="omfile" file="$WORK_DIR/star-critical.log"
        template="StarOutput" flushOnTXEnd="on")
}

ruleset(name="test_network_devices") {
$(ruleset_section network_devices "Phase 0.5")
$(ruleset_section network_devices "Critical severity logging")
    action(type="omfile" file="$WORK_DIR/star-network_devices.log"
        template="StarOutput" flushOnTXEnd="on")
}

ruleset(name="test_server_logs") {
$(ruleset_section server_logs "Phase 0.5")
    action(type="omfile" file="$WORK_DIR/star-server_logs.log"
        template="StarOutput" flushOnTXEnd="on")
}
CONFEOF

    $RSYSLOGD -n -f "$WORK_DIR/star.conf" -i "$WORK_DIR/rsyslog-star.pid" >/dev/null 2>&1 &
    STAR_PID=$!
    sleep 2

    if ! kill -0 $STAR_PID 2>/dev/null; then
        echo -e "${YELLOW}SKIP${NC} (rsyslogd failed to start -- may need root for UDP listener)"
        (( skip += 1 ))
    else
        ts="2024-11-28T12:03:00.000Z"
        for port in $NET_PORT $SRV_PORT; do
            echo "<134>1 $ts *.* rpd 1 - - star-info" | nc -u -w1 127.0.0.1 $port 2>/dev/null || true
            echo "<134>1 $ts rtr1 rpd 1 - - literal-info" | nc -u -w1 127.0.0.1 $port 2>/dev/null || true
        done
        echo "<130>1 $ts *.* rpd 1 - - star-crit" | nc -u -w1 127.0.0.1 $NET_PORT 2>/dev/null || true
        sleep 2

        kill $STAR_PID 2>/dev/null || true
        wait $STAR_PID 2>/dev/null || true

        star_errors=0
        for rs in network_devices server_logs; do
            out="$WORK_DIR/star-$rs.log"
            if ! grep -q "literal-info" "$out" 2>/dev/null; then
                echo -e "\n  ${RED}MISSING${NC}: $rs dropped a literal hostname (should be kept)"
                (( star_errors += 1 ))
            fi
            if grep -q "star-" "$out" 2>/dev/null; then
                echo -e "\n  ${RED}LEAK${NC}: $rs kept a hostname containing * (should be dropped)"
                (( star_errors += 1 ))
            fi
        done
        if ! grep -q "star-crit" "$WORK_DIR/star-critical.log" 2>/dev/null; then
            echo -e "\n  ${RED}MISSING${NC}: network_devices dropped a hostname containing * before output_critical"
            (( star_errors += 1 ))
        fi

        if [ $star_errors -eq 0 ]; then
            echo -e "${GREEN}PASS${NC}"
            (( pass += 1 ))
        else
            echo -e "${RED}FAIL${NC} ($star_errors errors)"
            (( fail += 1 ))
        fi
    fi
fi

# --- Summary ---
echo ""
echo "=== Results ==="
echo -e "  ${GREEN}Passed: $pass${NC}"
if [ $fail -gt 0 ]; then
    echo -e "  ${RED}Failed: $fail${NC}"
else
    echo "  Failed: 0"
fi
if [ $skip -gt 0 ]; then
    echo -e "  ${YELLOW}Skipped: $skip${NC}"
fi
echo ""

exit $fail
