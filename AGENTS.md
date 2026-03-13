# AGENTS.md

This file provides guidance to agents when working with code in this repository.

## Build/Test Commands
- Build: `make build` (outputs to `bin/openai-proxy`)
- Test all: `make test` or `go test -v ./...`
- Single test: `go test -v -run TestName ./path/to/package`

## Project-Specific Patterns

### Core Framework
- Uses `butterfly.orx.me/core` framework with custom app abstraction
- Entry point passes `Router` function and `InitFunc` callbacks to `core.New()`

### AI Workflow Tests
- Tests in `internal/workflows/` require environment variables:
  - `OPENAI_PROVIDER`, `OPENAI_KEY`, `OPENAI_HOST`, `OPENAI_MODEL`
- Tests skip automatically if env vars not set

### Linting Configuration
- Line length limit: 160 chars (not standard 80)
- Cyclomatic complexity threshold: 30 (very permissive)
- Tests excluded from linter (`tests: false` in golangci.yml)
