package main

import (
	_ "embed"
	"fmt"
	"regexp"
	"strings"
)

// The 81 acceptance cases. Given / Expected / gate come from the spec file
// (embedded, so the page always shows the contract the code is checked
// against); this file adds the Burmese summary, the current status from the
// review table, and either a runner or the reason it cannot run from here.

//go:embed acceptance/noc_remote_resolve_tdd_acceptance.md
var acceptSpec string

type acceptGroup struct {
	Key   string `json:"key"`
	Title string `json:"title"`
}

var acceptGroups = []acceptGroup{
	{"ADM", "1. Admission"},
	{"PUB", "2. MariaDB / Kafka / restart"},
	{"ACT", "3. BCS / RT လုပ်ဆောင်ချက်"},
	{"STALE", "4. Stale work / NOC လွှဲ"},
	{"OUT", "5. BCS ရလဒ် / report"},
	{"OPS", "6. Sweeper / capacity"},
	{"V2", "7. V2 admission / Redis"},
}

type acceptCase struct {
	ID       string `json:"id"`
	Group    string `json:"group"`
	Gate     string `json:"gate"`    // "P / integration" …
	Profile  string `json:"profile"` // P, U or V
	Given    string `json:"given"`
	Expected string `json:"expected"`
	Title    string `json:"title"`    // Burmese summary
	TitleEn  string `json:"title_en"` // English summary (language toggle)
	Known    string `json:"known"`    // status in the review table: ok, partial, missing, later, na
	Skip     string `json:"skip"`     // why it cannot run from rt_web_ui ("" = runnable)
	SkipEn   string `json:"skip_en,omitempty"`
	Gap      string `json:"gap,omitempty"` // why the review status is not ✅: what the release still lacks
	GapEn    string `json:"gap_en,omitempty"`
	Covered  string `json:"covered"`          // tests elsewhere that cover it
	Minutes  int    `json:"minutes"`          // rough run time
	Parent   string `json:"parent,omitempty"` // spec case a sub-case runs one part of
	run      func(*T)
}

type caseDef struct {
	title, known string
	run          func(*T)
	minutes      int
	skip         string
	covered      string
	gap          string // why known is partial / missing: what is still lacking
}

const (
	kOK      = "ok"      // ✅ code + automated test
	kPartial = "partial" // 🟡
	kMissing = "missing" // ❌
	kLater   = "later"   // ⚪ not this release
	kNA      = "na"      // ➖ does not apply to this design
)

// Skip reasons shared by many cases.
const (
	skipFault       = "API / rtutil ထဲက checkpoint တစ်ခုမှာ crash (သို့) barrier လိုတယ် — အပြင်ကနေ အချိန်ကိုက် မလုပ်နိုင်"
	skipU           = "U profile: retry worker / relay ကို ဒီ release မှာ ပိတ်ထား"
	skipV           = "V profile: Redis importer နဲ့ fault injection လို"
	skipNoRequestID = "Design မှာ client က request ID မပို့ပါ — run_id ကို RT External API ကပဲ ထုတ်ပါတယ်။ Duplicate ကို ticket ID အလိုက် active-run lock (ADM-07) နဲ့ cooldown (ADM-09) က ကာကွယ်ပါတယ်"
	skipContact     = "Peppermint / RT အစစ်ရဲ့ contract — staging မှာပဲ စစ်လို့ရ"
)

var caseDefs = map[string]caseDef{
	// 1. Admission
	"ADM-01": {title: "BCS လိုတဲ့ ticket အစအဆုံး (CRIT11)", known: kPartial, run: caseADM01, minutes: 4,
		gap: "Service verification (BCS က channel ပြောင်းပြီးနောက် customer ရဲ့ internet တကယ် ကောင်းသွားလား ပြန်စစ်တာ) ကို system က မလုပ်ပါ၊ မှတ်တမ်းလည်း မရှိပါ။ ဒီ release မှာ NOC က လက်နဲ့ စစ်ပါတယ် (OUT-03)။ BCS confirmation (is_bcs_success၊ FULLY_COMPLETED) ကတော့ ရှိပြီး case က စစ်ပါတယ်။"},
	"ADM-02": {title: "BCS မလိုတဲ့ ticket (CRIT30 transferred)", known: kOK, run: caseADM02, minutes: 4},
	"ADM-03": {title: "Gate ငြင်းတာ ၅ မျိုး + positive control", known: kOK, run: caseADM03, minutes: 5},
	"ADM-04": {title: "Rule ၂ ခု fail · field ပုံစံမှား", known: kPartial, run: caseADM04, minutes: 2},
	"ADM-05": {title: "Commit ပြီး crash → retry", known: kPartial, skip: skipFault + " (202 မပြန်ခင် API ကို kill)", covered: "noc_automation: active-ticket lock tests"},
	"ADM-06": {title: "Commit မတိုင်ခင် fail", known: kOK, skip: "DB transaction ကို ကြားမှာ fail ဖြစ်အောင် လုပ်ဖို့ API ထဲ hook လို", covered: "CreateWithEvent_RollsBack…, CreateRunFailureIs500"},
	"ADM-07": {title: "တစ်ပြိုင်နက် request ၂ ခု", known: kOK, run: caseADM07, minutes: 1},
	"ADM-08": {title: "Stable request ID (တူ ID၊ content ကွဲ)", known: kNA, skip: skipNoRequestID},
	"ADM-09": {title: "၂၄ နာရီ boundary + ALREADY_PROCESSED", known: kOK, run: caseADM09, minutes: 2},
	"ADM-10": {title: "Authorized reprocess", known: kLater, skip: "ဒီ release မှာ မလုပ်ပါ — reprocess feature မရှိ။ ပုံမှန် ပြန်ပို့ရင် cooldown အတိုင်း သွားတာကို ADM-09 က စစ်ပါတယ်"},
	"ADM-11": {title: "CPE fetch: OK · not found · timeout (CRIT30/40/42)", known: kOK, run: caseADM11, minutes: 3},
	"ADM-12": {title: "Kafka payload မမှန်", known: kPartial, skip: "Kafka topic ထဲ message တိုက်ရိုက် ထည့်ဖို့ producer လို (rt_web_ui မှာ Kafka client မရှိ)"},

	// 2. MariaDB / Kafka / restart
	"PUB-01": {title: "Publish မတိုင်ခင် transaction fail", known: kOK, skip: skipFault, covered: "TestGuard_PublishPathIsGuardedAtEveryStep"},
	"PUB-02": {title: "Commit ပြီး Kafka မပို့ခင် crash", known: kOK, skip: skipFault, covered: "TestCrash_BeforeProduce_SweeperClosesRun"},
	"PUB-03": {title: "Kafka ပို့ပြီး PUBLISHED မမှတ်ခင် crash", known: kOK, skip: skipFault, covered: "TestCrash_AfterProduce_RtutilStillClaims"},
	"PUB-04": {title: "PUBLISHED နောက်ကျ ရေး", known: kOK, skip: skipFault, covered: "TestCrash_LatePublishedDoesNotOverwrite"},
	"PUB-05": {title: "Sweeper အရင်ပိတ်ပြီးမှ message ရောက်", known: kOK, skip: skipFault, covered: "TestCrash_SweeperFirst_RtutilSkips, rtutil TestTransitionPipelineRun_Guard"},
	"PUB-06": {title: "Kafka ack ပျောက်", known: kPartial, skip: skipFault},
	"PUB-07": {title: "Relay ဖွင့်ပြီး PUB-02", known: kLater, skip: skipU},
	"PUB-08": {title: "Relay crash + duplicate", known: kLater, skip: skipU},
	"PUB-09": {title: "Kafka outage / retry budget", known: kLater, skip: skipU},
	"RCV-01": {title: "API state ၄ ခုမှာ kill", known: kOK, skip: skipFault + " (API state တွေ ms အတွင်း ပြီး)", covered: "SweepOnce, DefaultTimeouts"},
	"RCV-02": {title: "rtutil ကို CONSUMED / BCS_UPDATING / RT_UPDATING မှာ kill", known: kPartial, run: caseRCV02, minutes: 12},
	"RCV-03": {title: "ပြီးပြီး offset မ commit ခင် crash", known: kOK, skip: skipFault, covered: "TransitionPipelineRun_Guard"},
	"RCV-04": {title: "Partition တူ message ၂ ခု", known: kPartial, skip: skipFault + " (consumer rebalance)"},
	"RCV-05": {title: "Stuck timeout setting + cutoff အောက်/အထက်", known: kOK, run: caseRCV05, minutes: 4},
	"RCV-06": {title: "Upgrade: COMPLETED row အဟောင်း", known: kMissing, run: caseRCV06, minutes: 1},
	"RCV-07": {title: "Event insert fail → rollback", known: kOK, skip: "DB ထဲ event insert ကို fail ဖြစ်အောင် hook လို", covered: "RollsBackWhenEventFails"},

	// 3. BCS / RT
	"ACT-01": {title: "★ Peppermint လက်ခံပြီး reply ပျောက်", known: kOK, skip: skipContact, covered: "X-Request-ID=run_id, ReplayReturnsExistingJob"},
	"ACT-02": {title: "BCS job ref သိမ်းပြီး crash", known: kOK, run: caseACT02, minutes: 4},
	"ACT-03": {title: "Peppermint dedupe window", known: kPartial, skip: skipContact},
	"ACT-04": {title: "★ RT update ပြီး comment မတိုင်ခင် crash", known: kOK, skip: skipFault + " (status နဲ့ comment ကြား ms)", covered: "TestCheckQueue"},
	"ACT-05": {title: "Comment ရေးပြီး reply ပျောက်", known: kOK, skip: skipFault, covered: "HasCommentContaining, TestCommentMarker"},
	"ACT-06": {title: "Worker ၂ ခု comment တစ်ပြိုင်နက်", known: kMissing, skip: "rtutil ၂ ခုကို message တူ ပေးပြီး barrier လို"},
	"ACT-07": {title: "NOC က ticket ရွှေ့ / status ပြောင်း", known: kPartial, run: caseACT07, minutes: 6},
	"ACT-08": {title: "Read ပြီး write မတိုင်ခင် ပြောင်း", known: kMissing, skip: skipFault + " (RT precondition)"},
	"ACT-09": {title: "BCS ငြင်း (401) vs မရ (503)", known: kMissing, run: caseACT09, minutes: 5},
	"ACT-10": {title: "Marker နဲ့ ဆင်တဲ့ comment", known: kOK, run: caseACT10, minutes: 4},
	"ACT-11": {title: "Terminal run အတွက် duplicate event", known: kOK, skip: "Kafka message ထပ်ပို့ဖို့ producer လို"},

	// 4. Stale work
	"STALE-01": {title: "Sweeper ပိတ်ပြီး worker ပြန်လာ", known: kPartial, skip: skipFault},
	"STALE-02": {title: "Request ပို့ပြီး effect မတိုင်ခင် pause", known: kPartial, skip: skipFault},
	"STALE-03": {title: "★ Timeout ပြီး NOC ကိုင်ချိန် အလုပ်ဟောင်း ပြီး", known: kPartial, skip: skipContact + " · NOC manual reconciliation"},
	"STALE-04": {title: "Sweeper နဲ့ worker ပြိုင်", known: kOK, skip: skipFault, covered: "TransitionStateIf"},
	"STALE-05": {title: "Rerun က မပြီးသေးတဲ့ action ကို စစ်", known: kMissing, skip: "Rerun feature မရှိ"},
	"STALE-06": {title: "NOC လေ့ကျင့်ခန်း", known: kPartial, skip: "NOC operator နဲ့ manual exercise"},
	"STALE-07": {title: "Automated recovery မှာ STALE-02/03", known: kLater, skip: skipU},

	// 5. BCS result / report
	"OUT-01": {title: "BCS outcome တိုင်း report မှာ ခွဲပြ", known: kPartial, run: caseOUT01, minutes: 9},
	"OUT-02": {title: "BCS မဖြစ်မနေ / ရွေးချယ်", known: kPartial, skip: "Required vs optional BCS policy မရှိသေး"},
	"OUT-03": {title: "Service verification", known: kMissing, skip: "ဒီ release မှာ NOC manual (မှတ်တမ်း မရှိ)"},
	"OUT-04": {title: "Webhook ပျောက် → poll", known: kOK, run: caseOUT04, minutes: 9},
	"OUT-05": {title: "Webhook ထပ် / ဆန့်ကျင်", known: kPartial, skip: "Webhook ကို ထပ်ပို့/ပြိုင်ဖို့ barrier လို"},
	"OUT-06": {title: "Give-up → unknown (bcs_result_gave_up)", known: kMissing, run: caseOUT06, minutes: 14},
	"OUT-07": {title: "Job ref မတိုင်ခင် webhook / unknown job", known: kOK, skip: "Webhook ကို checkpoint မတိုင်ခင် ပို့ဖို့ timing လို", covered: "run_id lookup, unknown job → 404"},
	"OUT-08": {title: "BCS နာရီ (job-queued time)", known: kMissing, skip: "Clock ထိန်းဖို့ လို (bcs_updated_at)"},
	"OUT-09": {title: "Service measurement stale/fresh", known: kLater, skip: skipU},

	// 6. Sweeper / capacity
	"OPS-01": {title: "Retry ပိတ် → Node-RED fail", known: kPartial, run: caseOPS01, minutes: 7},
	"OPS-02": {title: "Retry: stage တစ်ခုပဲ", known: kLater, skip: skipU},
	"OPS-03": {title: "Fetch retry ပြီး publish fail", known: kLater, skip: skipU},
	"OPS-04": {title: "Attempt budget", known: kLater, skip: skipU},
	"OPS-05": {title: "Lease ကျော်", known: kLater, skip: skipU},
	"OPS-06": {title: "Publish sweeper batch (20 · 45)", known: kOK, run: caseOPS06, minutes: 4},
	"OPS-07": {title: "Round / time budget", known: kPartial, skip: "Slow query / close failure inject လို", covered: "round/time budget unit tests"},
	"OPS-08": {title: "ပိတ်ခါနီး recheck", known: kOK, skip: skipFault, covered: "close recheck tests"},
	"OPS-09": {title: "updated_at writer စည်းမျဉ်း", known: kPartial, skip: "Writer စာရင်း review (document)"},
	"OPS-10": {title: "State re-entry deadline", known: kLater, skip: skipU},
	"OPS-11": {title: "BCS poll တန်းစီ", known: kPartial, skip: "Peppermint နှေး/မရ အခြေအနေ mock မှာ မရှိ"},
	"OPS-12": {title: "Multi-cycle BCS poll", known: kLater, skip: skipU},
	"OPS-13": {title: "Load ၃၀၀ run drain", known: kLater, run: caseOPS13, minutes: 4},
	"OPS-14": {title: "Unattended peak load", known: kLater, skip: skipU},
	"OPS-15": {title: "Sweeper ရပ် → alert", known: kMissing, skip: "Alert မရှိ (log ပဲ)"},

	// 7. V2
	"V2-01": {title: "Rule တွေ API မှာ", known: kOK, run: caseV201, minutes: 3},
	"V2-02": {title: "Disposition ခွဲပြ (admitted / active / cooldown)", known: kPartial, run: caseV202, minutes: 1},
	"V2-03": {title: "Report မဖွင့်ဘဲ background sync", known: kMissing, skip: skipV},
	"V2-04": {title: "Uncertain Redis record", known: kPartial, skip: skipV, covered: "FailedAPIRequestSync_AlreadyProcessed"},
	"V2-05": {title: "Importer commit ပြီး crash", known: kOK, skip: skipV, covered: "DeleteFailureStillCounts"},
	"V2-06": {title: "MariaDB commit fail", known: kOK, skip: skipV, covered: "KeepsKeyOnDBError"},
	"V2-07": {title: "Importer ၂ ခု", known: kOK, skip: skipV, covered: "SkipsLockedKey"},
	"V2-08": {title: "Export side effect", known: kMissing, skip: skipV},
	"V2-09": {title: "Timeout / 429 / invalid / cooldown", known: kPartial, skip: skipV},
	"V2-10": {title: "Redis မရ", known: kPartial, skip: skipV},
	"V2-11": {title: "Import နောက်ကျ cooldown", known: kPartial, skip: skipV},
}

var specRow = regexp.MustCompile(`^\| ([A-Z0-9]+-[0-9]+) \| ([^|]+) \| ([^|]+) \| ([^|]+) \|$`)

// loadAcceptCases parses the spec's case tables and joins caseDefs.
func loadAcceptCases() ([]acceptCase, error) {
	var out []acceptCase
	for _, line := range strings.Split(acceptSpec, "\n") {
		m := specRow.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		id := m[1]
		def, ok := caseDefs[id]
		if !ok {
			return nil, fmt.Errorf("spec case %s has no entry in caseDefs", id)
		}
		group := strings.Split(id, "-")[0]
		if group == "RCV" {
			group = "PUB"
		}
		gate := strings.TrimSpace(m[2])
		c := acceptCase{
			ID: id, Group: group, Gate: gate, Profile: strings.TrimSpace(strings.Split(gate, "/")[0]),
			Given: unmd(m[3]), Expected: unmd(m[4]),
			Title: def.title, Known: def.known, Covered: def.covered, Minutes: def.minutes, run: def.run, Skip: def.skip, Gap: def.gap,
		}
		if c.run == nil && c.Skip == "" {
			return nil, fmt.Errorf("case %s has neither a runner nor a skip reason", id)
		}
		c.TitleEn, c.SkipEn, c.GapEn = en(c.Title), en(c.Skip), en(c.Gap)
		out = append(out, c)
		out = append(out, subCases(c)...)
	}
	if spec := len(out) - countSubCases(out); spec != len(caseDefs) {
		return nil, fmt.Errorf("spec has %d cases, caseDefs %d", spec, len(caseDefs))
	}
	return out, nil
}

// unmd drops the spec's markdown emphasis for plain-text display.
func unmd(s string) string {
	return strings.NewReplacer("**", "", "`", "").Replace(strings.TrimSpace(s))
}

// subCases splits a spec case into parts that can run on their own. ADM-03
// gets one sub-case per invalid gate (ADM-03a … ADM-03e).
func subCases(c acceptCase) []acceptCase {
	if c.ID != "ADM-03" {
		return nil
	}
	out := make([]acceptCase, 0, len(specGates))
	for _, g := range specGates {
		sub := c
		sub.ID, sub.Parent, sub.Title, sub.TitleEn, sub.Minutes = c.ID+g.sub, c.ID, g.title, en(g.title), 1
		sub.Given = fmt.Sprintf("ADM-03 part: submit only the %q sample (%s), which fails this one gate.", g.title, g.label())
		sub.run = gateRunner(g)
		out = append(out, sub)
	}
	return out
}

func countSubCases(cs []acceptCase) int {
	n := 0
	for _, c := range cs {
		if c.Parent != "" {
			n++
		}
	}
	return n
}
