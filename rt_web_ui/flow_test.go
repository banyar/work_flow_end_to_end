package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"git.frontiir.net/sa-dev/rtdatacore/pkg/core/domain/entities"
)

type fakeAPI struct {
	resp *APIResponse
	err  error
	body []byte
}

func (f *fakeAPI) Submit(_ context.Context, body []byte) (*APIResponse, error) {
	f.body = body
	return f.resp, f.err
}
func (f *fakeAPI) URL() string { return "http://api.test" }

type fakeRuns map[string]*entities.PipelineRun

func (f fakeRuns) Run(runID string) (*entities.PipelineRun, error) {
	if r, ok := f[runID]; ok {
		return r, nil
	}
	return nil, errors.New("not found")
}

func TestProcessSendsStraightToTheAPI(t *testing.T) {
	const runID = "6ad204d7-74f5-44c1-8ea2-bd32dc8adcb2"
	runs := fakeRuns{
		runID:   {RunID: runID, CurrentState: entities.PipelineStateFetchingCPEStatus},
		"gated": {RunID: "gated", CurrentState: entities.PipelineStateNotEligible, LastStateReason: "OPI customer"},
	}
	for _, tc := range []struct {
		name      string
		api       *fakeAPI
		outcome   Outcome
		wantRunID string
		state     string
	}{
		{"admitted", &fakeAPI{resp: &APIResponse{StatusCode: 202, RunID: runID}}, OutcomeAccepted, runID, "FETCHING_CPE_STATUS"},
		{"rules said no", &fakeAPI{resp: &APIResponse{StatusCode: 202, RunID: "gated"}}, OutcomeNotEligible, "gated", "NOT_ELIGIBLE"},
		{"active run or cooldown", &fakeAPI{resp: &APIResponse{StatusCode: 202}}, OutcomeSkipped, "", ""},
		{"rejected", &fakeAPI{resp: &APIResponse{StatusCode: 400, Message: "bad cpe_id"}}, OutcomeRejected, "", ""},
		{"unreachable", &fakeAPI{err: errors.New("dial tcp: refused")}, OutcomeUnreachable, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := LoadPayload(strings.NewReader(`{"id": 97, "run_id": "from-the-sample-file", "queue": "NOC"}`))
			if err != nil {
				t.Fatal(err)
			}
			res, err := Process(context.Background(), p, runs, tc.api)
			if err != nil {
				t.Fatal(err)
			}
			if res.Outcome != tc.outcome || res.RunID != tc.wantRunID || res.State != tc.state {
				t.Errorf("got %s run_id %q state %q, want %s %q %q", res.Outcome, res.RunID, res.State, tc.outcome, tc.wantRunID, tc.state)
			}
			var sent map[string]any
			if err := json.Unmarshal(tc.api.body, &sent); err != nil {
				t.Fatal(err)
			}
			if _, ok := sent["run_id"]; ok {
				t.Errorf("body carries run_id %v — the API makes the run_id", sent["run_id"])
			}
			if sent["id"] != "97" {
				t.Errorf("id %v, want the string \"97\"", sent["id"])
			}
		})
	}
}
