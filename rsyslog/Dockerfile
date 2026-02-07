FROM debian:trixie-slim

ENV DEBIAN_FRONTEND=noninteractive
RUN echo 'dbconfig-common dbconfig-common/dbconfig-install boolean false' | debconf-set-selections && \
    apt-get update && apt-get install -y --no-install-recommends \
    rsyslog \
    rsyslog-pgsql \
    && rm -rf /var/lib/apt/lists/* /etc/rsyslog.d/pgsql.conf

COPY rsyslog.conf /etc/rsyslog.conf
COPY conf.d/ /etc/rsyslog.d/conf.d/
COPY filters/ /etc/rsyslog.d/filters/

# Enable PostgreSQL output for Docker: uncomment ompgsql module, template, output, and ruleset call
RUN sed -i 's/^# module(load="ompgsql")/module(load="ompgsql")/' /etc/rsyslog.d/conf.d/00-modules.conf

# Uncomment PgSQLInsert template
RUN sed -i '/^# template(name="PgSQLInsert"/,/^# }/s/^# //' /etc/rsyslog.d/conf.d/01-templates.conf

# Uncomment output_pgsql ruleset and configure for Docker networking
RUN sed -i '/^# ruleset(name="output_pgsql")/,/^# }/s/^# //' /etc/rsyslog.d/conf.d/02-outputs.conf && \
    sed -i 's/server="127.0.0.1"/server="postgres"/' /etc/rsyslog.d/conf.d/02-outputs.conf && \
    sed -i 's/pwd="changeme"/pwd="taillight"/' /etc/rsyslog.d/conf.d/02-outputs.conf

# Uncomment PgSQLStatsInsert template
RUN sed -i '/^# template(name="PgSQLStatsInsert"/,/^# }/s/^# //' /etc/rsyslog.d/conf.d/01-templates.conf

# Uncomment stats ompgsql action and configure for Docker
RUN sed -i '/^    # action(type="ompgsql"/,/^    # )/s/^    # /    /' /etc/rsyslog.d/conf.d/03-operational-logging.conf && \
    sed -i '/stats_to_pgsql/,/action.resumeInterval/s/server="127.0.0.1"/server="postgres"/' /etc/rsyslog.d/conf.d/03-operational-logging.conf && \
    sed -i '/stats_to_pgsql/,/action.resumeInterval/s/pwd="changeme"/pwd="taillight"/' /etc/rsyslog.d/conf.d/03-operational-logging.conf

# Uncomment the call to output_pgsql in the ruleset
RUN sed -i 's/^    # call output_pgsql/    call output_pgsql/' /etc/rsyslog.d/conf.d/20-ruleset.conf

# Disable librenms output (not available in Docker)
RUN sed -i 's/^    call output_librenms/    # call output_librenms/' /etc/rsyslog.d/conf.d/20-ruleset.conf

# Create spool and log directories
RUN mkdir -p /var/spool/rsyslog /var/log/network

# Validate config
RUN rsyslogd -N1 -f /etc/rsyslog.conf

EXPOSE 514/udp 514/tcp 1514/udp

CMD ["rsyslogd", "-n", "-f", "/etc/rsyslog.conf"]
