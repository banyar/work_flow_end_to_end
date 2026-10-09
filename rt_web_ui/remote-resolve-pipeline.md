# Remote Resolve Pipeline

ဒီ document သည် NOC queue ticket တစ်ခုကို RT Web UI (`rt_web_ui`) က စစ်ဆေး၍ RT External API (`noc_automation`) သို့ ပို့ချိန်မှစ၍ Node-RED data fetch, workflow decision, Kafka publish, retry process နှင့် RTUtil (`remote-resolved-queue-transfer`) ဘက်မှ Kafka consume, CPEMS/BCS call, RT ticket update အထိ end-to-end flow ကို ရှင်းပြထားသည်။

## အဓိက ရည်ရွယ်ချက်

Pipeline ၏ ရည်ရွယ်ချက်မှာ RT ticket တစ်ခုအတွက် CPE/ONU network data ကိုစစ်ဆေးပြီး workflow rule များအရ ticket ကို remote resolve လုပ်မလား၊ ဘယ် queue သို့ပို့မလား၊ မည်သည့် comment ထည့်မလားကို ဆုံးဖြတ်ကာ ထိုဆုံးဖြတ်ချက်အတိုင်း RT ticket ကို အမှန်တကယ် update လုပ်ရန်ဖြစ်သည်။

## Service သုံးခု၏ တာဝန်ခွဲဝေမှု

Run တစ်ခုကို service သုံးခုက ဆက်တိုက် ရေးသည်။ `rt_web_ui` က run ကို ဖွင့်ပြီး `SUBMITTED_TO_API` အထိ ပိုင်သည်၊ `noc_automation` က `RECEIVED` မှ `PUBLISHED` အထိ ပိုင်သည်၊ RTUtil က Kafka consume မှစ၍ ဆက်လုပ်သည်။

| Service | Module | `pipeline_run_events.component` | တာဝန် |
|---|---|---|---|
| RT Web UI | `rt_web_ui` | `rt_web_ui` | Section 1 gate (NOC queue, ticket status, CPE ID / Local Service ID, OPI, Service Type) စစ်၊ run ဖွင့်၊ payload ကို API သို့ POST၊ API ၏ အဖြေကို မှတ် |
| RT External API | `noc_automation` | `remote_resolve_service` | Request validate, run ဖန်တီး, Node-RED fetch, normalize, workflow, Kafka publish |
| Retry worker | `noc_automation` (same process goroutine) | `retry_worker` | Node-RED fetch / Kafka publish ကို durable retry |
| RTUtil consumer | `remote-resolved-queue-transfer` (`rtutil ticket kafka-remote-resolve`) | `rtutil_consumer` | Kafka consume, message validate, status determination, CPEMS/BCS, RT REST2 update |

Entity နှင့် repository များ (`PipelineRun`, `PipelineRunEvent`, `RetryJob`, `TransitionState`) သည် shared library `rtdatacore` ထဲတွင်ရှိပြီး service နှစ်ခုလုံးက တူညီသော state enum ကို အသုံးပြုသည်။

## Table သုံးခု၏ ဆက်စပ်ပုံ

```text
                         1 : many
pipeline_runs  ------------------------>  pipeline_run_events
      |
      | 1 : many
      v
retry_jobs

pipeline_runs.active_retry_job_id  ----> retry_jobs.id
retry_jobs.run_id                  ----> pipeline_runs.run_id
pipeline_run_events.run_id         ----> pipeline_runs.run_id
```

| Table | သိမ်းထားသည့်အရာ | ရေးသော service | မေးခွန်းဖြေသည့်ပုံစံ |
|---|---|---|---|
| `pipeline_runs` | Run တစ်ခု၏ လက်ရှိ state နှင့် final summary | သုံးခုလုံး | “ဒီ ticket run အခုဘယ်အဆင့်လဲ?” |
| `pipeline_run_events` | State transition များ၏ append-only history | သုံးခုလုံး | “ဘယ်အချိန် ဘယ် component က ဘာလုပ်ခဲ့လဲ?” |
| `retry_jobs` | ပြန်လုပ်ရမည့် durable background job | `noc_automation` သာ | “ဘယ် retry ကို ဘယ်အချိန် ပြန်လုပ်ရမလဲ?” |

`run_id` သည် transaction တစ်ခုလုံး၏ UUID ဖြစ်ပြီး table သုံးခုနှင့် Kafka message ကို ဆက်သွယ်ပေးသည့် correlation ID ဖြစ်သည်။ `run_id` ကို **RT Web UI (`rt_web_ui`) က UUIDv4 ထုတ်ပြီး request payload ထဲတွင် ထည့်ပို့သည်** — API က ကိုယ်တိုင် generate မလုပ်ပါ (အသေးစိတ် Stage 0 နှင့် အဆင့် 1)။ `pipeline_runs.id` သည် database internal primary key သာဖြစ်သည်။

State ပြောင်းတိုင်း `TransitionState()` က `pipeline_runs` update နှင့် `pipeline_run_events` insert ကို transaction တစ်ခုတည်းဖြင့် လုပ်သည်။ Terminal state သို့ရောက်လျှင် `completed_at` ကို set လုပ်သည်။

## End-to-End Flow

```text
[rt_web_ui] ticket JSON (NOC queue)  --  run_id = UUIDv4
  |
  |  pipeline_runs row create (current_state = "")
  v
Section 1 gates --fail--> NOT_ELIGIBLE (terminal, "No Process")
  |   NOC queue · ticket status · CPE ID + Local Service ID · OPI field အလွတ် · eligible Service Type
  v
SUBMITTED_TO_API
  |
  |  POST /api/v1/cpe/remote-resolve  (payload ထဲတွင် run_id ပါ)
  |-- connection error / timeout --> API_UNREACHABLE (terminal)
  v
[noc_automation] validate  --invalid--> HTTP 400 --> API_REJECTED (terminal)
  |
  |  run ကို SUBMITTED_TO_API မှ RECEIVED သို့ ဆက်ယူ (row မရှိလျှင် အသစ် create)
  |    --run_id သည် အခြား state တွင် ရှိနေ--> HTTP 409 --> API_REJECTED
  |    --DB error--> HTTP 500 --> API_REJECTED
  |  HTTP 200 + run_id  --> [rt_web_ui] event api_accepted ("Ticket Processing started")
  v
Node-RED CPE status fetch (immediate retry x3)
  |-- 404 ---------------> CPE_NOT_FOUND (terminal)
  |-- still failing -----> retry job (NODE_RED_FETCH) --> retry worker
  |                        (RETRY_WORKER_ENABLED=false --> FAILED_PERMANENT)
  v
Workflow decision (WORKFLOW_EVALUATED)
  |
  v
Kafka publish --fail--> retry job (KAFKA_PUBLISH) --> retry worker
  |                     (RETRY_WORKER_ENABLED=false --> FAILED_PERMANENT)
  |
  v
PUBLISHED
  |
  |  Kafka topic (KAFKA_TOPIC_REMOTE_RESOLVE)
  v
[rtutil] consume --already terminal--> skip (redelivery)
  |
  |  CONSUMED
  v
Message validate --fail--> CONSUME_VALIDATION_FAILED (terminal)
  |
  v
RT ticket GET --fail--> RT_UPDATE_FAILED (terminal)
  |  (New ဖြစ်ပါက In_Progress သို့ အရင်ပြောင်း)
  v
Status determination --already final--> SKIPPED (terminal)
  |
  |-- is_remote_resolved = true --> BCS_UPDATING --> CPEMS/BCS call (result ကို မှတ်ပြီး ဆက်သွား)
  v
RT_UPDATING --> RT REST2 PUT --fail/non-200--> RT_UPDATE_FAILED (terminal)
  |
  v
Comment POST --> COMPLETED (terminal)
```

## Stage 0: RT Web UI (`rt_web_ui`)

[`rt_web_ui`](../../rt_web_ui/README.md) program သည် final.html section 1 (step 1–5) ကို လုပ်ဆောင်သည်။ RT Web UI ပို့မည့် ticket JSON ကို gate များဖြင့် စစ်ပြီး RT External API သို့ ပို့ကာ run ကို API မခေါ်မီ ကတည်းက `pipeline_runs` / `pipeline_run_events` တွင် မှတ်တမ်းတင်သည် (component `rt_web_ui`)။

### 0.1 Run ပုံ

```bash
go run ./rt_web_ui send --file ticket.json --env noc_automation/.env [--run-id <uuid>] [--set key=value ...]
cd rt_web_ui && make send                         # FILE=ticket_valid.json, ENV=.env (rt_web_ui/.env)
make send FILE=not_eligible.json                  # gate fail sample
make send ARGS='--set custom_fields.service_type="MNet Plus" --set custom_fields.opi_site_code='   # --set ကို အကြိမ်ကြိမ် သုံးနိုင်
make send ENV=../../NocAutomationCodeMerge/noc_automation/.env   # noc_automation ၏ .env ကို တိုက်ရိုက် သုံး
```

| Item | အသေးစိတ် |
|---|---|
| Input | `--file` (ticket JSON, `-` = stdin)။ `--set` ဖြင့် `queue=…` / `custom_fields.<key>=…` ကို override လုပ်နိုင်သည် |
| `run_id` | `--run-id` မပေးလျှင် UUIDv4 အသစ်ထုတ်သည် (UUID format မဟုတ်လျှင် exit 3) |
| Sample | `ticket_valid.json` (gate အားလုံး pass), `not_eligible.json` (Service Type `FR-Biz` + OPI `HMHY`)၊ RT DB မှ export (§0.4)၊ report criteria sample (§0.5) |
| Config | `--env` file (default `.env`)၊ process env က file ထက် ဦးစားပေး။ Load လုပ်ပြီး config ကို stderr တွင် ပြသည် (token / DB password ကို `****` ဖြင့် ဖုံး) |
| Exit code | `0` accepted · `1` not eligible · `2` API rejected / unreachable · `3` usage / config / DB error |

| Config key | Default | အဓိပ္ပာယ် |
|---|---|---|
| `RT_WEB_UI_API_URL` | `http://localhost:${DOMAIN_PORT}/api/v1/cpe/remote-resolve` | RT External API |
| `RT_WEB_UI_API_TOKEN` | `DEFAULT_TOKEN` | Bearer token |
| `RT_WEB_UI_API_TIMEOUT` | `30s` | API call timeout |
| `RT_WEB_UI_NOC_QUEUE` | `Network Operation Center (NOC)` | Gate 1 |
| `RT_WEB_UI_ALLOWED_TICKET_STATUSES` | `new,in_progress,re-open,re-open-1,re-open-2,re-open-3,re-open-4` | Gate 2 — စာလုံးအကြီးအသေး မခွဲ၊ space ကို `_` အဖြစ် ယူ (`In Progress` = `in_progress`) |
| `RT_WEB_UI_OPI_FIELDS` | `opi_site_code` | Gate 4 — `custom_fields` key များ (တစ်ခုခု value ရှိလျှင် OPI customer) |
| `RT_WEB_UI_ELIGIBLE_SERVICE_TYPES` | `MNet,MNet Plus,G2 Net,G2 Plus` | Gate 5 — စာလုံးအကြီးအသေး၊ space၊ `-`၊ `_` ကို ဂရုမစိုက်ဘဲ နှိုင်းယှဉ် (`G2net` = `G2 Net`) |
| `MYSQL_DB_*` | — | noc_automation နှင့် DB တူရမည် |

### 0.2 အဆင့်လိုက် flow

1. Payload ၏ `id` ကို ticket number အဖြစ် စစ်သည် (string / number နှစ်မျိုးလုံး လက်ခံ၊ မမှန်လျှင် DB မရေးဘဲ exit 3)။
2. `pipeline_runs` row ကို **`current_state = ""`** ဖြင့် INSERT လုပ်သည် (event မထည့်)။ Ticket column များ (`ticket_no`, `cpe_id`, `local_service_id`, `before_queue` …) နှင့် `rt_request_snapshot` ကို API ၏ `RECEIVED` row နှင့် တူအောင် ဖြည့်သည် — gate fail ဖြစ်သော run များကိုလည်း report တွင် ကြည့်နိုင်ရန်။
3. Gate ၅ ခုကို အစဉ်အတိုင်း စစ်ပြီး ပထမ fail တွင် ရပ်သည် (gate 2 ticket status သည် diagram တွင် မပါသော ထပ်တိုး rule ဖြစ်သည်)။

   | Gate | စစ်ဆေးချက် | Fail event | `last_state_reason` (ဥပမာ) |
   |---|---|---|---|
   | 1 | `queue` = `RT_WEB_UI_NOC_QUEUE` | `not_in_noc_queue` | ticket queue "Customer Support" is not "Network Operation Center (NOC)" |
   | 2 | Ticket `status` သည် allowed list ထဲတွင် ပါ | `ticket_status_not_allowed` | ticket status "Resolved" is not one of new, in_progress, re-open, re-open-1, re-open-2, re-open-3, re-open-4 |
   | 3 | `cpe_id`, `local_service_id` ပါဝင် | `cpe_or_local_service_id_missing` | CPE ID and Local Service ID are required (missing: cpe_id) |
   | 4 | OPI field အလွတ် | `opi_customer_excluded` | OPI customer (opi_site_code="HMHY") is excluded from automation |
   | 5 | `service_type` eligible | `service_type_not_eligible` | Service Type "FR-Biz" is not one of MNet, MNet Plus, G2 Net, G2 Plus |

   Fail ပါက `"" → NOT_ELIGIBLE` (terminal, detail `{"gate": …}`)၊ "No Process: …" ကို ပြပြီး API ကို **မခေါ်ပါ** (exit 1)။
4. Pass ပါက `"" → SUBMITTED_TO_API` (event `payload_submitted`, detail `{url, bytes}`) ပြီးမှ payload ကို POST လုပ်သည်။ ပို့သော body သည် input JSON အတိုင်းဖြစ်ပြီး `id` ကို string ပြောင်းကာ `run_id` ထည့်ထားသည်။
5. API ၏ အဖြေ —

   | API အဖြေ | State | Event (`rt_web_ui`) | ပြသမည့်စာ |
   |---|---|---|---|
   | 200 | API က `SUBMITTED_TO_API → RECEIVED` ဆက်ယူပြီး (rt_web_ui က state **မရေး**) | `api_accepted` (detail `{http_status, message}`) | Ticket Processing started |
   | 400 / 409 / 500 | `API_REJECTED` | `api_rejected` | RT External API returned … |
   | Connection error / timeout | `API_UNREACHABLE` | `api_unreachable` | RT External API unreachable: … |

   - 200 ပြန်လာချိန်တွင် API ၏ background processing က `FETCHING_CPE_STATUS` / `WORKFLOW_EVALUATED` သို့ ရောက်နေနိုင်သဖြင့် state ပြန်ရေးလျှင် run ကို နောက်ပြန်ဆွဲချမိမည်။ ထို့ကြောင့် `AppendEvent` (state မပြောင်း) ကိုသာ သုံးသည်။
   - `API_REJECTED` / `API_UNREACHABLE` ကို run သည် `SUBMITTED_TO_API` တွင် ရှိနေဆဲ ဖြစ်မှသာ ရေးသည်။ API က ဆက်ယူပြီးဖြစ်ပါက (ဥပမာ API လက်ခံပြီးမှ client timeout) event သာ ထည့်ပြီး API ၏ state ကို မထိပါ။

### 0.3 API ဘက် — run ကို ဆက်ယူခြင်း

`helpers.CreatePipelineRun` (noc_automation) သည် rtdatacore `AttachSubmittedRun` ကို အရင်ခေါ်သည် —

| `run_id` ၏ အခြေအနေ | API ၏ လုပ်ဆောင်ချက် | HTTP |
|---|---|---|
| Row မရှိ (rt_web_ui မသုံးဘဲ တိုက်ရိုက်ခေါ်) | `Create` ဖြင့် `RECEIVED` row အသစ် (ယခင်အတိုင်း) | 200 |
| `SUBMITTED_TO_API` | Row ကို lock (`SELECT … FOR UPDATE`) လုပ်ပြီး ticket column + `rt_request_snapshot` ကို update၊ `SUBMITTED_TO_API → RECEIVED` (event `api_received`, component `remote_resolve_service`) — transaction တစ်ခုတည်း | 200 |
| အခြား state (reuse / replay လုပ်ထားသော `run_id`) | `ErrRunNotClaimable` — processing မစ | **409** "run_id already in use" |
| DB error | — | 500 "Failed to initialize remote resolve run" |

### 0.4 RT DB မှ ticket sample export

`rt_web_ui export` သည် RT DB (`Tickets`, `ObjectCustomFieldValues`) ကို **read-only** ဖတ်ပြီး `send` လက်ခံသော format ဖြင့် JSON array ထုတ်သည်။

```bash
cd rt_web_ui
make samples                                  # queue 43, status re-open*/new/in_progress, LIMIT=1 → samples/tickets.json
make samples IDS=384,385 OUT=samples/two.json
make samples ALL_FIELDS=1                     # queue ၏ custom field အားလုံး (value မရှိလျှင် "")
make send-sample ID=384 ARGS='--set custom_fields.service_type="MNet Plus"'   # $(OUT) ထဲမှ ticket တစ်ခု ပို့
```

| Option | အသေးစိတ် |
|---|---|
| (default) | Ticket တွင် **value ရှိသော** custom field များသာ ထုတ်သည် — RT တွင် မဖြည့်ထားသော field (ဥပမာ `service_type`) ပါမလာ |
| `--all-fields` (`ALL_FIELDS=1`) | Queue တွင် သုံးနိုင်သော enabled custom field အားလုံး (global `ObjectId 0` + queue) ကို ထည့်ပြီး value မရှိလျှင် `""` (`tags` = `[]`)။ RT တွင် နာမည်တူ field (ဥပမာ `Township` ၂ ခု) ကို key တစ်ခုတည်း ထုတ်သည်။ ရှိပြီးသား value ကို မပြောင်း |

Key ပြည့်စုံသော်လည်း value အလွတ်ဖြစ်နိုင်သဖြင့် gate pass ရန် `--set` ဖြင့် ဖြည့်ရမည် (ဥပမာ `service_type`)။

### 0.5 Report criteria sample များ

[`rt_web_ui/criteria/`](../../rt_web_ui/criteria/README.md) တွင် pipeline report ([pipeline_report](../../pipeline_report/README.md)) ၏ card / bucket တစ်ခုချင်းစီကို ဖြစ်စေမည့် file များ ရှိသည်—

| ရလဒ် | ပြင်သော data | File |
|---|---|---|
| Not eligible (gate ၅ ခု) | Ticket JSON (`queue`, `status`, `cpe_id`, `opi_site_code`, `service_type`) | `tickets/01`–`05` |
| Success · Remote resolved / Kept in NOC / Transferred | Node-RED CPE status — ticket တွင် `cpe_id = CRIT-<number>` သာ ပြောင်းပြီး mock က ထို id အလိုက် response ပြန်ပေး | `tickets/10`–`36`, `cpe_status/`, `mockoon-criteria.json` |

```bash
cd rt_web_ui
make criteria-mock                            # Node-RED mock (:3002, docker mockoon/cli)
# noc_automation/.env → NODE_RED_BASE_URL=http://localhost:3002/ ပြီး restart
make send-criteria C=20_kept_olt_offline
```

Workflow engine နှင့် rt_web_ui gate ကို ဤ file များဖြင့် run ၍ ရလဒ်ကို စစ်ပြီး။ Success criteria များသည် rtutil မှတစ်ဆင့် RT ticket ကို တကယ် update လုပ်သဖြင့် test ticket ဖြင့်သာ ပို့ပါ။

## Stage 1–4: `noc_automation`

### 1. Request လက်ခံခြင်း

API က request ကို validate လုပ်သည် —

- `run_id` မဖြစ်မနေ လိုအပ်ပြီး canonical UUID format (`8-4-4-4-12` hex, စာလုံး ၃၆ လုံး) ဖြစ်ရမည်။ မပါပါက "missing required field: run_id"၊ format မမှန်ပါက "invalid run_id: must be a UUID" ဖြင့် HTTP 400 ပြန်သည်။ `{...}`, `urn:uuid:`, hyphen မပါသော ပုံစံနှင့် ရှေ့နောက် space ပါသော တန်ဖိုးများကို လက်မခံပါ။
- `id` နှင့် `queue` မဖြစ်မနေ လိုအပ်သည်။
- `REMOTE_RESOLVE_CUSTOM_FIELDS` ထဲရှိ custom field တိုင်း ပါဝင်ပြီး အလွတ်မဖြစ်ရ။ `.env.example` default ကို `cpe_id,local_service_id` နှစ်ခုသာ ဖြစ်အောင် လျှော့ထားသည် (ယခင်က `program`, `ticket_problem`, `tags`, `root_cause`, `plan_start_date` ပါ ပါဝင်)။ Environment တစ်ခုစီ၏ `.env` ကိုလည်း ကိုက်ညီအောင် ပြင်ရမည်။
- Validate မအောင်မြင်ပါက HTTP 400 ပြန်ပြီး run မဖန်တီးပါ။

**`run_id` ကို request မှ ယူသည်။** Caller (RT Web UI) က UUID `run_id` ကို payload top-level တွင် ထည့်ပို့ရမည် —

```json
{ "run_id": "6ad204d7-74f5-44c1-8ea2-bd32dc8adcb2", "id": "2851212", "queue": "...", "custom_fields": { ... } }
```

API သည် ထို `run_id` ၏ run ကို `RECEIVED` သို့ ရောက်စေပြီး (rt_web_ui ဖွင့်ထားသော run ကို ဆက်ယူ သို့မဟုတ် row အသစ် ဖန်တီး — Stage 0.3) HTTP 200 response ထဲတွင် တူညီသော `run_id` ကို ပြန်ပေးသည်။ `run_id` ကို caller ကိုယ်တိုင် ထုတ်သဖြင့် request မပို့မီကတည်းက **သိပြီးသား** ဖြစ်သည် (response ကို စောင့်စရာမလို)။ rt_web_ui ကို သုံးပါက row သည် API မခေါ်မီကတည်းက ရှိပြီး (gate fail / API error run များပါ) run state ကို query လုပ်ရန် API endpoint မရှိသေးပါ — ယခုအချိန်တွင် DB (`pipeline_runs` / `pipeline_run_events`) မှ `run_id` ဖြင့် တိုက်ရိုက် ကြည့်ရသည်။

Code — route `POST /api/v1/cpe/remote-resolve` (`frontiir/api/cpe_lookup_routes.go`) → `RemoteResolveController.RemoteResolve` (`frontiir/api/controllers/remote_resolve_controller.go`) → `helpers.CreatePipelineRun` (`frontiir/helpers/remote_resolve_helper.go`) → rtdatacore `PipelineRunRepository.AttachSubmittedRun`၊ row မရှိလျှင် `PipelineRunRepository.Create`။ Response struct သည် `models.RemoteResolveAcceptedResponse` (`frontiir/api/models/remote_resolve.go`)။

- `pipeline_runs.run_id` တွင် unique index ရှိသည်။ **`run_id` တူ request ထပ်ပို့ပါက** (run သည် `SUBMITTED_TO_API` မဟုတ်တော့) HTTP **409** "run_id already in use" ပြန်ကာ processing မစပါ။ ဆိုလိုသည်မှာ `run_id` အလိုက် duplicate guard ဖြစ်သည် (ticket အလိုက် မဟုတ်ပါ — ticket တူ `run_id` မတူ request ၂ ခုသည် run ၂ ခု ဖြစ်ဆဲ)။ Retry လုပ်လိုပါက RT Web UI က `run_id` အသစ်ဖြင့် ပို့ရမည်။
- Row ဖန်တီးပြီးလျှင် HTTP 200 ကို ချက်ချင်းပြန်ပြီး ကျန်အဆင့်များကို background goroutine ဖြင့် ဆက်လုပ်သည်။

Initial data —

- `run_id`, `ticket_id`, `ticket_no`, `cpe_id`, `local_service_id`
- `service_area`, `township`, `ticket_problem`, `ticket_created_at`, `ticket_status`
- `before_queue` = request ၏ `queue`
- `rt_request_snapshot` = RT request JSON အပြည့်အစုံ
- `current_state = RECEIVED`

### 2. Node-RED မှ CPE status fetch လုပ်ခြင်း

`current_state` ကို `FETCHING_CPE_STATUS` သို့ပြောင်းပြီး event `node_red_fetch_started` ကိုထည့်သည်။

- `NODE_RED_MAX_RETRIES` (default 3) ကြိမ် ချက်ချင်း retry လုပ်သည်၊ ကြားတွင် `NODE_RED_RETRY_DELAY_MS` (default 1000 ms) စောင့်သည်။
- HTTP 404 → `CPE_NOT_FOUND` (terminal, retry မလုပ်)။
- Timeout, connection error, non-200, JSON parse error → retry ကုန်လျှင် `NODE_RED_FETCH` retry job ဖန်တီးပြီး `CPE_FETCH_RETRY_SCHEDULED` (`RETRY_WORKER_ENABLED=false` ဆိုလျှင် `FAILED_PERMANENT`)။
- Mock mode မရှိတော့ပါ — `NODE_RED_BASE_URL` ကို HTTP ဖြင့် အမြဲ တကယ်ခေါ်သည်။ Failure test လုပ်လိုလျှင် `NODE_RED_BASE_URL` ကို မှားသော URL သို့ ပြောင်းပါ။

### 3. Normalize နှင့် Workflow decision

Node-RED response ကို normalized model သို့ ပြောင်းသည်။ Normalize အတွက် သီးခြား state မရှိပါ — response ပုံစံမှားလျှင် အဆင့် 2 တွင် JSON parse fail ဖြစ်ပြီး retry လုပ်ပြီးဖြစ်သည်။ Response/request `nil` ဖြစ်သော defensive case တွင်သာ event `normalize_failed` ဖြင့် `FAILED_PERMANENT` ပိတ်သည်။

Workflow engine (`remote_resolve_workflow_engine.json`) က rule အလိုက်စစ်ပြီး Kafka payload တစ်ခုထုတ်သည်။ ပြီးလျှင် `current_state = WORKFLOW_EVALUATED` ဖြစ်ပြီး အောက်ပါတို့ကို `pipeline_runs` တွင် သိမ်းသည် —

- Decision: `target_queue`, `is_remote_resolved`, `final_message` (comment)
- Network context: `onu_serial`, `olt_hostname`, `ca1`, `uplink`
- Event `detail` တွင် raw Node-RED response နှင့် workflow decision

### 4. Kafka publish

Payload ကို Kafka သို့ပို့သည် (5 s timeout)။ Publish အောင်မြင်လျှင် `current_state = PUBLISHED`။ Fail ဖြစ်လျှင် evaluate ပြီးသား payload ပါဝင်သော `KAFKA_PUBLISH` retry job ဖန်တီးပြီး `KAFKA_PUBLISH_RETRY_SCHEDULED` (`RETRY_WORKER_ENABLED=false` ဆိုလျှင် job မဖန်တီးဘဲ `FAILED_PERMANENT`)။

`PUBLISHED` သည် terminal **မဟုတ်ပါ**။ Kafka သို့ ရောက်ကြောင်းသာ ဆိုလိုပြီး RTUtil က ဆက်လုပ်ရန် ကျန်သေးသည်။ ထို့ကြောင့် ဒီအဆင့်တွင် `completed_at` မ set ပါ။

## Kafka Message Contract

`noc_automation` → RTUtil —

```json
{
  "id": "2851212",
  "run_id": "<uuid>",
  "is_remote_resolved": true,
  "queue": "Network Operation Center (NOC)",
  "target_queue": "Customer Support",
  "status": "Resolved",
  "comment": "Dear CS, ...",
  "message": "",
  "custom_fields": {
    "cpe_id": "...",
    "suspected_area_of_issue": "...",
    "root_cause_category": "...",
    "service_root_cause": "...",
    "root_cause": "...",
    "resolution_description": "...",
    "resolution_fiber_onu_signal_dbm": "-18.5",
    "on_site_resolution": "",
    "ticket_problem": ""
  }
}
```

- `is_remote_resolved` သည် Final Action (status Resolved) အတွက်သာ `true` ဖြစ်သည်။
- RTUtil က `target_queue` ရှိလျှင် ၎င်းကို၊ မရှိလျှင် `queue` ကို သုံးသည်။
- RTUtil က payload ၏ `status` နှင့် `message` ကို မသုံးပါ။ Status ကို RT ၏ လက်ရှိ status မှ ဆုံးဖြတ်သည် (အဆင့် 7)။
- `custom_fields` key များကို `.rtutil.json` ၏ field mapping ဖြင့် RT custom field name သို့ ပြောင်းသည် (ဥပမာ `root_cause_category` → "Root Cause Category")။

## Stage 5–9: RTUtil (`remote-resolved-queue-transfer`)

### 5. Kafka consume နှင့် redelivery guard

Consumer သည် message ကို တစ်ခုပြီးမှ တစ်ခု sequential လုပ်သည်။ Message တိုင်းတွင် RT DB မှ queue list နှင့် root-cause custom field value များကို ပြန် load လုပ်သည်။

- JSON parse မရပါက log သာလုပ်ပြီး ရပ်သည်။ `run_id` မသိနိုင်သဖြင့် run သည် `PUBLISHED` တွင် ကျန်နေသည်။
- `run_id` ၏ run သည် terminal state ရောက်ပြီးသားဆိုပါက redelivery ဟု သတ်မှတ်ပြီး ဘာမှမလုပ်ဘဲ skip လုပ်သည် (RT/CPEMS မခေါ်၊ DB မရေး)။ Run သည် `CONSUMED`, `BCS_UPDATING`, `RT_UPDATING` ကဲ့သို့ အလယ်တွင် ရပ်နေခဲ့ပါက ထပ်မံ process လုပ်သည်။
- ကျန်ပါက `CONSUMED` သို့ပြောင်းပြီး event detail တွင် Kafka `topic`, `partition`, `offset` ကို မှတ်သည်။

### 6. Message validation

အောက်ပါတို့ကို စစ်သည် —

- Ticket ID > 0
- Target queue သည် RT ၏ active Ticketing queue ဖြစ်ရမည်
- Root Cause သည် valid custom field value ဖြစ်ရမည်
- SAI → RCC → SRC chain မှန်ရမည် (RCC သည် SAI ၏ category အောက်၊ SRC သည် RCC ၏ category အောက်)

မအောင်မြင်ပါက `CONSUME_VALIDATION_FAILED` (terminal)၊ `last_state_reason` တွင် အကြောင်းရင်း (ဥပမာ "Unsupported Target Team") ကို မှတ်သည်။

### 7. RT ticket fetch နှင့် status determination

`GET /ticket/{id}` ဖြင့် လက်ရှိ status ကို ယူသည်။ Fail ဖြစ်ပါက `RT_UPDATE_FAILED` (event `rt_ticket_fetch_failed`)။

Ticket သည် **New** ဖြစ်ပါက `In_Progress` သို့ အရင် PUT လုပ်သည် (decision အားလုံးအတွက်)။

`is_remote_resolved = true` ဖြစ်မှသာ final status ကို ဆုံးဖြတ်သည် —

| လက်ရှိ RT status | သတ်မှတ်မည့် status |
|---|---|
| New, In_Progress, အခြား | Resolved |
| Re-Open | Resolved-2 |
| Re-Open-2 | Resolved-3 |
| Re-Open-3 | Resolved-4 |
| Re-Open-4 | Resolved-5 |
| Cancelled, Pending, Resolved, Resolved-2…5, Closed | Skip |

Skip ဖြစ်ပါက `SKIPPED` (terminal)၊ reason "Ticket status = <status>"။ RT update နှင့် CPEMS call မလုပ်ပါ။

`is_remote_resolved = false` ဖြစ်ပါက status ကို မပြောင်းဘဲ queue, custom fields, comment ကိုသာ update လုပ်သည်။

### 8. CPEMS / BCS call (remote-resolved ticket များသာ)

`BCS_UPDATING` (event `bcs_update_started`) သို့ပြောင်းပြီး CPEMS ကို `POST {"ticket_id", "cpe_id"}` ဖြင့် ခေါ်သည်။ HTTP 200 မဟုတ်ပါက `cpems_api.retry` (default 3) ကြိမ်အထိ delay မပါဘဲ ထပ်ခေါ်သည်။

Result ကို `is_bcs_success`, `bcs_status_message` တွင် မှတ်ပြီး event `bcs_update_succeeded` / `bcs_update_failed` ထည့်သည်။ BCS fail ဖြစ်လည်း run ကို မရပ်ပါ — RT update ကို ဆက်လုပ်သည်။ BCS failure အတွက် သီးခြား state မရှိပါ။

### 9. RT update နှင့် comment

`RT_UPDATING` (event `rt_update_started`) သို့ပြောင်းပြီး `PUT /ticket/{id}` ဖြင့် status, queue, custom fields ကို update လုပ်သည်။ "Resolved Fiber ONU Signal dBm" ကို ဒသမ ၂ နေရာသို့ round လုပ်သည်။

- Error သို့မဟုတ် non-200 → `RT_UPDATE_FAILED` (terminal)၊ reason ဥပမာ "RT returned status 500"။
- 200 → comment ရှိလျှင် `POST /ticket/{id}/comment` ပြီး `COMPLETED` (event `rt_updated`)။ `ticket_status` တွင် RTUtil သတ်မှတ်ခဲ့သော status (မသတ်မှတ်ခဲ့လျှင် fetch လုပ်ခဲ့သော status) ကို သိမ်းသည်။
- Comment fail ဖြစ်လည်း status/queue update ရောက်ပြီးဖြစ်သဖြင့် `COMPLETED` အဖြစ်သာ ထားပြီး `last_state_reason` တွင် "ticket comment failed: ..." ကို မှတ်သည်။

## `current_state` နှင့် `event` ကွာခြားချက်

`current_state` သည် run ၏ **လက်ရှိနေရာ** ဖြစ်သည်။ `event` သည် state transition ဖြစ်စေသော **action သို့မဟုတ်အကြောင်းရင်း** ဖြစ်သည်။ State တူနေသော်လည်း event အသစ်ဝင်နိုင်သည် (ဥပမာ `retry_claimed`, `bcs_update_failed`)။

| From state | To state | Event | Component | အဓိပ္ပာယ် |
|---|---|---|---|---|
| (new) | `""` | — (row create) | `rt_web_ui` | NOC queue ticket အတွက် run ဖွင့် (event မထည့်) |
| `""` | `NOT_ELIGIBLE` | `not_in_noc_queue` / `ticket_status_not_allowed` / `cpe_or_local_service_id_missing` / `opi_customer_excluded` / `service_type_not_eligible` | `rt_web_ui` | Section 1 gate fail ("No Process") |
| `""` | `SUBMITTED_TO_API` | `payload_submitted` | `rt_web_ui` | API သို့ POST မလုပ်မီ |
| `SUBMITTED_TO_API` | `RECEIVED` | `api_received` | `remote_resolve_service` | API က rt_web_ui ၏ run ကို ဆက်ယူ |
| (any) | (same) | `api_accepted` | `rt_web_ui` | API 200 — "Ticket Processing started" |
| `SUBMITTED_TO_API` | `API_REJECTED` | `api_rejected` | `rt_web_ui` | API 400 / 409 / 500 |
| `SUBMITTED_TO_API` | `API_UNREACHABLE` | `api_unreachable` | `rt_web_ui` | API connection error / timeout |
| (new) | `RECEIVED` | — (row create) | `remote_resolve_service` | rt_web_ui မပါဘဲ API ကို တိုက်ရိုက်ခေါ်ချိန် run ဖန်တီး |
| `RECEIVED` | `FETCHING_CPE_STATUS` | `node_red_fetch_started` | `remote_resolve_service` | Node-RED call စတင် |
| `FETCHING_CPE_STATUS` | `CPE_NOT_FOUND` | `cpe_not_found` | `remote_resolve_service` | Node-RED 404 |
| `FETCHING_CPE_STATUS` | `CPE_FETCH_RETRY_SCHEDULED` | `cpe_fetch_retry_scheduled` | `remote_resolve_service` | Fetch failure ကြောင့် retry schedule |
| `FETCHING_CPE_STATUS` | `FAILED_PERMANENT` | `cpe_fetch_failed_retry_disabled` | `remote_resolve_service` | Fetch failure၊ `RETRY_WORKER_ENABLED=false` ဖြစ်၍ retry မလုပ် |
| `*_RETRY_SCHEDULED` | (same) | `retry_claimed` | `retry_worker` | Worker က due job ကို claim |
| `*_RETRY_SCHEDULED` | (same) | `retry_attempt_failed` | `retry_worker` | Retry တစ်ကြိမ် မအောင်မြင်သေး |
| `CPE_FETCH_RETRY_SCHEDULED` | `CPE_NOT_FOUND` | `cpe_not_found_on_retry` | `retry_worker` | Retry တွင် 404 |
| `*_RETRY_SCHEDULED` | `FAILED_PERMANENT` | `retry_attempts_exhausted` | `retry_worker` | Max attempts ကုန် |
| `*_RETRY_SCHEDULED` | `FAILED_PERMANENT` | `unknown_job_type` | `retry_worker` | Job type မသိ (defensive) |
| `FETCHING_CPE_STATUS` / retry | `FAILED_PERMANENT` | `normalize_failed` / `normalize_failed_on_retry` | both | Normalize defensive error |
| `FETCHING_CPE_STATUS` / retry | `WORKFLOW_EVALUATED` | `workflow_evaluated` | both | Decision ထွက်ပြီ |
| `WORKFLOW_EVALUATED` | `KAFKA_PUBLISH_RETRY_SCHEDULED` | `kafka_publish_retry_scheduled` | both | Publish fail၊ retry schedule |
| `WORKFLOW_EVALUATED` | `FAILED_PERMANENT` | `kafka_publish_failed_retry_disabled` | `remote_resolve_service` | Publish fail၊ `RETRY_WORKER_ENABLED=false` ဖြစ်၍ retry မလုပ် |
| `WORKFLOW_EVALUATED` / retry | `PUBLISHED` | `kafka_published` | both | Kafka publish အောင်မြင် |
| `PUBLISHED` | `CONSUMED` | `kafka_consumed` | `rtutil_consumer` | RTUtil က message ယူပြီ |
| `CONSUMED` | `CONSUME_VALIDATION_FAILED` | `consume_validation_failed` | `rtutil_consumer` | Queue / root cause မမှန် |
| `CONSUMED` | `RT_UPDATE_FAILED` | `rt_ticket_fetch_failed` | `rtutil_consumer` | RT ticket GET fail |
| `CONSUMED` | `SKIPPED` | `ticket_skipped` | `rtutil_consumer` | Status final ဖြစ်ပြီးသား |
| `CONSUMED` | `BCS_UPDATING` | `bcs_update_started` | `rtutil_consumer` | CPEMS call စတင် |
| `BCS_UPDATING` | `BCS_UPDATING` | `bcs_update_succeeded` / `bcs_update_failed` | `rtutil_consumer` | CPEMS result |
| `CONSUMED` / `BCS_UPDATING` | `RT_UPDATING` | `rt_update_started` | `rtutil_consumer` | RT PUT စတင် |
| `RT_UPDATING` | `RT_UPDATE_FAILED` | `rt_update_failed` | `rtutil_consumer` | RT PUT error / non-200 |
| `RT_UPDATING` | `COMPLETED` | `rt_updated` | `rtutil_consumer` | RT update အောင်မြင် |

## Pipeline States

| State | Service | အဓိပ္ပာယ် | Terminal? |
|---|---|---|---|
| `""` (အလွတ်) | rt_web_ui | Run ဖွင့်ပြီး gate စစ်နေ (ခဏသာ) | မဟုတ် |
| `NOT_ELIGIBLE` | rt_web_ui | Section 1 gate fail — process မလုပ် | ဟုတ် |
| `SUBMITTED_TO_API` | rt_web_ui | API သို့ ပို့နေသည် | မဟုတ် |
| `API_REJECTED` | rt_web_ui | API က 4xx / 5xx ပြန် | ဟုတ် |
| `API_UNREACHABLE` | rt_web_ui | API ကို ချိတ်မရ / timeout | ဟုတ် |
| `ALREADY_PROCESSED` | (reserved) | 24h duplicate check — writer မရှိသေး | ဟုတ် |
| `RECEIVED` | noc_automation | Request လက်ခံပြီးပြီ | မဟုတ် |
| `FETCHING_CPE_STATUS` | noc_automation | Node-RED data fetch လုပ်နေသည် | မဟုတ် |
| `CPE_FETCH_RETRY_SCHEDULED` | noc_automation | CPE fetch retry စောင့်နေသည် | မဟုတ် |
| `CPE_NOT_FOUND` | noc_automation | CPE မတွေ့ပါ | ဟုတ် |
| `WORKFLOW_EVALUATED` | noc_automation | Decision workflow ပြီးဆုံးပြီ | မဟုတ် |
| `KAFKA_PUBLISH_RETRY_SCHEDULED` | noc_automation | Kafka publish retry စောင့်နေသည် | မဟုတ် |
| `PUBLISHED` | noc_automation | Kafka သို့ publish အောင်မြင်၊ RTUtil စောင့်နေ | မဟုတ် |
| `FAILED_PERMANENT` | noc_automation | Retry limit ကုန် သို့မဟုတ် retry ပိတ်ထားစဉ် fail (manual review) | ဟုတ် |
| `CONSUMED` | rtutil | Kafka message ယူပြီးပြီ | မဟုတ် |
| `CONSUME_VALIDATION_FAILED` | rtutil | Message validate မအောင်မြင် | ဟုတ် |
| `BCS_UPDATING` | rtutil | CPEMS/BCS call လုပ်နေ/ပြီး | မဟုတ် |
| `RT_UPDATING` | rtutil | RT ticket update လုပ်နေသည် | မဟုတ် |
| `SKIPPED` | rtutil | Ticket status final ဖြစ်ပြီးသား၊ update မလုပ် | ဟုတ် |
| `RT_UPDATE_FAILED` | rtutil | RT fetch/update မအောင်မြင် | ဟုတ် |
| `COMPLETED` | rtutil | RT ticket update အောင်မြင် | ဟုတ် (success) |

Terminal state သို့မဟုတ် `PUBLISHED` သို့ reason မပါဘဲ ရောက်လျှင် ယခင် failed attempt ၏ `last_state_reason` ကို ရှင်းပစ်သည်။

## Retry Flow (`noc_automation` သာ)

Node-RED သို့မဟုတ် Kafka အတွက် temporary failure ဖြစ်လျှင် retry job တစ်ခုဖန်တီးသည်။

```text
Fetch / publish failure
  → retry_jobs row create (PENDING, next_retry_at = now + schedule[1], default 1m)
  → pipeline run ကို active_retry_job_id ဖြင့် link လုပ်
  → worker က due job ကို claim လုပ် (RETRYING, locked_by/locked_at)
  → success: job RESOLVED, run PUBLISHED, active_retry_job_id = NULL
  → failure but attempts remain: attempt_count + 1, PENDING + next_retry_at (default +3m, +5m)
  → maximum attempts (schedule entry အရေအတွက်, default 3) reached: job + run FAILED_PERMANENT
```

- `NODE_RED_FETCH` job: re-fetch → normalize → workflow → publish။ Publish သာ fail ဖြစ်ပါက job ကို resolve လုပ်ပြီး `KAFKA_PUBLISH` job အသစ်ကို transaction တစ်ခုတည်းဖြင့် ဖန်တီးသည်။
- `KAFKA_PUBLISH` job: သိမ်းထားသော payload ကိုသာ ပြန် publish လုပ်သည် (Node-RED/workflow မ run)။
- Backoff ကို `RETRY_WORKER_BACKOFF_SCHEDULE` (comma-separated Go duration — `s`, `m`, `h`၊ default `1m,3m,5m`) ဖြင့် သတ်မှတ်သည်။ Entry အရေအတွက်သည် `max_attempts` ဖြစ်သည် (ဥပမာ `30s,2m,10m,1h` → 4 attempts)။ Unit မပါ (`1,3,5`)၊ format မှား၊ `0` / အနုတ် / entry အလွတ် ပါလျှင် entry တစ်ခုတည်း မှားရုံဖြင့် **schedule တစ်ခုလုံး** default သို့ ပြန်ပြီး warning log ထွက်သည်။ Process တစ်ခုလျှင် တစ်ကြိမ်သာ ဖတ်သဖြင့် ပြောင်းပါက restart လိုသည်။ `max_attempts` နှင့် ပထမ `next_retry_at` ကို job ဖန်တီးချိန်တွင် row ထဲ သိမ်းသဖြင့် ရှိပြီးသား job များသည် ယခင် `max_attempts` ကို ဆက်သုံးသည် (နောက်အကြိမ် delay များကိုမူ schedule အသစ်ဖြင့် တွက်သည်)။
- `attempt_count` ကို `Reschedule` တွင်သာ +1 တိုးသည်။ နောက်ဆုံး attempt fail ၍ `FAILED_PERMANENT` ဖြစ်သည့်အခါ မတိုးပါ — ထို့ကြောင့် `max_attempts = 3` ဖြင့် အကုန်ကြိုးစားပြီးသော job သည် `attempt_count = 2` ဖြင့် ကျန်သည် (worker attempt 3 ကြိမ် + live path မူလ attempt 1 ကြိမ်)။
- Poll `RETRY_WORKER_POLL_INTERVAL_MS` (30 s)၊ batch `RETRY_WORKER_BATCH_SIZE` (20)၊ worker crash ဖြစ်ပါက `RETRY_WORKER_LOCK_TTL_MS` (5 m) ပြီးနောက် lock ကို ပြန်ယူနိုင်သည်။
- Job row များကို မဖျက်ပါ။

### Retry ကို on/off လုပ်ခြင်း (`RETRY_WORKER_ENABLED`)

`RETRY_WORKER_ENABLED=false` ဆိုလျှင် durable retry **တစ်ခုလုံး** ပိတ်သည်—

```text
Fetch / publish failure (immediate retry ကုန်ပြီးနောက်)
  → retry_jobs row မဖန်တီးပါ
  → run ကို FAILED_PERMANENT သို့ တိုက်ရိုက် transition
      event = cpe_fetch_failed_retry_disabled
            / kafka_publish_failed_retry_disabled
      last_state_reason = မူလ error
  → Retry Worker goroutine ကိုလည်း မစတင်ပါ
```

- Env မရှိ / parse မရလျှင် default **`true`** ဖြစ်သည် (`utils.GetEnvBool` ကဲ့သို့ `false` မဟုတ်)။ Env ထည့်ရန် မေ့ခြင်းကြောင့် retry အလိုလို မပိတ်သွားစေရန်ဖြစ်သည်။
- Env ကို process တစ်ခုလျှင် တစ်ကြိမ်သာ ဖတ်သည် (worker နှင့် service အမြဲ တူညီစေရန်) — ပြောင်းလဲပါက restart လိုသည်။ ပိတ်ထားပါက startup log တွင် `Retry Worker disabled by RETRY_WORKER_ENABLED` warning ထွက်ပြီး fail ဖြစ်သည့် run တိုင်းတွင် `Retry disabled by RETRY_WORKER_ENABLED, marking run FAILED_PERMANENT` warning ထွက်သည်။
- ပိတ်မီက ရှိပြီးသား `PENDING` job များကို မထိပါ — ပြန်ဖွင့်သည်အထိ table ထဲ စောင့်နေမည်။
- Schema migration (`InitRetryJobsSchema`) ကို flag မည်သို့ပင်ဖြစ်စေ အမြဲ run သည်။
- Instance အများ run ပါက worker အားလုံး `retry_jobs` table တစ်ခုတည်းကို share သုံးသည်။ Instance အားလုံးတွင် တူညီစွာ set လုပ်ရမည်။
- Code: `retryworker.Enabled()` (`frontiir/retry_worker/retry_policy.go`)၊ worker gate — `InitializeRemoteResolve()` (`frontiir/services/init_service.go`)၊ enqueue gate — `enqueueCPEStatusFetchRetry` / `enqueueKafkaPublishRetry` / `failWithoutRetry` (`frontiir/services/remote_resolve_service.go`)။

ပိတ်ထားစဉ် fail ဖြစ်သော run ၏ ကျန်ခဲ့မည့် data—

| ဘယ်မှာ fail | `current_state` | Event | `last_state_reason` |
|---|---|---|---|
| Node-RED fetch | `FAILED_PERMANENT` | `cpe_fetch_failed_retry_disabled` | Node-RED error |
| Kafka publish | `FAILED_PERMANENT` | `kafka_publish_failed_retry_disabled` | Kafka error |

- `completed_at` — transition အချိန်ဖြင့် set သည် (`FAILED_PERMANENT` သည် `IsTerminal()` ဖြစ်သည်)။
- `active_retry_job_id` — `NULL` (job မဖန်တီးသဖြင့် link မရှိ)။ `retry_jobs` တွင် row မရှိပါ။
- Kafka fail case တွင် `target_queue`, `is_remote_resolved`, `final_message`, `onu_serial`, `olt_hostname`, `ca1`, `uplink` သည် `WORKFLOW_EVALUATED` တွင် ရေးပြီးသားဖြစ်၍ ကျန်နေမည်။
- Report `status` တွင် `FAILED` ဟု ပြမည်။

Event history ပုံစံ—

```text
Node-RED fail:
  RECEIVED → FETCHING_CPE_STATUS → FAILED_PERMANENT (cpe_fetch_failed_retry_disabled)

Kafka fail:
  RECEIVED → FETCHING_CPE_STATUS → WORKFLOW_EVALUATED → FAILED_PERMANENT (kafka_publish_failed_retry_disabled)
```

Retry ဖွင့်ထားခြင်း (`true`) နှင့် ပိတ်ထားခြင်း (`false`) နှိုင်းယှဉ်ချက်—

| | `true` | `false` |
|---|---|---|
| Fail ပြီးနောက် state | `*_RETRY_SCHEDULED` → `PUBLISHED` / `FAILED_PERMANENT` | `FAILED_PERMANENT` ချက်ချင်း |
| `retry_jobs` row | ဖန်တီးသည် | မဖန်တီးပါ |
| Report `status` | retry စောင့်နေစဉ် `PROCESSING` | `FAILED` |
| `completed_at` | retry ပြီးမှ (terminal ဖြစ်မှ) | ချက်ချင်း |
| Auto recovery | ရှိ (default 3 attempts: +1m, +3m, +5m) | မရှိ — manual ပြန်ပို့ |

ပိတ်ထားစဉ် သတိပြုရန်—

- Node-RED ခဏ down ရုံဖြင့် (immediate retry `NODE_RED_MAX_RETRIES` × `NODE_RED_RETRY_DELAY_MS` ≈ 3 s သာ) ထိုအချိန်အတွင်း ဝင်လာသော ticket အားလုံး `FAILED_PERMANENT` ဖြစ်မည်။ Auto recovery မရှိ — manual ပြန်ပို့ရမည်။
- Kafka publish fail ဖြစ်ပါက downstream (RTUtil) သို့ မရောက်ပါ။ Workflow payload ကို `pipeline_run_events` ၏ `workflow_evaluated` detail တွင် သိမ်းထားသဖြင့် manual replay လုပ်နိုင်သည်။
- `FAILED_PERMANENT` transition ကိုယ်တိုင် DB error ဖြင့် fail ဖြစ်ပါက (`transition()` သည် log-and-continue) run သည် `FETCHING_CPE_STATUS` / `WORKFLOW_EVALUATED` တွင် ကျန်နေမည်။ Retry ဖွင့်ထားစဉ် `ScheduleRetry` fail ဖြစ်သည့်အခါတွင်လည်း ဤ gap တူညီစွာ ရှိသည်။

Retry disabled ကြောင့် fail ဖြစ်သော run များကို ရှာရန်—

```sql
SELECT pr.run_id, pr.ticket_id, e.event, pr.last_state_reason, pr.completed_at
FROM pipeline_runs pr
JOIN pipeline_run_events e ON e.run_id = pr.run_id
WHERE e.event IN ('cpe_fetch_failed_retry_disabled', 'kafka_publish_failed_retry_disabled');
```

RTUtil ဘက်တွင် retry job မရှိပါ။ CPEMS call ၏ in-process retry သာရှိပြီး RT update fail ဖြစ်ပါက `RT_UPDATE_FAILED` ဖြင့် ရပ်သည်။

`retry_jobs` ရှိ field အရေးကြီးများ—

| Field | အဓိပ္ပာယ် |
|---|---|
| `job_type` | `NODE_RED_FETCH` သို့မဟုတ် `KAFKA_PUBLISH` |
| `request_payload` | Fetch job: RT request အပြည့်အစုံ၊ Publish job: Kafka payload |
| `attempt_count` | လုပ်ပြီးသား failed attempts အရေအတွက် |
| `max_attempts` | အများဆုံး retry အကြိမ် (`RETRY_WORKER_BACKOFF_SCHEDULE` entry အရေအတွက်, default 3) |
| `next_retry_at` | worker ပြန်ယူလုပ်ရမည့်အချိန် |
| `locked_by`, `locked_at` | worker နှစ်ခုတစ်ပြိုင်တည်း job တစ်ခုကို မလုပ်စေရန် lock |
| `status` | `PENDING`, `RETRYING`, `RESOLVED`, `FAILED_PERMANENT` |

## `pipeline_runs` Column များ — ဘယ် service က ရေးသလဲ

| Column group | Column | ရေးသော service / အဆင့် |
|---|---|---|
| Identity | `run_id`, `ticket_id`, `ticket_no`, `cpe_id`, `local_service_id` | rt_web_ui — row create (ရှိလျှင်) → noc_automation — RECEIVED (attach / create) |
| Ticket context | `service_area`, `township`, `ticket_created_at`, `ticket_problem`, `before_queue`, `rt_request_snapshot` | rt_web_ui — row create (ရှိလျှင်) → noc_automation — RECEIVED (attach / create) |
| Network context | `onu_serial`, `olt_hostname`, `ca1`, `uplink` | noc_automation — WORKFLOW_EVALUATED |
| Decision | `target_queue`, `is_remote_resolved`, `final_message` | noc_automation — WORKFLOW_EVALUATED |
| State | `current_state`, `last_state_reason`, `completed_at` | သုံးခုလုံး — transition တိုင်း |
| Retry | `active_retry_job_id` | noc_automation — retry schedule / complete |
| BCS | `is_bcs_success`, `bcs_status_message` | rtutil — BCS_UPDATING |
| Final ticket | `ticket_status` | rt_web_ui / noc_automation (request status) → rtutil (COMPLETED တွင် overwrite) |
| မရေးရသေး | `tags`, `before_bcs_channel`, `target_bcs_channel`, `ref_bcs_process_id` | — (BCS channel/process ID ကို CPEMS response မှ ရလာမှ ရေးရန်) |

## `pipeline_runs` Write အသေးစိတ် — State / Event အလိုက်

ဤ section သည် rtdatacore repository code (working tree — rt_web_ui state များ၊ `AppendEvent`, `AttachSubmittedRun` ပါ) နှင့် ၎င်းကို ခေါ်သော rt_web_ui / noc_automation / RTUtil code ကို အခြေခံသည်။

### rtdatacore ၏ write function များ

| Function | လုပ်ဆောင်ချက် | File |
|---|---|---|
| `PipelineRunRepository.Create` | Row အသစ် **INSERT** (event row မထည့်) | `pkg/driven/rtdb/pipeline_run_repository.go` |
| `PipelineRunRepository.TransitionState` | State ပြောင်း + event row ၁ ကြောင်း (transaction တစ်ခု) | `pipeline_run_repository.go` → `transitionPipelineRun` |
| `PipelineRetryRepository.ScheduleRetry` | `retry_jobs` insert (+ prior job ကို RESOLVED) + transition + `active_retry_job_id = job.id` (transaction တစ်ခု) | `pkg/driven/rtdb/pipeline_retry_repository.go` |
| `PipelineRetryRepository.CompleteRetry` | Job ကို RESOLVED / FAILED_PERMANENT + transition + `active_retry_job_id = NULL` (transaction တစ်ခု) | `pipeline_retry_repository.go` |
| `PipelineRunRepository.LinkRetryJob` | `active_retry_job_id` ကိုသာ update | **မည်သည့်နေရာကမှ မခေါ်ပါ** (wrapper သာရှိ) |
| `PipelineRunRepository.AppendEvent` | State မပြောင်းဘဲ event row ၁ ကြောင်း (`from_state` = `to_state` = လက်ရှိ state) | `pipeline_run_repository.go` — rt_web_ui `api_accepted` / API ဆက်ယူပြီးမှ ဖြစ်သော failure |
| `PipelineRunRepository.AttachSubmittedRun` | `SUBMITTED_TO_API` run ကို lock လုပ်ပြီး ticket column update + `RECEIVED` + event (transaction တစ်ခု)။ Row မရှိ → `ErrRecordNotFound`၊ အခြား state → `ErrRunNotClaimable` | `pipeline_run_repository.go` — noc_automation `CreatePipelineRun` |

### Transition တိုင်း၏ အလိုအလျောက် rule (`transitionPipelineRun`)

| Column | Rule |
|---|---|
| `current_state` | `ToState` ကို အမြဲရေးသည် |
| `last_state_reason` | Reason ပါလျှင် overwrite။ Reason မပါဘဲ terminal / `PUBLISHED` ဖြစ်လျှင် `NULL`။ အခြားအခြေအနေတွင် မထိ |
| `completed_at` | Terminal state ရောက်လျှင် `now` (အောက်ပါ အသေးစိတ်ကြည့်ပါ) |
| `updated_at` | GORM က update တိုင်း အလိုအလျောက် set |
| Optional field များ | `TransitionInput` တွင် `nil` မဟုတ်သည်ကိုသာ ရေးသည် — `target_queue`, `is_remote_resolved`, `final_message`, `onu_serial`, `olt_hostname`, `ca1`, `uplink`, `is_bcs_success`, `bcs_status_message`, `ticket_status` |
| `pipeline_run_events` | Row ၁ ကြောင်း INSERT — `run_id`, `from_state` (ယခင် state), `to_state`, `event`, `component`, `detail`, `occurred_at` |

### INSERT — `""` (rt_web_ui, `repoTracker.Open`)

API ၏ `RECEIVED` INSERT နှင့် column တူတူ ဖြည့်သည် (`ticket_status` = payload `status`, `before_queue` = payload `queue`, `rt_request_snapshot` = API သို့ ပို့မည့် body)။ ကွာခြားချက်မှာ `current_state = ""` ဖြစ်ပြီး event row မထည့်ခြင်းဖြစ်သည်။ ပထမ transition (`NOT_ELIGIBLE` / `SUBMITTED_TO_API`) ၏ `from_state` သည် `""` ဖြစ်သည်။

### UPDATE — `SUBMITTED_TO_API → RECEIVED` (noc_automation, `AttachSubmittedRun`)

Row lock ပြီး `ticket_id`, `ticket_no`, `service_area`, `township`, `ticket_created_at`, `ticket_problem`, `ticket_status`, `cpe_id`, `local_service_id`, `before_queue`, `rt_request_snapshot` ကို API ရရှိသော request ဖြင့် overwrite၊ `current_state = RECEIVED`၊ `last_state_reason = NULL`၊ event `api_received` (component `remote_resolve_service`)။

### INSERT — `RECEIVED` (noc_automation, `helpers.CreatePipelineRun` — rt_web_ui မပါသော caller)

| Column | တန်ဖိုး၏ ရင်းမြစ် |
|---|---|
| `run_id` | request `run_id` |
| `ticket_id` | request `id` (string → uint64) |
| `ticket_no`, `service_area`, `township`, `ticket_problem` | `custom_fields` (မပါလျှင် `""`) |
| `cpe_id`, `local_service_id` | `custom_fields` |
| `ticket_created_at` | request `created` (parse မရလျှင် `NULL`) |
| `ticket_status` | request `status` |
| `before_queue` | request `queue` (trim) |
| `rt_request_snapshot` | request JSON အပြည့်အစုံ |
| `current_state` | `RECEIVED` |
| `created_at`, `updated_at` | GORM အလိုအလျောက် |

### State / Event အလိုက် UPDATE

ဇယားတွင် အလိုအလျောက် rule အပြင် **ထပ်ရေးသော field** ကိုသာ ဖော်ပြသည်။ ⏹ = terminal (`completed_at` set)၊ 🔁 = `ScheduleRetry`၊ ✅ = `CompleteRetry`။

**rt_web_ui — `rt_web_ui`**

| To state | Event | ထပ်ရေးသော field | `last_state_reason` | Event `detail` |
|---|---|---|---|---|
| `NOT_ELIGIBLE` ⏹ | gate event ၅ မျိုး | `completed_at` | gate reason | `{"gate": …}` |
| `SUBMITTED_TO_API` | `payload_submitted` | — | မထိ | `{"url", "bytes"}` |
| (မပြောင်း — `AppendEvent`) | `api_accepted` | — | မထိ | `{"http_status", "message"}` |
| `API_REJECTED` ⏹ | `api_rejected` | `completed_at` | "RT External API returned …" | `{"http_status", "message"}` |
| `API_UNREACHABLE` ⏹ | `api_unreachable` | `completed_at` | "RT External API unreachable: …" | `{"error"}` |
| (မပြောင်း — `AppendEvent`) | `api_rejected` / `api_unreachable` | — | မထိ | API က run ကို ဆက်ယူပြီးဖြစ်လျှင် |

**noc_automation — `remote_resolve_service` (live path)**

| To state | Event | ထပ်ရေးသော field | `last_state_reason` | Event `detail` |
|---|---|---|---|---|
| `RECEIVED` | `api_received` | ticket column ၁၁ ခု (`AttachSubmittedRun`) | `NULL` | — |
| `FETCHING_CPE_STATUS` | `node_red_fetch_started` | — | မထိ | — |
| `CPE_NOT_FOUND` ⏹ | `cpe_not_found` | `completed_at` | error | — |
| `FAILED_PERMANENT` ⏹ | `normalize_failed` | `completed_at` | error | — |
| `WORKFLOW_EVALUATED` | `workflow_evaluated` | `target_queue`, `is_remote_resolved`, `final_message`, `onu_serial`, `olt_hostname`, `ca1`, `uplink` | မထိ | Node-RED response + workflow decision |
| `PUBLISHED` | `kafka_published` | — | `NULL` | — |
| `CPE_FETCH_RETRY_SCHEDULED` 🔁 | `cpe_fetch_retry_scheduled` | `active_retry_job_id` = job id | error | attempt error detail |
| `KAFKA_PUBLISH_RETRY_SCHEDULED` 🔁 | `kafka_publish_retry_scheduled` | `active_retry_job_id` = job id | error | attempt error detail |
| `FAILED_PERMANENT` ⏹ | `cpe_fetch_failed_retry_disabled` | `completed_at` | error | — |
| `FAILED_PERMANENT` ⏹ | `kafka_publish_failed_retry_disabled` | `completed_at` | error | — |

**noc_automation — `retry_worker`**

| To state | Event | ထပ်ရေးသော field | `last_state_reason` | Event `detail` |
|---|---|---|---|---|
| `*_RETRY_SCHEDULED` (မပြောင်း) | `retry_claimed` | — | မထိ | — |
| `*_RETRY_SCHEDULED` (မပြောင်း) | `retry_attempt_failed` | — (`retry_jobs` ကို `Reschedule` က သီးခြား update) | error | — |
| `WORKFLOW_EVALUATED` | `workflow_evaluated` | live path နှင့်တူသော field ၇ ခု | မထိ | Node-RED response + workflow decision |
| `KAFKA_PUBLISH_RETRY_SCHEDULED` 🔁 | `kafka_publish_retry_scheduled` | `active_retry_job_id` = **job အသစ်** id (အဟောင်းကို RESOLVED) | error | attempt error detail |
| `PUBLISHED` ✅ | `kafka_published` | `active_retry_job_id` = `NULL` | `NULL` | — |
| `CPE_NOT_FOUND` ⏹ ✅ | `cpe_not_found_on_retry` | `completed_at`, `active_retry_job_id` = `NULL` | error | attempt error detail |
| `FAILED_PERMANENT` ⏹ ✅ | `normalize_failed_on_retry` | `completed_at`, `active_retry_job_id` = `NULL` | error | attempt error detail |
| `FAILED_PERMANENT` ⏹ ✅ | `retry_attempts_exhausted` | `completed_at`, `active_retry_job_id` = `NULL` | error | attempt error detail |
| `FAILED_PERMANENT` ⏹ ✅ | `unknown_job_type` | `completed_at`, `active_retry_job_id` = `NULL` | "unknown retry job type" | — |

**RTUtil — `rtutil_consumer`**

| To state | Event | ထပ်ရေးသော field | `last_state_reason` | Event `detail` |
|---|---|---|---|---|
| `CONSUMED` | `kafka_consumed` | — | မထိ | Kafka `topic`, `partition`, `offset` |
| `CONSUME_VALIDATION_FAILED` ⏹ | `consume_validation_failed` | `completed_at` | error | — |
| `RT_UPDATE_FAILED` ⏹ | `rt_ticket_fetch_failed` | `completed_at` | error | — |
| `SKIPPED` ⏹ | `ticket_skipped` | `completed_at` | "Ticket status = …" | — |
| `BCS_UPDATING` | `bcs_update_started` | — | မထိ | — |
| `BCS_UPDATING` (မပြောင်း) | `bcs_update_succeeded` | `is_bcs_success = 1` | မထိ | — |
| `BCS_UPDATING` (မပြောင်း) | `bcs_update_failed` | `is_bcs_success = 0`, `bcs_status_message` = error | မထိ | — |
| `RT_UPDATING` | `rt_update_started` | — | မထိ | — |
| `RT_UPDATE_FAILED` ⏹ | `rt_update_failed` | `completed_at` | error / "RT returned status …" | — |
| `COMPLETED` ⏹ | `rt_updated` | `completed_at`, `ticket_status` (သတ်မှတ်ခဲ့သော status၊ မရှိလျှင် fetch လုပ်ခဲ့သော status) | comment fail လျှင် "ticket comment failed: …"၊ မဟုတ်လျှင် `NULL` | — |

`COMPLETED` သည် RT REST2 PUT 200 ကိုသာ ဆိုလိုသည်။ BCS fail ဖြစ်လည်း `COMPLETED` ဖြစ်သည် (`is_bcs_success = 0`)။

### Column တစ်ခုချင်းစီ ရေးသည့်နေရာ

| Column | INSERT (`RECEIVED`) | UPDATE နေရာ |
|---|---|---|
| `run_id`, `ticket_id`, `ticket_no`, `cpe_id`, `local_service_id`, `service_area`, `township`, `ticket_problem`, `ticket_created_at`, `before_queue`, `rt_request_snapshot` | ✅ (rt_web_ui သို့မဟုတ် API) | `AttachSubmittedRun` (`RECEIVED`) တွင် overwrite (`run_id` မှလွဲ) |
| `ticket_status` | ✅ request status | `COMPLETED` (RTUtil) တွင် overwrite |
| `current_state` | ✅ `""` (rt_web_ui) / `RECEIVED` (API) | Transition တိုင်း |
| `last_state_reason` | — | Transition rule အတိုင်း |
| `completed_at` | — | Terminal state ရောက်ချိန် |
| `target_queue`, `is_remote_resolved`, `final_message`, `onu_serial`, `olt_hostname`, `ca1`, `uplink` | — | `WORKFLOW_EVALUATED` (live / retry) |
| `active_retry_job_id` | — | `ScheduleRetry` တွင် set၊ `CompleteRetry` တွင် `NULL` |
| `is_bcs_success`, `bcs_status_message` | — | `bcs_update_succeeded` / `bcs_update_failed` |
| `tags`, `ref_bcs_process_id`, `before_bcs_channel`, `target_bcs_channel` | — | **မည်သည့်နေရာကမှ မရေးပါ** |

### `completed_at` အသေးစိတ်

```go
now := time.Now()
if input.ToState.IsTerminal() {
    updates["completed_at"] = now
}
...
event := entities.PipelineRunEvent{ ..., OccurredAt: now }
```

- **Terminal state** (`IsTerminal()`): `NOT_ELIGIBLE`, `API_REJECTED`, `API_UNREACHABLE`, `ALREADY_PROCESSED`, `CPE_NOT_FOUND`, `FAILED_PERMANENT`, `CONSUME_VALIDATION_FAILED`, `SKIPPED`, `RT_UPDATE_FAILED`, `COMPLETED`။ နောက်ထပ် state မပြောင်းတော့သော နောက်ဆုံး state ဖြစ်ပြီး အောင်မြင်မှု / fail နှစ်မျိုးလုံး ပါဝင်သည်။ `PUBLISHED` သည် RTUtil ဆက်လုပ်ရန် ကျန်သဖြင့် terminal မဟုတ်ပါ။
- **`now`** သည် DB ၏ `NOW()` မဟုတ်ပါ — transition လုပ်သော **Go process ၏ `time.Now()`** ဖြစ်သည် (`NOT_ELIGIBLE` / `API_*` = rt_web_ui ကို run သော machine၊ `COMPLETED` = RTUtil server၊ `CPE_NOT_FOUND` / `FAILED_PERMANENT` = noc_automation server)။
- Terminal event row ၏ `pipeline_run_events.occurred_at` နှင့် တန်ဖိုး အတူတူဖြစ်သည် (`now` variable တစ်ခုတည်း)။
- `Create` (`""` / `RECEIVED`) တွင် မပါသဖြင့် terminal မရောက်မချင်း `NULL` ဖြစ်သည်။

သတိပြုရန်—

- **Timezone:** DSN တွင် `loc=Local` သုံးသဖြင့် Go process ၏ local timezone ဖြင့် timezone မပါဘဲ သိမ်းသည်။ rt_web_ui / noc_automation (`created_at`) နှင့် RTUtil (`completed_at`) ကို timezone မတူသော server / container တွင် run ပါက duration မှားမည် — အားလုံး၏ `TZ` တူရမည်။
- **Terminal မှ ထပ်ပြောင်းခြင်း:** rtdatacore က မတားပါ။ Terminal → terminal ဆိုလျှင် overwrite၊ terminal → non-terminal ဆိုလျှင် `NULL` **ပြန်မလုပ်ပါ**။ ပုံမှန် flow တွင် RTUtil ၏ redelivery guard (`FinishedPipelineRunState`) ကြောင့် မဖြစ်ပါ။
- **Transition fail:** log-and-continue ဖြစ်သဖြင့် DB error ဖြစ်ပါက ticket ပြီးသွားသော်လည်း `completed_at = NULL` ဖြင့် အလယ် state တွင် ကျန်နိုင်သည်။
- **`completed_at` ≠ အောင်မြင်မှု:** Fail / skip state ၉ ခုတွင်လည်း set သည်။ အောင်မြင်မှုကို `current_state = 'COMPLETED'` ဖြင့် စစ်ပါ။

```sql
-- ပြီးဆုံးရန် ကြာမြင့်ချိန်
SELECT run_id, current_state, TIMESTAMPDIFF(SECOND, created_at, completed_at) AS duration_sec
FROM pipeline_runs WHERE completed_at IS NOT NULL;

-- Terminal မဟုတ်ဘဲ completed_at ရှိနေသော (data မကိုက်) run များ
SELECT run_id, current_state, completed_at FROM pipeline_runs
WHERE completed_at IS NOT NULL
  AND current_state NOT IN ('NOT_ELIGIBLE','API_REJECTED','API_UNREACHABLE','ALREADY_PROCESSED',
                            'CPE_NOT_FOUND','FAILED_PERMANENT','CONSUME_VALIDATION_FAILED',
                            'SKIPPED','RT_UPDATE_FAILED','COMPLETED');
```

### အခြား သတိပြုရန်

- `NORMALIZE_FAILED` state ကို rtdatacore မှ ဖယ်ရှားပြီး (commit `e6d1dd4`) normalize error ကို `FAILED_PERMANENT` ဖြင့် ပိတ်သည်။ DB တွင် တွေ့ပါက ယခင် data ဖြစ်ပြီး `IsTerminal()` က terminal ဟု မသိတော့ပါ။
- `TRIGGERED` state (rt_web_ui ၏ ယခင် design) ကို ဖယ်ရှားပြီး run ကို `""` ဖြင့် ဖွင့်သည်။ ယခင် test run များ၏ `TRIGGERED` row / event ကျန်နိုင်သည်။
- `ALREADY_PROCESSED` (final.html step 9–10 Redis 24h) ကို terminal state အဖြစ် ကြိုသတ်မှတ်ထားသော်လည်း writer မရှိသေးပါ။
- `completeJobOnly` (orphan job) သည် `retry_jobs` ကိုသာ update လုပ်ပြီး `pipeline_runs` ကို မထိပါ။
- RTUtil က terminal ဖြစ်ပြီးသား run ၏ redelivered message ကို skip လုပ်သည့်အခါ ဘာမှ မရေးပါ။

## Report အတွက် simple `status`

`pipeline_runs.current_state` သည် technical state အပြည့်အစုံဖြစ်သည်။ Report အတွက် simple status လိုအပ်လျှင် query/view တွင် derive လုပ်ရန် အကြံပြုသည်။

```sql
CASE
    WHEN current_state IN ('', 'SUBMITTED_TO_API', 'RECEIVED') THEN 'PENDING'
    WHEN current_state = 'NOT_ELIGIBLE' THEN 'NOT_ELIGIBLE'
    WHEN current_state = 'COMPLETED' THEN 'COMPLETED'
    WHEN current_state = 'SKIPPED' THEN 'SKIPPED'
    WHEN current_state IN ('API_REJECTED', 'API_UNREACHABLE', 'CPE_NOT_FOUND', 'FAILED_PERMANENT',
                           'CONSUME_VALIDATION_FAILED', 'RT_UPDATE_FAILED')
        THEN 'FAILED'
    ELSE 'PROCESSING'   -- PUBLISHED, CONSUMED, BCS_UPDATING, RT_UPDATING, *_RETRY_SCHEDULED ...
END AS status
```

`PUBLISHED` ကို `COMPLETED` ဟု မတွက်ရပါ — RT ticket update မဖြစ်သေးပါ။ Physical `status` column ထပ်သိမ်းလျှင် `TransitionState()` တစ်နေရာတည်းမှာ `current_state` နှင့်အတူ update လုပ်ရမည်။ မဟုတ်လျှင် drift ဖြစ်နိုင်သည်။

### Daily report (`pipeline_report`)

[`pipeline_report`](../../pipeline_report/README.md) (standalone Go program, DB read-only) သည် ယနေ့ run များကို live ပြသည်။ Spec / SQL — [mockups/pipeline-daily-report-v2.md](mockups/pipeline-daily-report-v2.md)။

| Card | State |
|---|---|
| Received | ယနေ့ `created_at` ရှိသော run အားလုံး |
| In progress | `completed_at IS NULL` (Retrying = `*_RETRY_SCHEDULED`၊ Stuck = retry မဟုတ်ဘဲ ၁၀ မိနစ် state မပြောင်း) |
| Success | `COMPLETED` — Remote resolved (`is_remote_resolved = 1`) · **Transferred** · **Kept in NOC** (`before_queue = target_queue = NOC`) |
| Not eligible | `NOT_ELIGIBLE`, `ALREADY_PROCESSED` |
| **Manual check** | `API_REJECTED`, `API_UNREACHABLE`, `CONSUME_VALIDATION_FAILED`, `FAILED_PERMANENT`, `CPE_NOT_FOUND`, `RT_UPDATE_FAILED` (validation failed + API error + failed ပေါင်းလဒ်) |

Kept in NOC သည် workflow က queue မပြောင်းဘဲ comment သာ ထည့်သော run များ ဖြစ်သည် (OLT down, Reseller, ChurnAI, ONU flapping စသည် — workflow ရလဒ် ၁၄ ခု)။ Queue ကို နာမည်ဖြင့် နှိုင်းယှဉ်သဖြင့် RT queue နာမည် ပြောင်းပါက `REPORT_NOC_QUEUE` ကို လိုက်ပြင်ရမည်။

## `run_id` အလွတ်ဖြစ်ခြင်း

- **Request တွင် `run_id` မပါ / အလွတ် / UUID မဟုတ်:** API validation က HTTP 400 ဖြင့် ငြင်းပြီး `pipeline_runs` row မဖန်တီးပါ။ ထို့ကြောင့် API မှ ဝင်လာသော run တိုင်းတွင် valid `run_id` ရှိသည်။ (Validation မထည့်မီက `run_id = ''` row ဖန်တီးမိပြီး tracking မရ၊ retry job ငြင်းခံရ၊ နောက်ထပ် request များ unique index ကြောင့် HTTP 500 ဖြစ်ခဲ့သည်။ DB တွင် `run_id = ''` row ကျန်နေပါက ဖျက်ပစ်ရန်။)
- **`retry_jobs.run_id` အလွတ်:** Job ကို retry worker က လုပ်နိုင်သော်လည်း pipeline run နှင့် event history update မလုပ်နိုင်ပါ (legacy job, manual SQL insert စသည်)။ Job builder သည် `run_id` အလွတ်ဖြင့် job အသစ် ဖန်တီးခြင်းကို ငြင်းသည်။
- **Kafka message တွင် `run_id` မပါ (legacy):** RTUtil က ticket ကို process ဆက်လုပ်သော်လည်း DB ထဲ ဘာမှမရေးပါ၊ redelivery guard လည်း အလုပ်မလုပ်ပါ။
- **RTUtil CSV command များ** (`update-remote-resolved`, `queue-transfer`) သည် `run_id` မရှိသဖြင့် pipeline tracking မရေးပါ။ ၎င်းတို့ ရလဒ်ကို CSV dump နှင့် email report ဖြင့်သာ ကြည့်နိုင်သည်။

Normal production flow တွင် `run_id` ကို မဖြစ်မနေထည့်သင့်သည်။

## Operational Notes

- `pipeline_runs` ကို dashboard list အတွက်သုံးပါ။ Run တစ်ခု၏ history အပြည့်အစုံလိုလျှင် `pipeline_run_events` ကို `run_id` ဖြင့် `occurred_at` အစဉ်လိုက် query လုပ်ပါ။
- Pending retry များကို `retry_jobs` မှ `status IN ('PENDING', 'RETRYING')` နှင့် စစ်ပါ။
- `PENDING` job များ `next_retry_at` ကျော်ပြီး ကြာရှည်စုနေပါက worker မ run ခြင်း (သို့) `RETRY_WORKER_ENABLED=false` မတိုင်မီက ကျန်ခဲ့သော job ဖြစ်နိုင်သည်။ `status = 'PENDING' AND next_retry_at < NOW() - INTERVAL 10 MINUTE` ဖြင့် စောင့်ကြည့်ပါ။
- Gate fail ဖြစ်သော ticket များကို အကြောင်းရင်းအလိုက် — `SELECT e.event, COUNT(*) FROM pipeline_run_events e WHERE e.to_state = 'NOT_ELIGIBLE' GROUP BY e.event`။
- `""` သို့မဟုတ် `SUBMITTED_TO_API` တွင် ကြာရှည်ရပ်နေသော run များ = rt_web_ui process သည် gate စစ်နေစဉ် / API အဖြေ မရမီ ရပ်သွားခြင်း (သို့) API က request ကို လက်ခံပြီး ဆက်မယူခင် crash ဖြစ်ခြင်း။ `current_state IN ('', 'SUBMITTED_TO_API') AND updated_at < NOW() - INTERVAL 10 MINUTE` ဖြင့် စောင့်ကြည့်ပါ။
- `API_REJECTED` ဖြစ်သော run ၏ HTTP code ကို `pipeline_run_events.detail` (`http_status`) တွင် ကြည့်ပါ — 400 = payload မမှန်၊ 409 = `run_id` ထပ်နေ၊ 500 = API ၏ DB error။
- `PUBLISHED` တွင် ကြာရှည်ရပ်နေသော run များ = RTUtil consumer မ run နေခြင်း၊ topic မကိုက်ခြင်း သို့မဟုတ် message parse fail ဖြစ်ခြင်း။ `PUBLISHED AND updated_at < NOW() - INTERVAL 10 MINUTE` ဖြင့် စောင့်ကြည့်ပါ။
- `CONSUMED` / `BCS_UPDATING` / `RT_UPDATING` တွင် ရပ်နေသော run များ = RTUtil process ကြားတွင် ရပ်သွားခြင်း။ Message redeliver ဖြစ်ပါက ဆက်လုပ်မည်ဖြစ်သော်လည်း auto commit ဖြစ်ပြီးသားဆိုပါက redeliver မဖြစ်ပါ။
- `is_bcs_success = 0` ဖြစ်ပြီး `COMPLETED` ဖြစ်နေသော run များ = RT update အောင်မြင်သော်လည်း CPEMS မသိရသေးသော ticket များ။ သီးခြား follow up လိုသည်။
- `retry_jobs.last_error` သည် `RESOLVED` ဖြစ်ပြီးနောက် ရှင်းသွားသည်။ `pipeline_runs.last_state_reason` သည် terminal/`PUBLISHED` ရောက်လျှင် ရှင်းသည်။
- RTUtil log တွင် `Failed to record pipeline run transition ... record not found` တွေ့ပါက `noc_automation` နှင့် RTUtil သည် database မတူခြင်း (သို့မဟုတ် run row မရှိခြင်း) ဖြစ်သည်။ နှစ်ဘက်လုံး DB တူရမည်။

## Known Gaps

| Gap | Impact |
|---|---|
| RTUtil Kafka consumer — DLQ မရှိ၊ handler retry မရှိ၊ auto offset commit | Fail ဖြစ်သော message ပျောက်နိုင်သည် (at-most-once) |
| Parse မရသော Kafka message | Run သည် `PUBLISHED` တွင် အမြဲကျန်နေသည် |
| CPEMS ကို RT update မတိုင်မီ ခေါ်သည် | RT update fail ဖြစ်ပါက CPEMS က remote resolve ဖြစ်ပြီဟု ထင်နေနိုင်သည် |
| RTUtil config တွင် `cpems_api.base_url` အလွတ် | BCS call အမြဲ fail (`unsupported protocol scheme`) |
| Payload key `resolution_fiber_onu_signal_dbm`, `on_site_resolution` သည် RTUtil mapping key (`rx_power`, `on-site_resolution`) နှင့် မကိုက် | "Resolved Fiber ONU Signal dBm" ကို ရည်ရွယ်သလို မရေးနိုင် |
| New ticket ကို comment-only decision တွင်ပါ `In_Progress` သို့ ပြောင်းသည် | Business rule အတည်ပြုရန် |
| Duplicate guard သည် `run_id` အလိုက်သာ (ticket အလိုက် မဟုတ်) | `run_id` တူ = HTTP 409၊ ticket တူ `run_id` မတူ = run ၂ ခု — ticket အလိုက် 24h guard (`ALREADY_PROCESSED`) မရှိသေး |
| `.env.example` ၏ `REMOTE_RESOLVE_CUSTOM_FIELDS` (2 field) နှင့် environment `.env` (7 field) မကိုက် | Environment အလိုက် validation မတူ |
| Kafka topic အမည် `rt-sit-remote-reolved` (typo) | Environment တိုင်းတွင် ထိုအမည်အတိအကျ ဖန်တီးရမည် |
| `RETRY_WORKER_ENABLED=false` တွင် immediate retry (~3 s) သာ ရှိ | Node-RED / Kafka ခဏ down ရုံဖြင့် ticket များ `FAILED_PERMANENT` ဖြစ်ပြီး manual ပြန်ပို့ရမည် |
| `ScheduleRetry` / `FAILED_PERMANENT` transition ကိုယ်တိုင် DB error ဖြင့် fail ဖြစ်ခြင်း | Run သည် `FETCHING_CPE_STATUS` / `WORKFLOW_EVALUATED` တွင် အမြဲကျန်နေသည် (log-and-continue) |
| Node-RED mock mode ဖယ်ရှားပြီး (`NODE_RED_USE_MOCK`, `cpe_status_mock.json` မရှိတော့) | Local / test environment တွင်လည်း reachable Node-RED လိုသည် (Mockoon — `Local` :3001၊ criteria :3002)။ Criteria mock သည် 200 response သာ ပြန်သဖြင့် 404/500 simulate မလုပ်နိုင်သေး |
| `category_type_workflow` ၏ ticket field များ (`program`, `ticket_problem`, `tags`, `root_cause`, `plan_start_date`) သည် `REMOTE_RESOLVE_CUSTOM_FIELDS` တွင် မပါလျှင် normalized payload ထဲ မရောက် | ထို step များ ဘယ်တော့မှ match မဖြစ် (local `.env` = `cpe_id,local_service_id`)။ ထည့်လျှင် required field ဖြစ်သွားပြီး မပါသော request = HTTP 400 |
| Workflow `is_low_dbm` step သည် `IS_FALSE` | Signal ကောင်းသော (`is_low_dbm: false`) ONU ကို "ONU has been offline for over 12 hours" ဖြင့် LANOps သို့ ရွှေ့ — ရည်ရွယ်ချက် ဟုတ်မဟုတ် အတည်ပြုရန် |
| rt_web_ui ၏ rtdatacore ပြောင်းလဲမှု (state ၅ ခု၊ `AppendEvent`, `AttachSubmittedRun`, `ErrRunNotClaimable`) သည် commit / tag မလုပ်ရသေး — tag `v1.1.14` တွင် မပါ | `GOWORK=off` (production) build fail — rtdatacore tag အသစ် (v1.1.15) ထုတ်ပြီး noc_automation နှင့် rt_web_ui ၏ `go.mod` ကို bump ရမည် (rt_web_ui သည် v1.1.13 တွင် ရှိနေဆဲ) |
| rt_web_ui တွင် automated test မရှိ (`flow_test.go` ဖယ်ရှားထား) | Gate / API response → state mapping ကို regression test မရှိ |
| rt_web_ui သည် CLI သာ — RT ၏ NOC queue-change event ကို အလိုအလျောက် မဖမ်း | Ticket တစ်ခုချင်း JSON ဖြင့် run ရသည်။ Watcher service (Phase 2) လိုသည် |
| rt_web_ui က `""` row create ပြီးနောက် process ရပ်သွားပါက | Run သည် `current_state = ""` (event မရှိ) ဖြင့် ကျန်နေမည် |
| OPI field (`opi_site_code`) နှင့် eligible Service Type list သည် default value သာ — RT DB တွင် "OPI Site" / "OPI Site Code" CF ၄ ခု ရှိပြီး Service Type သည် freeform ("FR-SLA", "MNet Plus", "MN Biz") | Business ဘက်မှ အတည်ပြုရန် |
