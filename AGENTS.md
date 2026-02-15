# DB-Stream Development Guide for Agentic Coding

## Project Overview

**Preferred Backend Language**: Go (Golang)  
**Tech Stack**: Docker, Kafka, ClickHouse, Debezium, Spark  
**Architecture**: Event-driven CDC pipeline with real-time analytics  

## Rules
always do this on any code change
- After execution run linter (lint tool) install it if not already installed
- After linting run gofmt on the whole codebase

## Build Commands

### Prerequisites
```bash
# Ensure Go 1.21+ is installed
go version

# Ensure Docker and Docker Compose are installed
docker --version
docker-compose --version

# Install development tools
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
go install github.com/air-verse/air@latest  # Hot reload tool
```

### Core Commands
```bash
# Build all services
make build

# Run with hot reload (development)
make dev

# Run tests
make test

# Run specific test
make test-single TEST=./path/to/test/file_test.go

# Run tests with coverage
make test-coverage

# Lint code
make lint

# Format code
make fmt

# Run entire stack
make up

# Stop stack
make down

# View logs
make logs

# Clean up
make clean
```

### Individual Service Commands
```bash
# Build CDC service
go build -o bin/cdc ./cmd/cdc

# Build API service  
go build -o bin/api ./cmd/api

# Build processor service
go build -o bin/processor ./cmd/processor

# Run with live reload
air -c .air.toml

# Docker operations
docker-compose up -d kafka clickhouse
docker-compose up --build cdc-service
```

## Code Style Guidelines

### Import Organization
```go
// Standard library imports first
import (
    "context"
    "fmt"
    "log"
    "time"
)

// Third-party imports second
import (
    "github.com/confluentinc/confluent-kafka-go/v2/kafka"
    "github.com/gin-gonic/gin"
    "github.com/jackc/pgx/v5"
    "go.uber.org/zap"
)

// Local imports last
import (
    "github.com/your-org/db-stream/internal/config"
    "github.com/your-org/db-stream/internal/cdc"
    "github.com/your-org/db-stream/pkg/logger"
)
```

### Naming Conventions

**Packages**: Lowercase, single word, descriptive
- `internal/cdc` - CDC functionality
- `internal/storage` - Storage abstractions  
- `pkg/logger` - Shared logging package

**Variables**: camelCase, meaningful names
```go
var (
    kafkaProducer *kafka.Producer
    dbConnection  *pgx.Conn
    logger        *zap.Logger
)

// Local variables
userProfile := &models.User{}
retryCount := 0
isHealthy := true
```

**Functions**: PascalCase for exported, camelCase for unexported
```go
// Exported function
func ProcessCDCEvent(ctx context.Context, event *CDCEvent) error {
    // Implementation
}

// Unexported helper
func validateEvent(event *CDCEvent) bool {
    // Implementation
}
```

**Constants**: UPPER_SNAKE_CASE
```go
const (
    DEFAULT_KAFKA_TOPIC = "db-stream.cdc.events"
    MAX_RETRY_ATTEMPTS   = 3
    CONNECTION_TIMEOUT   = 30 * time.Second
)
```

**Interfaces**: End with "-er" suffix, describe behavior
```go
type EventProcessor interface {
    Process(ctx context.Context, event *CDCEvent) error
    Close() error
}

type DatabaseConnector interface {
    Connect(ctx context.Context) error
    Query(ctx context.Context, query string, args ...interface{}) ([]map[string]interface{}, error)
}
```

### Error Handling

**Always handle errors explicitly**:
```go
// Good - explicit error handling
user, err := GetUserByID(ctx, userID)
if err != nil {
    return fmt.Errorf("failed to get user %s: %w", userID, err)
}

// Bad - ignoring errors
user, _ := GetUserByID(ctx, userID) // NEVER do this
```

**Error wrapping with context**:
```go
// Wrap errors with additional context
if err := saveToDatabase(ctx, data); err != nil {
    return fmt.Errorf("database save failed for user %s: %w", data.UserID, err)
}

// Use custom error types for expected errors
type ValidationError struct {
    Field   string
    Message string
}

func (e ValidationError) Error() string {
    return fmt.Sprintf("validation error in field '%s': %s", e.Field, e.Message)
}
```

### Struct Design

**Export fields with JSON tags**:
```go
type CDCEvent struct {
    ID        string    `json:"id" db:"id"`
    Table     string    `json:"table" db:"table_name"`
    Operation string    `json:"operation" db:"operation"` // INSERT, UPDATE, DELETE
    Data      string    `json:"data" db:"data"`          // JSON payload
    Timestamp time.Time `json:"timestamp" db:"created_at"`
    
    // Unexported fields
    processed bool `json:"-"`
    retryCount int  `json:"-"`
}
```

**Use constructor functions**:
```go
func NewCDCEvent(table, operation, data string) *CDCEvent {
    return &CDCEvent{
        ID:        generateID(),
        Table:     table,
        Operation: operation,
        Data:      data,
        Timestamp: time.Now().UTC(),
    }
}
```

### Testing Guidelines

**Test naming**: TestName_Condition_ExpectedResult
```go
func TestCDCProcessor_ProcessEvent_ValidInsert_Success(t *testing.T) {
    // Test implementation
}

func TestCDCProcessor_ProcessEvent_InvalidOperation_ReturnsError(t *testing.T) {
    // Test implementation
}
```

**Table-driven tests**:
```go
func TestValidateEvent(t *testing.T) {
    tests := []struct {
        name     string
        event    *CDCEvent
        wantErr  bool
        errContains string
    }{
        {
            name: "valid insert event",
            event: &CDCEvent{Operation: "INSERT", Table: "users"},
            wantErr: false,
        },
        {
            name: "missing operation",
            event: &CDCEvent{Table: "users"},
            wantErr: true,
            errContains: "operation is required",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := ValidateEvent(tt.event)
            if tt.wantErr {
                require.Error(t, err)
                assert.Contains(t, err.Error(), tt.errContains)
            } else {
                assert.NoError(t, err)
            }
        })
    }
}
```

### Concurrency Patterns

**Use contexts for cancellation**:
```go
func ProcessEvents(ctx context.Context, events <-chan *CDCEvent) error {
    for {
        select {
        case event, ok := <-events:
            if !ok {
                return nil // Channel closed
            }
            if err := processSingleEvent(ctx, event); err != nil {
                return err
            }
        case <-ctx.Done():
            return ctx.Err()
        }
    }
}
```

**Use sync patterns properly**:
```go
type EventProcessor struct {
    mu       sync.RWMutex
    events   map[string]*CDCEvent
    workers  int
}

func (p *EventProcessor) AddEvent(event *CDCEvent) {
    p.mu.Lock()
    defer p.mu.Unlock()
    p.events[event.ID] = event
}

func (p *EventProcessor) GetEvent(id string) (*CDCEvent, bool) {
    p.mu.RLock()
    defer p.mu.RUnlock()
    event, exists := p.events[id]
    return event, exists
}
```

### Docker Guidelines

**Multi-stage builds**:
```dockerfile
# Build stage
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o main ./cmd/cdc

# Runtime stage
FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/main .
COPY --from=builder /app/configs ./configs
CMD ["./main"]
```

### Configuration

**Use environment variables + config files**:
```go
type Config struct {
    Kafka   KafkaConfig   `yaml:"kafka"`
    Database DatabaseConfig `yaml:"database"`
    Server  ServerConfig  `yaml:"server"`
}

func LoadConfig() (*Config, error) {
    cfg := &Config{}
    
    // Load from file
    if err := loadFromFile("config.yaml", cfg); err != nil {
        return nil, err
    }
    
    // Override with environment variables
    if kafkaBrokers := os.Getenv("KAFKA_BROKERS"); kafkaBrokers != "" {
        cfg.Kafka.Brokers = strings.Split(kafkaBrokers, ",")
    }
    
    return cfg, nil
}
```

## Development Workflow

1. **Feature development**: Create feature branch, use `make dev` for hot reload
2. **Testing**: Run `make test` before committing
3. **Code quality**: Run `make lint` and fix issues
4. **Integration testing**: Use `make up` to spin up dependencies
5. **Plan Reference**: Try to follow the phase wise implementation in `internal/docs/`
6. **Documentation**: Update relevant docs in `internal/docs/`

## Performance Guidelines

- Use connection pooling for database connections
- Implement batch processing for high-throughput operations
- Use structured logging with appropriate log levels
- Monitor memory usage with Go's pprof tools
- Implement circuit breakers for external service calls

## Security Considerations

- Never commit secrets or API keys
- Use environment variables for sensitive configuration
- Implement proper authentication and authorization
- Validate all external inputs
- Use TLS for all network communications