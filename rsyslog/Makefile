DEPLOY_DIR ?= /etc/rsyslog.d

.PHONY: test validate deploy restart clean help

##@ General
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Quality
test: ## Build and test config in Docker
	docker build --no-cache -f Dockerfile.test .

validate: ## Validate config syntax locally (requires rsyslogd)
	@TMPDIR=$$(mktemp -d) && \
	cp rsyslog.conf $$TMPDIR/rsyslog.conf && \
	cp -r conf.d $$TMPDIR/conf.d && \
	cp -r filters $$TMPDIR/filters && \
	sed -i.bak 's|/etc/rsyslog.d/|'$$TMPDIR'/|g' $$TMPDIR/rsyslog.conf && \
	for f in $$TMPDIR/conf.d/*.conf; do \
		sed -i.bak 's|/etc/rsyslog.d/|'$$TMPDIR'/|g' "$$f" 2>/dev/null; \
	done && \
	rsyslogd -N1 -f $$TMPDIR/rsyslog.conf && \
	echo "Configuration OK" && \
	rm -rf $$TMPDIR || \
	(echo "VALIDATION FAILED" && rm -rf $$TMPDIR && exit 1)

##@ Deploy
deploy: ## Deploy config files (backs up existing first)
	@BACKUP=/tmp/rsyslog-backup-$$(date +%Y%m%d-%H%M%S) && \
	echo "Backing up to $$BACKUP" && \
	sudo mkdir -p $$BACKUP && \
	[ -f /etc/rsyslog.conf ] && sudo cp /etc/rsyslog.conf $$BACKUP/ || true && \
	[ -d $(DEPLOY_DIR) ] && sudo cp -r $(DEPLOY_DIR) $$BACKUP/rsyslog.d || true && \
	sudo mkdir -p $(DEPLOY_DIR)/conf.d $(DEPLOY_DIR)/filters && \
	sudo cp rsyslog.conf /etc/rsyslog.conf && \
	sudo cp conf.d/*.conf $(DEPLOY_DIR)/conf.d/ && \
	sudo cp filters/*.conf $(DEPLOY_DIR)/filters/ && \
	sudo chown -R root:root /etc/rsyslog.conf $(DEPLOY_DIR) && \
	sudo chmod 644 /etc/rsyslog.conf $(DEPLOY_DIR)/conf.d/*.conf $(DEPLOY_DIR)/filters/*.conf && \
	echo "Deployed. Run 'make validate' on host, then 'make restart'."

restart: ## Restart rsyslog service
	sudo systemctl restart rsyslog
	@sudo systemctl status rsyslog --no-pager

clean: ## Remove backup files
	rm -rf /tmp/rsyslog-backup-*

.DEFAULT_GOAL := help
