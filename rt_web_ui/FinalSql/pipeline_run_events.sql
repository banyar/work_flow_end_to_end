-- pipeline_run_events: append-only history of every pipeline run. Each state
-- change, and each notable step that doesn't change the state (api_accepted,
-- a failed retry attempt, ...), adds one row; rows are never updated. The
-- run's current state is in pipeline_runs.
-- The database schema is this file: apply it to the RT database (DBA /
-- release step). No service creates, alters or checks the table (there is
-- no AutoMigrate).
-- rtdatacore's entities.PipelineRunEvent tags describe the same columns, so a
-- change here must also go into those tags.
CREATE TABLE `pipeline_run_events` (
  `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT
    COMMENT 'Internal auto-increment primary key; also orders events written in the same millisecond',
  `run_id` char(36) DEFAULT NULL
    COMMENT 'UUID of the pipeline run (pipeline_runs.run_id; logical reference, no foreign key)',
  `from_state` varchar(40) DEFAULT NULL
    COMMENT 'State before the event; "" for the first transition of a run opened by rt_web_ui',
  `to_state` varchar(40) DEFAULT NULL
    COMMENT 'State after the event; equal to from_state when the event does not change the state',
  `event` varchar(80) DEFAULT NULL
    COMMENT 'What happened (e.g. payload_submitted, api_received, node_red_fetch_started, cpe_not_found, retry_attempt_failed)',
  `component` varchar(64) DEFAULT NULL
    COMMENT 'Writer: rt_web_ui, remote_resolve_service, retry_worker, publish_timeout_sweeper or rtutil_consumer',
  `detail` longtext CHARACTER SET utf8mb4 COLLATE utf8mb4_bin DEFAULT NULL
    COMMENT 'Event data as JSON (e.g. {"http_status":200}, {"error":"...","attempt":2,"max_attempts":3}); NULL when none'
    CHECK (json_valid(`detail`)),
  `occurred_at` datetime(3) DEFAULT NULL
    COMMENT 'When the event happened',
  PRIMARY KEY (`id`),
  -- A run's timeline in order (pipeline_report's run detail, debugging).
  KEY `idx_pipeline_run_events_run_occurred` (`run_id`,`occurred_at`)
) ENGINE=InnoDB DEFAULT CHARSET=latin1 COLLATE=latin1_swedish_ci
  COMMENT='Append-only history of state changes and events of each pipeline run';
