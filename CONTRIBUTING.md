# Contributing to Taillight

Thank you for your interest in contributing to Taillight! This guide will help you get started.

## Prerequisites

- Go 1.24+
- Node.js 20+
- Docker and Docker Compose
- PostgreSQL with TimescaleDB (or use Docker Compose)

## Development Setup

### Backend (API)

```sh
cd api
cp config.yaml.example config.yaml  # fill in database credentials
make build
make test
make lint
```

### Frontend

```sh
cd frontend
npm install
npm run dev           # start dev server
npm run build         # validate production build
make lint             # type-check
```

### Full Stack (Docker Compose)

```sh
docker compose up -d
```

## Making Changes

1. Fork the repository and create a feature branch
2. Make your changes
3. Run tests and linting before committing:
   ```sh
   cd api && make test && make lint
   cd frontend && npm run build
   ```
4. Commit with a descriptive message (see below)
5. Push your branch and open a pull request

## Commit Conventions

Use the format `type: description` (lowercase, imperative mood, no trailing period):

| Type | Purpose |
|------|---------|
| `feat` | New feature |
| `fix` | Bug fix |
| `refactor` | Code restructuring (no behavior change) |
| `test` | Adding or updating tests |
| `docs` | Documentation changes |
| `chore` | Maintenance tasks |
| `ci` | CI/CD changes |
| `build` | Build system changes |

Examples:
- `feat: add user lookup endpoint`
- `fix: handle nil pointer in session middleware`
- `test: add auth handler tests`
- `feat(auth): add token refresh`

## Branch Naming

Use the format `type/short-description`:
- `feat/user-lookup`
- `fix/nil-pointer-crash`
- `docs/api-reference`

## Pull Request Process

1. Ensure all tests pass and linting is clean
2. Update documentation if your change affects public APIs or configuration
3. Keep PRs focused — one logical change per PR
4. Fill out the PR template with a summary and test plan
5. A maintainer will review and merge your PR

## Code Style

### Go
- Follow standard Go conventions
- Use `fmt.Errorf("context: %w", err)` for error wrapping
- Write table-driven tests
- Use `log/slog` for structured logging

### Frontend
- TypeScript strict mode
- Vue 3 Composition API with `<script setup>`
- Tailwind CSS for styling

## Questions?

Open an issue if you have questions about contributing.
