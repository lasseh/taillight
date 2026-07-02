.PHONY: up down build logs ps help test lint api-test api-lint frontend-dev rsyslog-test rsyslog-reload psql python-test python-lint release

# Remote that release tags are pushed to.
RELEASE_REMOTE ?= origin

##@ General
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Stack
up: ## Start all services (creates api/config.yml from the example on first run)
	@test -f api/config.yml || { cp api/config.yml.example api/config.yml && echo "created api/config.yml from config.yml.example"; }
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
	$(MAKE) -C frontend test

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

##@ Release
release: ## Cut a release (prompts for vX.Y.Z; tests, tags, pushes — CI builds binaries, release, and image)
	@if [ -n "$$(git status --porcelain)" ]; then echo "error: working tree not clean — commit or stash first"; exit 1; fi
	@branch=$$(git rev-parse --abbrev-ref HEAD); \
	last=$$(git tag -l 'v*' --sort=-v:refname | head -n1); \
	echo "Branch:       $$branch"; \
	echo "Last release: $${last:-<none>}"; \
	printf "New release version (e.g. v1.2.3): "; \
	read version; \
	case "$$version" in \
		v[0-9]*.[0-9]*.[0-9]*) ;; \
		*) echo "error: version must look like vX.Y.Z"; exit 1 ;; \
	esac; \
	if git rev-parse "$$version" >/dev/null 2>&1; then echo "error: tag $$version already exists"; exit 1; fi; \
	if ! grep -q "^## \[$$version\]" CHANGELOG.md; then \
		echo "error: CHANGELOG.md has no '## [$$version]' section — move the [Unreleased] entries first"; exit 1; \
	fi; \
	echo "==> running tests"; \
	$(MAKE) test || exit 1; \
	echo "==> tagging $$version and pushing to $(RELEASE_REMOTE)"; \
	git tag -a "$$version" -m "Release $$version" || exit 1; \
	git push $(RELEASE_REMOTE) "$$version" || exit 1; \
	echo "==> pushed $$version — release.yml now builds binaries, the GitHub release, and the Docker image"

.DEFAULT_GOAL := help
