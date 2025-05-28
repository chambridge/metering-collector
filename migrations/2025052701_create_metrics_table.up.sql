-- Create a function to convert timestamp to date for partitioning
CREATE OR REPLACE FUNCTION timestamp_to_date(ts BIGINT) RETURNS DATE AS $$
BEGIN
    RETURN DATE_TRUNC('day', to_timestamp(ts / 1000.0));
END;
$$ LANGUAGE plpgsql IMMUTABLE;

-- Create partitioned table
CREATE TABLE metrics (
    id SERIAL,
    name VARCHAR(255) NOT NULL,
    org_id VARCHAR(255) NOT NULL,
    labels JSONB NOT NULL,
    timestamp BIGINT NOT NULL,
    value DOUBLE PRECISION NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (timestamp, id)
) PARTITION BY RANGE (timestamp_to_date(timestamp));
