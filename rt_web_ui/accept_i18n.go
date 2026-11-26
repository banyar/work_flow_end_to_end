package main

import "strings"

// English text for the acceptance page's language toggle. The Burmese text
// in caseDefs, acceptGroups, specGates and accept_reqs.go stays the source;
// this maps each string to its English version. A string with no entry is
// shown as is in both languages.

var enText = map[string]string{
	// Groups
	"1. Admission":                 "1. Admission",
	"2. MariaDB / Kafka / restart": "2. MariaDB / Kafka / restart",
	"3. BCS / RT လုပ်ဆောင်ချက်": "3. BCS / RT actions",
	"4. Stale work / NOC လွှဲ":  "4. Stale work / NOC handoff",
	"5. BCS ရလဒ် / report":      "5. BCS result / report",
	"6. Sweeper / capacity":     "6. Sweeper / capacity",
	"7. V2 admission / Redis":   "7. V2 admission / Redis",

	// 1. Admission
	"BCS လိုတဲ့ ticket အစအဆုံး (CRIT11)":                 "Ticket that needs BCS, end to end (CRIT11)",
	"BCS မလိုတဲ့ ticket (CRIT30 transferred)":            "Ticket that needs no BCS (CRIT30 transferred)",
	"Gate ငြင်းတာ ၅ မျိုး + positive control":            "Five gate rejections + positive control",
	"Rule ၂ ခု fail · field ပုံစံမှား":                   "Two rules fail · malformed field",
	"Commit ပြီး crash → retry":                          "Crash after commit → retry",
	"Commit မတိုင်ခင် fail":                              "Failure before commit",
	"တစ်ပြိုင်နက် request ၂ ခု":                          "Two concurrent requests",
	"Stable request ID (တူ ID၊ content ကွဲ)":             "Stable request ID (same ID, different content)",
	"၂၄ နာရီ boundary + ALREADY_PROCESSED":               "24-hour boundary + ALREADY_PROCESSED",
	"Authorized reprocess":                               "Authorized reprocess",
	"CPE fetch: OK · not found · timeout (CRIT30/40/42)": "CPE fetch: OK · not found · timeout (CRIT30/40/42)",
	"Kafka payload မမှန်":                                "Invalid Kafka payload",
	"CPE ID မပါ":                                         "CPE ID missing",
	"Local Service ID မပါ":                               "Local Service ID missing",
	"OPI site code ပါနေ":                                 "OPI site code present",
	"ခွင့်မပြုတဲ့ service type":                          "Service type not allowed",
	"ခွင့်မပြုတဲ့ ticket status":                         "Ticket status not allowed",
	"NOC queue မဟုတ်":                                    "Not in the NOC queue",

	// 2. MariaDB / Kafka / restart
	"Publish မတိုင်ခင် transaction fail":                        "Transaction fails before publish",
	"Commit ပြီး Kafka မပို့ခင် crash":                          "Crash after commit, before the Kafka send",
	"Kafka ပို့ပြီး PUBLISHED မမှတ်ခင် crash":                   "Crash after the Kafka send, before PUBLISHED is recorded",
	"PUBLISHED နောက်ကျ ရေး":                                     "PUBLISHED written late",
	"Sweeper အရင်ပိတ်ပြီးမှ message ရောက်":                      "Message arrives after the sweeper closed the run",
	"Kafka ack ပျောက်":                                          "Kafka ack lost",
	"Relay ဖွင့်ပြီး PUB-02":                                    "PUB-02 with the relay on",
	"Relay crash + duplicate":                                   "Relay crash + duplicate",
	"Kafka outage / retry budget":                               "Kafka outage / retry budget",
	"API state ၄ ခုမှာ kill":                                    "Kill the API in four states",
	"rtutil ကို CONSUMED / BCS_UPDATING / RT_UPDATING မှာ kill": "Kill rtutil in CONSUMED / BCS_UPDATING / RT_UPDATING",
	"ပြီးပြီး offset မ commit ခင် crash":                        "Crash after finishing, before the offset commit",
	"Partition တူ message ၂ ခု":                                 "Two messages on the same partition",
	"Stuck timeout setting + cutoff အောက်/အထက်":                 "Stuck timeout setting + below/above the cutoff",
	"Upgrade: COMPLETED row အဟောင်း":                            "Upgrade: old COMPLETED rows",
	"Event insert fail → rollback":                              "Event insert fails → rollback",

	// 3. BCS / RT
	"★ Peppermint လက်ခံပြီး reply ပျောက်":      "★ Peppermint accepts, reply lost",
	"BCS job ref သိမ်းပြီး crash":              "Crash after storing the BCS job ref",
	"Peppermint dedupe window":                 "Peppermint dedupe window",
	"★ RT update ပြီး comment မတိုင်ခင် crash": "★ Crash after the RT update, before the comment",
	"Comment ရေးပြီး reply ပျောက်":             "Comment written, reply lost",
	"Worker ၂ ခု comment တစ်ပြိုင်နက်":         "Two workers comment at once",
	"NOC က ticket ရွှေ့ / status ပြောင်း":      "NOC moves the ticket / changes status",
	"Read ပြီး write မတိုင်ခင် ပြောင်း":        "Changed between read and write",
	"BCS ငြင်း (401) vs မရ (503)":              "BCS rejected (401) vs unavailable (503)",
	"Marker နဲ့ ဆင်တဲ့ comment":                "Comment that looks like the marker",
	"Terminal run အတွက် duplicate event":       "Duplicate event for a terminal run",

	// 4. Stale work
	"Sweeper ပိတ်ပြီး worker ပြန်လာ":                 "Worker returns after the sweeper closed the run",
	"Request ပို့ပြီး effect မတိုင်ခင် pause":        "Pause after the request, before its effect",
	"★ Timeout ပြီး NOC ကိုင်ချိန် အလုပ်ဟောင်း ပြီး": "★ Old work finishes after timeout while NOC holds the ticket",
	"Sweeper နဲ့ worker ပြိုင်":                      "Sweeper and worker race",
	"Rerun က မပြီးသေးတဲ့ action ကို စစ်":             "Rerun checks unfinished actions",
	"NOC လေ့ကျင့်ခန်း":                               "NOC exercise",
	"Automated recovery မှာ STALE-02/03":             "STALE-02/03 under automated recovery",

	// 5. BCS result / report
	"BCS outcome တိုင်း report မှာ ခွဲပြ": "Report shows every BCS outcome separately",
	"BCS မဖြစ်မနေ / ရွေးချယ်":             "BCS required / optional",
	"Service verification":                    "Service verification",
	"Webhook ပျောက် → poll":                   "Webhook lost → poll",
	"Webhook ထပ် / ဆန့်ကျင်":                  "Duplicate / conflicting webhooks",
	"Give-up → unknown (bcs_result_gave_up)":  "Give-up → unknown (bcs_result_gave_up)",
	"Job ref မတိုင်ခင် webhook / unknown job": "Webhook before the job ref / unknown job",
	"BCS နာရီ (job-queued time)":              "BCS clock (job-queued time)",
	"Service measurement stale/fresh":         "Service measurement stale/fresh",

	// 6. Sweeper / capacity
	"Retry ပိတ် → Node-RED fail":      "Retry off → Node-RED fails",
	"Retry: stage တစ်ခုပဲ":            "Retry: one stage only",
	"Fetch retry ပြီး publish fail":   "Fetch retried, then publish fails",
	"Attempt budget":                  "Attempt budget",
	"Lease ကျော်":                     "Lease exceeded",
	"Publish sweeper batch (20 · 45)": "Publish sweeper batch (20 · 45)",
	"Round / time budget":             "Round / time budget",
	"ပိတ်ခါနီး recheck":               "Recheck before closing",
	"updated_at writer စည်းမျဉ်း":     "updated_at writer rules",
	"State re-entry deadline":         "State re-entry deadline",
	"BCS poll တန်းစီ":                 "BCS poll queue",
	"Multi-cycle BCS poll":            "Multi-cycle BCS poll",
	"Load ၃၀၀ run drain":              "Load: drain 300 runs",
	"Unattended peak load":            "Unattended peak load",
	"Sweeper ရပ် → alert":             "Sweeper stops → alert",

	// 7. V2
	"Rule တွေ API မှာ":                                 "Rules in the API",
	"Disposition ခွဲပြ (admitted / active / cooldown)": "Dispositions shown separately (admitted / active / cooldown)",
	"Report မဖွင့်ဘဲ background sync":                  "Background sync without opening the report",
	"Uncertain Redis record":                           "Uncertain Redis record",
	"Importer commit ပြီး crash":                       "Importer crashes after commit",
	"MariaDB commit fail":                              "MariaDB commit fails",
	"Importer ၂ ခု":                                    "Two importers",
	"Export side effect":                               "Export side effect",
	"Timeout / 429 / invalid / cooldown":               "Timeout / 429 / invalid / cooldown",
	"Redis မရ":                                         "Redis unavailable",
	"Import နောက်ကျ cooldown":                          "Late import cooldown",

	// Skip reasons
	skipFault:       "Needs a crash or barrier at a checkpoint inside the API / rtutil — cannot be timed from outside",
	skipU:           "U profile: the retry worker / relay is off in this release",
	skipV:           "V profile: needs the Redis importer and fault injection",
	skipNoRequestID: "In this design the client sends no request ID — only the RT External API makes the run_id. Duplicates are stopped per ticket ID by the active-run lock (ADM-07) and the cooldown (ADM-09)",
	skipContact:     "Contract of the real Peppermint / RT — can only be checked on staging",
	" (202 မပြန်ခင် API ကို kill)":    " (kill the API before it answers 202)",
	" (API state တွေ ms အတွင်း ပြီး)": " (the API states finish within ms)",
	" (RT precondition)":                   " (RT precondition)",
	" (consumer rebalance)":                " (consumer rebalance)",
	" (status နဲ့ comment ကြား ms)":        " (ms between status and comment)",
	" · NOC manual reconciliation":         " · NOC manual reconciliation",
	"Alert မရှိ (log ပဲ)":                  "No alert (log only)",
	"Clock ထိန်းဖို့ လို (bcs_updated_at)": "Needs clock control (bcs_updated_at)",
	"DB transaction ကို ကြားမှာ fail ဖြစ်အောင် လုပ်ဖို့ API ထဲ hook လို":                        "Needs a hook in the API to fail the DB transaction midway",
	"DB ထဲ event insert ကို fail ဖြစ်အောင် hook လို":                                            "Needs a hook to fail the event insert in the DB",
	"Kafka message ထပ်ပို့ဖို့ producer လို":                                                    "Needs a producer to resend the Kafka message",
	"Kafka topic ထဲ message တိုက်ရိုက် ထည့်ဖို့ producer လို (rt_web_ui မှာ Kafka client မရှိ)": "Needs a producer to put a message straight on the Kafka topic (rt_web_ui has no Kafka client)",
	"NOC operator နဲ့ manual exercise":                     "Manual exercise with a NOC operator",
	"Peppermint နှေး/မရ အခြေအနေ mock မှာ မရှိ":             "The mock has no slow / unavailable Peppermint mode",
	"Reprocess feature မရှိသေး":                            "No reprocess feature yet",
	"Required vs optional BCS policy မရှိသေး":              "No required vs optional BCS policy yet",
	"Rerun feature မရှိ":                                   "No rerun feature",
	"Slow query / close failure inject လို":                "Needs slow query / close failure injection",
	"Webhook ကို checkpoint မတိုင်ခင် ပို့ဖို့ timing လို": "Needs timing to send the webhook before the checkpoint",
	"Webhook ကို ထပ်ပို့/ပြိုင်ဖို့ barrier လို":           "Needs a barrier to resend / race webhooks",
	"Writer စာရင်း review (document)":                      "Review of the writer list (document)",
	"rtutil ၂ ခုကို message တူ ပေးပြီး barrier လို":        "Needs two rtutil instances given the same message, with a barrier",
	"ဒီ release မှာ NOC manual (မှတ်တမ်း မရှိ)":            "NOC does it by hand in this release (no record)",

	// Requirements popup (accept_reqs.go)
	"Run နဲ့ event တွေ သိမ်းဖို့၊ case က run ရလဒ်နဲ့ RT ticket ရဲ့ queue / status / comment ကို DB ကနေ ဖတ်ဖို့": "Stores runs and events; the case reads the run result and the RT ticket's queue / status / comments from the DB",
	"rt_web_ui ပို့တဲ့ ticket ကို လက်ခံပြီး workflow run ဖို့":                                                  "Accepts the ticket rt_web_ui sends and runs the workflow",
	"CRITxx CPE status ကို mock က ပြန်ပေးဖို့":                                                                  "The mock returns the CRITxx CPE status",
	"noc_automation က rtutil ဆီ ဆုံးဖြတ်ချက် publish လုပ်ဖို့":                                                  "noc_automation publishes its decision to rtutil",
	"Kafka message ယူပြီး RT ticket ကို update လုပ်ဖို့":                                                        "Consumes the Kafka message and updates the RT ticket",
	"Case တိုင်း test RT ticket အသစ် ဖန်တီးဖို့ (POST /ticket) — ticket ကို ပြန်ဖတ်တာကတော့ DB ကနေပါ":            "Every case creates a new test RT ticket (POST /ticket); reading the ticket back goes through the DB",
	"Case က report API ရဲ့ count ကို စစ်လို့":                                                                   "The case checks the report API's counts",
	bcsNotCalled:     "Checks the mock got 0 BCS requests",
	noteGate:         "The gate makes it NOT_ELIGIBLE, so no CPE fetch and nothing sent to Kafka: the Node-RED mock, Kafka and rtutil are not needed. RT REST2 is needed to create a new RT ticket and check it is left unchanged.",
	noteAdmit:        "Checks only the API's admission (run_id / cooldown / active run). An admitted run may carry on in the background, but the case does not check it, so the Node-RED mock, Kafka and rtutil are not needed.",
	rtutilToMock:     "rtutil's BCS calls reach the mock",
	rtutilToMockZero: "\"0 BCS requests at the mock\" only means something when rtutil points at the mock",
	"Case က rtutil ကို pid file နဲ့ kill ပြီး make up နဲ့ ပြန်စဖို့":                                                 "The case kills rtutil through its pid file and restarts it with make up",
	"Retry ပိတ်ထားတဲ့ release config ကို စစ်တာမို့ (true ဆိုရင် case skip)":                                          "The case checks the retry-off release config (it skips when true)",
	"Release timeout value တွေကို ဖတ်ပြီး release value နဲ့ တိုက်စစ်ဖို့":                                            "Reads the timeout values and compares them with the release values",
	"CRIT13 ရဲ့ job က running အတိုင်း ကျန်ပြီး noc sweeper က BCS_RESULT_GIVE_UP_MINUTES ကျော်ရင် give up လုပ်ပါတယ်။": "CRIT13's job stays running; the noc sweeper gives up after BCS_RESULT_GIVE_UP_MINUTES.",
	"Webhook မပို့ဘဲ noc sweeper ရဲ့ poll ကို စောင့်ပါတယ် (BCS_RESULT_TIMEOUT_MINUTES)။":                             "Sends no webhook and waits for the noc sweeper's poll (BCS_RESULT_TIMEOUT_MINUTES).",
	"Job ကို running အတိုင်း ပြန်ပေးဖို့":                                                                            "Keeps returning the job as running",
	noteSweeper: "Inserts synthetic rows in the DB and waits for the sweeper inside noc_automation to close them. No RT ticket is created.",
	"(a) CRIT30 run က Kafka နဲ့ rtutil ကို ဖြတ်သွားလို့ နှစ်ခုလုံး လိုပါတယ်။ (b) CRIT40 ကို Node-RED က 404 ပြန်ရပါမယ်။ (c) CRIT42 ကို mock က NODE_RED_TIMEOUT ထက် နှေးပြီးမှ ဖြေရပါမယ်။": "(a) The CRIT30 run goes through Kafka and rtutil, so both are needed. (b) Node-RED must answer 404 for CRIT40. (c) The mock must answer CRIT42 slower than NODE_RED_TIMEOUT.",
	"(a) fetch အောင်မြင်ပြီး evidence သိမ်းဖို့": "(a) The fetch succeeds and the evidence is stored",
	"(b) CPE_NOT_FOUND ဖြစ်ဖို့":                 "(b) Makes the run CPE_NOT_FOUND",
	"(c) Node-RED timeout ဖြစ်ဖို့":              "(c) Makes the Node-RED call time out",
	"Node-RED mock container ကို ခဏ stop ပြီး ပြန်စပါတယ်။ Run က CPE fetch မှာ ရပ်လို့ Kafka၊ rtutil မလိုပါ။": "Stops the Node-RED mock container for a while, then starts it again. The run stops at the CPE fetch, so Kafka and rtutil are not needed.",
	"PUBLISHED sweeper ဖွင့်ထားဖို့":               "The PUBLISHED sweeper must be on",
	"DB ထဲက COMPLETED row အဟောင်းကို ရေတွက်ရုံပါ။": "Only counts old COMPLETED rows in the DB.",
	bcsCalled: "Checks the mock got the BCS start request",
	"All good ဖြစ်ပြီး Customer Support (Resolved) သို့ ရောက်ဖို့": "All good, so it lands in Customer Support (Resolved)",
	"rtutil က BCS job စဖို့": "rtutil starts the BCS job",
	"Case က CPEMS ကိုယ်စား webhook ပို့ဖို့ (မထည့်ရင် API က 503 ပြန်)":                "The case sends the webhook in place of CPEMS (without it the API answers 503)",
	"Queue ကို Passive-Infra ပြောင်းဖို့ (network_good_workflow · ca1_status=Broken)": "Moves the queue to Passive-Infra (network_good_workflow · ca1_status=Broken)",
	"ရွှေ့ထားတဲ့ ticket အတွက် BCS request မရှိတာ စစ်ဖို့":                             "Checks there is no BCS request for the moved ticket",
	"BCS start ကို 401 / 503 ပြန်ဖို့":                                                "BCS start answers 401 / 503",
	"noc sweeper က mock ကို poll ဖို့":                                                "The noc sweeper polls the mock",
	"BCS sweeper poll ကို စောင့်ဖို့":                                                 "Waits for the BCS sweeper's poll",
	"BCS ok / failed / pending ဖြစ်ဖို့":                                              "Produces BCS ok / failed / pending",
	"noc sweeper က job status ကို poll ဖို့":                                          "The noc sweeper polls the job status",
	"Job status ပြန်ပေးဖို့":                                                          "Returns the job status",
	"Sweeper ရဲ့ poll request ကို စစ်ဖို့":                                            "Checks the sweeper's poll request",
	"Mock container ကို stop / start လုပ်ဖို့":                                        "Stops / starts the mock container",

	"CRIT11 က remote resolved ဖြစ်ပြီး BCS (Peppermint) ကို ခေါ်ပါတယ်။ BCS ရလဒ်ကို webhook နဲ့ ပို့ပါတယ်။": "CRIT11 is remote resolved and calls BCS (Peppermint). The BCS result arrives by webhook.",
	"CRIT30 က BCS ကို မခေါ်ပါဘူး။ ဒါကြောင့် BCS ဘက်က setting (BCS_API_*၊ webhook token) တွေ မလိုပါဘူး။":    "CRIT30 does not call BCS, so the BCS settings (BCS_API_*, webhook token) are not needed.",
	"rtutil ကို kill/restart လုပ်ပါတယ်။ make up နဲ့ စထားတဲ့ rtutil ဖြစ်ရပါမယ်။":                            "Kills and restarts rtutil. rtutil must have been started by make up.",
	"CRIT14 (401) နဲ့ CRIT15 (503) ကို BCS mock က ငြင်းပါတယ်။":                                             "The BCS mock rejects CRIT14 (401) and CRIT15 (503).",
	"BCS outcome တိုင်း report မှာ ခွဲပြတာကို pipeline_report API ကနေ စစ်ပါတယ်။":                           "Checks through the pipeline_report API that the report shows every BCS outcome separately.",
}

// enFixes translates the Burmese "how to fix" hints from check.go, matched
// as substrings because they carry paths and ports.
var enFixes = [][2]string{
	{" ထဲ ထည့်ပြီး noc_automation restart", ": add it, then restart noc_automation"},
	{" ထဲ RT_TOKEN ထည့်ပါ", ": add RT_TOKEN"},
	{" ထဲ ထည့်ပါ", ": add it"},
	{" ကို mock သို့ ညွှန်ပါ", ": point it at the mock"},
	{"noc .env NODE_RED_TIMEOUT ကို 35 အောက် ထားပါ", "set NODE_RED_TIMEOUT below 35 in noc's .env"},
	{"criteria/mockoon-criteria.json ပြင်ပြီး mock restart", "fix criteria/mockoon-criteria.json and restart the mock"},
	{"MySQL run နေလား၊ MYSQL_DB_* မှန်လား စစ်ပါ", "check MySQL is running and MYSQL_DB_* is right"},
	{"rtdatacore ၏ pipeline table migration ကို run ပါ", "run rtdatacore's pipeline table migration"},
	{"URL ကို ပြင်ပါ", "fix the URL"},
	{"noc_automation ကို start ပါ", "start noc_automation"},
	{"noc_automation log ကို စစ်ပါ", "check the noc_automation log"},
	{"Kafka broker / VPN ကို စစ်ပါ", "check the Kafka broker / VPN"},
	{"RT server / VPN ကို စစ်ပါ", "check the RT server / VPN"},
	{"pipeline_report log / DB ကို စစ်ပါ", "check the pipeline_report log / DB"},
	{"ကို တခြား program သုံးနေသလား စစ်ပါ", ": check whether another program uses it"},
	{"criteria 10–36 result မမှန်နိုင်ပါ · mock သုံးရင် ", "criteria 10–36 results may be wrong · to use the mock set "},
	{" ပြီး noc_automation restart", " and restart noc_automation"},
	{"(သို့)", "(or)"},
	{"ကို start ပါ", "start it"},
	{"(docker/k8s မှာ run ရင် ignore)", "(ignore if it runs in docker/k8s)"},
	{"criteria mock အတွက် Docker ထည့်ပါ", "install Docker for the criteria mock"},
	{"docker group permission စစ်ပါ", "check the docker group permission"},
	{" မဟုတ်)", " expected)"},
}

// en returns the English version of s. Strings built from a known prefix and
// suffix (skipFault + " (…)") are translated part by part.
func en(s string) string {
	if v, ok := enText[s]; ok {
		return v
	}
	for _, p := range []string{skipFault, skipContact} {
		if rest, ok := strings.CutPrefix(s, p); ok {
			if v, ok := enText[rest]; ok {
				return enText[p] + v
			}
		}
	}
	return s
}

func enFix(s string) string {
	for _, f := range enFixes {
		s = strings.ReplaceAll(s, f[0], f[1])
	}
	return s
}

// groupsWithEn is acceptGroups with each title's English version.
func groupsWithEn() []map[string]string {
	out := make([]map[string]string, len(acceptGroups))
	for i, g := range acceptGroups {
		out[i] = map[string]string{"key": g.Key, "title": g.Title, "title_en": en(g.Title)}
	}
	return out
}
