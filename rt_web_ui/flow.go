package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"git.frontiir.net/sa-dev/rtdatacore/pkg/core/domain/entities"
)

// Outcome is what the RT Web UI shows for the ticket.
type Outcome string

const (
	OutcomeAccepted    Outcome = "ACCEPTED"     // 200: "Ticket Processing started"
	OutcomeNotEligible Outcome = "NOT_ELIGIBLE" // a gate failed: "No Process"
	OutcomeRejected    Outcome = "REJECTED"     // 4xx/5xx: error message
	OutcomeUnreachable Outcome = "UNREACHABLE"  // no HTTP answer
)

type Result struct {
	RunID      string  `json:"run_id"`
	TicketID   uint64  `json:"ticket_id"`
	Outcome    Outcome `json:"outcome"`
	State      string  `json:"state"`
	HTTPStatus int     `json:"http_status,omitempty"`
	Message    string  `json:"message"`
}

// Process runs final.html section 1 for one ticket: open the run, check
// the gates (NOT_ELIGIBLE on failure), send the payload
// (SUBMITTED_TO_API) and record the API's answer. On 200 the API itself has
// already moved the run to RECEIVED and may be further along, so only an
// event is added - writing a state here could roll the run back.
func Process(ctx context.Context, cfg Config, p *Payload, runID string, tr Tracker, api Submitter) (Result, error) {
	ticketID, err := p.TicketID()
	if err != nil {
		return Result{}, err
	}
	res := Result{RunID: runID, TicketID: ticketID}

	snapshot, _ := p.Body(ticketID, runID)
	if err := tr.Open(newRun(runID, ticketID, p, snapshot)); err != nil {
		return res, fmt.Errorf("open pipeline run: %w", err)
	}

	if gate := CheckEligibility(cfg, p); gate != nil {
		if err := tr.Transition(runID, entities.PipelineStateNotEligible, gate.Event, gate.Reason,
			map[string]any{"gate": gate.Gate}); err != nil {
			return res, fmt.Errorf("record %s: %w", entities.PipelineStateNotEligible, err)
		}
		res.Outcome, res.State, res.Message = OutcomeNotEligible, string(entities.PipelineStateNotEligible), "No Process: "+gate.Reason
		return res, nil
	}

	body, err := p.Body(ticketID, runID)
	if err != nil {
		return res, err
	}
	if err := tr.Transition(runID, entities.PipelineStateSubmittedToAPI, EventPayloadSubmitted, "",
		map[string]any{"url": api.URL(), "bytes": len(body)}); err != nil {
		return res, fmt.Errorf("record %s: %w", entities.PipelineStateSubmittedToAPI, err)
	}

	resp, err := api.Submit(ctx, body)
	switch {
	case err != nil:
		res.Outcome, res.Message = OutcomeUnreachable, "RT External API unreachable: "+err.Error()
		res.State, err = recordFailure(tr, runID, entities.PipelineStateAPIUnreachable, EventAPIUnreachable,
			res.Message, map[string]any{"error": err.Error()})
	case resp.StatusCode == http.StatusOK:
		res.Outcome, res.HTTPStatus, res.Message = OutcomeAccepted, resp.StatusCode, "Ticket Processing started"
		err = tr.AppendEvent(runID, EventAPIAccepted, map[string]any{"http_status": resp.StatusCode, "message": resp.Message})
		res.State = currentState(tr, runID)
	default:
		res.Outcome, res.HTTPStatus = OutcomeRejected, resp.StatusCode
		res.Message = fmt.Sprintf("RT External API returned %d: %s", resp.StatusCode, resp.Message)
		res.State, err = recordFailure(tr, runID, entities.PipelineStateAPIRejected, EventAPIRejected,
			res.Message, map[string]any{"http_status": resp.StatusCode, "message": resp.Message})
	}
	if err != nil {
		return res, fmt.Errorf("record API result: %w", err)
	}
	return res, nil
}

// recordFailure moves the run to a failure state only while it is still
// SUBMITTED_TO_API. If the API already took it over (e.g. a timeout after the
// API accepted), the failure is only logged as an event.
func recordFailure(tr Tracker, runID string, state entities.PipelineState, event, reason string, detail map[string]any) (string, error) {
	current, err := tr.CurrentState(runID)
	if err != nil {
		return "", err
	}
	if current != entities.PipelineStateSubmittedToAPI {
		return string(current), tr.AppendEvent(runID, event, detail)
	}
	return string(state), tr.Transition(runID, state, event, reason, detail)
}

func currentState(tr Tracker, runID string) string {
	s, err := tr.CurrentState(runID)
	if err != nil {
		return ""
	}
	return string(s)
}

// newRun mirrors the columns the RT External API writes at RECEIVED, so
// runs stopped by a gate are just as reportable.
func newRun(runID string, ticketID uint64, p *Payload, snapshot []byte) *entities.PipelineRun {
	return &entities.PipelineRun{
		RunID:             runID,
		TicketID:          ticketID,
		TicketNo:          p.CustomField("ticket_no"),
		ServiceArea:       p.CustomField("service_area"),
		Township:          p.CustomField("township"),
		TicketCreatedAt:   parseCreated(p.Field("created")),
		TicketProblem:     p.CustomField("ticket_problem"),
		TicketStatus:      p.Field("status"),
		CpeID:             p.CustomField("cpe_id"),
		LocalServiceID:    p.CustomField("local_service_id"),
		BeforeQueue:       p.Field("queue"),
		RtRequestSnapshot: json.RawMessage(snapshot),
	}
}

func parseCreated(s string) *time.Time {
	s = strings.TrimSpace(s)
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return &t
		}
	}
	return nil
}
