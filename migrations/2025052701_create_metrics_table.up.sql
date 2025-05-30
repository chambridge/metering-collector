-- Create a function to convert timestamp to date for partitioning
CREATE OR REPLACE FUNCTION timestamp_to_date(ts BIGINT) RETURNS DATE AS $$
BEGIN
    RETURN DATE_TRUNC('day', to_timestamp(ts / 1000.0));
END;
$$ LANGUAGE plpgsql IMMUTABLE;

-- Create partitioned metrics table
CREATE TABLE metrics (
    id SERIAL,
    name VARCHAR(255) NOT NULL,
    org_id VARCHAR(255) NOT NULL,
    labels JSONB NOT NULL,
    timestamp BIGINT NOT NULL,
    value DOUBLE PRECISION NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
) PARTITION BY RANGE (timestamp_to_date(timestamp));

-- Create partitioned daily_system_cpu_logical_count table
CREATE TABLE daily_system_cpu_logical_count (
    id SERIAL,
    system_id UUID NOT NULL,
    display_name TEXT NOT NULL,
    org_id TEXT NOT NULL,
    product TEXT NOT NULL,
    socket_count INTEGER NOT NULL,
    date DATE NOT NULL,
    total_uptime DOUBLE PRECISION NOT NULL DEFAULT 0,
    UNIQUE (system_id, date)
) PARTITION BY RANGE (date);

-- Create function to update daily_system_cpu_logical_count
CREATE OR REPLACE FUNCTION update_daily_system_cpu_logical_count()
RETURNS TRIGGER AS $$
BEGIN
    -- Process only system_cpu_logical_count metrics with value = 1
    IF NEW.name = 'system_cpu_logical_count' AND NEW.value = 1 THEN
        INSERT INTO daily_system_cpu_logical_count (
            system_id,
            display_name,
            org_id,
            product,
            socket_count,
            date,
            total_uptime
        )
        VALUES (
            (NEW.labels->>'_id')::UUID,
            NEW.labels->>'display_name',
            NEW.labels->>'external_organization',
            NEW.labels->>'product',
            (NEW.labels->>'socket_count')::INTEGER,
            TO_DATE(TO_CHAR(TO_TIMESTAMP(NEW.timestamp / 1000), 'YYYY-MM-DD'), 'YYYY-MM-DD'),
            0.1667 -- 10 minutes = 1/6 hour
        )
        ON CONFLICT (system_id, date)
        DO UPDATE SET
            total_uptime = daily_system_cpu_logical_count.total_uptime + 0.1667,
            display_name = EXCLUDED.display_name,
            org_id = EXCLUDED.org_id,
            product = EXCLUDED.product,
            socket_count = EXCLUDED.socket_count;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create trigger to call function after INSERT on metrics
CREATE TRIGGER trigger_update_daily_system_cpu_logical_count
AFTER INSERT ON metrics
FOR EACH ROW
EXECUTE FUNCTION update_daily_system_cpu_logical_count();
