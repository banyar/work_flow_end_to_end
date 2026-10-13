-- retry_jobs: the agreed target schema.
-- The database schema is this file: apply it to the RT database (DBA /
-- release step). No service creates, alters or checks the table (there is
-- no AutoMigrate).
-- rtdatacore's entities.RetryJob tags describe the same columns, so a
-- change here must also go into those tags.
--
-- One row per failed pipeline stage that the Retry Worker retries later. A
-- run (pipeline_runs.run_id) can have several jobs over time; the one being
-- retried now is linked from pipeline_runs.active_retry_job_id.
CREATE TABLE `retry_jobs` (
  `id`              BIGINT UNSIGNED   NOT NULL AUTO_INCREMENT
    COMMENT 'Internal auto-increment primary key; referenced by pipeline_runs.active_retry_job_id',
  `run_id`          CHAR(36)          NOT NULL
    COMMENT 'UUID of the pipeline run this job retries (pipeline_runs.run_id; logical reference, no foreign key)',
  `job_type`        VARCHAR(32)       NOT NULL
    COMMENT 'Stage to retry: NODE_RED_FETCH (CPE status fetch) or KAFKA_PUBLISH (publish the evaluated payload)',
  `ticket_id`       BIGINT UNSIGNED   NOT NULL
    COMMENT 'Related RT ticket ID (same as pipeline_runs.ticket_id)',
  `cpe_id`          VARCHAR(12)       DEFAULT NULL
    COMMENT 'CPE the retry is for',
  `request_payload` JSON              DEFAULT NULL
    COMMENT 'Everything the retry needs without in-memory context: the original RT request (NODE_RED_FETCH) or the evaluated payload (KAFKA_PUBLISH)',
  `last_error`      TEXT              DEFAULT NULL
    COMMENT 'Error of the most recent failed attempt; cleared when the job is resolved',
  `attempt_count`   TINYINT UNSIGNED  NOT NULL DEFAULT 0
    COMMENT 'Scheduled retry attempts made so far; never exceeds max_attempts',
  `max_attempts`    TINYINT UNSIGNED  NOT NULL
    COMMENT 'Attempts allowed before FAILED_PERMANENT: the number of entries in RETRY_WORKER_BACKOFF_SCHEDULE (default 3)',
  `status`          VARCHAR(20)       NOT NULL DEFAULT 'PENDING'
    COMMENT 'PENDING (waiting for next_retry_at), RETRYING (claimed by a worker), RESOLVED (retry succeeded), FAILED_PERMANENT (attempts exhausted)',
  `next_retry_at`   DATETIME(3)       NOT NULL
    COMMENT 'Earliest time a worker may claim the job, from the backoff schedule',
  `locked_by`       VARCHAR(100)      DEFAULT NULL
    COMMENT 'Worker holding the job as <hostname>-<pid>; NULL when not claimed',
  `locked_at`       DATETIME(3)       DEFAULT NULL
    COMMENT 'When locked_by claimed the job; a lock older than RETRY_WORKER_LOCK_TTL_MS (default 5m) can be reclaimed',
  `created_at`      DATETIME(3)       NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
    COMMENT 'Job creation timestamp',
  `updated_at`      DATETIME(3)       NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3)
    COMMENT 'Last update timestamp',
  `resolved_at`     DATETIME(3)       DEFAULT NULL
    COMMENT 'When the job reached RESOLVED; NULL otherwise',
  PRIMARY KEY (`id`),
  -- Jobs of one run.
  KEY `idx_retry_jobs_run_id` (`run_id`),
  -- The Retry Worker's claim query (status IN (PENDING, RETRYING) AND
  -- next_retry_at <= now ... FOR UPDATE): without it the SELECT scans, and
  -- so locks, the whole table.
  KEY `idx_retry_jobs_claim` (`status`, `next_retry_at`),
  CONSTRAINT `chk_retry_jobs_status`   CHECK (`status` IN ('PENDING','RETRYING','RESOLVED','FAILED_PERMANENT')),
  CONSTRAINT `chk_retry_jobs_job_type` CHECK (`job_type` IN ('NODE_RED_FETCH','KAFKA_PUBLISH')),
  CONSTRAINT `chk_retry_jobs_attempts` CHECK (`attempt_count` <= `max_attempts`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='Failed pipeline stages waiting to be retried by the Retry Worker';


  
