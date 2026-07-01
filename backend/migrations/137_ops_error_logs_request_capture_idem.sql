-- Idempotent safety migration for ops_error_logs request capture columns.
-- Some deployments may have skipped the earlier backfill while running a newer
-- binary that writes Kiro upstream request bodies/headers for Claude debugging.

ALTER TABLE ops_error_logs
  ADD COLUMN IF NOT EXISTS request_body JSONB;

ALTER TABLE ops_error_logs
  ADD COLUMN IF NOT EXISTS request_headers JSONB;

ALTER TABLE ops_error_logs
  ADD COLUMN IF NOT EXISTS request_body_truncated BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE ops_error_logs
  ADD COLUMN IF NOT EXISTS request_body_bytes INT;

ALTER TABLE ops_error_logs
  ADD COLUMN IF NOT EXISTS is_retryable BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE ops_error_logs
  ADD COLUMN IF NOT EXISTS retry_count INT NOT NULL DEFAULT 0;
