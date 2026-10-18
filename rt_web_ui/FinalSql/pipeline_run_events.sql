CREATE TABLE `pipeline_run_events` (
  `id` int(10) unsigned NOT NULL AUTO_INCREMENT COMMENT 'Internal auto-increment primary key; also orders events written in the same millisecond',
  `run_id` char(36) CHARACTER SET ascii COLLATE ascii_general_ci NOT NULL COMMENT 'UUID of the pipeline run (pipeline_runs.run_id; logical reference, no foreign key)',
  `from_state` varchar(40) DEFAULT NULL COMMENT 'State before the event; "" for the first transition of a run opened by rt_web_ui',
  `to_state` varchar(40) DEFAULT NULL COMMENT 'State after the event; equal to from_state when the event does not change the state',
  `event` varchar(45) DEFAULT NULL COMMENT 'What happened (e.g. payload_submitted, api_received, node_red_fetch_started, cpe_not_found, retry_attempt_failed)',
  `component` varchar(30) DEFAULT NULL COMMENT 'Writer: rt_web_ui, remote_resolve_service, retry_worker, publish_timeout_sweeper or rtutil_consumer',
  `detail` longtext CHARACTER SET utf8mb4 COLLATE utf8mb4_bin DEFAULT NULL COMMENT 'Event data as JSON (e.g. {"http_status":200}, {"error":"...","attempt":2,"max_attempts":3}); NULL when none' CHECK (json_valid(`detail`)),
  `occurred_at` datetime(3) DEFAULT NULL COMMENT 'When the event happened',
  PRIMARY KEY (`id`),
  KEY `idx_pipeline_run_events_run_occurred` (`run_id`,`occurred_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='Append-only history of state changes and events of each pipeline run'