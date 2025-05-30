# metering-collector
Captures Prometheus remote write data into a PostgreSQL database, enabling local processing of metering data without Prometheus. Aggregates `system_cpu_logical_count` metrics into daily uptime summaries for querying via an API.

## Features

- Receives Prometheus remote write data via an HTTP endpoint (`/receive`).
- Stores raw metrics in a PostgreSQL `metrics` table, partitioned by day (`timestamp`).
- Aggregates `system_cpu_logical_count` metrics into a `daily_system_cpu_logical_count` table, partitioned by `date`, with `total_uptime` in hours.
- Provides an API endpoint (`/api/metering/v1/system_cpu_logical_count`) to query aggregated data with filtering and pagination (JSON or CSV output).
- Includes scripts to create daily partitions (90 days forward) and delete old partitions (older than 90 days) for both tables.
- Uses Red Hat UBI9 images for building and running containers.
- Provides a `Makefile` for building binaries and managing containers.
- Includes a `podman-compose.yml` for local testing with PostgreSQL.
- Health check endpoint (`/health`) for monitoring.

## Prerequisites

- **Go**: Version 1.23 or later.
- **Podman**: For containerized deployment and testing.
- **podman-compose**: For managing multi-container setups.
- **PostgreSQL**: Version 13 or later (for partitioning support).
- **migrate**: For applying database migrations (`go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest`).
- **host-metering**: Configured to send `system_cpu_logical_count` metrics to the application (https://github.com/RedHatInsights/host-metering).

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
export POSTGRES_HOST=localhost
export POSTGRES_PORT=5432
export PORT=8080
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
- `bin/create_daily_partitions`: Partition creation script for `metrics` and `daily_system_cpu_logical_count`.
- `bin/delete_old_partitions`: Partition deletion script for both tables.
- Container image `metering-collector:latest`.

### 5. Apply Database Migrations

Apply the migration to create the partitioned `metrics` and `daily_system_cpu_logical_count` tables:

```bash
make migrate-up
```

To roll back:

```bash
make migrate-down
```

### 6. Run Partition Scripts

Run the partition management scripts to create partitions for both tables:

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
- Starts the `metering-collector` service, listening on `http://localhost:8080` for `/receive`, `/health`, and `/api/metering/v1/system_cpu_logical_count`.

Stop the services:

```bash
make compose-down
```

### 8. Configure host-metering

Configure the `host-metering` client to send `system_cpu_logical_count` metrics every ~10 minutes to:

```bash
sudo subscription-manager register
sudo subscription-manager refresh
sudo um install -y host-metering
sudo systemctl edit host-metering.service

[Service]
Environment="LC_ALL=C.UTF-8"
Environment="HOST_METERING_WRITE_URL=http://<metering-collector-address>:8080/receive"

----

sudo setenforce 0  # Use Permissive mode for testing
sudo systemctl enable --now host-metering.service
```

### 9. Clean Up

Remove binaries and container images:

```bash
make clean
```

## API Endpoints

### GET /api/metering/v1/system_cpu_logical_count

Queries aggregated `system_cpu_logical_count` data from the `daily_system_cpu_logical_count` table.

- **Query Parameters**:
  - `system_id`: Exact match for system UUID (e.g., `ff3849ac-8a31-42f2-9d38-0acdaa15c271`).
  - `org_id`: Exact match for organization ID (e.g., `10000`).
  - `display_name`: Case-insensitive partial match (e.g., `myhost-04`).
  - `start_date`: Date filter (YYYY-MM-DD, default: first day of current month, e.g., `2025-05-01`).
  - `end_date`: Date filter (YYYY-MM-DD, default: current day, e.g., `2025-05-30`).
  - `limit`: Pagination limit (1 to 10,000, default: 100).
  - `offset`: Pagination offset (non-negative, default: 0).

- **Response Formats**:
  - **JSON** (default, `Accept: application/json` or omitted):
    ```json
    {
        "metadata": {
            "total": 1,
            "limit": 100,
            "offset": 0
        },
        "data": [
            {
                "id": 1,
                "system_id": "ff3849ac-8a31-42f2-9d38-0acdaa15c271",
                "display_name": "myhost-04",
                "org_id": "10000",
                "product": "69",
                "socket_count": 1,
                "date": "2025-05-30T00:00:00Z",
                "total_uptime": 0.3334
            }
        ]
    }
    ```
  - **CSV** (`Accept: text/csv`):
    ```
    id,system_id,display_name,org_id,product,socket_count,date,total_uptime
    1,ff3849ac-8a31-42f2-9d38-0acdaa15c271,myhost-04-guest10.lab.eng.rdu2.redhat.com,10000,69,1,2025-05-30,0.3334
    ```

- **Example**:
  ```bash
  curl "http://localhost:8080/api/metering/v1/system_cpu_logical_count?org_id=10000&display_name=myhost-04"
  curl -H "Accept: text/csv" "http://localhost:8080/api/metering/v1/system_cpu_logical_count?org_id=10000"
  ```

### GET /health

Checks the service and database connectivity.

- **Response**:
  ```json
  {"status": "ok"}
  ```

### POST /receive

Accepts Prometheus remote write data (snappy-compressed protobuf). Requires `external_organization` label for `org_id`.

## Database Schema

- **metrics**:
  - Partitioned by `timestamp` (daily) using `timestamp_to_date` function.
  - Columns: `id` (SERIAL), `name` (VARCHAR), `org_id` (VARCHAR), `labels` (JSONB), `timestamp` (BIGINT, ms), `value` (DOUBLE PRECISION), `created_at` (TIMESTAMP).
  - Partitions: `metrics_YYYYMMDD`.
  - Stores raw metrics from `host-metering` (e.g., `system_cpu_logical_count`).

- **daily_system_cpu_logical_count**:
  - Partitioned by `date` (daily).
  - Columns: `id` (SERIAL), `system_id` (UUID, from `labels._id`), `display_name` (TEXT, from `labels.display_name`), `org_id` (TEXT, from `labels.external_organization`), `product` (TEXT, from `labels.product`), `socket_count` (INTEGER, from `labels.socket_count`), `date` (DATE), `total_uptime` (DOUBLE PRECISION, hours).
  - Partitions: `daily_system_cpu_logical_count_YYYYMMDD`.
  - Aggregates `system_cpu_logical_count` metrics via a trigger, incrementing `total_uptime` by 0.1667 hours (10 minutes) per metric.
  - Unique constraint: `(system_id, date)`.

## Partition Management

- **Tables**:
  - `metrics`: Partitioned by `timestamp_to_date(timestamp)`, named `metrics_YYYYMMDD`.
  - `daily_system_cpu_logical_count`: Partitioned by `date`, named `daily_system_cpu_logical_count_YYYYMMDD`.
- **Create Partitions**: The `create_daily_partitions` script creates partitions for both tables (90 days forward).
- **Delete Old Partitions**: The `delete_old_partitions` script drops partitions older than 90 days.
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

### Via API

Use the `/api/metering/v1/system_cpu_logical_count` endpoint (recommended):

```bash
curl "http://localhost:8080/api/metering/v1/system_cpu_logical_count?org_id=10000&start_date=2025-05-01&end_date=2025-05-30"
```

### Via SQL

Query all partitions in `metrics`:

```sql
SELECT * FROM metrics WHERE org_id = '10000' AND timestamp_to_date(timestamp) = '2025-05-30';
```

Query a specific `metrics` partition:

```sql
SELECT * FROM metrics_20250530 WHERE org_id = '10000';
```

Query `daily_system_cpu_logical_count`:

```sql
SELECT * FROM daily_system_cpu_logical_count WHERE org_id = '10000' AND date = '2025-05-30';
```

Query a specific `daily_system_cpu_logical_count` partition:

```sql
SELECT * FROM daily_system_cpu_logical_count_20250530 WHERE org_id = '10000';
```

## Troubleshooting

- **Dependency Issues**: If `go mod tidy` fails, verify the Prometheus version in `go.mod`. Use `go get github.com/prometheus/prometheus@latest`.
- **Container Issues**: Ensure Podman has access to `registry.access.redhat.com` and `quay.io`.
- **Migration Issues**: Verify the `migrate` tool is installed and PostgreSQL is accessible.
- **API Issues**: Check query parameters and database connection (`curl -v` output, `podman logs collector`).
- **Metric Ingestion**: Ensure `host-metering` includes `external_organization` label (`sudo journalctl -u host-metering`).

## Contributing

Contributions are welcome! Please submit pull requests or open issues on the [GitHub repository](https://github.com/chambridge/metering-collector).

## License

This project is licensed under the MIT License.