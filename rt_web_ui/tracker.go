package main

import (
	"git.frontiir.net/sa-dev/rtdatacore/pkg/core/domain/entities"
	"git.frontiir.net/sa-dev/rtdatacore/pkg/core/ports/repositories"
)

// repoRunReader reads pipeline_runs through rtdatacore; rt_web_ui only reads.
type repoRunReader struct {
	repo repositories.PipelineRunRepository
}

func NewRunReader(repo repositories.PipelineRunRepository) RunReader {
	return &repoRunReader{repo: repo}
}

func (r *repoRunReader) Run(runID string) (*entities.PipelineRun, error) {
	return r.repo.GetByRunID(runID)
}
