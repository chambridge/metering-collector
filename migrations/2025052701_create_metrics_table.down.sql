-- Drop trigger and function
DROP TRIGGER IF EXISTS trigger_update_daily_system_cpu_logical_count ON metrics;
DROP FUNCTION IF EXISTS update_daily_system_cpu_logical_count;

-- Drop partitioned tables
DROP TABLE IF EXISTS daily_system_cpu_logical_count CASCADE;
DROP TABLE IF EXISTS metrics CASCADE;

-- Drop timestamp_to_date function
DROP FUNCTION IF EXISTS timestamp_to_date(BIGINT);
