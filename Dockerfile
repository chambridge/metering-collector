# Stage 1: Build the Go binaries
FROM registry.access.redhat.com/ubi9/go-toolset:1.23 AS builder

WORKDIR /app

# Install migrate CLI (version 4.17.0) for amd64
RUN curl -L https://github.com/golang-migrate/migrate/releases/download/v4.17.0/migrate.linux-amd64.tar.gz | tar xvz && \
    mkdir bin && \
    mv migrate bin/migrate && \
    chmod +x bin/migrate

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



# Stage 1: Build the Go binaries
FROM registry.access.redhat.com/ubi9/go-toolset:1.23 AS builder

WORKDIR /app

# Define build arguments for platform and architecture
ARG TARGETPLATFORM
ARG TARGETARCH

# Install migrate CLI (version 4.17.0) based on platform and architecture
RUN case "${TARGETPLATFORM}/${TARGETARCH}" in \
        "linux/amd64") MIGRATE_URL="https://github.com/golang-migrate/migrate/releases/download/v4.17.0/migrate.linux-amd64.tar.gz" ;; \
        "linux/arm64") MIGRATE_URL="https://github.com/golang-migrate/migrate/releases/download/v4.17.0/migrate.linux-arm64.tar.gz" ;; \
        "darwin/amd64") MIGRATE_URL="https://github.com/golang-migrate/migrate/releases/download/v4.17.0/migrate.darwin-amd64.tar.gz" ;; \
        "darwin/arm64") MIGRATE_URL="https://github.com/golang-migrate/migrate/releases/download/v4.17.0/migrate.darwin-arm64.tar.gz" ;; \
        *) echo "Unsupported platform/arch: ${TARGETPLATFORM}/${TARGETARCH}" && exit 1 ;; \
    esac && \
    curl -L "${MIGRATE_URL}" | tar xvz && \
    mv migrate bin/migrate && \
    chmod +x bin/migrate

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