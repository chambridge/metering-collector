ifneq (,$(wildcard ./.env))
    include .env
    export
endif

# Makefile for metering-collector
.PHONY: all build build-container compose-up compose-down clean test

# Output directory for binaries
BINDIR := bin

# Container image name and tag
IMAGE_NAME := metering-collector
IMAGE_TAG := latest

# Ensure output directory exists
$(shell mkdir -p $(BINDIR))

# Build all binaries locally
build:
	@echo "Building metering-collector..."
	go build -o $(BINDIR)/metering-collector main.go
	@echo "Building create_daily_partitions..."
	go build -o $(BINDIR)/create_daily_partitions scripts/create_daily_partitions.go
	@echo "Building delete_old_partitions..."
	go build -o $(BINDIR)/delete_old_partitions scripts/delete_old_partitions.go

# Build container image
build-container:
	@echo "Building container image $(IMAGE_NAME):$(IMAGE_TAG)..."
	podman build -t $(IMAGE_NAME):$(IMAGE_TAG) -f Dockerfile .

# Start podman-compose services
compose-up: build-container
	@echo "Starting podman-compose services..."
	podman-compose -f podman-compose.yml up -d

# Stop and remove podman-compose services
compose-down:
	@echo "Stopping podman-compose services..."
	podman-compose -f podman-compose.yml down

# Run tests
test:
	go test ./...

# Clean up binaries and container images
clean:
	@echo "Cleaning up..."
	rm -rf $(BINDIR)
	podman rmi -f $(IMAGE_NAME):$(IMAGE_TAG) || true
	podman volume rm -f metering-collector_postgres_data || true