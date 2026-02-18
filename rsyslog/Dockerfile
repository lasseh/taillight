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

# Create spool and log directories
RUN mkdir -p /var/spool/rsyslog /var/log/network

# Validate config
RUN rsyslogd -N1 -f /etc/rsyslog.conf

EXPOSE 514/udp 514/tcp 1514/udp

CMD ["rsyslogd", "-n", "-f", "/etc/rsyslog.conf"]
