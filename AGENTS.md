# AGENTS.md

This file provides guidance to agents when working with code in this repository.

## Build/Test Commands

### Build
- `make build` - Builds binary to `bin/openai-proxy`
- `CGO_ENABLED=0 go build -o bin/openai-proxy -v cmd/openai-proxy/main.go`

### Test
- `make test` or `go test -v ./...` - Run all tests
- `go test -v -run TestName ./path/to/package` - Run single test
- `go test -v -run TestTranslationWorkflow ./internal/workflows` - Example: specific workflow test

### Lint
- `golangci-lint run` - Run all linters
- Linters include: errcheck, govet, staticcheck, revive, goconst, gocyclo, lll, ineffassign, unused, whitespace, bodyclose

### Buf/Protobuf Commands
- `buf lint` - Lint protobuf files in `proto/` directory
- `buf format -w` - Format protobuf files
- `buf generate` - Generate Go code from proto files (outputs to `pkg/proto/`)
- `buf breaking --against '.git#branch=main'` - Check for breaking changes
- `buf mod update` - Update buf dependencies (googleapis, protoc-gen-validate)

## Project Architecture

### Core Framework
- Uses `butterfly.orx.me/core` framework with custom app abstraction
- Entry point: `cmd/openai-proxy/main.go` passes `Router` function and `InitFunc` callbacks to `core.New()`
- Router: `internal/handler/handler.go` defines all HTTP routes using Gin
- Config: `internal/config/config.go` - Global config via `config.Conf` singleton

### Package Structure
- `internal/handler/` - HTTP handlers and routing
- `internal/vendor/` - API vendor management (OpenAI, SiliconFlow, OpenRouter)
- `internal/mcp/` - MCP (Model Context Protocol) server management
- `internal/workflows/` - AI workflows using Firebase Genkit
- `internal/config/` - Configuration types and loading
- `proto/` - Protobuf definitions for management API
- `pkg/proto/` - Generated Go code from protobuf (excluded from linting)

## Protobuf API Management

### Overview
- Use Buf CLI for protobuf management (buf.yaml, buf.gen.yaml)
- Proto files located in `proto/` directory
- Generated Go code outputs to `pkg/proto/`

### Buf Configuration
- `buf.yaml` - Module config with STANDARD lint rules, FILE breaking change detection
- Dependencies: `buf.build/googleapis/googleapis`, `buf.build/envoyproxy/protoc-gen-validate`
- `buf.gen.yaml` - Code generation plugins:
  - `buf.build/grpc/go` - gRPC Go stubs
  - `buf.build/grpc-ecosystem/gateway` - gRPC-Gateway (REST→gRPC)
  - `buf.build/connectrpc/go` - ConnectRPC Go client/server
  - `buf.build/protocolbuffers/go` - Protobuf Go messages
  - `buf.build/bufbuild/validate-go` - Validation rules
  - `protoc-gen-twirp` (local) - Twirp RPC framework

### Protobuf Style Guidelines
- Use `buf lint` to validate proto files before committing
- Follow STANDARD lint rules (service names, package naming, etc.)
- Use `validate` annotations for request validation
- Package naming: use lowercase with dots (e.g., `aiproxy.v1`)
- Service naming: PascalCase with `Service` suffix (e.g., `VendorService`)
- RPC naming: PascalCase verb+noun (e.g., `ListVendors`, `GetVendor`)

### Workflow
1. Define proto in `proto/` directory
2. Run `buf lint` to validate
3. Run `buf generate` to generate Go code
4. Import generated code from `pkg/proto/`
5. Check breaking changes: `buf breaking --against '.git#branch=main'`

## Code Style Guidelines

### Imports
- Group imports: standard library, then external packages, then internal packages
- Use import aliases for clarity: `oai "github.com/firebase/genkit/go/plugins/compat_oai"`
- Example order:
```go
import (
    "context"
    "fmt"
    "net/http"
    
    "butterfly.orx.me/core/log"
    "github.com/gin-gonic/gin"
    "github.com/openai/openai-go"
    
    "github.com/orvice/aiproxy/internal/config"
)
```

### Formatting
- Line length limit: 160 characters (configured in .golangci.yml)
- Use `gofmt` and `goimports` for formatting
- Multi-line function calls align arguments
- Prefer `strings.TrimRight(s, "/")` over `strings.TrimSuffix` for multiple chars

### Types and Structs
- Exported types: PascalCase (e.g., `VendorManager`, `TranslationOutput`)
- Unexported types/fields: camelCase (e.g., `validKeys`, `modelsCache`)
- Struct tags: use snake_case for YAML/JSON (e.g., `yaml:"googleAIAPIKey"`)
- Type aliases for clarity: `type VendorType string`
- Constants grouped with iota or as typed constants:
```go
const (
    VendorTypeOpenAI      VendorType = "openai"
    VendorTypeSiliconFlow VendorType = "siliconflow"
)
```

### Naming Conventions
- Variables: camelCase (e.g., `vendorManager`, `mcpManager`)
- Functions: PascalCase for exported, camelCase for private
- Interfaces: noun or verb + "er" (e.g., `Manager`, `Vender`)
- Receivers: short, typically 1-2 letters (e.g., `v *Vender`, `m *Manager`)
- Error variables: `Err` prefix (e.g., `ErrEmptyInput`, `ErrUnsupportedLanguage`)
- Mock/test helpers: `testing` prefix (e.g., `testingGenkit`)

### Error Handling
- Wrap errors with context: `fmt.Errorf("failed to parse vendor host: %w", err)`
- Use sentinel errors for validation: `var ErrEmptyInput = errors.New("input text cannot be empty")`
- Check errors with `errors.Is()` for comparison
- Log errors with structured logging: `slog.Error("message", "key", value, "error", err)`
- Return errors early, don't continue after error
- Use defer for cleanup: `defer res.Body.Close()`

### Logging
- Use structured logging with `slog` or `log.FromContext(ctx)`
- Context-aware logging pattern:
```go
logger := log.FromContext(ctx)
logger.Info("message", "key", value)
```
- Include relevant context in logs (vendor, model, method, path)
- Use `slog.Debug` for verbose logs, `slog.Info` for important events
- Mask sensitive data: `maskKey(key)` shows `abcd...xyz4` format

### Testing
- Use `t.Parallel()` for tests that can run concurrently
- Table-driven tests pattern:
```go
tests := []struct {
    name        string
    input       InputType
    wantErr     bool
    expectedErr error
}{
    {name: "case1", input: InputType{...}, wantErr: false},
}
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        // test body
    })
}
```
- Helper functions call `t.Helper()`
- Skip tests requiring external resources:
```go
if os.Getenv("OPENAI_KEY") == "" {
    t.Skip("OPENAI_KEY not set")
}
```
- Property-based tests using `testing/quick` for validation logic

### Concurrency
- Use `sync.RWMutex` for read-heavy concurrent access
- Lock pattern: `m.mutex.RLock(); defer m.mutex.RUnlock()`
- Global random source with mutex protection:
```go
var rng = rand.New(rand.NewSource(time.Now().UnixNano()))
var rngMutex sync.Mutex
```
- Goroutines for background tasks: `go v.periodicKeyCheck()`

### HTTP Handlers
- Use Gin framework: `func(c *gin.Context)`
- JSON responses: `c.JSON(http.StatusOK, gin.H{"key": value})`
- Error responses include JSON error objects
- Request binding: `c.ShouldBindBodyWithJSON(&req)`
- Middleware pattern for logging/tracing

### Configuration
- YAML configuration with snake_case field names
- Environment variables for secrets (e.g., `OPENAI_KEY`)
- Default fallbacks in config methods
- Config singleton: `config.Conf` initialized at startup

## AI Workflow Tests

Tests in `internal/workflows/` require environment variables:
- `OPENAI_PROVIDER` - Provider name
- `OPENAI_KEY` - API key
- `OPENAI_HOST` - API endpoint
- `OPENAI_MODEL` - Default model name

Tests skip automatically if env vars not set.

## Linting Configuration

Key settings in `.golangci.yml`:
- Line length: 160 chars (not standard 80)
- Cyclomatic complexity: 30 (very permissive)
- Tests excluded from linter (`tests: false`)
- Excluded paths: `pkg/ent`, `pkg/proto`
- Enabled linters: bodyclose, errcheck, govet, goconst, revive, staticcheck, ineffassign, unused

## OpenTelemetry Integration

- HTTP transport wrapped: `otelhttp.NewTransport(http.DefaultTransport)`
- Gin middleware: `otelgin.Middleware("aiproxy")`
- Span attributes for tracing: `span.SetAttributes(attribute.String("key", value))`
- Status codes: `span.SetStatus(codes.Error, message)`