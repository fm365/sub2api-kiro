-- Backfill ops_error_logs retry-capture columns for databases created before
-- 033_ops_monitoring_vnext.sql included these fields. The current INSERT path in
-- internal/repository/ops_repo.go requires these columns to exist; keep this
-- migration idempotent so upgraded installs can safely recover request-body
-- capture for Kiro/Claude Code upstream errors.

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

COMMENT ON COLUMN ops_error_logs.request_body IS 'Sanitized client request body captured for failed requests; used for ops troubleshooting and retry.';
COMMENT ON COLUMN ops_error_logs.request_headers IS 'Whitelisted client request headers captured for failed requests; excludes credentials.';
COMMENT ON COLUMN ops_error_logs.request_body_truncated IS 'Whether request_body was truncated before persistence.';
COMMENT ON COLUMN ops_error_logs.request_body_bytes IS 'Original request body size in bytes before sanitization/truncation.';
COMMENT ON COLUMN ops_error_logs.is_retryable IS 'Best-effort classification indicating whether the error can be retried.';
COMMENT ON COLUMN ops_error_logs.retry_count IS 'Number of retry attempts recorded for this ops error log entry.';
