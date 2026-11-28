package main

// "How it runs" in the requirements popup: what each system does for a case,
// step by step, with the event that system writes for the step and the code
// that makes the decision. Text is Burmese with an English version. Paths in
// ref are relative to the RT projects folder.

type flowItem struct {
	Text   string `json:"text"`
	TextEn string `json:"text_en"`
}

type flowStep struct {
	Text   string     `json:"text"`
	TextEn string     `json:"text_en"`
	Event  string     `json:"event,omitempty"` // pipeline_run_events.event the system writes
	Ref    string     `json:"ref,omitempty"`   // code that decides the step
	Items  []flowItem `json:"items,omitempty"`
}

type flowGroup struct {
	System    string     `json:"system"`
	SystemEn  string     `json:"system_en"`
	Component string     `json:"component,omitempty"` // pipeline_run_events.component
	Steps     []flowStep `json:"steps"`
}

// ---------------------------------------------------------------------------
// Builders

func st(my, en, event, ref string, items ...flowItem) flowStep {
	return flowStep{Text: my, TextEn: en, Event: event, Ref: ref, Items: items}
}

func it(my, en string) flowItem { return flowItem{Text: my, TextEn: en} }

const (
	refWorkflow     = "noc_automation/frontiir/services/workflow_engine_service.go:412"
	refNetworkGood  = "noc_automation/workflow_engine_configs/network_good_workflow.json"
	refCategory     = "noc_automation/workflow_engine_configs/category_type_workflow.json"
	refGates        = "noc_automation/workflow_engine_configs/main.json · ticket_eligible_criterias"
	refCheckTicket  = "noc_automation/frontiir/services/eligibility.go · CheckTicket"
	refProcessedQ   = "rtdatacore/pkg/driven/rtdb/pipeline_run_repository.go:48 · FindProcessedRunSince"
	refController   = "noc_automation/frontiir/api/controllers/remote_resolve_controller.go"
	refBCSBlock     = "remote-resolved-queue-transfer/internal/services/rtrest2/ticket_service.go:196"
	refCheckQueue   = "remote-resolved-queue-transfer/internal/services/rtrest2/ticket_service.go:111 · checkQueue"
	refCpems        = "remote-resolved-queue-transfer/internal/services/cpems/cpems_service.go:46"
	refMarker       = "remote-resolved-queue-transfer/internal/services/rtrest2/ticket_service.go:256"
	refSweepTimeout = "noc_automation/frontiir/noc_sweeper/publish_timeout_sweeper.go"
	refSweepBCS     = "noc_automation/frontiir/noc_sweeper/noc_result_sweeper.go"
	refFailNoRetry  = "noc_automation/frontiir/services/remote_resolve_service.go:339"
	refFetchCPE     = "noc_automation/frontiir/services/remote_resolve_service.go:102 · fetchCPEStatus"
	refNodeRedTries = "noc_automation/frontiir/services/node_red_service.go:123 · FetchCPEStatus"
	refEvidence     = "noc_automation/frontiir/services/remote_resolve_service.go:245 · transitionWorkflowEvaluated"
)

// testGroup is rt_web_ui's part: it only sets the case up and reads what
// the systems recorded.
func testGroup(steps ...flowStep) flowGroup {
	return flowGroup{System: "(က) rt_web_ui (test)", SystemEn: "(a) rt_web_ui (test)", Steps: steps}
}

func apiGroup(steps ...flowStep) flowGroup {
	return flowGroup{System: "(ခ) RT External API (noc_automation)", SystemEn: "(b) RT External API (noc_automation)", Component: compAPI, Steps: steps}
}

func rtutilGroup(steps ...flowStep) flowGroup {
	return flowGroup{System: "(ဂ) Kafka → rtutil", SystemEn: "(c) Kafka → rtutil", Component: compRtutil, Steps: steps}
}

func bcsGroup(steps ...flowStep) flowGroup {
	return flowGroup{System: "(ဃ) BCS mock (Peppermint)", SystemEn: "(d) BCS mock (Peppermint)", Steps: steps}
}

func sweeperGroup(comp string, steps ...flowStep) flowGroup {
	return flowGroup{System: "(င) noc_automation sweeper", SystemEn: "(e) noc_automation sweeper", Component: comp, Steps: steps}
}

func webhookGroup(steps ...flowStep) flowGroup {
	return flowGroup{System: "(င) RT External API · BCS webhook", SystemEn: "(e) RT External API · BCS webhook", Component: compWebhook, Steps: steps}
}

// Shared steps.
var (
	stSubmit = func(sample string) flowStep {
		return st("RT REST2 နဲ့ test RT ticket အသစ် ဖန်တီးပြီး "+sample+" ကို API ဆီ တိုက်ရိုက် POST လုပ်ပါတယ် (RT extension လိုပဲ)။ ဒီနောက် ဘာမှ မဆုံးဖြတ်ဘဲ system တွေရဲ့ မှတ်တမ်းကို စောင့်စစ်ရုံပါ။",
			"Creates a new test RT ticket through RT REST2 and POSTs "+sample+" straight to the API (as the RT extension does). After that it decides nothing: it waits and reads what the systems recorded.", "", "")
	}
	stReceived  = st("Run row ကို ဖန်တီးပြီး 202 နဲ့ run_id ပြန်ပေးပါတယ်။", "Creates the run row and answers 202 with a run_id.", "api_received", refController)
	stGatesPass = st("Gate ၄ ခု (CPE & Local Service ID၊ OPI site code ဗလာ၊ service type၊ ticket status) စစ်ပြီး ကျော်ပါတယ်။",
		"Checks the 4 gates (CPE & Local Service ID, empty OPI site code, service type, ticket status); the ticket passes.", "", refGates)
	stFetch = func(cpe string) flowStep {
		return st("Node-RED ဆီက "+cpe+" ရဲ့ CPE status ကို ယူပါတယ်။", "Fetches "+cpe+"'s CPE status from Node-RED.", "node_red_fetch_started", "")
	}
	stPublish  = st("ဆုံးဖြတ်ချက်ကို Kafka သို့ publish လုပ်ပါတယ်။", "Publishes the decision to Kafka.", "kafka_published", "")
	stConsume  = st("rtutil က message ကို ယူပါတယ်။ ဒီ event ရှိတာက message က Kafka ကနေ တကယ်ရောက်လာတဲ့ သက်သေပါ။", "rtutil consumes the message. This event is the proof the message really came through Kafka.", "kafka_consumed", "")
	stRTUpdate = func(queue string) flowStep {
		return st("RT REST2 နဲ့ ticket ကို update လုပ်ပါတယ်။", "Updates the ticket through RT REST2.", "", "",
			it("Queue ကို "+queue+" ပြောင်းပါတယ် (rt_status_updated)။", "Moves the queue to "+queue+" (rt_status_updated)."),
			it("Comment ထည့်ပါတယ် (rt_comment_added)။ Comment ထဲမှာ run ရဲ့ marker <!-- rr:<run_id> --> ပါပါတယ်။", "Adds the comment (rt_comment_added), carrying the run's marker <!-- rr:<run_id> -->."))
	}
	stDone   = st("Run ကို TICKET_COMPLETED လို့ ပိတ်ပါတယ်။", "Closes the run as TICKET_COMPLETED.", "rt_updated", "")
	stBCSRun = st("is_remote_resolved = true မို့ BCS ကို စခိုင်းပါတယ်: POST /api/v2/cpes/<cpe>/channel-selections (X-Request-ID = run_id)။ RT update က BCS ရလဒ်ကို မစောင့်ပါ။",
		"is_remote_resolved is true, so it starts BCS: POST /api/v2/cpes/<cpe>/channel-selections (X-Request-ID = run_id). The RT update does not wait for the BCS result.", "bcs_update_started", refBCSBlock)
	stJobQueued = st("Peppermint က 202 နဲ့ job_id ပြန်ပေးရင် ref_bcs_process_id မှာ သိမ်းပါတယ်။", "When Peppermint answers 202 with a job_id, stores it in ref_bcs_process_id.", "bcs_job_queued", "")
	stMockNone  = st("Request တစ်ခုမှ မရောက်ပါဘူး။ ဒါကို mock ကိုယ်တိုင်ရဲ့ request log ကနေ စစ်ပါတယ်။", "Receives no request at all. The case checks this in the mock's own request log.", "", "")
	stNotTouch  = st("ဘာမှ မလုပ်ပါ။ Kafka message မရောက်လို့ rtutil event ၀ ခု ဖြစ်ရပါမယ်။", "Does nothing: no Kafka message arrives, so rtutil writes 0 events.", "", "")
)

// allGood is the workflow path of a CPE whose network is fine (CRIT11–15):
// no network rule matches and category_type_workflow's final_action applies.
func allGood(cpe string) []flowStep {
	return []flowStep{
		stReceived, stGatesPass, stFetch(cpe),
		st("Workflow ကို run ပါတယ်။ Network အကုန်ကောင်းလို့ ပထမဆုံး ကိုက်တာ final_action ပါ။", "Runs the workflow. The network is fine, so the first match is final_action.", "workflow_evaluated", refCategory,
			it("network_good_workflow ရဲ့ rule တစ်ခုမှ မကိုက်ပါ (OLT Online၊ CA1/uplink OK၊ ONU Working)", "No network_good_workflow rule matches (OLT Online, CA1/uplink OK, ONU Working)"),
			it("category_type_workflow ရဲ့ decision rule တွေလည်း မကိုက်ပါ (ticket field တွေ မကိုက်)", "No category_type_workflow decision rule matches (the ticket fields do not)"),
			it("final_action: queue Customer Support · status Resolved · root cause Remote Resolve", "final_action: queue Customer Support · status Resolved · root cause Remote Resolve")),
		st("Status က Resolved မို့ is_remote_resolved = true ဖြစ်ပါတယ်။", "The status is Resolved, so is_remote_resolved is true.", "", refWorkflow),
		stPublish,
	}
}

// transferred is the CRIT30 path: CA1 Broken moves the ticket to Passive-Infra.
func transferred() []flowStep {
	return []flowStep{
		stReceived, stGatesPass, stFetch("CRIT30"),
		st("Workflow ကို run ပါတယ်။ network_good_workflow.json ထဲက rule အစဉ်အတိုင်း စစ်ပြီး ပထမဆုံး ကိုက်တဲ့ rule ကို ယူပါတယ်။",
			"Runs the workflow: checks the rules in network_good_workflow.json in order and takes the first one that matches.", "workflow_evaluated", refNetworkGood,
			it("OLT Status Offline → OLT က Online မို့ မကိုက်ပါ", "OLT Status Offline → no match, the OLT is Online"),
			it("CA1 Status Broken → ကိုက်ပါတယ် ✔", "CA1 Status Broken → match ✔"),
			it("queue: Passive-Infra", "queue: Passive-Infra"),
			it("status: In Progress", "status: In Progress"),
			it("suspected area: Access Network-Passive", "suspected area: Access Network-Passive"),
			it("root cause category: Infra-Access Splitter", "root cause category: Infra-Access Splitter"),
			it("comment: \"Dear All, Customer ONU is under an uplink Passive-Infra down area…\"", "comment: \"Dear All, Customer ONU is under an uplink Passive-Infra down area…\"")),
		st("is_remote_resolved ကို ဆုံးဖြတ်ပါတယ်: isRemoteResolved := strings.EqualFold(event.Status, \"Resolved\")။ Status က In Progress (Resolved မဟုတ်) မို့ false ဖြစ်ပါတယ်။",
			"Decides is_remote_resolved: isRemoteResolved := strings.EqualFold(event.Status, \"Resolved\"). The status is In Progress (not Resolved), so it is false.", "", refWorkflow),
		stPublish,
	}
}

// gateFlow is one ADM-03 sub-case: the API's own rule fails and the run
// stops at NOT_ELIGIBLE before any other system is involved.
func gateFlow(sample, rule, ruleEn, event string) []flowGroup {
	return []flowGroup{
		testGroup(stSubmit(sample),
			st("rt_web_ui ကိုယ်ပိုင် gate ရဲ့ အဖြေကို log မှာ info အဖြစ်ပဲ ပြပါတယ်။ ရလဒ်ကို မဆုံးဖြတ်ပါ။", "Logs rt_web_ui's own gate verdict as info only; it does not decide the result.", "", "")),
		apiGroup(stReceived,
			st("Gate rule \""+rule+"\" မကိုက်လို့ run ကို NOT_ELIGIBLE နဲ့ ပိတ်ပါတယ်။", "Gate rule \""+ruleEn+"\" fails, so the run is closed as NOT_ELIGIBLE.", event, refGates),
			st("ဒီမှာ ရပ်ပါတယ်: Node-RED မခေါ် (node_red_fetch_started မရှိ)၊ Kafka မပို့ (kafka_published မရှိ)။", "Stops here: no Node-RED call (no node_red_fetch_started) and nothing sent to Kafka (no kafka_published).", "", "")),
		rtutilGroup(stNotTouch),
		{System: "(ဃ) RT", SystemEn: "(d) RT", Steps: []flowStep{st("Ticket က NOC queue / new အတိုင်း ကျန်ရပါမယ် (RT DB ကနေ စစ်)။", "The ticket must stay in the NOC queue / new (checked in the RT DB).", "", "")}},
	}
}

// ---------------------------------------------------------------------------
// Per case

var caseFlows = map[string][]flowGroup{
	"ADM-01": {
		testGroup(stSubmit("11_bcs_success (CRIT11)"),
			st("Run ပြီးရင် CPEMS ကိုယ်စား BCS webhook ပို့ပါတယ်: POST /api/v1/bcs/callback · Authorization: Bearer <BCS_WEBHOOK_TOKEN> · status succeeded_new_channel · channel 11 → 6။ ပြီးရင် အတူတူ ထပ်ပို့ပါတယ် (CPEMS retry)။",
				"After the run, sends the BCS webhook in place of CPEMS: POST /api/v1/bcs/callback · Authorization: Bearer <BCS_WEBHOOK_TOKEN> · status succeeded_new_channel · channel 11 → 6, then sends the same one again (a CPEMS retry).", "", "")),
		apiGroup(allGood("CRIT11")...),
		rtutilGroup(stConsume, stBCSRun, stJobQueued, stRTUpdate("Customer Support (status Resolved)"), stDone),
		bcsGroup(st("BCS start request ၁ ခုပဲ ရရပါမယ်။ CRIT11 မို့ 202 · job_id ok-<run_id> ပြန်ပေးပါတယ်။ Webhook ကို mock က မပို့ပါ။", "Must get exactly 1 BCS start request. For CRIT11 it answers 202 · job_id ok-<run_id>. The mock sends no webhook.", "", "")),
		webhookGroup(st("Webhook ကို လက်ခံပြီး is_bcs_success = 1၊ channel 11 → 6 သိမ်းပါတယ်။ Run က FULLY_COMPLETED ဖြစ်ပါတယ်။", "Accepts the webhook, stores is_bcs_success = 1 and channel 11 → 6. The run becomes FULLY_COMPLETED.", "bcs_update_succeeded", ""),
			st("ထပ်ပို့တဲ့ webhook ကို 200 ပြန်ပေမယ့် တစ်ခါပဲ မှတ်ပါတယ်။", "Answers the repeated webhook with 200 but records it only once.", "", "")),
	},
	"ADM-02": {
		testGroup(stSubmit("30_transferred_ca1_broken (CRIT30)"),
			st("Run ပြီးရင် API ရဲ့ workflow_evaluated event ထဲက ဆုံးဖြတ်ချက် (status၊ comment) ကို RT ticket ရဲ့ status နဲ့ comment စာသားနဲ့ တိုက်စစ်ပါတယ်။ ပြီးရင် pipeline_report API ကို မေးပါတယ်။",
				"When the run ends, compares the decision in the API's workflow_evaluated event (status, comment) with the RT ticket's status and comment text, then asks the pipeline_report API.", "", "")),
		apiGroup(transferred()...),
		rtutilGroup(stConsume, stRTUpdate("Passive-Infra"),
			st("BCS ကို မခေါ်ပါဘူး: else if isKafkaTicket && kafkaTicket.IsRemoteResolved {။ IsRemoteResolved က false မို့ BCS block ထဲ မဝင်ပါဘူး။",
				"Does not call BCS: else if isKafkaTicket && kafkaTicket.IsRemoteResolved {. IsRemoteResolved is false, so the BCS block is skipped.", "", refBCSBlock),
			stDone),
		bcsGroup(stMockNone),
		{System: "(စ) pipeline_report", SystemEn: "(f) pipeline_report", Steps: []flowStep{
			st("Run ကို \"No channel change needed\" (no_bcs_needed) နဲ့ \"Transferred\" list တွေမှာ ပြရပါမယ်: state TICKET_COMPLETED၊ queue Passive-Infra၊ is_remote_resolved 0၊ BCS ရလဒ် မရှိ၊ bcs_job_id မရှိ။",
				"Must show the run in the \"No channel change needed\" (no_bcs_needed) and \"Transferred\" lists: state TICKET_COMPLETED, queue Passive-Infra, is_remote_resolved 0, no BCS result, no bcs_job_id.", "", "pipeline_report/store.go · condNotRemote")}},
	},
	"ADM-03": {
		testGroup(st("Gate ၅ မျိုး (ADM-03a–e) ကို တစ်ခုချင်း ticket အသစ်နဲ့ ပို့ပါတယ်။ ပြီးရင် positive control: CRIT30 ကို service type G2 Net · status In_Progress နဲ့ ပို့ပါတယ်။",
			"Sends the 5 gates (ADM-03a–e), each on a new ticket. Then the positive control: CRIT30 with service type G2 Net · status In_Progress.", "", "")),
		apiGroup(st("Gate ၅ ခုစလုံးကို NOT_ELIGIBLE နဲ့ ပိတ်ပြီး Node-RED / Kafka မသုံးပါ (အသေးစိတ်က ADM-03a–e)။", "Closes all 5 gate samples as NOT_ELIGIBLE without Node-RED / Kafka (details in ADM-03a–e).", "", refGates),
			st("Positive control: G2 Net က service type list ထဲ ပါပြီး In_Progress က status regex (?i)^(new|in_progress|re-?open(-\\d+)?)$ နဲ့ ကိုက်လို့ gate ကျော်ပြီး FETCHING_CPE_STATUS ကို ဆက်သွားရပါမယ်။",
				"Positive control: G2 Net is in the service type list and In_Progress matches the status regex (?i)^(new|in_progress|re-?open(-\\d+)?)$, so it passes the gates and goes on to FETCHING_CPE_STATUS.", "", refGates)),
	},
	"ADM-03a": gateFlow("02_not_eligible_cpe_missing (empty cpe_id)", "CPE & Local Service ID included", "CPE & Local Service ID included", "cpe_or_local_service_id_missing"),
	"ADM-03b": gateFlow("30_transferred_ca1_broken + empty local_service_id", "CPE & Local Service ID included", "CPE & Local Service ID included", "cpe_or_local_service_id_missing"),
	"ADM-03c": gateFlow("03_not_eligible_opi (opi_site_code HMHY)", "OPI Site Code empty", "OPI Site Code empty", "opi_site_code_present"),
	"ADM-03d": gateFlow("04_not_eligible_service_type (service_type Other)", "Service Type eligible", "Service Type eligible", "service_type_not_eligible"),
	"ADM-03e": gateFlow("05_not_eligible_ticket_status (status Resolved)", "Ticket Status eligible", "Ticket Status eligible", "ticket_status_not_eligible"),
	"ADM-04": {
		testGroup(st("Rule ၂ ခု fail တဲ့ sample (cpe_id ဗလာ + queue Customer Support) ကို ticket တစ်ခုတည်းနဲ့ ၂ ခါ ပို့ပါတယ်။ ပြီးရင် cpe_id = CRIT-30 (- ပါ) ကို ticket အသစ်နဲ့ ပို့ပါတယ်။",
			"Sends a sample that fails two rules (empty cpe_id + queue Customer Support) twice on one ticket, then cpe_id = CRIT-30 (with a -) on a new ticket.", "", "")),
		apiGroup(st("API မှာ queue rule မရှိလို့ CPE rule တစ်ခုတည်းနဲ့ NOT_ELIGIBLE ဖြစ်ပါတယ်။ ၂ ခါလုံး reason တူရပါမယ် (deterministic)။", "The API has no queue rule, so only the CPE rule fails → NOT_ELIGIBLE. Both times the reason must be the same (deterministic).", "cpe_or_local_service_id_missing", refGates),
			st("CRIT-30: request validation က cpe_id ^[A-Za-z0-9]+$ မကိုက်လို့ HTTP 400 \"invalid cpe_id: must be alphanumeric\" နဲ့ ချက်ချင်း ငြင်းပါတယ်။ Run မဖန်တီး၊ Kafka မပို့ပါ။",
				"CRIT-30: request validation rejects a cpe_id that does not match ^[A-Za-z0-9]+$ at once with HTTP 400 \"invalid cpe_id: must be alphanumeric\". No run is created and nothing reaches Kafka.", "", "noc_automation/frontiir/api/validation/remote_resolve_validation.go:10")),
		rtutilGroup(stNotTouch),
	},
	"ADM-07": {
		testGroup(st("Ticket တစ်ခုတည်းကို request ၂ ခု တစ်ပြိုင်နက် ပို့ပါတယ်။", "Sends two requests for the same ticket at the same time.", "", "")),
		apiGroup(st("CheckTicket က active run ရှိမရှိ စစ်ပြီး CreateRun က DB ရဲ့ active_ticket_id lock နဲ့ run တစ်ခုပဲ ဖန်တီးပါတယ်။", "CheckTicket looks for an active run and CreateRun uses the DB's active_ticket_id lock, so only one run is created.", "api_received", refCheckTicket),
			st("ရှုံးတဲ့ request ကို 202 ပဲ ပြန်ပေးပြီး run_id မပါ၊ DB မှာ ဘာမှ မရေးပါ။", "The losing request gets 202 without a run_id, and nothing is written for it.", "", refController),
			st("Admit ဖြစ်တဲ့ run က နောက်ကွယ်မှာ ဆက်သွားနိုင်ပေမယ့် case က မစစ်ပါ။", "The admitted run may carry on in the background; the case does not check it.", "", "")),
	},
	"ADM-09": {
		testGroup(st("(1) Ticket အတွက် ၂၃ နာရီ ၅၈ မိနစ်က ပြီးခဲ့တဲ့ TICKET_COMPLETED run ကို DB ထဲ ထည့် (seed) ပြီး ticket ကို ပို့ပါတယ်။", "(1) Seeds a TICKET_COMPLETED run that finished 23 h 58 min ago and submits the ticket.", "", ""),
			st("(2) အဲဒီ run ကို ၂၄ နာရီ ၂ မိနစ်ဆီ နောက်ရွှေ့ပြီး ထပ်ပို့ပါတယ်။ (1) က ရေးခဲ့တဲ့ ALREADY_PROCESSED record ကတော့ အသစ်အတိုင်း ကျန်ပါတယ်။", "(2) Moves that run back to 24 h 2 min and submits again; the ALREADY_PROCESSED record from (1) stays recent.", "", ""),
			st("(3) (2) admit မဖြစ်ရင်ပဲ: ဘယ် run က တားလဲ ပြပြီး ALREADY_PROCESSED record တွေကိုပါ ၂၅ နာရီဆီ ရွှေ့ပြီး ထပ်ပို့ပါတယ်။", "(3) Only if (2) is not admitted: shows which run blocked it, moves the ALREADY_PROCESSED records back 25 h and submits again.", "", ""),
			st("(4) Ticket အသစ်မှာ ၁ နာရီက NOT_ELIGIBLE run ကို seed လုပ်ပြီး ပို့ပါတယ်။", "(4) On a new ticket, seeds a NOT_ELIGIBLE run from 1 h ago and submits.", "", "")),
		apiGroup(st("CheckTicket က run_id မထုတ်ခင် completed_at ≥ now − 24h ဖြစ်တဲ့ NOT_ELIGIBLE မဟုတ်တဲ့ run ကို ရှာပါတယ်။ တွေ့ရင် 202 run_id မပါဘဲ ပြန်ပြီး ALREADY_PROCESSED run (completed_at = now) တစ်ခု မှတ်ပါတယ်။",
			"Before making a run_id, CheckTicket looks for a run other than NOT_ELIGIBLE with completed_at ≥ now − 24h. If found, it answers 202 without a run_id and records one ALREADY_PROCESSED run (completed_at = now).", "already_processed", refCheckTicket),
			st("Window ကို run ပြီးတဲ့ အချိန် (completed_at) ကနေ ရေတွက်ပါတယ်။ ≥ ဖြစ်လို့ ၂၄ နာရီ အတိအကျမှာလည်း cooldown ထဲမှာပဲ ရှိပါတယ် (window = main.json already_processed · 24 hours)။",
				"The window counts from when the run finished (completed_at); with ≥, exactly 24 h is still inside (window = main.json already_processed · 24 hours).", "", refProcessedQ),
			st("လိုအပ်ချက်: တကယ့် run ၂၄ နာရီ ကျော်ရင် admit ဖြစ်ရမယ်။ ALREADY_PROCESSED record က window ကို မရှည်စေရဘူး။ NOT_ELIGIBLE run က cooldown မစရဘူး။",
				"Requirement: once the real run is past 24 h the ticket is admitted; an ALREADY_PROCESSED record must not extend the window; a NOT_ELIGIBLE run does not start the cooldown.", "", ""),
			st("Query က NOT_ELIGIBLE နဲ့ ALREADY_PROCESSED နှစ်ခုစလုံးကို ချန်ပါတယ်။ နှစ်ခုစလုံး ticket ပေါ်မှာ ဘာမှ မလုပ်ခဲ့လို့ပါ။ ALREADY_PROCESSED ကို ရေတွက်ရင် request တိုင်းက window အသစ် စပြီး ticket က ဘယ်တော့မှ ပြန်မ run တော့ပါဘူး။",
				"The query leaves out both NOT_ELIGIBLE and ALREADY_PROCESSED, since neither did any work on the ticket. Counting ALREADY_PROCESSED would let every request start a new window, so the ticket would never run again.", "", refProcessedQ)),
	},
	"ADM-11": {
		testGroup(st("Test RT ticket ၃ ခု ဖန်တီးပြီး (a) 30_transferred_ca1_broken (CRIT30)၊ (b) 40_cpe_not_found (CRIT40)၊ (c) 42_node_red_timeout (CRIT42) ကို API ဆီ တိုက်ရိုက် POST လုပ်ပါတယ်။ (a) မှာ mock ရဲ့ CRIT30 အဖြေကို ကိုယ်တိုင် ဖတ်ပြီး API သိမ်းထားတာနဲ့ တိုက်စစ်ပါတယ်။",
			"Creates 3 test RT tickets and POSTs (a) 30_transferred_ca1_broken (CRIT30), (b) 40_cpe_not_found (CRIT40) and (c) 42_node_red_timeout (CRIT42) straight to the API. For (a) it reads the mock's CRIT30 answer itself and compares it with what the API stored.", "", "")),
		apiGroup(stReceived, stGatesPass,
			st("(a) CRIT30: Node-RED က ဖြေလို့ workflow run ပါတယ်။ workflow_evaluated event detail မှာ node_red_response (evidence) နဲ့ workflow_decision ကို သိမ်းပြီး pipeline_runs ရဲ့ onu_serial / olt_hostname / ca1 / uplink ကို ဖြည့်ပါတယ်။",
				"(a) CRIT30: Node-RED answers, so the workflow runs. The workflow_evaluated event detail keeps node_red_response (the evidence) and workflow_decision, and pipeline_runs gets onu_serial / olt_hostname / ca1 / uplink.", "workflow_evaluated", refEvidence),
			st("(b) CRIT40: mock က 404 ပြန်လို့ ERR_CPE_NOT_FOUND ဖြစ်ပြီး retry မလုပ်ဘဲ run ကို CPE_NOT_FOUND နဲ့ ပိတ်ပါတယ်။",
				"(b) CRIT40: the mock answers 404, so ERR_CPE_NOT_FOUND closes the run as CPE_NOT_FOUND without a retry.", "cpe_not_found", refFetchCPE),
			st("(c) CRIT42: mock က 35 s ကြာမှ ဖြေလို့ NODE_RED_TIMEOUT (30 s) ကျော်ပါတယ်။ Node-RED client က NODE_RED_MAX_RETRIES (3) ကြိမ် ကြိုးစားပြီး မရတော့ retry ဆက်လုပ်ပါတယ်။",
				"(c) CRIT42: the mock answers after 35 s, past NODE_RED_TIMEOUT (30 s). The Node-RED client tries NODE_RED_MAX_RETRIES (3) times, then hands over to the retry path.", "node_red_fetch_started", refNodeRedTries),
			st("RETRY_WORKER_ENABLED=false ဆိုရင် retry job မရေးဘဲ run ကို FAILED_PERMANENT (manual check) နဲ့ ပိတ်ပါတယ်။ Reason မှာ timeout ပါရပါမယ်။ (true ဆိုရင် CPE_FETCH_RETRY_SCHEDULED + retry job ၁ ခု)",
				"With RETRY_WORKER_ENABLED=false it writes no retry job and closes the run as FAILED_PERMANENT (manual check); the reason must say timeout. (With true: CPE_FETCH_RETRY_SCHEDULED and one retry job.)", "cpe_fetch_failed_retry_disabled", refFailNoRetry),
			st("(b) နဲ့ (c) မှာ workflow မ run၊ Kafka မပို့ပါ (workflow_evaluated / kafka_published ၀ ခု)။",
				"In (b) and (c) no workflow runs and nothing goes to Kafka (0 workflow_evaluated / kafka_published).", "", "")),
		rtutilGroup(st("(a) ရဲ့ run ကိုတော့ ပုံမှန်အတိုင်း ယူပြီး RT ticket ကို update လုပ်ပါတယ် (case က မစစ်ပါ)။ (b) (c) မှာ Kafka message မရောက်လို့ rtutil event ၀ ခု ဖြစ်ရပါမယ်။",
			"Takes (a)'s run as usual and updates its RT ticket (the case does not check it). In (b) and (c) no Kafka message arrives, so rtutil writes 0 events.", "", "")),
		bcsGroup(st("(b) (c) မှာ BCS request ၀ ခု ဖြစ်ရပါမယ်။ Mock ရဲ့ request log ကနေ စစ်ပါတယ်။ (a) CRIT30 ကလည်း transferred ဖြစ်လို့ BCS မခေါ်ပါ။",
			"(b) and (c) send 0 BCS requests, checked in the mock's request log. (a) CRIT30 is transferred, so it does not call BCS either.", "", "")),
	},
	"RCV-02": {
		testGroup(st("CRIT11 run ကို CONSUMED၊ BCS_UPDATING၊ RT_UPDATING state တစ်ခုချင်းမှာ စောင့်ပြီး rtutil ကို SIGKILL (.run/rtutil.pid) လုပ်ပါတယ်။ ပြီးရင် make up နဲ့ ပြန်စပါတယ်။",
			"Waits for a CRIT11 run in each of CONSUMED, BCS_UPDATING and RT_UPDATING, SIGKILLs rtutil (.run/rtutil.pid), then starts it again with make up.", "", "")),
		apiGroup(allGood("CRIT11")...),
		rtutilGroup(stConsume,
			st("Kill ဖြစ်တော့ Kafka offset မ commit ရသေးလို့ ပြန်စတဲ့ rtutil က message တူကို ပြန်ရပါတယ်။", "Killed before the offset commit, so the restarted rtutil gets the same message again.", "", ""),
			st("RunProgress (BCSDone၊ StatusUpdated၊ comment marker) ကို ဖတ်ပြီး ပြီးသား step တွေကို ကျော်ပါတယ်။ BCS start က ၁ ခါပဲ ဖြစ်ရပါမယ်။", "Reads RunProgress (BCSDone, StatusUpdated, comment marker) and skips the steps already done. BCS must start only once.", "", refBCSBlock),
			stDone),
		bcsGroup(st("Kill တစ်ခါစီအတွက် BCS start request ၁ ခုပဲ ရရပါမယ်။ X-Request-ID = run_id ဖြစ်လို့ ထပ်ပို့ရင်လည်း job တူ ပြန်ရပါတယ်။", "Must get exactly 1 BCS start per kill. X-Request-ID = run_id, so a repeat gets the same job back.", "", refCpems)),
	},
	"RCV-05": {
		testGroup(st("noc .env ထဲက timeout ၆ ခု (API_STUCK၊ PUBLISHED၊ CPE_FETCH၊ RTUTIL_STUCK၊ BCS_RESULT၊ BCS_RESULT_GIVE_UP) နဲ့ PUBLISHED_TIMEOUT_ENABLED ကို release value နဲ့ တိုက်စစ်ပါတယ်။",
			"Compares the 6 timeouts in the noc .env (API_STUCK, PUBLISHED, CPE_FETCH, RTUTIL_STUCK, BCS_RESULT, BCS_RESULT_GIVE_UP) and PUBLISHED_TIMEOUT_ENABLED with the release values.", "", ""),
			st("RT_UPDATING run ၂ ခုကို DB ထဲ ထည့်ပါတယ်: တစ်ခုက cutoff အောက် (limit − 4 min)၊ တစ်ခုက cutoff အထက် (limit + 1 min)။ Sweep ၂ ခါ စောင့်ပါတယ်။",
				"Seeds two RT_UPDATING runs: one under the cutoff (limit − 4 min), one over (limit + 1 min), and waits two sweeps.", "", "")),
		sweeperGroup("publish_timeout_sweeper",
			st("rtutil state (CONSUMED / BCS_UPDATING / RT_UPDATING) မှာ RTUTIL_STUCK_TIMEOUT_MINUTES ကျော်တဲ့ run ကို FAILED_PERMANENT နဲ့ ပိတ်ပါတယ်။", "Closes a run left in an rtutil state (CONSUMED / BCS_UPDATING / RT_UPDATING) for over RTUTIL_STUCK_TIMEOUT_MINUTES as FAILED_PERMANENT.", "rtutil_unavailable", refSweepTimeout),
			st("Cutoff အောက်က run ကို မထိပါ။", "Leaves the run under the cutoff alone.", "", "")),
	},
	"RCV-06": {
		testGroup(st("pipeline_runs ထဲက COMPLETED state (rtdatacore v1.1.21 မတိုင်ခင်) row တွေကို ရေတွက်ပါတယ်။ ၀ ဖြစ်ရပါမယ်။ ကျန်တဲ့ system တွေ မပါပါ။",
			"Counts pipeline_runs rows still in the pre-rtdatacore-v1.1.21 COMPLETED state. It must be 0. No other system takes part.", "", "")),
	},
	"ACT-02": {
		testGroup(st("CRIT11 run က ref_bcs_process_id (job_id) သိမ်းပြီးတာနဲ့ rtutil ကို SIGKILL လုပ်ပြီး make up နဲ့ ပြန်စပါတယ်။", "As soon as the CRIT11 run has stored ref_bcs_process_id (job_id), SIGKILLs rtutil and starts it again with make up.", "", "")),
		apiGroup(allGood("CRIT11")...),
		rtutilGroup(stConsume, stBCSRun, stJobQueued,
			st("ပြန်စတဲ့ rtutil က message တူကို ပြန်ရပြီး BCSDone ဖြစ်နေလို့ \"Skipping BCS: an earlier attempt of this run already sent it\" ဆိုပြီး BCS ကို ထပ်မခေါ်ပါ။ Job reference မူလအတိုင်း ကျန်ရပါမယ်။",
				"The restarted rtutil gets the same message, sees BCSDone and logs \"Skipping BCS: an earlier attempt of this run already sent it\". The original job reference must stay.", "", refBCSBlock),
			stRTUpdate("Customer Support (status Resolved)"), stDone),
		bcsGroup(st("BCS job ၁ ခုပဲ လက်ခံရပါမယ်။", "Must accept exactly 1 BCS job.", "", "")),
	},
	"ACT-07": {
		testGroup(st("CRIT11 ပို့ပြီးတာနဲ့ NOC operator လို RT REST2 နဲ့ ticket ကို Customer Support queue ကို ရွှေ့ပါတယ်။ ပြီးရင် CRIT30 ပို့ပြီး status ကို open ပြောင်းပါတယ်။",
			"Right after submitting CRIT11, moves the ticket to Customer Support through RT REST2, like a NOC operator. Then submits CRIT30 and changes its status to open.", "", "")),
		apiGroup(st("ပုံမှန်အတိုင်း run ပြီး Kafka သို့ publish လုပ်ပါတယ် (ADM-01 / ADM-02 လိုပဲ)။", "Runs as usual and publishes to Kafka (as in ADM-01 / ADM-02).", "kafka_published", "")),
		rtutilGroup(stConsume,
			st("Ticket ကို မထိခင် (BCS အပါအဝင်) queue ကို စစ်ပါတယ်။ လက်ရှိ queue က before (NOC) လည်း မဟုတ်၊ target လည်း မဟုတ်ရင် \"someone moved it\" ဖြစ်ပြီး RT_UPDATE_FAILED နဲ့ ပိတ်ပါတယ်။",
				"Checks the queue before touching the ticket (BCS included). If the current queue is neither the before queue (NOC) nor the target, someone moved it: the run closes as RT_UPDATE_FAILED.", "rt_update_failed", refCheckQueue),
			st("ဒါကြောင့် BCS မခေါ်၊ comment မထည့်ဘဲ operator ရဲ့ queue ကို ထိန်းထားရပါမယ်။", "So no BCS call and no comment, and the operator's queue is kept.", "", ""),
			st("Status ပြောင်းထားတဲ့ CRIT30 ticket: run က မပြီးရ (skip / hand off)၊ ဒါမှမဟုတ် RT status က open အတိုင်း ကျန်ရပါမယ်။", "For the CRIT30 ticket with its status changed: the run must not complete (skip / hand off), or the RT status must stay open.", "", "")),
		bcsGroup(st("ရွှေ့ထားတဲ့ ticket အတွက် BCS request ၀ ခု ဖြစ်ရပါမယ်။", "Must get 0 BCS requests for the moved ticket.", "", "")),
	},
	"ACT-09": {
		testGroup(stSubmit("14_bcs_rejected (CRIT14) + 15_bcs_unavailable (CRIT15)")),
		apiGroup(st("CRIT14 / CRIT15 နှစ်ခုလုံး all good ဖြစ်လို့ is_remote_resolved = true နဲ့ publish လုပ်ပါတယ် (ADM-01 လိုပဲ)။", "Both CRIT14 and CRIT15 are all good, so they publish with is_remote_resolved = true (as in ADM-01).", "kafka_published", refCategory)),
		rtutilGroup(stConsume, stBCSRun,
			st("CRIT14: mock က 401 ပြန်ပါတယ်။ 401 က final ဖြစ်လို့ retry မလုပ်ဘဲ BCS failed (is_bcs_success = 0) လို့ မှတ်ပါတယ်။", "CRIT14: the mock answers 401. 401 is final, so there is no retry: BCS is recorded as failed (is_bcs_success = 0).", "bcs_update_failed", refCpems),
			st("CRIT15: mock က 503 ပြန်ပါတယ်။ X-Request-ID တူနဲ့ cpems_api.retry (3) ကြိမ်အထိ retry လုပ်ပြီး မရလို့ BCS failed လို့ မှတ်ပါတယ်။", "CRIT15: the mock answers 503. It retries with the same X-Request-ID up to cpems_api.retry (3) times, then records BCS as failed.", "bcs_update_failed", refCpems),
			st("BCS fail ဖြစ်ပေမယ့် RT update ဆက်လုပ်ပြီး TICKET_COMPLETED ဖြစ်ရပါမယ်။ 401 နဲ့ 503 ရဲ့ bcs_status_message ကွဲရပါမယ်။", "BCS failing does not stop the RT update: the run must reach TICKET_COMPLETED, with different bcs_status_message for 401 and 503.", "rt_updated", "")),
		bcsGroup(st("CRIT14 → 401 unauthorized · CRIT15 → 503 service_unavailable", "CRIT14 → 401 unauthorized · CRIT15 → 503 service_unavailable", "", "")),
	},
	"ACT-10": {
		testGroup(st("Ticket ပေါ်မှာ တခြား run ရဲ့ marker (<!-- rr:00000000-… -->) ပါတဲ့ operator comment ကို RT REST2 နဲ့ ထည့်ပြီးမှ CRIT30 ကို ပို့ပါတယ်။",
			"Adds an operator comment carrying another run's marker (<!-- rr:00000000-… -->) through RT REST2, then submits CRIT30.", "", "")),
		apiGroup(transferred()...),
		rtutilGroup(stConsume,
			st("Comment marker စစ်တာက ပြန်စတဲ့ run (RTUpdateStarted) မှာပဲ ဖြစ်ပြီး ဒီ run ရဲ့ marker အတိအကျကိုပဲ ရှာပါတယ်။ ဆင်တူ marker ကြောင့် comment မကျော်ရပါ။",
				"The marker check runs only on a resumed run (RTUpdateStarted) and looks for this run's exact marker, so a lookalike must not make it skip the comment.", "", refMarker),
			stRTUpdate("Passive-Infra"), stDone),
		{System: "(ဃ) RT", SystemEn: "(d) RT", Steps: []flowStep{st("ဒီ run ရဲ့ marker ပါတဲ့ automation comment ၁ ခုတည်း ရှိရပါမယ်။", "There must be exactly 1 automation comment with this run's marker.", "", "")}},
	},
	"OUT-01": {
		testGroup(st("CRIT30 (no BCS)၊ CRIT11 (ok)၊ CRIT12 (fail)၊ CRIT13 (pending) run ၄ ခု ပို့ပါတယ်။ BCS ရလဒ်ကို စောင့်ပြီး pipeline_report API ကနေ run တစ်ခုချင်း မှန်တဲ့ list ထဲ ရှိမရှိ စစ်ပါတယ်။",
			"Submits 4 runs: CRIT30 (no BCS), CRIT11 (ok), CRIT12 (fail), CRIT13 (pending). Waits for the BCS results, then checks through the pipeline_report API that each run is in the right list.", "", "")),
		apiGroup(st("CRIT30 → Passive-Infra (is_remote_resolved false)၊ CRIT11/12/13 → Customer Support Resolved (true)။", "CRIT30 → Passive-Infra (is_remote_resolved false); CRIT11/12/13 → Customer Support Resolved (true).", "kafka_published", refWorkflow)),
		rtutilGroup(stConsume, st("CRIT11/12/13 မှာ BCS စပြီး job_id (ok- / fail- / run-) သိမ်းပါတယ်။ ၄ ခုလုံး TICKET_COMPLETED ဖြစ်ရပါမယ်။", "Starts BCS for CRIT11/12/13 and stores the job_id (ok- / fail- / run-). All 4 must reach TICKET_COMPLETED.", "bcs_job_queued", "")),
		sweeperGroup(compBCSSweeper,
			st("BCS_RESULT_TIMEOUT_MINUTES ကျော်ရင် job status ကို poll ပါတယ်: ok- → succeeded (FULLY_COMPLETED)၊ fail- → failed၊ run- → running (ဆက်စောင့်)။", "After BCS_RESULT_TIMEOUT_MINUTES polls the job status: ok- → succeeded (FULLY_COMPLETED), fail- → failed, run- → running (keeps waiting).", "bcs_update_succeeded", refSweepBCS)),
		{System: "(စ) pipeline_report", SystemEn: "(f) pipeline_report", Steps: []flowStep{st("no_bcs_needed / bcs_ok / bcs_failed / bcs_pending list တစ်ခုစီမှာ သက်ဆိုင်ရာ run ရှိရပါမယ်။", "Each run must be in its no_bcs_needed / bcs_ok / bcs_failed / bcs_pending list.", "", "")}},
	},
	"OUT-04": {
		testGroup(stSubmit("11_bcs_success (CRIT11)"), st("Webhook မပို့ပါ (ပျောက်တယ်လို့ ယူဆ)။", "Sends no webhook (treated as lost).", "", "")),
		apiGroup(allGood("CRIT11")...),
		rtutilGroup(stConsume, stBCSRun, stJobQueued, stDone),
		bcsGroup(st("Mock log မှာ job အတွက် GET poll အနည်းဆုံး ၁ ခု ရှိရပါမယ်။", "The mock log must show at least 1 GET poll for the job.", "", "")),
		sweeperGroup(compBCSSweeper,
			st("BCS_RESULT_TIMEOUT_MINUTES ကျော်ရင် GET /api/v2/channel-selections/<job_id> နဲ့ မူလ job ကို poll ပါတယ်။", "After BCS_RESULT_TIMEOUT_MINUTES polls the original job with GET /api/v2/channel-selections/<job_id>.", "", refSweepBCS),
			st("succeeded ရလို့ is_bcs_success = 1 သိမ်းပြီး run ကို FULLY_COMPLETED ပြောင်းပါတယ် (result event ၁ ခု)။", "Gets succeeded, stores is_bcs_success = 1 and moves the run to FULLY_COMPLETED (one result event).", "bcs_update_succeeded", "")),
	},
	"OUT-06": {
		testGroup(stSubmit("13_bcs_pending (CRIT13)"), st("BCS_RESULT_GIVE_UP_MINUTES + 3 မိနစ် စောင့်ပါတယ်။", "Waits BCS_RESULT_GIVE_UP_MINUTES + 3 minutes.", "", "")),
		apiGroup(allGood("CRIT13")...),
		rtutilGroup(stConsume, stBCSRun, st("Job_id run-<run_id> ကို သိမ်းပြီး TICKET_COMPLETED ဖြစ်ပါတယ်။", "Stores job_id run-<run_id> and reaches TICKET_COMPLETED.", "bcs_job_queued", "")),
		bcsGroup(st("run- job ကို အမြဲ running လို့ ပြန်ပါတယ်။", "Always answers the run- job as running.", "", "")),
		sweeperGroup(compBCSSweeper,
			st("BCS_RESULT_GIVE_UP_MINUTES ကျော်ရင် give up လုပ်ပါတယ်။ Spec အရ bcs_result_gave_up (unknown) ရေးပြီး is_bcs_success NULL ထားရပါမယ်။", "Gives up after BCS_RESULT_GIVE_UP_MINUTES. Per the spec it must write bcs_result_gave_up (unknown) and leave is_bcs_success NULL.", "bcs_result_gave_up", refSweepBCS),
			st("လက်ရှိ code က give up မှာ bcs_update_failed ရေးလို့ ဒီ case fail ပါတယ် (review ❌ မရှိ)။", "The current code writes bcs_update_failed on give-up, so this case fails (review ❌ missing).", "bcs_update_failed", refSweepBCS)),
	},
	"OPS-01": {
		testGroup(st("RETRY_WORKER_ENABLED = false ဖြစ်ကြောင်း စစ်ပြီး Node-RED mock container ကို docker stop လုပ်ပါတယ်။ CRIT30 ပို့ပြီး ပြီးရင် make criteria-mock-up နဲ့ mock ကို ပြန်စပါတယ်။",
			"Checks RETRY_WORKER_ENABLED = false, stops the Node-RED mock container (docker stop), submits CRIT30, and afterwards starts the mock again with make criteria-mock-up.", "", "")),
		apiGroup(stReceived, stGatesPass, stFetch("CRIT30"),
			st("Node-RED မရလို့ fetch fail ဖြစ်ပြီး retry ပိတ်ထားလို့ failWithoutRetry က run ကို FAILED_PERMANENT နဲ့ ပိတ်ပါတယ်။ Reason မှာ stage နာမည် ပါရပါမယ်။",
				"Node-RED is down, so the fetch fails; with retries off, failWithoutRetry closes the run as FAILED_PERMANENT. The reason must name the stage.", "cpe_fetch_failed_retry_disabled", refFailNoRetry),
			st("retry_jobs ထဲ job ၀ ခု ဖြစ်ရပါမယ်။", "There must be 0 jobs in retry_jobs.", "", "")),
		rtutilGroup(stNotTouch),
	},
	"OPS-06": {
		testGroup(st("PUBLISHED_TIMEOUT_MINUTES + 2 မိနစ် ကြာနေတဲ့ PUBLISHED run ၂၀ ခု၊ ပြီးရင် ၄၅ ခုကို DB ထဲ ထည့်ပါတယ်။ ပြီးရင် ပြန်ဖျက်ပါတယ်။", "Seeds 20, then 45, PUBLISHED runs idle for PUBLISHED_TIMEOUT_MINUTES + 2 minutes, and deletes them afterwards.", "", "")),
		sweeperGroup("publish_timeout_sweeper",
			st("PUBLISHED_SWEEP_INTERVAL_MS တိုင်း SWEEPER_BATCH_SIZE (20) ခုစီ ယူပြီး rtutil မယူတဲ့ PUBLISHED run ကို FAILED_PERMANENT နဲ့ ပိတ်ပါတယ်။", "Every PUBLISHED_SWEEP_INTERVAL_MS takes up to SWEEPER_BATCH_SIZE (20) runs and closes PUBLISHED runs rtutil never consumed as FAILED_PERMANENT.", "publish_consume_timeout", refSweepTimeout),
			st("Interval ၂ ခု + 30s အတွင်း အကုန်ပိတ်ပြီး run တစ်ခုစီမှာ closing event ၁ ခုပဲ ရှိရပါမယ်။", "All must close within 2 intervals + 30 s, each with exactly one closing event.", "", "")),
	},
	"OPS-13": {
		testGroup(st("PUBLISHED run ၃၀၀ ခုကို DB ထဲ ထည့်ပါတယ်။ ပြီးရင် ပြန်ဖျက်ပါတယ်။", "Seeds 300 PUBLISHED runs and deletes them afterwards.", "", "")),
		sweeperGroup("publish_timeout_sweeper",
			st("Batch အလိုက် PUBLISHED run တွေကို FAILED_PERMANENT နဲ့ ပိတ်ပါတယ်။ Interval + 2 မိနစ်အတွင်း ၃၀၀ လုံး ပိတ်ရပါမယ်။", "Closes the PUBLISHED runs batch by batch as FAILED_PERMANENT; all 300 must close within one interval + 2 minutes.", "publish_consume_timeout", refSweepTimeout)),
	},
	"V2-01": {
		testGroup(st("Queue gate (01_not_eligible_queue) နဲ့ ADM-03 ရဲ့ gate ၅ ခုကို API ဆီ တိုက်ရိုက် ပို့ပါတယ်။", "Sends the queue gate (01_not_eligible_queue) and ADM-03's 5 gates straight to the API.", "", "")),
		apiGroup(st("API မှာ gate rule ၄ ခု ပဲ ရှိပြီး queue rule မပါပါ။ ဒါကြောင့် queue sample က admit ဖြစ်သွားပြီး ဒီ check fail ဖြစ်နိုင်ပါတယ် (queue ကို RT ဘက်က စစ်တာ)။",
			"The API has only 4 gate rules and no queue rule, so the queue sample gets admitted and that check can fail (the RT side checks the queue).", "", refGates),
			st("ကျန်တဲ့ gate ၅ ခုကို NOT_ELIGIBLE နဲ့ ပိတ်ပါတယ် (ADM-03a–e လိုပဲ)။", "Closes the other 5 gates as NOT_ELIGIBLE (as in ADM-03a–e).", "", "")),
		rtutilGroup(stNotTouch),
	},
	"V2-02": {
		testGroup(st("Ticket တစ်ခုကို ၂ ခါ ပို့ပါတယ် (admitted → active run)။ Ticket နောက်တစ်ခုမှာ ၁ နာရီက ပြီးတဲ့ run ကို seed လုပ်ပြီး ပို့ပါတယ် (cooldown)။", "Sends one ticket twice (admitted → active run), and another ticket with a run seeded as finished 1 h ago (cooldown).", "", "")),
		apiGroup(st("Admitted: 202 + run_id + \"Processing Remote Resolve\"။", "Admitted: 202 + run_id + \"Processing Remote Resolve\".", "api_received", refController),
			st("Active run: 202၊ run_id မပါ၊ DB မှာ ဘာမှ မရေးပါ။", "Active run: 202, no run_id, nothing written.", "", refCheckTicket),
			st("Cooldown: 202၊ run_id မပါ၊ ALREADY_PROCESSED run တစ်ခု မှတ်ပါတယ်။", "Cooldown: 202, no run_id, one ALREADY_PROCESSED run recorded.", "already_processed", refCheckTicket),
			st("Active run နဲ့ cooldown က status နဲ့ message တူနေလို့ client က ခွဲမသိနိုင်ပါ။ ဒီ check fail ပါတယ် (review 🟡)။", "Active run and cooldown share the same status and message, so a client cannot tell them apart; that check fails (review 🟡).", "", refController)),
	},
}
