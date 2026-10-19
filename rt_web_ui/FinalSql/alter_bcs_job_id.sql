-- BCS async API (Peppermint): store the BCS job_id on pipeline_runs.
-- Brings an existing pipeline_runs table in line with pipeline_runs.sql; a
-- table created from pipeline_runs.sql already has both changes.
--
-- 1. ref_bcs_process_id holds the job_id, a UUID (36 characters). The old
--    varchar(30) column would reject it in strict mode, or silently cut it
--    to 30 characters otherwise, so the BCS webhook could never find the run.
-- 2. The BCS webhook and the BCS result sweeper look runs up by job_id.
--
-- Safe to run more than once (MariaDB 10.4: MODIFY is a no-op when the
-- column already matches, ADD INDEX IF NOT EXISTS skips an existing index).
-- Check the current definition first:
--   SHOW CREATE TABLE pipeline_runs;
--   SELECT MAX(CHAR_LENGTH(ref_bcs_process_id)) FROM pipeline_runs;

ALTER TABLE pipeline_runs
  MODIFY COLUMN `ref_bcs_process_id` varchar(36) DEFAULT ''
    COMMENT 'Peppermint BCS async job_id (UUID), set by rtutil when the BCS job is queued; '''' until then',
  ADD INDEX IF NOT EXISTS `idx_pipeline_runs_ref_bcs_process_id` (`ref_bcs_process_id`);




  ALTER TABLE pipeline_runs
  ADD active_ticket_id bigint unsigned
      GENERATED ALWAYS AS (IF(completed_at IS NULL, ticket_id, NULL)) STORED,
  ADD UNIQUE KEY uq_pipeline_runs_active_ticket (active_ticket_id);
