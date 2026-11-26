package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"git.frontiir.net/sa-dev/frontiirgopackages/pkg/frontlog"
	"go.uber.org/zap"
)

// The acceptance harness drives the real stack (RT External API, MariaDB,
// Kafka, rtutil, RT REST2 and the criteria mock) from the outside, like the
// RT extension does, and checks what each system stored on its own: the
// pipeline_runs row, RT's Tickets / Transactions tables and the mock's
// request log. It never trusts a component's own success message alone.

// acceptEnv is what the cases need to reach the stack. noc holds
// noc_automation's .env (RT REST2 token, sweeper and BCS settings).
type acceptEnv struct {
	cfg       Config
	db        *sql.DB
	api       Submitter
	noc       map[string]string
	nocEnv    string
	dir       string // criteria tickets
	mockPort  string
	mockToken string
	runDir    string // make up's pid files
	selfDir   string // rt_web_ui, where `make up` runs
}

// Step levels shown in the UI timeline.
const (
	lvInfo = "info"
	lvPass = "pass"
	lvFail = "fail"
	lvWarn = "warn"
)

type acceptStep struct {
	At    string `json:"at"`
	Level string `json:"level"`
	Msg   string `json:"msg"`
}

// errAbort stops a case after Fatal / Skip; the result is already recorded.
var errAbort = errors.New("abort")

// T is one case run. Steps are appended under mu so the UI can read them
// while the case is still running.
type T struct {
	id      string // case ID, on every log line
	ctx     context.Context
	env     *acceptEnv
	mu      sync.Mutex
	steps   []acceptStep
	runs    []string
	tickets []uint64
	failed  bool
	skipped string
	cleanup []func()
}

// add records a step for the UI timeline and writes it to frontlog.Logger.
// skip is how many frames above add the logged caller is: 2 for a case
// calling t.Log / t.Check, so the log line points at the case's own line.
func (t *T) add(skip int, level, format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	t.mu.Lock()
	t.steps = append(t.steps, acceptStep{At: time.Now().Format("15:04:05"), Level: level, Msg: msg})
	if level == lvFail {
		t.failed = true
	}
	t.mu.Unlock()
	logStep(skip, t.id, level, msg)
}

// logStep writes one step to frontlog.Logger: info and pass as Info, warn
// (and skip) as Warn, fail as Error.
func logStep(skip int, caseID, level, msg string) {
	if frontlog.Logger == nil {
		return
	}
	log := frontlog.Logger.WithOptions(zap.AddCallerSkip(skip + 1)) // +1 for logStep itself
	fields := []zap.Field{zap.String("case", caseID), zap.String("step", level)}
	switch level {
	case lvFail:
		log.Error(msg, fields...)
	case lvWarn:
		log.Warn(msg, fields...)
	default:
		log.Info(msg, fields...)
	}
}

func (t *T) Log(format string, a ...any)  { t.add(2, lvInfo, format, a...) }
func (t *T) Pass(format string, a ...any) { t.add(2, lvPass, format, a...) }
func (t *T) Fail(format string, a ...any) { t.add(2, lvFail, format, a...) }
func (t *T) Warn(format string, a ...any) { t.add(2, lvWarn, format, a...) }

// Check records a pass or a fail and reports which.
func (t *T) Check(ok bool, format string, a ...any) bool {
	level := lvPass
	if !ok {
		level = lvFail
	}
	t.add(2, level, format, a...)
	return ok
}

// Fatal records a failure and stops the case.
func (t *T) Fatal(format string, a ...any) {
	t.add(2, lvFail, format, a...)
	panic(errAbort)
}

// Skip stops the case without a verdict, e.g. when the stack is not set up
// for it (a setting is off, a service was not started by make up).
func (t *T) Skip(format string, a ...any) {
	t.mu.Lock()
	t.skipped = fmt.Sprintf(format, a...)
	t.mu.Unlock()
	t.add(2, lvWarn, "skip: "+format, a...)
	panic(errAbort)
}

func (t *T) Defer(f func()) { t.cleanup = append(t.cleanup, f) }

// Sleep waits d unless the job was cancelled.
func (t *T) Sleep(d time.Duration) {
	select {
	case <-t.ctx.Done():
		t.Fatal("cancelled")
	case <-time.After(d):
	}
}

func (t *T) trackRun(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.runs = append(t.runs, id)
}

// ---------------------------------------------------------------------------
// Settings

// nocInt reads an integer setting of noc_automation's .env.
func (t *T) nocInt(key string, fallback int) int {
	if v, err := strconv.Atoi(strings.TrimSpace(t.env.noc[key])); err == nil {
		return v
	}
	return fallback
}

func (t *T) nocBool(key string) bool {
	v := strings.ToLower(strings.TrimSpace(t.env.noc[key]))
	return v == "true" || v == "1" || v == "yes"
}

// ---------------------------------------------------------------------------
// RT (REST2 for changes, the RT tables for checks)

func (t *T) rtDo(method, path, contentType string, body []byte) ([]byte, int) {
	base := strings.TrimRight(t.env.noc["RT_BASE_URL"], "/")
	token := t.env.noc["RT_TOKEN"]
	if base == "" || token == "" {
		t.Skip("RT_BASE_URL / RT_TOKEN not set in %s", t.env.nocEnv)
	}
	ctx, cancel := context.WithTimeout(t.ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, base+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal("RT %s %s: %v", method, path, err)
	}
	req.Header.Set("Authorization", rtAuthHeader(token))
	req.Header.Set("Content-Type", contentType)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal("RT %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return raw, resp.StatusCode
}

// rtAuthHeader is the RT REST2 Authorization header for RT_TOKEN. Like
// noc_automation and rtutil, RT_TOKEN normally holds the whole header value
// ("token 1-66-…"); a bare token gets the "token " prefix.
func rtAuthHeader(rtToken string) string {
	rtToken = strings.TrimSpace(rtToken)
	if strings.HasPrefix(strings.ToLower(rtToken), "token ") {
		return rtToken
	}
	return "token " + rtToken
}

// NewTicket creates a fresh RT ticket in the NOC queue, so every case starts
// outside the 24-hour rule and without another run holding the ticket.
func (t *T) NewTicket(subject string) uint64 {
	t.RequireAPI() // no orphan RT tickets when the API cannot take them
	body, _ := json.Marshal(map[string]string{
		"Queue": t.env.cfg.NOCQueue, "Subject": "[acceptance] " + subject, "Status": "new",
	})
	raw, code := t.rtDo(http.MethodPost, "/ticket", "application/json", body)
	if code != http.StatusCreated && code != http.StatusOK {
		t.Fatal("RT create ticket: HTTP %d %s", code, strings.TrimSpace(string(raw)))
	}
	var out struct {
		ID json.Number `json:"id"`
	}
	json.Unmarshal(raw, &out)
	id, err := strconv.ParseUint(out.ID.String(), 10, 64)
	if err != nil || id == 0 {
		t.Fatal("RT create ticket: no id in %s", strings.TrimSpace(string(raw)))
	}
	t.mu.Lock()
	t.tickets = append(t.tickets, id)
	t.mu.Unlock()
	t.Log("RT ticket #%d created in %q", id, t.env.cfg.NOCQueue)
	return id
}

// SetTicket changes RT fields (Queue, Status) the way an operator would.
func (t *T) SetTicket(id uint64, fields map[string]string) {
	body, _ := json.Marshal(fields)
	raw, code := t.rtDo(http.MethodPut, fmt.Sprintf("/ticket/%d", id), "application/json", body)
	if code != http.StatusOK {
		t.Fatal("RT update ticket #%d: HTTP %d %s", id, code, strings.TrimSpace(string(raw)))
	}
	t.Log("operator change on #%d: %v", id, fields)
}

// Comment adds an RT comment as an operator would.
func (t *T) Comment(id uint64, text string) {
	raw, code := t.rtDo(http.MethodPost, fmt.Sprintf("/ticket/%d/comment", id), "text/html", []byte(text))
	if code != http.StatusCreated && code != http.StatusOK {
		t.Fatal("RT comment on #%d: HTTP %d %s", id, code, strings.TrimSpace(string(raw)))
	}
}

type rtTicket struct{ Queue, Status string }

// Ticket reads the ticket from RT's own tables.
func (t *T) Ticket(id uint64) rtTicket {
	var r rtTicket
	err := t.env.db.QueryRowContext(t.ctx,
		`SELECT q.Name, t.Status FROM Tickets t JOIN Queues q ON q.id = t.Queue WHERE t.id = ?`, id).Scan(&r.Queue, &r.Status)
	if err != nil {
		t.Fatal("read RT ticket #%d: %v", id, err)
	}
	return r
}

// CommentCount counts the ticket's RT comments containing text.
func (t *T) CommentCount(id uint64, text string) int {
	var n int
	err := t.env.db.QueryRowContext(t.ctx, `
		SELECT COUNT(DISTINCT tr.id) FROM Transactions tr JOIN Attachments a ON a.TransactionId = tr.id
		WHERE tr.ObjectType = 'RT::Ticket' AND tr.ObjectId = ? AND tr.Type = 'Comment' AND a.Content LIKE ?`,
		id, "%"+escapeLikeText(text)+"%").Scan(&n)
	if err != nil {
		t.Fatal("count RT comments on #%d: %v", id, err)
	}
	return n
}

func escapeLikeText(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// runMarker is rtutil's hidden comment marker for a run.
func runMarker(runID string) string { return "<!-- rr:" + runID + " -->" }

// ---------------------------------------------------------------------------
// RT External API

type admission struct {
	Status  int
	RunID   string
	Message string
}

// Submit sends a criteria ticket to the RT External API the way the RT
// extension does: straight to the API, which creates its own run. rt_web_ui's
// Process is not used because it opens a run of its own first, and that run
// would hold the ticket's active-run lock against the API.
func (t *T) Submit(criteria string, ticketID uint64, sets ...string) admission {
	p, err := readPayload(filepath.Join(t.env.dir, criteria+".json"))
	if err != nil {
		t.Fatal("criteria %s: %v", criteria, err)
	}
	p.Set(fmt.Sprintf("id=%d", ticketID))
	t.fillFromRT(p, ticketID) // before sets: a case's own --set still wins
	for _, s := range sets {
		if err := p.Set(s); err != nil {
			t.Fatal("criteria %s: --set %s: %v", criteria, s, err)
		}
	}
	body, err := p.Body(ticketID)
	if err != nil {
		t.Fatal("criteria %s: %v", criteria, err)
	}
	return t.post(body, fmt.Sprintf("%s (#%d, cpe %s)", criteria, ticketID, p.CustomField("cpe_id")))
}

// rtTicketNoField is the RT custom field RT fills on create, e.g.
// "Nov26-TKT-2851373"; the payload carries it as custom_fields.ticket_no.
const rtTicketNoField = "Ticket No."

// fillFromRT replaces the sample file's ticket data with the new RT
// ticket's own: created / started / resolved as RT stores them
// ("YYYY-MM-DD HH:MM:SS", UTC) and custom_fields.ticket_no from RT's
// "Ticket No." field. The rest of the sample (cpe_id, service type, ...) is
// kept, since those are what each case controls.
func (t *T) fillFromRT(p *Payload, ticketID uint64) {
	var created, started, resolved string
	err := t.env.db.QueryRowContext(t.ctx, `
		SELECT DATE_FORMAT(Created, '%Y-%m-%d %H:%i:%s'), DATE_FORMAT(Started, '%Y-%m-%d %H:%i:%s'),
		       DATE_FORMAT(Resolved, '%Y-%m-%d %H:%i:%s')
		FROM Tickets WHERE id = ?`, ticketID).Scan(&created, &started, &resolved)
	if err != nil {
		t.Fatal("read RT ticket #%d: %v", ticketID, err)
	}
	p.Set("created=" + created)
	p.Set("started=" + started)
	p.Set("resolved=" + resolved)

	var ticketNo string
	err = t.env.db.QueryRowContext(t.ctx, `
		SELECT o.Content FROM ObjectCustomFieldValues o JOIN CustomFields cf ON cf.id = o.CustomField
		WHERE o.ObjectType = 'RT::Ticket' AND o.ObjectId = ? AND o.Disabled = 0 AND cf.Name = ?
		ORDER BY o.id DESC LIMIT 1`, ticketID, rtTicketNoField).Scan(&ticketNo)
	switch {
	case err == nil && ticketNo != "":
		p.Set("custom_fields.ticket_no=" + ticketNo)
	case err == nil || errors.Is(err, sql.ErrNoRows):
		t.Warn("RT ticket #%d has no %q value — custom_fields.ticket_no stays %q from the sample", ticketID, rtTicketNoField, p.CustomField("ticket_no"))
	default:
		t.Fatal("read %q of RT ticket #%d: %v", rtTicketNoField, ticketID, err)
	}
	t.Log("ticket data from RT #%d: created %s · ticket_no %s", ticketID, created, p.CustomField("ticket_no"))
}

func (t *T) post(body []byte, what string) admission {
	resp, err := t.env.api.Submit(t.ctx, body)
	if err != nil {
		t.Skip("POST %s: API unreachable (%v) — start noc_automation (make up)", what, err)
	}
	a := admission{Status: resp.StatusCode, RunID: resp.RunID, Message: resp.Message}
	if a.Status == http.StatusNotFound || a.Status == http.StatusMethodNotAllowed {
		t.Fatal("POST %s → HTTP %d: the API has no route %s — set RT_WEB_UI_API_URL to …/api/v1/noc-automation/remote-resolve", what, a.Status, t.env.cfg.APIURL)
	}
	if a.Status == http.StatusServiceUnavailable {
		t.Skip("API answered 503 %q — noc_automation's Kafka / workflow config is not ready (KAFKA_BROKERS in %s)", a.Message, t.env.nocEnv)
	}
	if a.RunID != "" {
		t.trackRun(a.RunID)
	}
	t.Log("POST %s → HTTP %d run_id=%q %s", what, a.Status, a.RunID, a.Message)
	return a
}

// RequireAPI skips the case when noc_automation is down: its sweepers are
// what the sweeper cases observe.
func (t *T) RequireAPI() {
	ctx, cancel := context.WithTimeout(t.ctx, checkTimeout)
	defer cancel()
	if r := checkAPI(ctx, t.env.cfg, nil); r.Level != levelOK {
		t.Skip("noc_automation is not running (%s) — start it with make up", r.Detail)
	}
}

// Suppressed checks a request the API turned away without a run (cooldown
// or active run): the same 202 as an admission, but no run_id. Any other
// status is not a suppression, even without a run_id.
func (t *T) Suppressed(a admission, what string) bool {
	return t.Check(a.Status == http.StatusAccepted && a.RunID == "",
		"%s: suppressed — 202 without run_id (HTTP %d run_id %q)", what, a.Status, a.RunID)
}

// Admitted fails the case unless the API answered 202 with a run_id.
func (t *T) Admitted(a admission) string {
	if a.Status != http.StatusAccepted || a.RunID == "" {
		t.Fatal("expected 202 with a run_id, got HTTP %d run_id=%q", a.Status, a.RunID)
	}
	return a.RunID
}

// ---------------------------------------------------------------------------
// pipeline_runs

type runRow struct {
	RunID, State, Reason, TargetQueue, BCSJob, BCSMessage, CPEID string
	TicketID                                                     uint64
	RemoteResolved, BCSSuccess                                   sql.NullBool
	BeforeChannel, TargetChannel                                 sql.NullInt64
	CreatedAt, UpdatedAt, CompletedAt                            sql.NullTime
}

func (r runRow) Done() bool { return r.CompletedAt.Valid }

// TicketCompleted reports a run whose RT update succeeded: COMPLETED before
// rtdatacore v1.1.21, TICKET_COMPLETED after, and FULLY_COMPLETED once a
// succeeded BCS result arrives for it.
func (r runRow) TicketCompleted() bool {
	return r.State == "TICKET_COMPLETED" || r.State == "FULLY_COMPLETED" || r.State == "COMPLETED"
}

func (t *T) Run(runID string) runRow {
	var r runRow
	err := t.env.db.QueryRowContext(t.ctx, `
		SELECT run_id, COALESCE(ticket_id, 0), COALESCE(current_state, ''), COALESCE(last_state_reason, ''),
		       COALESCE(target_queue, ''), COALESCE(ref_bcs_process_id, ''), COALESCE(bcs_status_message, ''),
		       COALESCE(cpe_id, ''), is_remote_resolved, is_bcs_success, before_bcs_channel, target_bcs_channel,
		       created_at, updated_at, completed_at
		FROM pipeline_runs WHERE run_id = ?`, runID).Scan(
		&r.RunID, &r.TicketID, &r.State, &r.Reason, &r.TargetQueue, &r.BCSJob, &r.BCSMessage, &r.CPEID,
		&r.RemoteResolved, &r.BCSSuccess, &r.BeforeChannel, &r.TargetChannel, &r.CreatedAt, &r.UpdatedAt, &r.CompletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		t.Fatal("run %s has no pipeline_runs row", runID)
	}
	if err != nil {
		t.Fatal("read run %s: %v", runID, err)
	}
	return r
}

// WaitRun polls the run until until() holds, logging each state change.
func (t *T) WaitRun(runID string, timeout time.Duration, what string, until func(runRow) bool) runRow {
	deadline := time.Now().Add(timeout)
	last := ""
	for {
		r := t.Run(runID)
		if r.State != last {
			t.Log("run %s… state %s", runID[:8], r.State)
			last = r.State
		}
		if until(r) {
			return r
		}
		if time.Now().After(deadline) {
			t.Fatal("run %s… not %s within %s (state %s, reason %q)", runID[:8], what, timeout, r.State, r.Reason)
		}
		t.Sleep(2 * time.Second)
	}
}

// WaitDone waits for the run's terminal state. rtutil normally needs 2-3 min.
func (t *T) WaitDone(runID string) runRow {
	return t.WaitRun(runID, 6*time.Minute, "finished", runRow.Done)
}

type eventRow struct{ Event, From, To, Component string }

func (t *T) Events(runID string) []eventRow {
	rows, err := t.env.db.QueryContext(t.ctx, `
		SELECT COALESCE(event, ''), COALESCE(from_state, ''), COALESCE(to_state, ''), COALESCE(component, '')
		FROM pipeline_run_events WHERE run_id = ? ORDER BY id`, runID)
	if err != nil {
		t.Fatal("read events of %s: %v", runID, err)
	}
	defer rows.Close()
	var out []eventRow
	for rows.Next() {
		var e eventRow
		rows.Scan(&e.Event, &e.From, &e.To, &e.Component)
		out = append(out, e)
	}
	return out
}

// Components as each system writes them into pipeline_run_events. The
// acceptance checks count an event only when the system that owns the step
// wrote it, so a pass is that system's own record, not rt_web_ui's.
const (
	compAPI        = "remote_resolve_service" // noc_automation RT External API
	compRtutil     = "rtutil_consumer"        // rtutil Kafka consumer
	compWebhook    = "bcs_webhook"            // noc_automation BCS callback
	compBCSSweeper = "bcs_result_sweeper"     // noc_automation BCS poll
)

// systemNames labels a component in step messages.
var systemNames = map[string]string{
	compAPI: "RT External API", compRtutil: "rtutil", compWebhook: "API BCS webhook", compBCSSweeper: "BCS sweeper",
}

// countBy counts events named name that component wrote.
func countBy(evs []eventRow, component, name string) int {
	n := 0
	for _, e := range evs {
		if e.Component == component && e.Event == name {
			n++
		}
	}
	return n
}

// countComponent counts every event component wrote.
func countComponent(evs []eventRow, component string) int {
	n := 0
	for _, e := range evs {
		if e.Component == component {
			n++
		}
	}
	return n
}

// Proved checks that component itself recorded each step for the run.
func (t *T) Proved(evs []eventRow, component string, steps ...string) {
	for _, s := range steps {
		n := countBy(evs, component, s)
		t.Check(n >= 1, "%s recorded %s itself (%s · %d event(s))", systemNames[component], s, component, n)
	}
}

func countEvents(evs []eventRow, name string) int {
	n := 0
	for _, e := range evs {
		if e.Event == name {
			n++
		}
	}
	return n
}

// RunsForTicket counts the ticket's runs created since t0.
func (t *T) RunsForTicket(ticketID uint64, since time.Time) []runRow {
	rows, err := t.env.db.QueryContext(t.ctx,
		`SELECT run_id FROM pipeline_runs WHERE ticket_id = ? AND created_at >= ? ORDER BY created_at`, ticketID, since.Add(-time.Second))
	if err != nil {
		t.Fatal("list runs of #%d: %v", ticketID, err)
	}
	var ids []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	out := make([]runRow, 0, len(ids))
	for _, id := range ids {
		out = append(out, t.Run(id))
	}
	return out
}

// Exec runs a write the case needs to set up a condition (seeding, moving a
// clock back). Only ever used on rows the case created itself.
func (t *T) Exec(query string, args ...any) int64 {
	res, err := t.env.db.ExecContext(t.ctx, query, args...)
	if err != nil {
		t.Fatal("%s: %v", strings.Fields(query)[0], err)
	}
	n, _ := res.RowsAffected()
	return n
}

// ---------------------------------------------------------------------------
// BCS webhook (what CPEMS posts when a channel-change job finishes)

type bcsResult struct {
	Status        string // succeeded_new_channel, succeeded_same_channel, failed, skipped or unconfirmed
	Before, After uint16 // channels
}

// SendBCSWebhook posts a CPEMS BCS job result for the run's job to the
// API's callback, authenticated the way Peppermint is: "Authorization:
// Bearer <token>", where the token equals noc's BCS_WEBHOOK_TOKEN (which may
// itself be written "Bearer <token>", the old way).
func (t *T) SendBCSWebhook(r runRow, res bcsResult) (int, string) {
	token := strings.TrimSpace(t.env.noc["BCS_WEBHOOK_TOKEN"])
	if scheme, rest, ok := strings.Cut(token, " "); ok && strings.EqualFold(scheme, "Bearer") {
		token = strings.TrimSpace(rest)
	}
	if token == "" {
		t.Skip("BCS_WEBHOOK_TOKEN is not set in %s — the API refuses every BCS webhook", t.env.nocEnv)
	}
	u, err := url.Parse(t.env.cfg.APIURL)
	if err != nil {
		t.Fatal("API URL %q: %v", t.env.cfg.APIURL, err)
	}
	u.Path, u.RawQuery = "/api/v1/bcs/callback", ""
	now := time.Now().UTC().Format(time.RFC3339)
	body, _ := json.Marshal(map[string]any{
		"event": "channel_selection." + res.Status, "job_id": r.BCSJob, "request_id": r.RunID,
		"cid": r.CPEID, "band": "2.4g", "status": res.Status,
		"before_channel": res.Before, "after_channel": res.After, "created_at": now, "updated_at": now,
	})
	ctx, cancel := context.WithTimeout(t.ctx, 15*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token) // as Peppermint sends it
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal("BCS webhook: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	msg := strings.TrimSpace(string(raw))
	t.Log("BCS webhook %s for job %s → HTTP %d %s", res.Status, r.BCSJob, resp.StatusCode, msg)
	return resp.StatusCode, msg
}

// ---------------------------------------------------------------------------
// Criteria mock (Node-RED + Peppermint BCS)

type mockCall struct {
	Method, Path string
	Status       int
	At           time.Time
}

// MockCalls reads the mock's own request log through its admin API, so BCS
// calls are counted by the simulator and not by rtutil.
func (t *T) MockCalls() []mockCall {
	ctx, cancel := context.WithTimeout(t.ctx, 10*time.Second)
	defer cancel()
	// The admin API pages its log (10 entries by default): ask for all of it.
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+t.env.mockPort+"/mockoon-admin/logs?limit=5000", nil)
	req.Header.Set("Authorization", "Bearer "+t.env.mockToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Skip("criteria mock on :%s not reachable: %v", t.env.mockPort, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		t.Skip("criteria mock admin API refused the token — restart it with `make criteria-mock-stop criteria-mock-up` so it gets --admin-api-token")
	}
	var raw []struct {
		Request struct {
			Method  string `json:"method"`
			URLPath string `json:"urlPath"`
		} `json:"request"`
		Response struct {
			StatusCode int `json:"statusCode"`
		} `json:"response"`
		TimestampMs int64 `json:"timestampMs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatal("criteria mock logs: %v", err)
	}
	out := make([]mockCall, 0, len(raw))
	for _, r := range raw {
		out = append(out, mockCall{Method: r.Request.Method, Path: r.Request.URLPath, Status: r.Response.StatusCode,
			At: time.UnixMilli(r.TimestampMs)})
	}
	return out
}

// BCSStarts counts the BCS channel-change requests the mock received for
// cpeID since t0 (POST /api/v2/cpes/<cpe>/channel-selections).
func (t *T) BCSStarts(cpeID string, since time.Time) int {
	n := 0
	for _, c := range t.MockCalls() {
		if strings.EqualFold(c.Method, http.MethodPost) && c.Path == "/api/v2/cpes/"+cpeID+"/channel-selections" && !c.At.Before(since) {
			n++
		}
	}
	return n
}

// StopMock / StartMock take the Node-RED mock away to force a CPE fetch
// failure, and bring it back.
func (t *T) StopMock() {
	t.docker("stop", mockContainer)
	t.Log("criteria mock stopped")
}

func (t *T) StartMock() {
	// Not t.ctx: the mock must come back even when the job was cancelled.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "make", "criteria-mock-up", "CRITERIA_MOCK_PORT="+t.env.mockPort)
	cmd.Dir = t.env.selfDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fail("make criteria-mock-up: %v %s", err, lastLine(out))
		return
	}
	t.Log("criteria mock started again")
}

func (t *T) docker(args ...string) {
	ctx, cancel := context.WithTimeout(t.ctx, 60*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput(); err != nil {
		t.Fatal("docker %s: %v %s", strings.Join(args, " "), err, lastLine(out))
	}
}

// ---------------------------------------------------------------------------
// Processes started by make up (fault cases)

// KillService SIGKILLs a process make up started (name = its pid file), the
// way a crash would: no shutdown hook runs.
func (t *T) KillService(name string) {
	raw, err := os.ReadFile(filepath.Join(t.env.runDir, name+".pid"))
	if err != nil {
		t.Skip("%s was not started by make up (no %s/%s.pid) — this fault case needs it", name, t.env.runDir, name)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	p, err := os.FindProcess(pid)
	if err != nil || pid == 0 || p.Signal(syscall.Signal(0)) != nil {
		t.Skip("%s pid %d from %s is not running", name, pid, t.env.runDir)
	}
	p.Signal(syscall.SIGKILL)
	t.Log("%s killed (SIGKILL pid %d)", name, pid)
}

// Up restarts whatever make up manages that is down.
func (t *T) Up() {
	ctx, cancel := context.WithTimeout(t.ctx, 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "make", "up")
	cmd.Dir = t.env.selfDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Warn("make up exited with %v (a warning there is normal): %s", err, lastLine(out))
	}
	t.Log("make up done")
}
