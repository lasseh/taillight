.PHONY: test lint build clean help

##@ General
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Quality
test: ## Run tests
	python3 -m unittest discover -s tests -v

lint: ## Run ruff linter
	python3 -m ruff check src/ tests/

##@ Build
build: ## Build sdist and wheel
	python3 -m build

clean: ## Remove build artifacts
	rm -rf dist/ build/ src/*.egg-info

.DEFAULT_GOAL := help
