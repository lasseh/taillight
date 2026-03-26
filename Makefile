.PHONY: up down build logs ps help test lint api-test api-lint frontend-dev rsyslog-test rsyslog-reload psql python-test python-lint

##@ General
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Stack
up: ## Start all services
	docker compose up -d

down: ## Stop all services
	docker compose down

build: ## Build all containers
	docker compose build

logs: ## Tail logs from all services
	docker compose logs -f

ps: ## Show running services
	docker compose ps

##@ Quality
test: ## Run all tests
	$(MAKE) -C api test

lint: ## Lint all components
	$(MAKE) -C api lint
	$(MAKE) -C frontend lint

python-test: ## Run Python SDK tests
	$(MAKE) -C sdk/python test

python-lint: ## Lint Python SDK
	$(MAKE) -C sdk/python lint

##@ Components
api-test: ## Run API tests
	$(MAKE) -C api test

api-lint: ## Lint API
	$(MAKE) -C api lint

frontend-dev: ## Start frontend dev server
	$(MAKE) -C frontend dev

rsyslog-test: ## Validate rsyslog config
	$(MAKE) -C rsyslog test

rsyslog-reload: ## Rebuild and restart rsyslog container
	docker compose up -d --build rsyslog

psql: ## Connect to the database via psql
	docker compose exec postgres psql -U taillight -d taillight

##@ Verification
verify-features: ## Verify frontend/backend feature flags match
	@api=$$(grep -A3 '^features:' api/config.yml | grep -oE '(netlog|srvlog|applog): true' | sort); \
	 fe=$$(grep -oE '(netlog|srvlog|applog): true' frontend/src/config.ts | sort); \
	 if [ "$$api" != "$$fe" ]; then echo "Feature flags out of sync"; echo "api: $$api"; echo "fe: $$fe"; exit 1; fi
	@echo "Feature flags in sync"

.DEFAULT_GOAL := help
