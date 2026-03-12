#!/bin/sh
set -e

# Substitute PGSQL_* environment variables into ompgsql config files.
# Defaults match docker-compose.yml so existing behavior is unchanged.
PGSQL_SERVER="${PGSQL_SERVER:-postgres}"
PGSQL_PORT="${PGSQL_PORT:-5432}"
PGSQL_DB="${PGSQL_DB:-taillight}"
PGSQL_USER="${PGSQL_USER:-taillight}"
PGSQL_PASSWORD="${PGSQL_PASSWORD:-taillight}"

# Stage config in a writable directory.
# The image or volume-mounted config may live on a read-only filesystem,
# so we always work on copies under /tmp.
RUNTIME_DIR=/tmp/rsyslog
mkdir -p "$RUNTIME_DIR/conf.d"

cp /etc/rsyslog.conf "$RUNTIME_DIR/rsyslog.conf"
cp /etc/rsyslog.d/conf.d/* "$RUNTIME_DIR/conf.d/"

# Point includes at the writable copies
sed -i 's|/etc/rsyslog.d/conf.d/|/tmp/rsyslog/conf.d/|g' "$RUNTIME_DIR/rsyslog.conf"

# Substitute PGSQL_* variables in ompgsql config files
for conf in "$RUNTIME_DIR/conf.d/02-outputs.conf" "$RUNTIME_DIR/conf.d/03-operational-logging.conf"; do
    [ -f "$conf" ] || continue
    sed -i \
        -e "s|server=\"postgres\"|server=\"${PGSQL_SERVER}\"|g" \
        -e "s|port=\"5432\"|port=\"${PGSQL_PORT}\"|g" \
        -e "s|db=\"taillight\"|db=\"${PGSQL_DB}\"|g" \
        -e "s|uid=\"taillight\"|uid=\"${PGSQL_USER}\"|g" \
        -e "s|pwd=\"taillight\"|pwd=\"${PGSQL_PASSWORD}\"|g" \
        "$conf"
done

# If invoked as rsyslogd, use the processed runtime config.
# -e: disable container-specific defaults (rsyslog 8.2504+ auto-loads imuxsock
# and /var/log/syslog rules when running as PID 1, which fails as non-root).
if [ "$1" = "rsyslogd" ]; then
    exec rsyslogd -n -e -f "$RUNTIME_DIR/rsyslog.conf"
fi

exec "$@"
