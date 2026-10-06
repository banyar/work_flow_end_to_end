package main

import (
	"encoding/json"

	"git.frontiir.net/sa-dev/rtdatacore/pkg/core/domain/entities"
	"git.frontiir.net/sa-dev/rtdatacore/pkg/core/ports/repositories"
)

// component identifies rt_web_ui's rows in pipeline_run_events, alongside
// remote_resolve_service, retry_worker and rtutil_consumer.
const component = "rt_web_ui"

// Event names written by rt_web_ui besides the gate events.
const (
	EventPayloadSubmitted = "payload_submitted"
	EventAPIAccepted      = "api_accepted"
	EventAPIRejected      = "api_rejected"
	EventAPIUnreachable   = "api_unreachable"
)

// Tracker records the run's state in pipeline_runs and its history in
// pipeline_run_events.
type Tracker interface {
	// Open creates the run without a state; the first transition (to
	// NOT_ELIGIBLE or SUBMITTED_TO_API) reads "" -> <state>.
	Open(run *entities.PipelineRun) error
	Transition(runID string, to entities.PipelineState, event, reason string, detail any) error
	// AppendEvent records an event without changing the run's state.
	AppendEvent(runID, event string, detail any) error
	CurrentState(runID string) (entities.PipelineState, error)
}

type repoTracker struct {
	repo repositories.PipelineRunRepository
}

func NewRepoTracker(repo repositories.PipelineRunRepository) Tracker {
	return &repoTracker{repo: repo}
}

func (t *repoTracker) Open(run *entities.PipelineRun) error {
	run.CurrentState = ""
	return t.repo.Create(run)
}

func (t *repoTracker) Transition(runID string, to entities.PipelineState, event, reason string, detail any) error {
	return t.repo.TransitionState(repositories.TransitionInput{
		RunID:           runID,
		ToState:         to,
		Event:           event,
		Component:       component,
		LastStateReason: reason,
		Detail:          marshalDetail(detail),
	})
}

func (t *repoTracker) AppendEvent(runID, event string, detail any) error {
	return t.repo.AppendEvent(repositories.EventInput{
		RunID:     runID,
		Event:     event,
		Component: component,
		Detail:    marshalDetail(detail),
	})
}

func (t *repoTracker) CurrentState(runID string) (entities.PipelineState, error) {
	run, err := t.repo.GetByRunID(runID)
	if err != nil {
		return "", err
	}
	return run.CurrentState, nil
}

func marshalDetail(detail any) json.RawMessage {
	if detail == nil {
		return nil
	}
	b, err := json.Marshal(detail)
	if err != nil {
		return nil // detail is best-effort audit data
	}
	return b
}
