.PHONY: build run clean test install setup dev web-build web-dev

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GORUN=$(GOCMD) run
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

# Binary names
BINARY_NAME=deployerd
DEPLOYER_NAME=deployer

# Directories
BUILD_DIR=build
INSTALL_DIR=$(HOME)/deployer/bin
WEB_DIR=web

# Version
VERSION?=0.2.0
LDFLAGS=-ldflags "-X main.version=$(VERSION)"

# Build the frontend (clean build with static generation)
web-build:
	@echo "Building frontend..."
	@rm -rf $(WEB_DIR)/.output $(WEB_DIR)/.nuxt
	@cd $(WEB_DIR) && bun install && bunx nuxi generate

# Run frontend dev server
web-dev:
	@cd $(WEB_DIR) && bun run dev

# Build the server (includes frontend)
build: web-build
	@echo "Building deployer..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/deployerd
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(DEPLOYER_NAME) ./cmd/deployer

# Build Go only (skip frontend)
build-go:
	@echo "Building deployer (Go only)..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/deployerd
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(DEPLOYER_NAME) ./cmd/deployer

# Build for multiple platforms (includes frontend)
build-all: web-build
	@echo "Building for all platforms..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/deployerd
	GOOS=linux GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/deployerd
	GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/deployerd
	GOOS=darwin GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/deployerd

# Run the server
run:
	$(GORUN) ./cmd/deployerd

# Run with hot reload (requires air)
dev:
	@which air > /dev/null || go install github.com/air-verse/air@latest
	air

# Install dependencies
deps:
	$(GOMOD) download
	$(GOMOD) tidy

# Install to ~/deployer/bin
install: build
	@echo "Installing to $(INSTALL_DIR)..."
	@mkdir -p $(INSTALL_DIR)
	@cp $(BUILD_DIR)/$(BINARY_NAME) $(INSTALL_DIR)/
	@cp $(BUILD_DIR)/$(DEPLOYER_NAME) $(INSTALL_DIR)/
	@echo "Installed successfully!"
	@echo "Run setup with: $(INSTALL_DIR)/$(BINARY_NAME) --setup"

# Run setup
setup: install
	$(INSTALL_DIR)/$(BINARY_NAME) --setup

# Run tests
test:
	$(GOTEST) -v ./...

# Clean build artifacts
clean:
	@rm -rf $(BUILD_DIR)
	$(GOCMD) clean

# Format code
fmt:
	$(GOCMD) fmt ./...

# Lint code (requires golangci-lint)
lint:
	@which golangci-lint > /dev/null || go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	golangci-lint run

# Show help
help:
	@echo "Available targets:"
	@echo "  build      - Build frontend + backend"
	@echo "  build-go   - Build backend only (skip frontend)"
	@echo "  build-all  - Build for all platforms"
	@echo "  web-build  - Build frontend only"
	@echo "  web-dev    - Run frontend dev server"
	@echo "  run        - Run the server"
	@echo "  dev        - Run with hot reload"
	@echo "  deps       - Download dependencies"
	@echo "  install    - Install to ~/deployer/bin"
	@echo "  setup      - Install and run setup"
	@echo "  test       - Run tests"
	@echo "  clean      - Clean build artifacts"
	@echo "  fmt        - Format code"
	@echo "  lint       - Lint code"
