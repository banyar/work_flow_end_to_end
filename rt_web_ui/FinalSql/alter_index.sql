ALTER TABLE pipeline_runs
  DROP INDEX idx_pipeline_runs_current_state,
  ADD INDEX idx_pipeline_runs_state_created (current_state, created_at);
