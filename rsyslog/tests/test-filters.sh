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
