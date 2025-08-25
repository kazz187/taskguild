# TaskGuild Makefile
.PHONY: build build-mcp clean test lint fmt vet deps install run help all gen-proto

# Variables
BINARY_NAME=taskguild
MCP_BINARY_NAME=mcp-taskguild
BUILD_DIR=bin
CMD_DIR=./cmd/taskguild
MCP_CMD_DIR=./cmd/mcp-taskguild

# Build info
VERSION := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

# Go build flags
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildTime=$(BUILD_TIME)"
BUILD_FLAGS := -trimpath $(LDFLAGS)

# Default target
all: fmt vet test build build-mcp

# Help target
help: ## Show this help message
	@echo "TaskGuild Build System"
	@echo ""
	@echo "Available targets:"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# Build the main binary
build: ## Build the taskguild binary
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	@go build $(BUILD_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_DIR)
	@echo "Built $(BUILD_DIR)/$(BINARY_NAME)"

# Build the MCP server binary
build-mcp: ## Build the mcp-taskguild binary
	@echo "Building $(MCP_BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	@go build $(BUILD_FLAGS) -o $(BUILD_DIR)/$(MCP_BINARY_NAME) $(MCP_CMD_DIR)
	@echo "Built $(BUILD_DIR)/$(MCP_BINARY_NAME)"

# Build for multiple platforms
build-all: ## Build for multiple platforms (Linux, macOS, Windows)
	@echo "Building for multiple platforms..."
	@mkdir -p $(BUILD_DIR)
	
	@echo "Building taskguild for Linux/amd64..."
	@GOOS=linux GOARCH=amd64 go build $(BUILD_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 $(CMD_DIR)
	@GOOS=linux GOARCH=amd64 go build $(BUILD_FLAGS) -o $(BUILD_DIR)/$(MCP_BINARY_NAME)-linux-amd64 $(MCP_CMD_DIR)
	
	@echo "Building taskguild for Linux/arm64..."
	@GOOS=linux GOARCH=arm64 go build $(BUILD_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 $(CMD_DIR)
	@GOOS=linux GOARCH=arm64 go build $(BUILD_FLAGS) -o $(BUILD_DIR)/$(MCP_BINARY_NAME)-linux-arm64 $(MCP_CMD_DIR)
	
	@echo "Building taskguild for macOS/amd64..."
	@GOOS=darwin GOARCH=amd64 go build $(BUILD_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 $(CMD_DIR)
	@GOOS=darwin GOARCH=amd64 go build $(BUILD_FLAGS) -o $(BUILD_DIR)/$(MCP_BINARY_NAME)-darwin-amd64 $(MCP_CMD_DIR)
	
	@echo "Building taskguild for macOS/arm64..."
	@GOOS=darwin GOARCH=arm64 go build $(BUILD_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 $(CMD_DIR)
	@GOOS=darwin GOARCH=arm64 go build $(BUILD_FLAGS) -o $(BUILD_DIR)/$(MCP_BINARY_NAME)-darwin-arm64 $(MCP_CMD_DIR)
	
	@echo "Building taskguild for Windows/amd64..."
	@GOOS=windows GOARCH=amd64 go build $(BUILD_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe $(CMD_DIR)
	@GOOS=windows GOARCH=amd64 go build $(BUILD_FLAGS) -o $(BUILD_DIR)/$(MCP_BINARY_NAME)-windows-amd64.exe $(MCP_CMD_DIR)
	
	@echo "Build complete. Binaries in $(BUILD_DIR)/"

# Install dependencies
deps: ## Download and install dependencies
	@echo "Installing dependencies..."
	@go mod download
	@go mod tidy

# Run tests
test: ## Run all tests
	@echo "Running tests..."
	@go test -v -race -cover ./...

# Run tests with coverage
test-coverage: ## Run tests with coverage report
	@echo "Running tests with coverage..."
	@go test -v -race -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run specific package tests
test-pkg: ## Run tests for specific package (usage: make test-pkg PKG=internal/task)
	@if [ -z "$(PKG)" ]; then echo "Usage: make test-pkg PKG=internal/task"; exit 1; fi
	@echo "Running tests for $(PKG)..."
	@go test -v -race -cover ./$(PKG)/...

# Format code
fmt: ## Format Go code
	@echo "Formatting code..."
	@go fmt ./...

# Run vet
vet: ## Run go vet
	@echo "Running go vet..."
	@go vet ./...

# Run linter (requires golangci-lint)
lint: ## Run golangci-lint (install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	@echo "Running golangci-lint..."
	@golangci-lint run

# Install the binaries
install: build build-mcp ## Install both binaries to $GOPATH/bin
	@echo "Installing $(BINARY_NAME) to $$GOPATH/bin..."
	@go install $(BUILD_FLAGS) $(CMD_DIR)
	@echo "Installing $(MCP_BINARY_NAME) to $$GOPATH/bin..."
	@go install $(BUILD_FLAGS) $(MCP_CMD_DIR)

# Run the application
run: ## Run the application with arguments (usage: make run ARGS="agent list")
	@go run $(CMD_DIR) $(ARGS)

# Clean build artifacts
clean: ## Clean build artifacts
	@echo "Cleaning build artifacts..."
	@rm -rf $(BUILD_DIR)
	@rm -f coverage.out coverage.html

# Generate protobuf files
gen-proto: ## Generate protobuf files using buf
	@echo "Generating protobuf files..."
	@pushd proto && go tool buf generate && popd
	@echo "Protobuf generation complete"

# Check for security vulnerabilities
security: ## Check for security vulnerabilities (requires gosec)
	@echo "Running security check..."
	@gosec ./...

# Update dependencies
update-deps: ## Update all dependencies to latest versions
	@echo "Updating dependencies..."
	@go get -u ./...
	@go mod tidy

# Docker build
docker-build: ## Build Docker image
	@echo "Building Docker image..."
	@docker build -t $(BINARY_NAME):$(VERSION) .

# Create release archives
release: build-all ## Create release archives
	@echo "Creating release archives..."
	@mkdir -p release
	
	@tar -czf release/$(BINARY_NAME)-$(VERSION)-linux-amd64.tar.gz -C $(BUILD_DIR) $(BINARY_NAME)-linux-amd64
	@tar -czf release/$(BINARY_NAME)-$(VERSION)-linux-arm64.tar.gz -C $(BUILD_DIR) $(BINARY_NAME)-linux-arm64
	@tar -czf release/$(BINARY_NAME)-$(VERSION)-darwin-amd64.tar.gz -C $(BUILD_DIR) $(BINARY_NAME)-darwin-amd64
	@tar -czf release/$(BINARY_NAME)-$(VERSION)-darwin-arm64.tar.gz -C $(BUILD_DIR) $(BINARY_NAME)-darwin-arm64
	@zip -j release/$(BINARY_NAME)-$(VERSION)-windows-amd64.zip $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe
	
	@echo "Release archives created in release/"

# Development setup
dev-setup: ## Setup development environment
	@echo "Setting up development environment..."
	@go mod download
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@go install github.com/vektra/mockery/v2@latest
	@go install github.com/securecodewarrior/gosec/v2/cmd/gosec@latest
	@echo "Development environment setup complete"

# Show build info
info: ## Show build information
	@echo "Build Information:"
	@echo "  Binary Name: $(BINARY_NAME)"
	@echo "  Version:     $(VERSION)"
	@echo "  Commit:      $(COMMIT)"
	@echo "  Build Time:  $(BUILD_TIME)"
	@echo "  Go Version:  $(shell go version)"

# Quick development build and run
dev: fmt vet ## Quick development build and run (usage: make dev ARGS="agent list")
	@go run $(CMD_DIR) $(ARGS)

# Integration tests (placeholder)
test-integration: ## Run integration tests
	@echo "Running integration tests..."
	@go test -v -tags=integration ./...

# Benchmark tests
bench: ## Run benchmark tests
	@echo "Running benchmark tests..."
	@go test -bench=. -benchmem ./...

# Check if .taskguild directory exists and clean it
clean-config: ## Clean .taskguild configuration directory
	@echo "Cleaning .taskguild configuration..."
	@rm -rf .taskguild

# Initialize development config
init-config: clean-config ## Initialize development configuration
	@echo "Initializing development configuration..."
	@$(BUILD_DIR)/$(BINARY_NAME) agent list > /dev/null 2>&1 || true
	@echo "Development configuration initialized"

# Full development cycle
dev-full: clean deps fmt vet test build build-mcp init-config ## Full development cycle: clean, deps, format, vet, test, build, init

# Show current configuration
show-config: build ## Show current taskguild configuration
	@echo "Current TaskGuild Configuration:"
	@$(BUILD_DIR)/$(BINARY_NAME) agent list || echo "No agents configured"