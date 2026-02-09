#!/bin/sh
set -e

# Replace placeholder password with environment variable at container start.
# This avoids baking credentials into Docker image layers.
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-taillight}"

sed -i "s/pwd=\"changeme\"/pwd=\"${POSTGRES_PASSWORD}\"/" /etc/rsyslog.d/conf.d/02-outputs.conf
sed -i "s/pwd=\"changeme\"/pwd=\"${POSTGRES_PASSWORD}\"/" /etc/rsyslog.d/conf.d/03-operational-logging.conf

exec "$@"
