package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"git.frontiir.net/sa-dev/rtdatacore/pkg/driven/rtdb"
)

// serve runs a small web UI for the report criteria samples: pick a ticket
// id, a criteria file and optionally a CPE id, then send it the same way as
// `send --file criteria/tickets/<C>.json --set id=… [--set custom_fields.cpe_id=…]`.
// It listens on localhost by default because every run updates a real RT ticket.

//go:embed criteria_ui.html
var criteriaUI []byte

const (
	defaultServeAddr   = "127.0.0.1:8090"
	defaultCriteriaDir = "criteria/tickets"
)

var (
	criteriaNamePattern = regexp.MustCompile(`^[0-9A-Za-z_-]+$`)
	cpeIDPattern        = regexp.MustCompile(`^[0-9A-Za-z._-]+$`)
)

type runRequest struct {
	TicketID string `json:"ticket_id"`
	Criteria string `json:"criteria"`
	CPEID    string `json:"cpe_id"` // "" keeps the criteria file's cpe_id (CRIT-xx for the mock)
	RunID    string `json:"run_id"` // "" generates a new UUID
}

type runResponse struct {
	Result
	Criteria string `json:"criteria"`
	CPEID    string `json:"cpe_id"`
	Error    string `json:"error,omitempty"`
}

type criteriaServer struct {
	cfg     Config
	envPath string
	dir     string
	tracker Tracker
	api     Submitter
	mu      sync.Mutex // one run at a time, like running make by hand
}

func runServe(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	envPath := fs.String("env", ".env", "")
	addr := fs.String("addr", defaultServeAddr, "")
	dir := fs.String("criteria", defaultCriteriaDir, "")
	fs.Usage = func() { fmt.Fprintln(stderr, usage) }
	if err := fs.Parse(args); err != nil {
		return 3
	}

	fail := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "error: "+format+"\n", a...)
		return 3
	}

	if st, err := os.Stat(*dir); err != nil || !st.IsDir() {
		return fail("criteria folder %q not found", *dir)
	}
	cfg, err := LoadConfig(*envPath)
	if err != nil {
		return fail("%v", err)
	}
	db, err := openDB(cfg.DB)
	if err != nil {
		return fail("%v", err)
	}

	s := &criteriaServer{
		cfg:     cfg,
		envPath: *envPath,
		dir:     *dir,
		tracker: NewRepoTracker(rtdb.NewPipelineRunRepository(db)),
		api:     NewAPIClient(cfg.APIURL, cfg.APIToken, cfg.APITimeout),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /api/info", s.handleInfo)
	mux.HandleFunc("POST /api/run", s.handleRun)

	fmt.Fprintf(stdout, "criteria UI on http://%s (env %s, criteria %s)\n", *addr, *envPath, *dir)
	srv := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		return fail("%v", err)
	}
	return 0
}

func (s *criteriaServer) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(criteriaUI)
}

// handleInfo lists the criteria files (with the cpe_id each one carries) and
// the config the runs will use.
func (s *criteriaServer) handleInfo(w http.ResponseWriter, _ *http.Request) {
	names, err := s.criteriaNames()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	cpeIDs := make(map[string]string, len(names))
	for _, name := range names {
		if p, err := readPayload(filepath.Join(s.dir, name+".json")); err == nil {
			cpeIDs[name] = p.CustomField("cpe_id")
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"criteria": names,
		"cpe_ids":  cpeIDs,
		"env":      s.envPath,
		"api_url":  s.cfg.APIURL,
		"db":       fmt.Sprintf("%s:%s/%s", s.cfg.DB.Host, s.cfg.DB.Port, s.cfg.DB.Name),
	})
}

func (s *criteriaServer) handleRun(w http.ResponseWriter, r *http.Request) {
	var req runRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, runResponse{Error: "invalid request JSON: " + err.Error()})
		return
	}
	p, runID, err := s.prepare(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, runResponse{Criteria: req.Criteria, Error: err.Error()})
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := Process(r.Context(), s.cfg, p, runID, s.tracker, s.api)
	out := runResponse{Result: res, Criteria: req.Criteria, CPEID: p.CustomField("cpe_id")}
	if err != nil {
		out.Error = fmt.Sprintf("run %s: %v", runID, err)
		writeJSON(w, http.StatusInternalServerError, out)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// prepare validates req and builds the payload from the criteria file.
func (s *criteriaServer) prepare(req runRequest) (*Payload, string, error) {
	ticket := strings.TrimSpace(req.TicketID)
	if id, err := strconv.ParseUint(ticket, 10, 64); err != nil || id == 0 {
		return nil, "", fmt.Errorf("ticket_id must be a positive number, got %q", req.TicketID)
	}
	if !criteriaNamePattern.MatchString(req.Criteria) {
		return nil, "", fmt.Errorf("criteria %q is not a criteria file name", req.Criteria)
	}
	cpe := strings.TrimSpace(req.CPEID)
	if cpe != "" && !cpeIDPattern.MatchString(cpe) {
		return nil, "", fmt.Errorf("cpe_id %q may only contain letters, digits, '.', '_' and '-'", req.CPEID)
	}
	runID := strings.TrimSpace(req.RunID)
	if runID == "" {
		runID = newRunID()
	} else if !uuidPattern.MatchString(runID) {
		return nil, "", fmt.Errorf("run_id %q is not a UUID", req.RunID)
	}

	p, err := readPayload(filepath.Join(s.dir, req.Criteria+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", fmt.Errorf("no criteria file %s.json in %s", req.Criteria, s.dir)
	}
	if err != nil {
		return nil, "", err
	}
	p.Set("id=" + ticket)
	if cpe != "" {
		p.Set("custom_fields.cpe_id=" + cpe)
	}
	return p, runID, nil
}

func (s *criteriaServer) criteriaNames() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), ".json"); ok && !e.IsDir() {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
