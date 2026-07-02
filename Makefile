.PHONY: up down build logs ps help test lint api-test api-lint frontend-dev rsyslog-test rsyslog-reload psql python-test python-lint release

# Cross-compile matrix and remote for release binaries.
RELEASE_PLATFORMS ?= linux/amd64 linux/arm64 darwin/amd64 darwin/arm64
RELEASE_REMOTE    ?= origin

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
release: ## Cut a GitHub release (prompts for version; tags, cross-builds binaries, generates notes)
	@command -v gh >/dev/null 2>&1 || { echo "error: gh CLI not found (https://cli.github.com)"; exit 1; }
	@gh auth status >/dev/null 2>&1 || { echo "error: not logged in to GitHub (run: gh auth login)"; exit 1; }
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
	echo "==> running tests"; \
	$(MAKE) -C api test || exit 1; \
	echo "==> building release binaries for $$version"; \
	rm -rf dist && mkdir -p dist; \
	for platform in $(RELEASE_PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		echo "    taillight $$os/$$arch"; \
		(cd api && GOOS=$$os GOARCH=$$arch go build -trimpath \
			-ldflags="-X main.Version=$$version" \
			-o "../dist/taillight-$$version-$$os-$$arch" ./cmd/taillight) || exit 1; \
		echo "    taillight-shipper $$os/$$arch"; \
		(cd api && GOOS=$$os GOARCH=$$arch go build -trimpath \
			-o "../dist/taillight-shipper-$$version-$$os-$$arch" ./cmd/taillight-shipper) || exit 1; \
	done; \
	echo "==> tagging $$version and pushing to $(RELEASE_REMOTE)"; \
	git tag -a "$$version" -m "Release $$version" || exit 1; \
	git push $(RELEASE_REMOTE) "$$version" || exit 1; \
	echo "==> creating GitHub release"; \
	gh release create "$$version" dist/* --title "$$version" --generate-notes || exit 1; \
	echo "==> released $$version"

.DEFAULT_GOAL := help
