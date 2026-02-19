#!/bin/sh
set -e

# Substitute PGSQL_* environment variables into ompgsql config files.
# Defaults match docker-compose.yml so existing behavior is unchanged.
PGSQL_SERVER="${PGSQL_SERVER:-postgres}"
PGSQL_PORT="${PGSQL_PORT:-5432}"
PGSQL_DB="${PGSQL_DB:-taillight}"
PGSQL_USER="${PGSQL_USER:-taillight}"
PGSQL_PASSWORD="${PGSQL_PASSWORD:-taillight}"

for conf in /etc/rsyslog.d/conf.d/02-outputs.conf /etc/rsyslog.d/conf.d/03-operational-logging.conf; do
    [ -f "$conf" ] || continue
    sed -i \
        -e "s|server=\"postgres\"|server=\"${PGSQL_SERVER}\"|g" \
        -e "s|port=\"5432\"|port=\"${PGSQL_PORT}\"|g" \
        -e "s|db=\"taillight\"|db=\"${PGSQL_DB}\"|g" \
        -e "s|uid=\"taillight\"|uid=\"${PGSQL_USER}\"|g" \
        -e "s|pwd=\"taillight\"|pwd=\"${PGSQL_PASSWORD}\"|g" \
        "$conf"
done

exec "$@"
