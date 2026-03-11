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
COPY docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh

# Create spool and log directories, create non-root user.
RUN useradd --system --no-create-home --shell /usr/sbin/nologin rsyslog && \
    mkdir -p /var/spool/rsyslog /var/log/network && \
    chown -R rsyslog /var/spool/rsyslog /var/log/network

# Validate config
RUN rsyslogd -N1 -f /etc/rsyslog.conf

EXPOSE 514/udp 514/tcp 1514/udp

USER rsyslog

ENTRYPOINT ["/docker-entrypoint.sh"]
CMD ["rsyslogd", "-n", "-f", "/etc/rsyslog.conf"]
