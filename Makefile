.PHONY: build clean test run-sensor run-edge run-cloud all-tests install-deps

# Build directory
BIN_DIR=bin
LOG_DIR=logs

# Build all binaries
build:
	@echo "Building all components..."
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/sensor ./cmd/sensor
	go build -o $(BIN_DIR)/edge ./cmd/edge
	go build -o $(BIN_DIR)/cloud ./cmd/cloud
	@echo "Build complete!"

# Install dependencies
install-deps:
	@echo "Installing Go dependencies..."
	go mod download
	go mod tidy

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -rf $(BIN_DIR)
	rm -rf $(LOG_DIR)
	@echo "Clean complete!"

# Run sensor
run-sensor:
	@mkdir -p $(BIN_DIR)
	@if [ ! -f $(BIN_DIR)/sensor ]; then $(MAKE) build; fi
	./$(BIN_DIR)/sensor

# Run edge node
run-edge:
	@mkdir -p $(BIN_DIR)
	@if [ ! -f $(BIN_DIR)/edge ]; then $(MAKE) build; fi
	./$(BIN_DIR)/edge

# Run cloud processor
run-cloud:
	@mkdir -p $(BIN_DIR)
	@if [ ! -f $(BIN_DIR)/cloud ]; then $(MAKE) build; fi
	./$(BIN_DIR)/cloud

# Make test scripts executable
setup-scripts:
	chmod +x scripts/*.sh

# Run all tests
all-tests: build setup-scripts
	@mkdir -p $(LOG_DIR)
	@echo "Running all tests..."
	@echo ""
	./scripts/test1_scalability.sh
	@echo ""
	./scripts/test2_latency.sh
	@echo ""
	./scripts/test3_edge_failure.sh
	@echo ""
	./scripts/test4_noise_filtering.sh
	@echo ""
	./scripts/test5_resource_usage.sh

# Run individual tests
test1: build setup-scripts
	@mkdir -p $(LOG_DIR)
	./scripts/test1_scalability.sh

test2: build setup-scripts
	@mkdir -p $(LOG_DIR)
	./scripts/test2_latency.sh

test3: build setup-scripts
	@mkdir -p $(LOG_DIR)
	./scripts/test3_edge_failure.sh

test4: build setup-scripts
	@mkdir -p $(LOG_DIR)
	./scripts/test4_noise_filtering.sh

test5: build setup-scripts
	@mkdir -p $(LOG_DIR)
	./scripts/test5_resource_usage.sh

