CREATE TABLE IF NOT EXISTS parser_last_runs (
    parser_name VARCHAR(255) PRIMARY KEY,
    last_run_timestamp TIMESTAMPTZ NOT NULL
);