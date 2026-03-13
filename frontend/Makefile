.PHONY: install dev build preview lint test clean help

##@ General
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development
install: ## Install dependencies
	npm install

dev: ## Start dev server
	npm run dev

preview: ## Preview production build
	npm run preview

##@ Build
build: ## Build for production
	npm run build

##@ Quality
test: ## Run unit tests
	npm test

lint: ## Lint (type-check)
	npm run type-check

##@ Utility
clean: ## Clean build artifacts
	rm -rf dist node_modules

.DEFAULT_GOAL := help
