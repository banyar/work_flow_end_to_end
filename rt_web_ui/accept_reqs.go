package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Requirements popup on the acceptance page: what a case needs from the
// stack, why, and whether each one is ready right now. The status comes from
// the same checks as `make check`, run live when the popup opens.

type reqItem struct {
	Name     string `json:"name"`
	Why      string `json:"why"`
	WhyEn    string `json:"why_en"`
	Level    string `json:"level"` // ok, warn, fail, skip
	Detail   string `json:"detail"`
	Fix      string `json:"fix,omitempty"`
	FixEn    string `json:"fix_en,omitempty"`
	Optional bool   `json:"optional,omitempty"`
}

type reqDef struct {
	name, why string
	optional  bool
	check     func(context.Context, *acceptEnv) checkResult
}

type caseReqs struct {
	note string   // one line shown above the table
	reqs []reqDef // everything the case needs, in display order
}

var levelName = map[checkLevel]string{levelOK: "ok", levelSkip: "skip", levelWarn: "warn", levelFail: "fail"}

// ---------------------------------------------------------------------------
// Requirement building blocks

func reqMySQL() reqDef {
	return reqDef{name: "MySQL (rt5 DB · pipeline_runs / pipeline_run_events)", why: "Run နဲ့ event တွေ သိမ်းဖို့၊ case က run ရလဒ်နဲ့ RT ticket ရဲ့ queue / status / comment ကို DB ကနေ ဖတ်ဖို့",
		check: func(ctx context.Context, e *acceptEnv) checkResult { return checkMySQL(ctx, e.cfg, nil) }}
}

func reqAPI() reqDef {
	return reqDef{name: "RT External API (noc_automation)", why: "rt_web_ui ပို့တဲ့ ticket ကို လက်ခံပြီး workflow run ဖို့",
		check: func(ctx context.Context, e *acceptEnv) checkResult { return checkAPI(ctx, e.cfg, nil) }}
}

func reqNodeRed() reqDef {
	return reqDef{name: "Node-RED mock + NODE_RED_BASE_URL", why: "CRITxx CPE status ကို mock က ပြန်ပေးဖို့",
		check: func(ctx context.Context, e *acceptEnv) checkResult {
			r := checkMock(ctx, e.mockPort)
			if r.Level != levelOK {
				return r
			}
			u := checkNodeRedURL(e.noc, nil, e.nocEnv, e.mockPort)
			if u.Level != levelOK {
				return u
			}
			r.Detail += " · " + u.Detail
			return r
		}}
}

func reqKafka() reqDef {
	return reqDef{name: "Kafka", why: "noc_automation က rtutil ဆီ ဆုံးဖြတ်ချက် publish လုပ်ဖို့",
		check: func(ctx context.Context, e *acceptEnv) checkResult { return checkKafka(ctx, e.noc, nil, e.nocEnv) }}
}

func reqRtutil() reqDef {
	return reqDef{name: "rtutil consumer", why: "Kafka message ယူပြီး RT ticket ကို update လုပ်ဖို့",
		check: func(ctx context.Context, _ *acceptEnv) checkResult { return checkRtutil(ctx) }}
}

func reqRT() reqDef {
	return reqDef{name: "RT REST2 + RT_TOKEN", why: "Case တိုင်း test RT ticket အသစ် ဖန်တီးဖို့ (POST /ticket) — ticket ကို ပြန်ဖတ်တာကတော့ DB ကနေပါ",
		check: func(ctx context.Context, e *acceptEnv) checkResult {
			r := checkRTREST(ctx, e.noc, nil, e.nocEnv)
			if r.Level == levelOK && strings.TrimSpace(e.noc["RT_TOKEN"]) == "" {
				r.Level, r.Fix = levelFail, e.nocEnv+" ထဲ RT_TOKEN ထည့်ပါ"
				r.Detail += " · RT_TOKEN not set"
			}
			return r
		}}
}

func reqReport() reqDef {
	return reqDef{name: "pipeline_report", why: "Case က report API ရဲ့ count ကို စစ်လို့",
		check: func(ctx context.Context, _ *acceptEnv) checkResult { return checkReport(ctx, defaultReportDir) }}
}

// reqMockAdmin: cases that count the BCS requests the mock received read its
// admin log with the admin token.
func reqMockAdmin(why string) reqDef {
	return reqDef{name: "Mock admin API (request log)", why: why,
		check: func(ctx context.Context, e *acceptEnv) checkResult {
			r := checkResult{Name: "mock admin"}
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+e.mockPort+"/mockoon-admin/logs?limit=1", nil)
			req.Header.Set("Authorization", "Bearer "+e.mockToken)
			resp, err := http.DefaultClient.Do(req)
			switch {
			case err != nil:
				r.Level, r.Detail, r.Fix = levelFail, "127.0.0.1:"+e.mockPort+" not answering", "make criteria-mock"
			case resp.StatusCode == http.StatusUnauthorized:
				r.Level, r.Detail, r.Fix = levelFail, "admin token refused", "make criteria-mock-stop criteria-mock-up"
			case resp.StatusCode != http.StatusOK:
				r.Level, r.Detail = levelFail, fmt.Sprintf("HTTP %d", resp.StatusCode)
			default:
				r.Level, r.Detail = levelOK, "/mockoon-admin/logs — 200"
			}
			if resp != nil {
				resp.Body.Close()
			}
			return r
		}}
}

// reqMockCPE checks that the mock answers cpe the way the case expects:
// field (dot path under data) = want, or wantStatus when want is "".
func reqMockCPE(cpe, field, want string, wantStatus int, why string) reqDef {
	label := cpe + " → " + field + " " + want
	if want == "" {
		label = fmt.Sprintf("%s → HTTP %d", cpe, wantStatus)
	}
	return reqDef{name: "Mock CPE " + label, why: why,
		check: func(ctx context.Context, e *acceptEnv) checkResult {
			r := checkResult{Name: cpe}
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+e.mockPort+"/api/v1/cpe/"+cpe, nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				r.Level, r.Detail, r.Fix = levelFail, "mock not answering", "make criteria-mock"
				return r
			}
			defer resp.Body.Close()
			if want == "" {
				if resp.StatusCode != wantStatus {
					r.Level, r.Detail = levelFail, fmt.Sprintf("HTTP %d (%d expected)", resp.StatusCode, wantStatus)
				} else {
					r.Level, r.Detail = levelOK, fmt.Sprintf("HTTP %d", resp.StatusCode)
				}
				return r
			}
			var body struct {
				Data map[string]any `json:"data"`
			}
			json.NewDecoder(resp.Body).Decode(&body)
			var v any = body.Data
			for k := range strings.SplitSeq(field, ".") {
				m, _ := v.(map[string]any)
				v = m[k]
			}
			got := fmt.Sprint(v)
			if got != want {
				r.Level, r.Detail, r.Fix = levelFail, fmt.Sprintf("%s = %q (%q expected)", field, got, want), "criteria/mockoon-criteria.json ပြင်ပြီး mock restart"
				return r
			}
			r.Level, r.Detail = levelOK, field+" = "+got
			return r
		}}
}

// reqBCSMock checks the Peppermint routes on the mock with a read-only job
// status call (no channel-selection is started).
func reqBCSMock(why string) reqDef {
	return reqDef{name: "BCS (Peppermint) mock routes", why: why,
		check: func(ctx context.Context, e *acceptEnv) checkResult {
			r := checkResult{Name: "BCS mock"}
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+e.mockPort+"/api/v2/channel-selections/ok-requirements-check", nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				r.Level, r.Detail, r.Fix = levelFail, "mock not answering", "make criteria-mock"
				return r
			}
			defer resp.Body.Close()
			var body struct {
				Status string `json:"status"`
			}
			json.NewDecoder(resp.Body).Decode(&body)
			if resp.StatusCode != http.StatusOK || body.Status != "succeeded_new_channel" {
				r.Level, r.Detail = levelFail, fmt.Sprintf("job status HTTP %d status=%q", resp.StatusCode, body.Status)
				return r
			}
			r.Level, r.Detail = levelOK, "GET /api/v2/channel-selections/ok-… → succeeded_new_channel"
			return r
		}}
}

// reqNocKey checks that noc_automation's .env sets key (and, when want is
// not "", that it contains want).
func reqNocKey(key, want, why string) reqDef {
	return reqDef{name: key + " (noc .env)", why: why,
		check: func(_ context.Context, e *acceptEnv) checkResult {
			r := checkResult{Name: key}
			v := strings.TrimSpace(e.noc[key])
			switch {
			case v == "":
				r.Level, r.Detail, r.Fix = levelFail, "not set in "+e.nocEnv, e.nocEnv+" ထဲ ထည့်ပြီး noc_automation restart"
			case want != "" && !strings.Contains(v, want):
				r.Level, r.Detail, r.Fix = levelWarn, v+" ("+want+" မဟုတ်)", key+" ကို mock သို့ ညွှန်ပါ"
			case strings.Contains(strings.ToLower(key), "token"):
				r.Level, r.Detail = levelOK, "set"
			default:
				r.Level, r.Detail = levelOK, v
			}
			return r
		}}
}

// reqRtutilBCS checks that the running rtutil consumer sends BCS calls to
// the mock: cpems_api.base_url in the --config file it was started with.
// Without it a "0 BCS requests" check passes for the wrong reason.
func reqRtutilBCS(why string) reqDef {
	return reqDef{name: "rtutil cpems_api.base_url → mock", why: why,
		check: func(_ context.Context, e *acceptEnv) checkResult {
			r := checkResult{Name: "rtutil cpems_api"}
			pids := rtutilConsumerPIDs()
			if len(pids) == 0 {
				r.Level, r.Detail, r.Fix = levelFail, "rtutil consumer not running", "make up"
				return r
			}
			path := processFlag(pids[0], "--config")
			if path == "" {
				r.Level, r.Detail = levelWarn, "pid "+pids[0]+" has no --config argument"
				return r
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				r.Level, r.Detail = levelFail, err.Error()
				return r
			}
			var c struct {
				CPEMS struct {
					BaseURL string `json:"base_url"`
				} `json:"cpems_api"`
			}
			if err := json.Unmarshal(raw, &c); err != nil {
				r.Level, r.Detail = levelFail, filepath.Base(path)+": "+err.Error()
				return r
			}
			u, err := url.Parse(c.CPEMS.BaseURL)
			host := ""
			if err == nil {
				host = u.Hostname()
			}
			if (host != "localhost" && host != "127.0.0.1") || u.Port() != e.mockPort {
				r.Level, r.Detail = levelFail, fmt.Sprintf("%s · cpems_api.base_url = %q", filepath.Base(path), c.CPEMS.BaseURL)
				r.Fix = filepath.Base(path) + " ထဲ cpems_api.base_url = http://127.0.0.1:" + e.mockPort + " ထားပြီး rtutil restart"
				return r
			}
			r.Level, r.Detail = levelOK, filepath.Base(path)+" · "+c.CPEMS.BaseURL
			return r
		}}
}

// processFlag returns the value after flag in a process's arguments,
// resolved against the process's working directory.
func processFlag(pid, flag string) string {
	raw, err := os.ReadFile("/proc/" + pid + "/cmdline")
	if err != nil {
		return ""
	}
	argv := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == flag {
			v := argv[i+1]
			if !filepath.IsAbs(v) {
				if cwd, err := os.Readlink("/proc/" + pid + "/cwd"); err == nil {
					v = filepath.Join(cwd, v)
				}
			}
			return v
		}
	}
	return ""
}

// reqRtutilManaged: fault cases SIGKILL rtutil through make up's pid file
// and bring it back with make up; an rtutil started by hand makes them skip.
func reqRtutilManaged() reqDef {
	return reqDef{name: "rtutil started by make up (.run/rtutil.pid)", why: "Case က rtutil ကို pid file နဲ့ kill ပြီး make up နဲ့ ပြန်စဖို့",
		check: func(_ context.Context, e *acceptEnv) checkResult {
			r := checkResult{Name: "rtutil pid"}
			raw, err := os.ReadFile(filepath.Join(e.runDir, "rtutil.pid"))
			if err != nil {
				r.Level, r.Detail, r.Fix = levelFail, "no "+filepath.Join(e.runDir, "rtutil.pid"), "rtutil ကို make up နဲ့ စပါ"
				return r
			}
			pid := strings.TrimSpace(string(raw))
			if _, err := os.Stat("/proc/" + pid); err != nil {
				r.Level, r.Detail, r.Fix = levelFail, "pid "+pid+" not running", "make up"
				return r
			}
			r.Level, r.Detail = levelOK, "pid "+pid
			return r
		}}
}

// reqNocNotTrue: the case skips when key is true in noc_automation's .env.
func reqNocNotTrue(key, why string) reqDef {
	return reqDef{name: key + " = false (noc .env)", why: why,
		check: func(_ context.Context, e *acceptEnv) checkResult {
			r := checkResult{Name: key}
			if b, _ := strconv.ParseBool(strings.TrimSpace(e.noc[key])); b {
				r.Level, r.Detail, r.Fix = levelFail, key+"=true", e.nocEnv+" ထဲ "+key+"=false ထားပြီး noc_automation restart"
				return r
			}
			r.Level, r.Detail = levelOK, key+"="+strings.TrimSpace(e.noc[key])
			if e.noc[key] == "" {
				r.Detail = "not set (off)"
			}
			return r
		}}
}

// reqNocEnv: the case reads timeout values from noc_automation's .env.
// reqMockSlow checks that the mock answers cpe slower than noc's
// NODE_RED_TIMEOUT without waiting for it: no answer within a few seconds
// means the slow route is loaded; a quick answer means the mock was started
// from an older mockoon-criteria.json.
func reqMockSlow(cpe, why string) reqDef {
	return reqDef{name: "Mock CPE " + cpe + " → slow (35 s)", why: why,
		check: func(ctx context.Context, e *acceptEnv) checkResult {
			r := checkResult{Name: cpe}
			if t, err := strconv.Atoi(strings.TrimSpace(e.noc["NODE_RED_TIMEOUT"])); err == nil && t >= 35 {
				r.Level, r.Detail, r.Fix = levelFail, fmt.Sprintf("NODE_RED_TIMEOUT=%d s — the 35 s mock would not time out", t), "noc .env NODE_RED_TIMEOUT ကို 35 အောက် ထားပါ"
				return r
			}
			ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+e.mockPort+"/api/v1/cpe/"+cpe, nil)
			resp, err := http.DefaultClient.Do(req)
			switch {
			case err == nil:
				resp.Body.Close()
				r.Level, r.Detail, r.Fix = levelFail, fmt.Sprintf("answered at once (HTTP %d) — the mock has no slow %s route", resp.StatusCode, cpe), "make criteria-mock-stop criteria-mock-up"
			case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded):
				r.Level, r.Detail = levelOK, "no answer within 2 s (slow route loaded)"
			default:
				r.Level, r.Detail, r.Fix = levelFail, "mock not answering", "make criteria-mock-up"
			}
			return r
		}}
}

func reqNocEnv(why string) reqDef {
	return reqDef{name: "noc_automation .env", why: why,
		check: func(_ context.Context, e *acceptEnv) checkResult {
			r := checkResult{Name: "noc env"}
			if len(e.noc) == 0 {
				r.Level, r.Detail, r.Fix = levelFail, e.nocEnv+" not found or empty", "NOC_ENV=<noc_automation .env> ဖြင့် make up"
				return r
			}
			r.Level, r.Detail = levelOK, e.nocEnv
			return r
		}}
}

func reqDocker(why string) reqDef {
	return reqDef{name: "Docker", why: why,
		check: func(ctx context.Context, _ *acceptEnv) checkResult { return checkDocker(ctx) }}
}

// ---------------------------------------------------------------------------
// Per-case lists. Each list names only what the case's runner uses
// (accept_runs.go); a service a case's runs touch but the case never checks
// is left out.

// admit: create an RT ticket, submit it to the API, read the run from the DB.
func admit() []reqDef { return []reqDef{reqMySQL(), reqAPI(), reqRT()} }

// chain: admit, then the run goes all the way through (CPE fetch, Kafka,
// rtutil's RT update), and the case checks the RT ticket afterwards.
func chain(extra ...reqDef) []reqDef {
	return append(append(admit(), reqNodeRed(), reqKafka(), reqRtutil()), extra...)
}

const (
	bcsNotCalled     = "Mock က BCS request ၀ ခု ရတာ စစ်ဖို့"
	bcsCalled        = "Mock က BCS start request ရတာ စစ်ဖို့"
	noteGate         = "Gate မှာ NOT_ELIGIBLE ဖြစ်လို့ CPE မယူ၊ Kafka မပို့ပါ။ ဒါကြောင့် Node-RED mock၊ Kafka၊ rtutil မလိုပါ။ RT ticket အသစ် ဖန်တီးပြီး မပြောင်းတာကို စစ်ဖို့ RT REST2 လိုပါတယ်။"
	noteAdmit        = "API ရဲ့ admission (run_id / cooldown / active run) ကိုပဲ စစ်ပါတယ်။ Admit ဖြစ်တဲ့ run က နောက်ကွယ်မှာ ဆက် run နိုင်ပေမယ့် case က အဲဒါကို မစစ်လို့ Node-RED mock၊ Kafka၊ rtutil မလိုပါ။"
	rtutilToMock     = "rtutil ရဲ့ BCS call က mock ကို ရောက်ဖို့"
	rtutilToMockZero = "Mock မှာ BCS request ၀ ခု ဆိုတာ rtutil က mock ကို ညွှန်ထားမှ အဓိပ္ပာယ်ရှိ"
	noteSweeper      = "DB ထဲ synthetic row ထည့်ပြီး noc_automation ထဲက sweeper ပိတ်တာကို စောင့်ပါတယ်။ RT ticket မဖန်တီးပါ။"
)

func caseRequirements(id, mockPort string) caseReqs {
	mock := "127.0.0.1:" + mockPort
	switch id {
	case "ADM-01":
		return caseReqs{
			note: "CRIT11 က remote resolved ဖြစ်ပြီး BCS (Peppermint) ကို ခေါ်ပါတယ်။ BCS ရလဒ်ကို webhook နဲ့ ပို့ပါတယ်။",
			reqs: chain(
				reqMockCPE("CRIT11", "ca1.status", "OK", 0, "All good ဖြစ်ပြီး Customer Support (Resolved) သို့ ရောက်ဖို့"),
				reqBCSMock("rtutil က BCS job စဖို့"),
				reqRtutilBCS(rtutilToMock),
				reqNocKey("BCS_WEBHOOK_TOKEN", "", "Case က CPEMS ကိုယ်စား webhook ပို့ဖို့ (မထည့်ရင် API က 503 ပြန်)"),
				reqMockAdmin(bcsCalled),
			)}
	case "ADM-02":
		return caseReqs{
			note: "CRIT30 က BCS ကို မခေါ်ပါဘူး။ ဒါကြောင့် BCS ဘက်က setting (BCS_API_*၊ webhook token) တွေ မလိုပါဘူး။",
			reqs: chain(
				reqMockCPE("CRIT30", "ca1.status", "Broken", 0, "Queue ကို Passive-Infra ပြောင်းဖို့ (network_good_workflow · ca1_status=Broken)"),
				reqRtutilBCS(rtutilToMockZero),
				reqMockAdmin(bcsNotCalled),
			)}
	case "ADM-03", "ADM-03a", "ADM-03b", "ADM-03c", "ADM-03d", "ADM-03e", "ADM-04", "V2-01":
		return caseReqs{note: noteGate, reqs: admit()}
	case "ADM-07", "ADM-09", "V2-02":
		return caseReqs{note: noteAdmit, reqs: admit()}
	case "ADM-11":
		return caseReqs{
			note: "(a) CRIT30 run က Kafka နဲ့ rtutil ကို ဖြတ်သွားလို့ နှစ်ခုလုံး လိုပါတယ်။ (b) CRIT40 ကို Node-RED က 404 ပြန်ရပါမယ်။ (c) CRIT42 ကို mock က NODE_RED_TIMEOUT ထက် နှေးပြီးမှ ဖြေရပါမယ်။",
			reqs: chain(
				reqMockCPE("CRIT30", "ca1.status", "Broken", 0, "(a) fetch အောင်မြင်ပြီး evidence သိမ်းဖို့"),
				reqMockCPE("CRIT40", "", "", http.StatusNotFound, "(b) CPE_NOT_FOUND ဖြစ်ဖို့"),
				reqMockSlow("CRIT42", "(c) Node-RED timeout ဖြစ်ဖို့"),
				reqMockAdmin(bcsNotCalled)),
		}
	case "ACT-02", "RCV-02":
		return caseReqs{
			note: "rtutil ကို kill/restart လုပ်ပါတယ်။ make up နဲ့ စထားတဲ့ rtutil ဖြစ်ရပါမယ်။",
			reqs: chain(reqRtutilManaged(), reqBCSMock("rtutil က BCS job စဖို့"), reqRtutilBCS(rtutilToMock), reqMockAdmin(bcsCalled)),
		}
	case "ACT-07":
		return caseReqs{reqs: chain(reqRtutilBCS(rtutilToMockZero), reqMockAdmin("ရွှေ့ထားတဲ့ ticket အတွက် BCS request မရှိတာ စစ်ဖို့"))}
	case "ACT-09":
		return caseReqs{
			note: "CRIT14 (401) နဲ့ CRIT15 (503) ကို BCS mock က ငြင်းပါတယ်။",
			reqs: chain(reqBCSMock("BCS start ကို 401 / 503 ပြန်ဖို့"), reqRtutilBCS(rtutilToMock)),
		}
	case "ACT-10":
		return caseReqs{reqs: chain()}
	case "OUT-01":
		return caseReqs{
			note: "BCS outcome တိုင်း report မှာ ခွဲပြတာကို pipeline_report API ကနေ စစ်ပါတယ်။",
			reqs: chain(reqBCSMock("BCS ok / failed / pending ဖြစ်ဖို့"), reqRtutilBCS(rtutilToMock),
				reqNocKey("BCS_API_BASE_URL", mock, "noc sweeper က job status ကို poll ဖို့"),
				reqReport()),
		}
	case "OUT-04":
		return caseReqs{
			note: "Webhook မပို့ဘဲ noc sweeper ရဲ့ poll ကို စောင့်ပါတယ် (BCS_RESULT_TIMEOUT_MINUTES)။",
			reqs: chain(reqBCSMock("Job status ပြန်ပေးဖို့"), reqRtutilBCS(rtutilToMock),
				reqNocKey("BCS_API_BASE_URL", mock, "noc sweeper က mock ကို poll ဖို့"),
				reqMockAdmin("Sweeper ရဲ့ poll request ကို စစ်ဖို့")),
		}
	case "OUT-06":
		return caseReqs{
			note: "CRIT13 ရဲ့ job က running အတိုင်း ကျန်ပြီး noc sweeper က BCS_RESULT_GIVE_UP_MINUTES ကျော်ရင် give up လုပ်ပါတယ်။",
			reqs: chain(reqBCSMock("Job ကို running အတိုင်း ပြန်ပေးဖို့"), reqRtutilBCS(rtutilToMock),
				reqNocKey("BCS_API_BASE_URL", mock, "noc sweeper က mock ကို poll ဖို့")),
		}
	case "OPS-01":
		return caseReqs{
			note: "Node-RED mock container ကို ခဏ stop ပြီး ပြန်စပါတယ်။ Run က CPE fetch မှာ ရပ်လို့ Kafka၊ rtutil မလိုပါ။",
			reqs: append(admit(), reqNodeRed(), reqDocker("Mock container ကို stop / start လုပ်ဖို့"),
				reqNocNotTrue("RETRY_WORKER_ENABLED", "Retry ပိတ်ထားတဲ့ release config ကို စစ်တာမို့ (true ဆိုရင် case skip)")),
		}
	case "RCV-05":
		return caseReqs{note: noteSweeper, reqs: []reqDef{reqMySQL(), reqAPI(),
			reqNocEnv("Release timeout value တွေကို ဖတ်ပြီး release value နဲ့ တိုက်စစ်ဖို့")}}
	case "OPS-06", "OPS-13":
		return caseReqs{note: noteSweeper, reqs: []reqDef{reqMySQL(), reqAPI(),
			reqNocKey("PUBLISHED_TIMEOUT_ENABLED", "true", "PUBLISHED sweeper ဖွင့်ထားဖို့")}}
	case "RCV-06":
		return caseReqs{note: "DB ထဲက COMPLETED row အဟောင်းကို ရေတွက်ရုံပါ။", reqs: []reqDef{reqMySQL()}}
	}
	// A case without its own list: assume the full chain.
	return caseReqs{reqs: chain()}
}

// requirements runs the case's checks in parallel and keeps the list order.
func (s *acceptServer) requirements(ctx context.Context, c acceptCase) (string, []reqItem) {
	cr := caseRequirements(c.ID, s.env.mockPort)
	defs := cr.reqs

	out := make([]reqItem, len(defs))
	var wg sync.WaitGroup
	for i, d := range defs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, checkTimeout)
			defer cancel()
			r := d.check(cctx, s.env)
			out[i] = reqItem{Name: d.name, Why: d.why, WhyEn: en(d.why), Level: levelName[r.Level], Detail: r.Detail,
				Fix: r.Fix, FixEn: enFix(r.Fix), Optional: d.optional}
		}()
	}
	wg.Wait()
	return cr.note, out
}

func (s *acceptServer) handleRequirements(w http.ResponseWriter, r *http.Request) {
	c, ok := s.cases[r.PathValue("id")]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown case"})
		return
	}
	if c.Skip != "" {
		writeJSON(w, http.StatusOK, map[string]any{"id": c.ID, "title": c.Title, "title_en": c.TitleEn, "skip": c.Skip, "skip_en": c.SkipEn, "items": []reqItem{}})
		return
	}
	note, items := s.requirements(r.Context(), c)
	ready := true
	for _, it := range items {
		if !it.Optional && it.Level != "ok" {
			ready = false
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": c.ID, "title": c.Title, "title_en": c.TitleEn,
		"note": note, "note_en": en(note), "items": items, "ready": ready, "flow": caseFlows[c.ID]})
}
