# metering-collector
Captures Prometheus remote write data into a Postgres database, so metering data can be processed locally without Prometheus.

## Features

- Receives Prometheus remote write data via an HTTP endpoint (`/receive`).
- Supports client certificate authentication (optional) or `ORG_ID` environment variable for local testing.
- Stores metrics in a PostgreSQL table partitioned by day (`timestamp`).
- Includes scripts to create daily partitions (90 days forward) and delete old partitions (older than 90 days).
- Uses Red Hat UBI9 images for building and running containers.
- Provides a `Makefile` for building binaries and managing containers.
- Includes a `podman-compose.yml` for local testing with PostgreSQL.

## Prerequisites

- **Go**: Version 1.18 or later.
- **Podman**: For containerized deployment and testing.
- **podman-compose**: For managing multi-container setups.
- **PostgreSQL**: Version 10 or later (for partitioning support).
- **migrate**: For applying database migrations (`go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest`).
- **host-metering**: Configured to send metrics to the application.

## Setup Instructions

### 1. Clone the Repository

```bash
git clone https://github.com/chambridge/metering-collector.git
cd metering-collector
```

### 2. Set Environment Variables

For local testing, set the following environment variables in your `.env` file:

```bash
export POSTGRES_USER=youruser
export POSTGRES_PASSWORD=yourpassword
export POSTGRES_DB=metering
export ORG_ID=org1
export POSTGRES_HOST=localhost
export POSTGRES_PORT=5432
export PORT=8080
```

For production with TLS, add:

```bash
export CA_CERT_PATH=/path/to/ca.crt
export SERVER_CERT_PATH=/path/to/server.crt
export SERVER_KEY_PATH=/path/to/server.key
```

### 3. Install Dependencies

Ensure Go dependencies are downloaded:

```bash
go mod tidy
```

Install the `migrate` tool for database migrations:

```bash
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
```

Install Podman and podman-compose:

```bash
sudo dnf install -y podman podman-compose
```

### 4. Build the Application

Use the `Makefile` to build binaries and the container image:

```bash
make build-container
```

This creates:
- `bin/metering-collector`: Main application.
- `bin/create_daily_partitions`: Partition creation script.
- `bin/delete_old_partitions`: Partition deletion script.
- Container image `metering-collector:latest`.

### 5. Apply Database Migrations

Apply the migration to create the partitioned `metrics` table:

```bash
make migrate-up
```

To roll back:

```bash
make migrate-down
```

### 6. Run Partition Scripts

Run the partition management scripts:

```bash
./bin/create_daily_partitions
./bin/delete_old_partitions
```

Alternatively, the `podman-compose.yml` includes a `partition-scripts` service that runs both scripts.

### 7. Run with Podman Compose

Start the services (PostgreSQL, metering-collector, partition-scripts):

```bash
make compose-up
```

This:
- Starts a PostgreSQL container.
- Runs the `partition-scripts` service to create/delete partitions (exits after completion).
- Starts the `metering-collector` service, listening on `http://localhost:8080/receive`.

Stop the services:

```bash
make compose-down
```

### 8. Configure host-metering

Configure the `host-metering` client to send metrics to:

```bash
export HOST_METERING_WRITE_URL=http://localhost:8080/receive
```

For production with TLS, use `https://<your-server>:8080/receive`.

### 9. Clean Up

Remove binaries and container images:

```bash
make clean
```

## Partition Management

- **Table Structure**: The `metrics` table is partitioned by `timestamp` (daily) using the `timestamp_to_date` function. Partitions are named `metrics_YYYYMMDD`.
- **Create Partitions**: The `create_daily_partitions` script creates partitions from today, to plus 90 days).
- **Delete Old Partitions**: The `delete_old_partitions` script drops partitions older than 90 days from the current date.
- **Automation**: Schedule partition scripts via cron:

```bash
0 0 * * * /path/to/metering-collector/bin/create_daily_partitions
0 0 * * * /path/to/metering-collector/bin/delete_old_partitions
```

Or use the containerized version:

```bash
0 0 * * * podman run --rm --network metering-network -e POSTGRES_HOST=postgres -e POSTGRES_PORT=5432 -e POSTGRES_USER=youruser -e POSTGRES_PASSWORD=yourpassword -e POSTGRES_DB=metering metering-collector:latest /bin/sh -c "/app/bin/create_daily_partitions && /app/bin/delete_old_partitions"
```

## Querying the Database

Query all partitions:

```sql
SELECT * FROM metrics WHERE org_id = 'org1' AND timestamp_to_date(timestamp) = '2025-05-27';
```

Query a specific partition:

```sql
SELECT * FROM metrics_20250527 WHERE org_id = 'org1';
```

## Troubleshooting

- **Dependency Issues**: If `go mod tidy` fails, verify the Prometheus version in `go.mod`. Use `go get github.com/prometheus/prometheus@latest` to fetch the latest version.
- **Container Issues**: Ensure Podman has access to `registry.access.redhat.com`.
- **Migration Issues**: Verify the `migrate` tool is installed and PostgreSQL is accessible.

## Contributing

Contributions are welcome! Please submit pull requests or open issues on the [GitHub repository](https://github.com/yourusername/metering-collector).

## License

This project is licensed under the MIT License.