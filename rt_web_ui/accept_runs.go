package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Runners for the cases that can be driven from outside. Each one creates
// its own RT ticket (or synthetic rows it deletes afterwards), so cases do
// not depend on each other or on the 24-hour rule.

// start creates a ticket and submits a criteria sample to the API.
func start(t *T, criteria string, sets ...string) (uint64, string, time.Time) {
	tk := t.NewTicket(criteria)
	t0 := time.Now()
	return tk, t.Admitted(t.Submit(criteria, tk, sets...)), t0
}

// checkCompletedOnce asserts the RT side of a finished run: the run reached
// ticket-completed, RT holds the run's target queue, and exactly one
// automation comment with this run's marker exists.
func checkCompletedOnce(t *T, tk uint64, r runRow) {
	t.Check(r.TicketCompleted(), "run ended %s (TICKET_COMPLETED expected) %s", r.State, r.Reason)
	rt := t.Ticket(tk)
	t.Check(r.TargetQueue != "" && rt.Queue == r.TargetQueue, "RT queue %q = run target_queue %q", rt.Queue, r.TargetQueue)
	n := t.CommentCount(tk, runMarker(r.RunID))
	t.Check(n == 1, "RT has %d automation comment(s) with this run's marker (1 expected)", n)
}

// ---------------------------------------------------------------------------
// 1. Admission

func caseADM01(t *T) {
	tk, run, t0 := start(t, "11_bcs_success")
	t.Run(run) // Fatal if missing
	t.Pass("durable run row exists when the 202 arrives")
	r := t.WaitDone(run)
	checkCompletedOnce(t, tk, r)
	t.Check(r.RemoteResolved.Valid && r.RemoteResolved.Bool, "is_remote_resolved = 1")
	t.Check(r.BCSJob != "", "BCS job reference stored (%q)", r.BCSJob)
	n := t.BCSStarts(r.CPEID, t0)
	t.Check(n == 1, "Peppermint mock received %d BCS start request(s) for %s (1 expected)", n, r.CPEID)
	evs := t.Events(run)
	t.Check(countEvents(evs, "kafka_consumed") == 1, "%d kafka_consumed event(s) (one logical Kafka result)", countEvents(evs, "kafka_consumed"))
	if r.BCSSuccess.Valid {
		t.Check(r.BCSSuccess.Bool, "BCS outcome recorded separately: is_bcs_success = %v", r.BCSSuccess.Bool)
	} else {
		t.Warn("BCS outcome still pending — the BCS sweeper polls after BCS_RESULT_TIMEOUT_MINUTES=%d (OUT-04 waits for it)",
			t.nocInt("BCS_RESULT_TIMEOUT_MINUTES", 5))
	}
	t.Warn("service verification has no record — NOC verifies by hand this release (OUT-03)")
}

func caseADM02(t *T) {
	tk, run, t0 := start(t, "30_transferred_ca1_broken")
	r := t.WaitDone(run)
	checkCompletedOnce(t, tk, r)
	t.Check(r.RemoteResolved.Valid && !r.RemoteResolved.Bool, "is_remote_resolved = 0")
	t.Check(r.BCSJob == "" && !r.BCSSuccess.Valid, "no BCS job and no BCS result (BCS not required)")
	n := t.BCSStarts(r.CPEID, t0)
	t.Check(n == 0, "Peppermint mock received %d BCS request(s) (0 expected)", n)
}

// gateCase is one invalid eligibility gate: the criteria sample (plus field
// overrides) that fails only that gate, and the RT-side gate event expected.
type gateCase struct {
	sub, title string // sub-case suffix (ADM-03a …) and Burmese title
	criteria   string
	sets       []string
	event      string
}

// specGates are the five invalid gates ADM-03 names, one sub-case each.
var specGates = []gateCase{
	{"a", "CPE ID မပါ", "02_not_eligible_cpe_missing", nil, EventCPEOrLocalServiceMissing},
	{"b", "Local Service ID မပါ", "30_transferred_ca1_broken", []string{"custom_fields.local_service_id="}, EventCPEOrLocalServiceMissing},
	{"c", "OPI site code ပါနေ", "03_not_eligible_opi", nil, EventOPICustomerExcluded},
	{"d", "ခွင့်မပြုတဲ့ service type", "04_not_eligible_service_type", nil, EventServiceTypeNotEligible},
	{"e", "ခွင့်မပြုတဲ့ ticket status", "05_not_eligible_ticket_status", nil, EventTicketStatusNotAllowed},
}

// queueGate is not in ADM-03's list (the RT side checks the queue before it
// calls the API), but V2-01 asks the API to apply the same rule set.
var queueGate = gateCase{"", "NOC queue မဟုတ်", "01_not_eligible_queue", nil, EventNotInNOCQueue}

// label names a gate sample in step messages.
func (g gateCase) label() string {
	if len(g.sets) == 0 {
		return g.criteria
	}
	return g.criteria + " + " + strings.Join(g.sets, " ")
}

// checkGate runs one invalid gate on both sides: rt_web_ui's gates (what
// the RT side does before calling the API) and the API on a fresh ticket.
func checkGate(t *T, g gateCase) {
	p, err := readPayload(filepath.Join(t.env.dir, g.criteria+".json"))
	if err != nil {
		t.Fatal("%v", err)
	}
	for _, set := range g.sets {
		if err := p.Set(set); err != nil {
			t.Fatal("%s: %v", set, err)
		}
	}
	got := CheckEligibility(t.env.cfg, p)
	t.Check(got != nil && got.Event == g.event, "RT side: %s → gate %s (%s expected)", g.label(), gateEvent(got), g.event)
	apiRejects(t, g.criteria, g.sets...)
}

// gateRunner is the runner of one ADM-03 sub-case.
func gateRunner(g gateCase) func(*T) {
	return func(t *T) { checkGate(t, g) }
}

// apiRejects submits a gate sample straight to the API on a fresh ticket
// (a shared ticket would be locked by any sample the API admits, and the
// later samples would only get "202, no run_id" without being evaluated)
// and checks it is refused with a reason and no automation effect.
func apiRejects(t *T, sample string, sets ...string) {
	tk := t.NewTicket(sample)
	a := t.Submit(sample, tk, sets...)
	criteria := gateCase{criteria: sample, sets: sets}.label() // for messages
	switch {
	case a.Status >= 500:
		t.Fail("%s: API answered %d — a failed rule must not look like an API outage", criteria, a.Status)
		return
	case a.Status >= 400:
		t.Pass("%s: API rejected it up front (HTTP %d %s)", criteria, a.Status, a.Message)
	case a.RunID == "":
		t.Fail("%s: HTTP %d without run_id — suppressed (active run / cooldown), the rule was not evaluated", criteria, a.Status)
		return
	default:
		// Stop waiting once the run is past the rules: a gate the API does
		// not apply shows up as WORKFLOW_EVALUATED / PUBLISHED, not a timeout.
		r := t.WaitRun(a.RunID, time.Minute, "finished or past the rules", func(r runRow) bool {
			return r.Done() || (r.State != "" && r.State != "RECEIVED" && r.State != "FETCHING_CPE_STATUS")
		})
		if !r.Done() {
			t.Fail("%s: the API does not apply this rule — the run passed it and went on to %s (target_queue %q); rtutil may now change RT ticket #%d",
				criteria, r.State, r.TargetQueue, tk)
			return
		}
		t.Check(r.State == "NOT_ELIGIBLE" && r.Reason != "", "%s: %s · reason %q", criteria, r.State, r.Reason)
		t.Check(countEvents(t.Events(a.RunID), "kafka_consumed") == 0, "%s: nothing reached Kafka / rtutil", criteria)
	}
	rt := t.Ticket(tk)
	t.Check(rt.Queue == t.env.cfg.NOCQueue && rt.Status == "new", "%s: RT ticket #%d untouched (%s / %s)", criteria, tk, rt.Queue, rt.Status)
}

func caseADM03(t *T) {
	for _, g := range specGates {
		t.Log("— %s", g.title)
		checkGate(t, g)
	}

	t.Log("positive controls: service type G2 Net + status In_Progress")
	_, run, _ := start(t, "30_transferred_ca1_broken", "custom_fields.service_type=G2 Net", "status=In_Progress")
	r := t.WaitRun(run, 2*time.Minute, "past the rules", func(r runRow) bool {
		return r.State != "" && r.State != "RECEIVED" && r.State != "FETCHING_CPE_STATUS"
	})
	t.Check(r.State != "NOT_ELIGIBLE", "accepted service type / status alias passes the rules (%s)", r.State)
}

func gateEvent(g *GateFailure) string {
	if g == nil {
		return "none"
	}
	return g.Event
}

func caseADM04(t *T) {
	sets := []string{"queue=Customer Support"} // + cpe missing in the sample
	p, _ := readPayload(t.env.dir + "/02_not_eligible_cpe_missing.json")
	for _, s := range sets {
		p.Set(s)
	}
	g := CheckEligibility(t.env.cfg, p)
	t.Check(g != nil && g.Event == EventNotInNOCQueue, "two failed rules → first gate in documented order wins (%s)", gateEvent(g))

	tk := t.NewTicket("two rules")
	var reasons []string
	for i := 0; i < 2; i++ {
		a := t.Submit("02_not_eligible_cpe_missing", tk, sets...)
		if a.RunID == "" {
			reasons = append(reasons, fmt.Sprintf("HTTP %d %s", a.Status, a.Message))
			continue
		}
		reasons = append(reasons, t.WaitRun(a.RunID, time.Minute, "finished", runRow.Done).Reason)
	}
	t.Check(reasons[0] == reasons[1], "API is deterministic: same reason twice (%q)", reasons[0])

	t.Log("malformed field: cpe_id with '-'")
	tk2 := t.NewTicket("malformed cpe")
	a := t.Submit("30_transferred_ca1_broken", tk2, "custom_fields.cpe_id=CRIT-30")
	if a.RunID == "" {
		t.Check(a.Status == http.StatusBadRequest, "malformed cpe_id rejected up front (HTTP %d)", a.Status)
		return
	}
	r := t.WaitRun(a.RunID, time.Minute, "finished", runRow.Done)
	t.Check(!r.TicketCompleted() && r.Reason != "", "malformed cpe_id → %s · reason %q", r.State, r.Reason)
	t.Check(countEvents(t.Events(a.RunID), "kafka_consumed") == 0, "malformed cpe_id never reached rtutil")
}

func caseADM07(t *T) {
	tk := t.NewTicket("concurrent")
	t0 := time.Now()
	var wg sync.WaitGroup
	res := make([]admission, 2)
	for i := range res {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res[i] = t.Submit("30_transferred_ca1_broken", tk)
		}()
	}
	wg.Wait()
	admitted := 0
	for _, a := range res {
		if a.RunID != "" {
			admitted++
		}
	}
	t.Check(admitted == 1, "%d of 2 concurrent requests got a run_id (1 expected)", admitted)
	active := 0
	for _, r := range t.RunsForTicket(tk, t0) {
		if !r.Done() {
			active++
		}
	}
	t.Check(active <= 1, "%d active run(s) for the ticket (at most 1)", active)
	t.Warn("the losing request gets 202 without run_id — the same answer as cooldown (see V2-02)")
}

func caseADM08(t *T) {
	reqID := newRunID()
	tkA := t.NewTicket("request id A")
	a1 := t.Submit("30_transferred_ca1_broken", tkA, "run_id="+reqID)
	t.Check(a1.RunID == reqID, "API keeps the caller's stable request ID (sent %s, got %q)", reqID[:8], a1.RunID)

	tkB := t.NewTicket("request id B")
	t0 := time.Now()
	a2 := t.Submit("31_transferred_ca1_low_signal", tkB, "run_id="+reqID)
	runs := t.RunsForTicket(tkB, t0)
	t.Check(a2.RunID == "" && len(runs) == 0,
		"same request ID with different content → explicit conflict, no new run (HTTP %d, run_id %q, %d run(s))", a2.Status, a2.RunID, len(runs))
}

// seedRun inserts a finished run for the case's own ticket, with times moved
// back by ago, and deletes it when the case ends.
func seedRun(t *T, tk uint64, state string, ago time.Duration) string {
	id := newRunID()
	at := time.Now().Add(-ago)
	t.Exec(`INSERT INTO pipeline_runs (run_id, ticket_id, current_state, last_state_reason, created_at, updated_at, completed_at)
		VALUES (?, ?, ?, 'seeded by rt_web_ui acceptance', ?, ?, ?)`, id, tk, state, at, at, at)
	t.Defer(func() { t.env.db.Exec(`DELETE FROM pipeline_runs WHERE run_id = ?`, id) })
	t.Log("seeded %s run %s… for #%d, finished %s ago", state, id[:8], tk, ago)
	return id
}

func caseADM09(t *T) {
	const window = 24 * time.Hour
	tk := t.NewTicket("cooldown")
	seedRun(t, tk, "TICKET_COMPLETED", window-2*time.Minute)

	a := t.Submit("30_transferred_ca1_broken", tk)
	t.Check(a.RunID == "", "just before 24 h: suppressed (run_id %q)", a.RunID)
	var processed []string
	for _, r := range t.RunsForTicket(tk, time.Now().Add(-time.Minute)) {
		if r.State == "ALREADY_PROCESSED" {
			processed = append(processed, r.RunID)
		}
	}
	t.Check(len(processed) == 1, "one ALREADY_PROCESSED record for the suppressed request (%d)", len(processed))

	t.Log("move the real run past 24 h; the ALREADY_PROCESSED record stays recent")
	t.Exec(`UPDATE pipeline_runs SET created_at = created_at - INTERVAL 4 MINUTE, completed_at = completed_at - INTERVAL 4 MINUTE,
		updated_at = updated_at - INTERVAL 4 MINUTE WHERE ticket_id = ? AND current_state = 'TICKET_COMPLETED'`, tk)
	a = t.Submit("30_transferred_ca1_broken", tk)
	t.Check(a.RunID != "", "just after 24 h: admitted — an ALREADY_PROCESSED record must not extend the window (run_id %q)", a.RunID)
	if a.RunID != "" {
		return
	}

	t.Log("move the ALREADY_PROCESSED records past 24 h too")
	t.Exec(`UPDATE pipeline_runs SET created_at = NOW() - INTERVAL 25 HOUR, completed_at = NOW() - INTERVAL 25 HOUR
		WHERE ticket_id = ? AND current_state = 'ALREADY_PROCESSED'`, tk)
	a = t.Submit("30_transferred_ca1_broken", tk)
	t.Check(a.RunID != "", "with every record past 24 h the ticket is admitted (run_id %q)", a.RunID)

	tk2 := t.NewTicket("cooldown not eligible")
	seedRun(t, tk2, "NOT_ELIGIBLE", time.Hour)
	a = t.Submit("30_transferred_ca1_broken", tk2)
	t.Check(a.RunID != "", "a NOT_ELIGIBLE attempt 1 h ago does not start the cooldown (run_id %q)", a.RunID)
	t.Warn("the exact cutoff instant needs a controllable clock — covered only by unit tests")
}

func caseADM11(t *T) {
	_, run, t0 := start(t, "40_cpe_not_found")
	r := t.WaitRun(run, 2*time.Minute, "finished", runRow.Done)
	t.Check(r.State == "CPE_NOT_FOUND", "confirmed missing CPE → %s", r.State)
	evs := t.Events(run)
	t.Check(countEvents(evs, "kafka_consumed") == 0, "no evaluated result published")
	t.Check(t.BCSStarts(r.CPEID, t0) == 0, "no BCS request")
	t.Warn("Node-RED timeout path is exercised by OPS-01 (mock stopped)")
}

// ---------------------------------------------------------------------------
// 2. Restart recovery

// waitState polls fast for one state, so a short-lived state is not missed.
func waitState(t *T, runID, state string, timeout time.Duration) (runRow, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		r := t.Run(runID)
		if r.State == state {
			return r, true
		}
		if r.Done() {
			return r, false
		}
		t.Sleep(300 * time.Millisecond)
	}
	return t.Run(runID), false
}

func caseRCV02(t *T) {
	for _, state := range []string{"CONSUMED", "BCS_UPDATING", "RT_UPDATING"} {
		t.Log("— kill rtutil in %s", state)
		tk, run, t0 := start(t, "11_bcs_success")
		if r, ok := waitState(t, run, state, 5*time.Minute); !ok {
			t.Warn("run passed %s before it was seen (now %s) — state not exercised", state, r.State)
			continue
		}
		t.KillService("rtutil")
		t.Up()
		r := t.WaitDone(run)
		checkCompletedOnce(t, tk, r)
		n := t.BCSStarts(r.CPEID, t0)
		t.Check(n == 1, "after the restart: %d BCS start request(s) (1 expected)", n)
	}
}

func caseRCV05(t *T) {
	want := map[string]int{"API_STUCK_TIMEOUT_MINUTES": 5, "PUBLISHED_TIMEOUT_MINUTES": 5, "CPE_FETCH_TIMEOUT_MINUTES": 5,
		"RTUTIL_STUCK_TIMEOUT_MINUTES": 10, "BCS_RESULT_TIMEOUT_MINUTES": 5, "BCS_RESULT_GIVE_UP_MINUTES": 10}
	for _, k := range []string{"API_STUCK_TIMEOUT_MINUTES", "PUBLISHED_TIMEOUT_MINUTES", "CPE_FETCH_TIMEOUT_MINUTES",
		"RTUTIL_STUCK_TIMEOUT_MINUTES", "BCS_RESULT_TIMEOUT_MINUTES", "BCS_RESULT_GIVE_UP_MINUTES"} {
		got := t.nocInt(k, -1)
		t.Check(got == want[k], "%s = %d (release value %d)", k, got, want[k])
	}
	t.Check(t.nocBool("PUBLISHED_TIMEOUT_ENABLED"), "PUBLISHED_TIMEOUT_ENABLED = true")

	t.RequireAPI()
	limit := time.Duration(t.nocInt("RTUTIL_STUCK_TIMEOUT_MINUTES", 10)) * time.Minute
	// Below stays below while two sweeps pass (~2 min); above is over at once.
	below := seedOverdue(t, "RT_UPDATING", 1, limit-4*time.Minute)[0]
	above := seedOverdue(t, "RT_UPDATING", 1, limit+time.Minute)[0]
	waitSweeps(t, 2)
	rb, ra := t.Run(below), t.Run(above)
	t.Check(!rb.Done(), "below the cutoff (seeded %s idle): left alone (%s)", limit-4*time.Minute, rb.State)
	t.Check(ra.Done() && ra.State == "FAILED_PERMANENT", "above the cutoff (%s): closed for NOC (%s %q)", limit+time.Minute, ra.State, ra.Reason)
}

func caseRCV06(t *T) {
	var legacy int
	if err := t.env.db.QueryRowContext(t.ctx,
		`SELECT COUNT(*) FROM pipeline_runs WHERE current_state = 'COMPLETED'`).Scan(&legacy); err != nil {
		t.Fatal("%v", err)
	}
	t.Check(legacy == 0, "%d run(s) still in the pre-upgrade state COMPLETED (rtdatacore ≥ v1.1.21 writes TICKET_COMPLETED and no longer treats COMPLETED as terminal) — needs the data migration", legacy)
}

// ---------------------------------------------------------------------------
// 3. BCS / RT boundaries

func caseACT02(t *T) {
	tk, run, t0 := start(t, "11_bcs_success")
	r := t.WaitRun(run, 5*time.Minute, "holding a BCS job", func(r runRow) bool { return r.BCSJob != "" || r.Done() })
	if r.Done() {
		t.Warn("run finished before the job reference was seen (%s)", r.State)
	} else {
		job := r.BCSJob
		t.Log("BCS job %s stored in %s", job, r.State)
		t.KillService("rtutil")
		t.Up()
		r = t.WaitDone(run)
		t.Check(r.BCSJob == job, "restart kept the original job reference (%q)", r.BCSJob)
	}
	checkCompletedOnce(t, tk, r)
	n := t.BCSStarts(r.CPEID, t0)
	t.Check(n == 1, "Peppermint mock accepted %d BCS job(s) (1 expected)", n)
}

func caseACT07(t *T) {
	t.Log("— NOC moves the ticket out of NOC before rtutil checks it")
	tk, run, t0 := start(t, "11_bcs_success")
	t.SetTicket(tk, map[string]string{"Queue": "Customer Support"})
	r := t.WaitDone(run)
	t.Check(r.State == "RT_UPDATE_FAILED" && strings.Contains(strings.ToLower(r.Reason), "queue"),
		"stale instruction → %s · reason %q", r.State, r.Reason)
	t.Check(t.BCSStarts(r.CPEID, t0) == 0, "no BCS request for a moved ticket")
	rt := t.Ticket(tk)
	t.Check(rt.Queue == "Customer Support", "operator's queue kept (%s)", rt.Queue)
	t.Check(t.CommentCount(tk, runMarker(run)) == 0, "no automation comment on the moved ticket")

	t.Log("— NOC changes the status while the ticket stays in NOC")
	tk2, run2, _ := start(t, "30_transferred_ca1_broken")
	t.SetTicket(tk2, map[string]string{"Status": "open"})
	r2 := t.WaitDone(run2)
	rt2 := t.Ticket(tk2)
	t.Check(!r2.TicketCompleted() || rt2.Status == "open",
		"operator status change honored: run %s, RT status %q (automation must skip or hand off, not overwrite)", r2.State, rt2.Status)
}

func caseACT09(t *T) {
	msgs := map[string]string{}
	for _, c := range []string{"14_bcs_rejected", "15_bcs_unavailable"} {
		tk, run, _ := start(t, c)
		r := t.WaitDone(run)
		checkCompletedOnce(t, tk, r)
		t.Check(r.BCSSuccess.Valid && !r.BCSSuccess.Bool && r.BCSMessage != "", "%s: BCS failed with message %q", c, r.BCSMessage)
		msgs[c] = r.BCSMessage
	}
	t.Check(msgs["14_bcs_rejected"] != msgs["15_bcs_unavailable"],
		"confirmed rejection (401) and unavailable (503) are told apart")
	t.Warn("RT timeout before / after RT accepts needs an RT simulator that withholds the reply — not covered here")
}

func caseACT10(t *T) {
	tk := t.NewTicket("marker lookalike")
	t.Comment(tk, "operator note <!-- rr:00000000-0000-4000-8000-000000000000 --> rr: lookalike")
	t.Log("operator comment with another run's marker added")
	run := t.Admitted(t.Submit("30_transferred_ca1_broken", tk))
	r := t.WaitDone(run)
	checkCompletedOnce(t, tk, r)
}

// ---------------------------------------------------------------------------
// 5. BCS results / report

// waitBCSResult waits for the BCS sweeper's poll (BCS_RESULT_TIMEOUT_MINUTES
// after the job) to fill is_bcs_success.
func waitBCSResult(t *T, run string) runRow {
	d := time.Duration(t.nocInt("BCS_RESULT_TIMEOUT_MINUTES", 5)+3) * time.Minute
	return t.WaitRun(run, d, "with a BCS result", func(r runRow) bool { return r.BCSSuccess.Valid })
}

func caseOUT01(t *T) {
	want := map[string]string{
		"30_transferred_ca1_broken": "no_bcs_needed", "11_bcs_success": "bcs_ok",
		"12_bcs_failed": "bcs_failed", "13_bcs_pending": "bcs_pending",
	}
	runs := map[string]string{}
	for c := range want {
		_, run, _ := start(t, c)
		runs[c] = run
	}
	for c, run := range runs {
		r := t.WaitDone(run)
		t.Check(r.TicketCompleted(), "%s: %s", c, r.State)
	}
	waitBCSResult(t, runs["11_bcs_success"])
	waitBCSResult(t, runs["12_bcs_failed"])

	base := "http://" + reportAddr(defaultReportDir)
	for c, cs := range want {
		in, err := reportHas(t.ctx, base, cs, runs[c])
		if err != nil {
			t.Skip("pipeline_report at %s: %v", base, err)
		}
		t.Check(in, "%s shows in the report's %s list", c, cs)
	}
	t.Warn("submission unknown / result unknown have no report bucket yet (🟡)")
}

// reportHas reports whether pipeline_report lists runID under case c.
func reportHas(ctx context.Context, base, c, runID string) (bool, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/pipeline-runs/today?summary=0&limit=200&case="+c, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("HTTP %d for case %s", resp.StatusCode, c)
	}
	var out struct {
		Runs struct {
			Items []struct {
				RunID string `json:"run_id"`
			} `json:"items"`
		} `json:"runs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return false, err
	}
	for _, it := range out.Runs.Items {
		if it.RunID == runID {
			return true, nil
		}
	}
	return false, nil
}

func caseOUT04(t *T) {
	_, run, t0 := start(t, "11_bcs_success")
	t.WaitDone(run)
	t.Log("the mock sends no webhook; waiting for the BCS sweeper's poll")
	r := waitBCSResult(t, run)
	t.Check(r.BCSSuccess.Bool, "poll recorded success against job %s", r.BCSJob)
	polls := 0
	for _, c := range t.MockCalls() {
		if strings.EqualFold(c.Method, http.MethodGet) && strings.HasSuffix(c.Path, "/"+r.BCSJob) && !c.At.Before(t0) {
			polls++
		}
	}
	t.Check(polls >= 1, "Peppermint mock was polled %d time(s) for the original job", polls)
	n := countEvents(t.Events(run), "bcs_update_succeeded")
	t.Check(n == 1, "%d bcs_update_succeeded event(s) (1 expected)", n)
	t.Check(t.Run(run).TicketCompleted(), "run stays ticket-completed")
}

func caseOUT06(t *T) {
	_, run, _ := start(t, "13_bcs_pending")
	t.WaitDone(run)
	giveUp := time.Duration(t.nocInt("BCS_RESULT_GIVE_UP_MINUTES", 10)+3) * time.Minute
	t.Log("the mock keeps the job running; waiting past the give-up time (%s)", giveUp)
	deadline := time.Now().Add(giveUp)
	var evs []eventRow
	for time.Now().Before(deadline) {
		evs = t.Events(run)
		if countEvents(evs, "bcs_result_gave_up") > 0 || countEvents(evs, "bcs_update_failed") > 0 {
			break
		}
		t.Sleep(15 * time.Second)
	}
	r := t.Run(run)
	t.Check(countEvents(evs, "bcs_result_gave_up") == 1, "give-up recorded as bcs_result_gave_up (unknown), not bcs_update_failed (%d / %d)",
		countEvents(evs, "bcs_result_gave_up"), countEvents(evs, "bcs_update_failed"))
	t.Check(!r.BCSSuccess.Valid, "outcome stays unknown (is_bcs_success NULL), not a confirmed failure")
	t.Check(r.TicketCompleted(), "run stays ticket-completed")
	t.Warn("late authoritative callback after give-up is not sent by the mock — not covered")
}

// ---------------------------------------------------------------------------
// 6. Sweepers

func caseOPS01(t *T) {
	if t.nocBool("RETRY_WORKER_ENABLED") {
		t.Skip("RETRY_WORKER_ENABLED=true in %s — OPS-01 checks the retry-off release config", t.env.nocEnv)
	}
	tk := t.NewTicket("node-red down")
	t.StopMock()
	t.Defer(t.StartMock)
	run := t.Admitted(t.Submit("30_transferred_ca1_broken", tk))
	d := time.Duration(t.nocInt("CPE_FETCH_TIMEOUT_MINUTES", 5)+3) * time.Minute
	r := t.WaitRun(run, d, "finished", runRow.Done)
	t.Check(r.State == "FAILED_PERMANENT" || r.State == "CPE_NOT_FOUND", "Node-RED down → manual-check state %s", r.State)
	t.Check(r.Reason != "", "reason names the stage: %q", r.Reason)
	var retries int
	t.env.db.QueryRowContext(t.ctx, `SELECT COUNT(*) FROM retry_jobs WHERE run_id = ?`, run).Scan(&retries)
	t.Check(retries == 0, "%d retry job(s) queued (0 with retries off)", retries)
}

// seedOverdue inserts n synthetic runs stuck in state for age, on ticket ids
// no RT ticket uses, and deletes them (and their events) when the case ends.
func seedOverdue(t *T, state string, n int, age time.Duration) []string {
	base := uint64(4_000_000_000) + uint64(time.Now().UnixNano()%90_000_000)
	at := time.Now().Add(-age)
	ids := make([]string, n)
	for i := range ids {
		ids[i] = newRunID()
		t.Exec(`INSERT INTO pipeline_runs (run_id, ticket_id, current_state, last_state_reason, created_at, updated_at)
			VALUES (?, ?, ?, 'seeded by rt_web_ui acceptance', ?, ?)`, ids[i], base+uint64(i), state, at, at)
	}
	t.Defer(func() {
		for _, id := range ids {
			t.env.db.Exec(`DELETE FROM pipeline_run_events WHERE run_id = ?`, id)
			t.env.db.Exec(`DELETE FROM pipeline_runs WHERE run_id = ?`, id)
		}
	})
	t.Log("seeded %d %s run(s), last change %s ago", n, state, age)
	return ids
}

func waitSweeps(t *T, n int) {
	d := time.Duration(t.nocInt("PUBLISHED_SWEEP_INTERVAL_MS", 60000)*n)*time.Millisecond + 10*time.Second
	t.Log("waiting %s for %d sweep(s)", d.Round(time.Second), n)
	t.Sleep(d)
}

// drain seeds n overdue PUBLISHED runs and measures how the sweeper closes
// them: every run closed once, as FAILED_PERMANENT, within the time limit.
func drain(t *T, n int, limit time.Duration) {
	if !t.nocBool("PUBLISHED_TIMEOUT_ENABLED") {
		t.Skip("PUBLISHED_TIMEOUT_ENABLED is off in %s", t.env.nocEnv)
	}
	t.RequireAPI()
	age := time.Duration(t.nocInt("PUBLISHED_TIMEOUT_MINUTES", 5)+2) * time.Minute
	ids := seedOverdue(t, "PUBLISHED", n, age)
	t0 := time.Now()
	var first time.Duration
	closed := 0
	for time.Since(t0) < limit {
		closed = 0
		for _, id := range ids {
			if t.Run(id).Done() {
				closed++
			}
		}
		if closed > 0 && first == 0 {
			first = time.Since(t0)
			t.Log("first handoff visible after %s", first.Round(time.Second))
		}
		if closed == n {
			break
		}
		t.Sleep(2 * time.Second)
	}
	t.Check(closed == n, "%d of %d overdue runs closed in %s (limit %s)", closed, n, time.Since(t0).Round(time.Second), limit)
	bad := 0
	for _, id := range ids {
		r := t.Run(id)
		evs := 0
		for _, e := range t.Events(id) {
			if e.To == r.State {
				evs++
			}
		}
		if r.State != "FAILED_PERMANENT" || evs != 1 {
			bad++
		}
	}
	t.Check(bad == 0, "every closed run is FAILED_PERMANENT with exactly one closing event (%d not)", bad)
}

func caseOPS06(t *T) {
	interval := time.Duration(t.nocInt("PUBLISHED_SWEEP_INTERVAL_MS", 60000)) * time.Millisecond
	for _, n := range []int{20, 45} {
		t.Log("— %d overdue runs (batch %d)", n, t.nocInt("SWEEPER_BATCH_SIZE", 20))
		drain(t, n, 2*interval+30*time.Second)
	}
}

func caseOPS13(t *T) {
	interval := time.Duration(t.nocInt("PUBLISHED_SWEEP_INTERVAL_MS", 60000)) * time.Millisecond
	drain(t, 300, interval+2*time.Minute)
	t.Warn("pilot traffic during the drain and API/DB latency are not measured here — record them on staging")
}

// ---------------------------------------------------------------------------
// 7. V2 admission

func caseV201(t *T) {
	for _, g := range append([]gateCase{queueGate}, specGates...) {
		t.Log("— %s", g.title)
		apiRejects(t, g.criteria, g.sets...)
	}
}

func caseV202(t *T) {
	tk := t.NewTicket("dispositions")
	first := t.Submit("30_transferred_ca1_broken", tk)
	t.Check(first.Status == http.StatusAccepted && first.RunID != "", "admitted: 202 with run_id")
	active := t.Submit("30_transferred_ca1_broken", tk)
	t.Check(active.RunID == "", "active run: no new run")

	tk2 := t.NewTicket("dispositions cooldown")
	seedRun(t, tk2, "TICKET_COMPLETED", time.Hour)
	cool := t.Submit("30_transferred_ca1_broken", tk2)
	t.Check(cool.RunID == "", "cooldown: suppressed")

	t.Check(active.Status != cool.Status || active.Message != cool.Message,
		"client can tell active-run from cooldown (%d %q vs %d %q)", active.Status, active.Message, cool.Status, cool.Message)
	t.Check(active.Message != first.Message || active.RunID != "",
		"a suppressed request does not read like a new admission (%q vs %q)", active.Message, first.Message)
}
