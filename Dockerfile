# Stage 1: Build the Go binaries
FROM registry.access.redhat.com/ubi9/go-toolset:1.23 AS builder

WORKDIR /app

# Ensure root user for builder stage
USER root

# Install migrate CLI (version 4.17.0) for linux/amd64
RUN mkdir -p bin tmp && \
    curl -L https://github.com/golang-migrate/migrate/releases/download/v4.17.0/migrate.linux-amd64.tar.gz -o tmp/migrate.tar.gz && \
    tar -xvf tmp/migrate.tar.gz -C tmp && \
    mv tmp/migrate bin/migrate && \
    chmod +x bin/migrate && \
    rm -rf tmp

# Copy go.mod and go.sum to cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source files
COPY main.go ./
COPY scripts/ ./scripts/
COPY migrations/ ./migrations/

# Build project binaries
RUN go build -o bin/metering-collector main.go
RUN go build -o bin/create_daily_partitions scripts/create_daily_partitions.go
RUN go build -o bin/delete_old_partitions scripts/delete_old_partitions.go

# Stage 2: Create minimal runtime image
FROM registry.access.redhat.com/ubi9-minimal

WORKDIR /app

# Copy binaries from builder stage
COPY --from=builder /app/bin/metering-collector /app/bin/metering-collector
COPY --from=builder /app/bin/create_daily_partitions /app/bin/create_daily_partitions
COPY --from=builder /app/bin/delete_old_partitions /app/bin/delete_old_partitions
COPY --from=builder /app/bin/migrate /app/bin/migrate

# Copy migration files for potential use
COPY --from=builder /app/migrations /app/migrations

# Install dependencies for PostgreSQL client (for migrations)
RUN microdnf install -y postgresql && microdnf clean all

# Expose port for metering-collector
EXPOSE 8080

# Default entrypoint for metering-collector
ENTRYPOINT ["/app/bin/metering-collector"]
