package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

// The acceptance page (/accept): lists the 81 cases by group and runs the
// selected ones as a background job. One job at a time, because the cases
// share the stack (they kill rtutil, stop the mock, seed sweeper backlog).
// The page polls the job and shows each case's steps as they happen.

//go:embed accept_ui.html
var acceptUI []byte

// coverage_ui.html (/coverage) maps each Story AC (Redmine Epic #22947) to
// the code that implements it and the unit tests that assert it. It is a
// generated snapshot with its data inline; regenerate it after code changes.
//
//go:embed coverage_ui.html
var coverageUI []byte

const defaultMockAdminToken = "rt-web-ui-accept"

type caseResult struct {
	ID       string       `json:"id"`
	Status   string       `json:"status"` // queued, running, pass, fail, skip, error
	Steps    []acceptStep `json:"steps"`
	Runs     []string     `json:"runs"`
	Tickets  []uint64     `json:"tickets"`
	Started  string       `json:"started,omitempty"`
	Seconds  int          `json:"seconds"`
	t        *T
	startedT time.Time
}

type acceptJob struct {
	ID      string        `json:"id"`
	Status  string        `json:"status"` // running, done, cancelled
	Results []*caseResult `json:"results"`
	cancel  context.CancelFunc
}

type acceptServer struct {
	env   *acceptEnv
	cases map[string]acceptCase
	list  []acceptCase
	mu    sync.Mutex
	job   *acceptJob
}

func newAcceptServer(env *acceptEnv) (*acceptServer, error) {
	list, err := loadAcceptCases()
	if err != nil {
		return nil, err
	}
	links, err := loadAcceptACs(list)
	if err != nil {
		return nil, err
	}
	for i := range list {
		list[i].ACs = links.Cases[list[i].ID]
	}
	s := &acceptServer{env: env, list: list, cases: map[string]acceptCase{}}
	for _, c := range list {
		s.cases[c.ID] = c
	}
	return s, nil
}

func (s *acceptServer) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /accept", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(acceptUI)
	})
	mux.HandleFunc("GET /coverage", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(coverageUI)
	})
	mux.HandleFunc("GET /api/accept/cases", s.handleCases)
	mux.HandleFunc("GET /api/accept/cases/{id}/requirements", s.handleRequirements)
	mux.HandleFunc("POST /api/accept/jobs", s.handleStart)
	mux.HandleFunc("GET /api/accept/jobs/current", s.handleJob)
	mux.HandleFunc("POST /api/accept/jobs/current/cancel", s.handleCancel)
}

func (s *acceptServer) handleCases(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"groups": groupsWithEn(), "cases": s.list,
		"env": map[string]string{
			"api": s.env.cfg.APIURL, "db": fmt.Sprintf("%s:%s/%s", s.env.cfg.DB.Host, s.env.cfg.DB.Port, s.env.cfg.DB.Name),
			"rt": s.env.noc["RT_BASE_URL"], "noc_env": s.env.nocEnv, "mock": "127.0.0.1:" + s.env.mockPort,
			"report": "http://" + reportAddr(defaultReportDir),
		},
	})
}

func (s *acceptServer) handleStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil || len(req.IDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "send {\"ids\": [\"ADM-01\", …]}"})
		return
	}
	job := &acceptJob{ID: newRunID(), Status: "running"}
	for _, id := range req.IDs {
		if _, ok := s.cases[id]; !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown case " + id})
			return
		}
		job.Results = append(job.Results, &caseResult{ID: id, Status: "queued"})
	}

	s.mu.Lock()
	if s.job != nil && s.job.Status == "running" {
		s.mu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{"error": "a job is already running — wait or cancel it"})
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	job.cancel = cancel
	s.job = job
	s.mu.Unlock()

	go s.runJob(ctx, job)
	writeJSON(w, http.StatusAccepted, s.snapshot())
}

func (s *acceptServer) handleJob(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.snapshot())
}

func (s *acceptServer) handleCancel(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	if s.job != nil && s.job.Status == "running" {
		s.job.cancel()
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, s.snapshot())
}

// snapshot copies the job under the lock, including each running case's
// steps so far.
func (s *acceptServer) snapshot() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.job == nil {
		return map[string]any{"job": nil}
	}
	out := *s.job
	out.Results = nil
	for _, r := range s.job.Results {
		c := *r
		if r.t != nil {
			r.t.mu.Lock()
			c.Steps = append([]acceptStep(nil), r.t.steps...)
			c.Runs = append([]string(nil), r.t.runs...)
			c.Tickets = append([]uint64(nil), r.t.tickets...)
			r.t.mu.Unlock()
		}
		if c.Status == "running" {
			c.Seconds = int(time.Since(r.startedT).Seconds())
		}
		out.Results = append(out.Results, &c)
	}
	return map[string]any{"job": out}
}

func (s *acceptServer) runJob(ctx context.Context, job *acceptJob) {
	for _, res := range job.Results {
		c := s.cases[res.ID]
		t := &T{id: c.ID, ctx: ctx, env: s.env}
		s.mu.Lock()
		if ctx.Err() != nil {
			res.Status = "skip"
			res.Steps = []acceptStep{{At: time.Now().Format("15:04:05"), Level: lvWarn, Msg: "job cancelled"}}
			s.mu.Unlock()
			continue
		}
		res.Status, res.t, res.startedT, res.Started = "running", t, time.Now(), time.Now().Format("15:04:05")
		s.mu.Unlock()

		status := runCase(t, c)

		s.mu.Lock()
		res.Status, res.Seconds = status, int(time.Since(res.startedT).Seconds())
		s.mu.Unlock()
	}
	s.mu.Lock()
	job.Status = "done"
	if ctx.Err() != nil {
		job.Status = "cancelled"
	}
	s.mu.Unlock()
}

// runCase runs one case and returns its verdict. A case without a runner is
// a skip with its reason; a panic other than Fatal/Skip is an error.
func runCase(t *T, c acceptCase) (status string) {
	if c.run == nil {
		t.add(1, lvWarn, "skip: %s", c.Skip)
		if c.Covered != "" {
			t.Log("covered elsewhere: %s", c.Covered)
		}
		return "skip"
	}
	defer func() {
		for i := len(t.cleanup) - 1; i >= 0; i-- {
			func() {
				defer func() { recover() }() // a failed cleanup must not take the server down
				t.cleanup[i]()
			}()
		}
	}()
	defer func() {
		if p := recover(); p != nil {
			if err, ok := p.(error); !ok || !errors.Is(err, errAbort) {
				t.Fail("panic: %v", p)
			}
		}
		switch {
		case t.skipped != "":
			status = "skip"
		case t.failed:
			status = "fail"
		default:
			status = "pass"
		}
	}()
	t.Log("%s · %s", c.ID, c.Title)
	c.run(t)
	return "pass"
}

// mockAdminToken is the criteria mock's admin API token, shared by the
// process that starts the mock (up) and the one that reads its log (serve).
func mockAdminToken() string {
	for _, k := range []string{"RT_WEB_UI_MOCK_ADMIN_TOKEN", "CRITERIA_MOCK_ADMIN_TOKEN"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return defaultMockAdminToken
}
