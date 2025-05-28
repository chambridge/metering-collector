-- Drop partitioned table and function
DROP TABLE IF EXISTS metrics CASCADE;
DROP FUNCTION IF EXISTS timestamp_to_date(BIGINT);
