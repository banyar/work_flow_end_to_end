package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"git.frontiir.net/sa-dev/rtdatacore/pkg/core/domain/entities"
)

// Outcome is what the RT Web UI shows for the ticket.
type Outcome string

const (
	OutcomeAccepted    Outcome = "ACCEPTED"     // 202 with run_id: "Ticket Processing started"
	OutcomeNotEligible Outcome = "NOT_ELIGIBLE" // the API's eligibility rules turned the run away
	OutcomeSkipped     Outcome = "SKIPPED"      // 202 without run_id: active run or cooldown
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

// RunReader reads the run the RT External API created, to show its state.
type RunReader interface {
	Run(runID string) (*entities.PipelineRun, error)
}

// rulesWait is how long Process waits for the API to apply its eligibility
// rules to an admitted run. The API answers 202 first and runs the rules in
// the background; they take milliseconds, so a run still RECEIVED after this
// is only reported as such.
const rulesWait = 3 * time.Second

// Process sends one ticket the way the RT extension does: straight to the RT
// External API, which checks the ticket (cooldown, active run, eligibility
// rules), creates the run and makes its run_id. rt_web_ui writes nothing to
// pipeline_runs: a run row of its own would hold the ticket's active-run
// lock against the API, and only the API decides whether the ticket runs.
func Process(ctx context.Context, p *Payload, runs RunReader, api Submitter) (Result, error) {
	ticketID, err := p.TicketID()
	if err != nil {
		return Result{}, err
	}
	res := Result{TicketID: ticketID}

	body, err := p.Body(ticketID)
	if err != nil {
		return res, err
	}
	resp, err := api.Submit(ctx, body)
	switch {
	case err != nil:
		res.Outcome, res.Message = OutcomeUnreachable, "RT External API unreachable: "+err.Error()
	case resp.StatusCode == http.StatusAccepted && resp.RunID == "":
		res.Outcome, res.HTTPStatus = OutcomeSkipped, resp.StatusCode
		res.Message = "Not started: the ticket has an active run or was processed within the cooldown window (202 without run_id)"
	case resp.StatusCode == http.StatusAccepted:
		res.Outcome, res.HTTPStatus, res.RunID = OutcomeAccepted, resp.StatusCode, resp.RunID
		res.Message = "Ticket Processing started"
		if run := waitPastRules(ctx, runs, resp.RunID); run != nil {
			res.State = string(run.CurrentState)
			if run.CurrentState == entities.PipelineStateNotEligible {
				res.Outcome, res.Message = OutcomeNotEligible, "No Process: "+run.LastStateReason
			}
		}
	default:
		res.Outcome, res.HTTPStatus = OutcomeRejected, resp.StatusCode
		res.Message = fmt.Sprintf("RT External API returned %d: %s", resp.StatusCode, resp.Message)
	}
	return res, nil
}

// waitPastRules reads the API's run until it is past RECEIVED (the rules
// ran) or rulesWait is over; nil when the run cannot be read.
func waitPastRules(ctx context.Context, runs RunReader, runID string) *entities.PipelineRun {
	deadline := time.Now().Add(rulesWait)
	for {
		run, err := runs.Run(runID)
		if err != nil {
			return nil
		}
		if run.CurrentState != entities.PipelineStateReceived || time.Now().After(deadline) {
			return run
		}
		select {
		case <-ctx.Done():
			return run
		case <-time.After(100 * time.Millisecond):
		}
	}
}
