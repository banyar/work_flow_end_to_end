# Report criteria samples

Pipeline report ([pipeline_report](../../pipeline_report/README.md)) ၏ card / bucket တစ်ခုချင်းစီကို ဖြစ်စေရန် ပို့ရမည့် ticket JSON နှင့် Node-RED CPE status JSON များ။

| Folder / file | အသုံး |
|---|---|
| `tickets/<id>.json` | rt_web_ui က ပို့မည့် ticket ([ticket_valid.json](../ticket_valid.json) ကို အခြေခံ၍ ပြင်ထား) |
| `cpe_status/<id>.json` | Node-RED `GET /api/v1/cpe/<cpe_id>` ၏ response (ရည်ညွှန်းရန်) |
| `mockoon-criteria.json` | Mockoon environment (port **3002**) — Node-RED: `cpe_id` (`CRIT<number>`) အလိုက် `cpe_status/` ထဲက response ကို ပြန်ပေးသည်၊ အခြား CPE id များအတွက် "all good" response (default)။ BCS (Peppermint): start နှင့် job status route များ ([BCS](#bcs-success--fail)) |

## ဘယ်လို ဆုံးဖြတ်သလဲ

1. **rt_web_ui gate များ** — ticket JSON ကိုသာ ကြည့်သည်။ Fail ဖြစ်ပါက `NOT_ELIGIBLE` (Not eligible card)။
2. **noc_automation workflow** ([remote_resolve_workflow_engine.json](../../../NocAutomationCodeMerge/noc_automation/remote_resolve_workflow_engine.json)) — Node-RED CPE status ကို `network_good_workflow` → `category_type_workflow` → `final_action` အစီအစဉ်ဖြင့် စစ်ပြီး **ပထမဆုံး ကိုက်သော step** ၏ queue ကို ယူသည်။ ထို့ကြောင့် criteria တစ်ခုချင်းစီတွင် field **တစ်ခု (သို့ နှစ်ခု)** သာ ပြောင်းထားပြီး ကျန် field များသည် "all good" အတိုင်း ဖြစ်သည်။
3. Category rule များသည် ticket ၏ custom field (`program`, `ticket_problem`, `tags`, `root_cause`, `plan_start_date`) ကို တိုက်ရိုက် ဖတ်သည် (case-sensitive)။ ဤ folder ၏ ticket များ (`root_cause: "2.4G Interference"`, `ticket_problem: "Unstable Connection"`, `tags: ["test-Tag"]`, `plan_start_date: "2025-07-14"`, `program` မပါ) သည် မည်သည့် category rule နှင့်မှ မကိုက်ပါ။ Field မပါလျှင် rule သည် ကိုက်ခြင်း မရှိ (SKIP)။
4. Node-RED contract (noc_automation `10e7f90`): `GET {NODE_RED_BASE_URL}/api/v1/cpe/{cpe_id}`၊ `cpe_id` သည် `^[A-Za-z0-9]+$` ဖြစ်ရမည် (`-` ပါလျှင် HTTP မခေါ်ဘဲ `FAILED_PERMANENT` · `cpe_invalid_request`)၊ `onu.24h_onu_usage_gb` သည် number (string မဟုတ်)။ ထို့ကြောင့် mock id များသည် `CRIT<number>` ဖြစ်သည် (ယခင် `CRIT-<number>`)။

## Criteria list

### Not eligible (ticket JSON သာ ပြင်)

| File | Criteria | Gate event | ပြင်ထားသော field |
|---|---|---|---|
| [`01_not_eligible_queue.json`](tickets/01_not_eligible_queue.json) | Ticket queue NOC မဟုတ် | `not_in_noc_queue` | `queue: "Customer Support"` |
| [`02_not_eligible_cpe_missing.json`](tickets/02_not_eligible_cpe_missing.json) | cpe_id ဗလာ | `cpe_or_local_service_id_missing` | `custom_fields.cpe_id: ""` |
| [`03_not_eligible_opi.json`](tickets/03_not_eligible_opi.json) | OPI customer | `opi_customer_excluded` | `custom_fields.opi_site_code: "HMHY"` |
| [`04_not_eligible_service_type.json`](tickets/04_not_eligible_service_type.json) | Service type eligible မဟုတ် | `service_type_not_eligible` | `custom_fields.service_type: "Other"` |
| [`05_not_eligible_ticket_status.json`](tickets/05_not_eligible_ticket_status.json) | Ticket status allowed list ထဲ မပါ | `ticket_status_not_allowed` | `status: "Resolved"` |

### Success (Node-RED CPE status ပြင် — ticket တွင် `cpe_id` သာ ပြောင်း)

Run သည် rtutil က RT ticket ကို update လုပ်ပြီးမှ `COMPLETED` (Success) ဖြစ်ပြီး report ၏ Success panel တွင် ဝင်သည်။

| File (`cpe_id`) | Report | Criteria | Workflow step | `target_queue` | Node-RED field ပြင်ချက် |
|---|---|---|---|---|---|
| [`10_remote_resolved.json`](tickets/10_remote_resolved.json) (`CRIT10`) | Success · Remote resolved | Network နှင့် CPE အားလုံး good | category_type_workflow.final_action | Customer Support (Resolved) | ပြောင်းစရာ မလို (good data အတိုင်း) |
| [`20_kept_olt_offline.json`](tickets/20_kept_olt_offline.json) (`CRIT20`) | Success · Kept in NOC | OLT Offline | network_good_workflow · olt_status | Network Operation Center (NOC) | `olt.status: "Offline"` |
| [`21_kept_onu_on_off.json`](tickets/21_kept_onu_on_off.json) (`CRIT21`) | Success · Kept in NOC | ONU online/offline > 20 ကြိမ် | network_good_workflow · is_onu_onoff | Network Operation Center (NOC) | `onu.is_onu_on_off: true` |
| [`22_kept_port_loss.json`](tickets/22_kept_port_loss.json) (`CRIT22`) | Success · Kept in NOC | PortLoss > 20 | network_good_workflow · is_port_loss | Network Operation Center (NOC) | `onu.is_port_loss: true` |
| [`23_kept_bip8_error.json`](tickets/23_kept_bip8_error.json) (`CRIT23`) | Success · Kept in NOC | BIP8 error > 20 | network_good_workflow · is_bip8_error | Network Operation Center (NOC) | `onu.is_bip8_error: true` |
| [`24_kept_line_profile_mismatch.json`](tickets/24_kept_line_profile_mismatch.json) (`CRIT24`) | Success · Kept in NOC | Line profile (plan) မှား | network_good_workflow · is_line_profile_mismatch | Network Operation Center (NOC) | `onu.profile.is_line_profile_mismatch: true` |
| [`25_kept_cpe_down.json`](tickets/25_kept_cpe_down.json) (`CRIT25`) | Success · Kept in NOC | ONU Working, CPE Down | network_good_workflow · ONU Phase State=Working → cpe_down | Network Operation Center (NOC) | `cpe.status: "Down"` |
| [`26_kept_epc_outage.json`](tickets/26_kept_epc_outage.json) (`CRIT26`) | Success · Kept in NOC | ONU Working မဟုတ် + EPC outage | network_good_workflow · ONU Phase State=Default → epc_outage | Network Operation Center (NOC) | `onu.phase_state: "Offline"` · `onu.is_epc_outage: true` |
| [`27_kept_low_usage.json`](tickets/27_kept_low_usage.json) (`CRIT27`) | Success · Kept in NOC | 24h usage < 1 GB | category_type_workflow · abnormal_usage | Network Operation Center (NOC) | `onu.24h_onu_usage_gb: 0.5` · `onu.is_usage_over_gb: false` |
| [`28_kept_node_red_error.json`](tickets/28_kept_node_red_error.json) (`CRIT28`) | Success · Kept in NOC | Node-RED က `olt.status` ကို error ဖြင့် ပြန် (value ဗလာ) | `error_event` (OLT rule တွင် UNKNOWN → manual review) | Network Operation Center (NOC) | `olt.status: ""` · `errors: [{"field":"olt.status","error_code":2002}]` — run message `workflow engine: failed value (error_code 2002: OLT status job unavailable)` |
| [`30_transferred_ca1_broken.json`](tickets/30_transferred_ca1_broken.json) (`CRIT30`) | Success · Transferred | CA1 Broken | network_good_workflow · ca1_status=Broken | Passive-Infra | `ca1.status: "Broken"` |
| [`31_transferred_ca1_low_signal.json`](tickets/31_transferred_ca1_low_signal.json) (`CRIT31`) | Success · Transferred | CA1 Low Signal | network_good_workflow · ca1_status=Low Signal | Passive-Infra | `ca1.status: "Low Signal"` |
| [`32_transferred_uplink_broken.json`](tickets/32_transferred_uplink_broken.json) (`CRIT32`) | Success · Transferred | Uplink Broken | network_good_workflow · uplink_status=Broken | Passive-Infra | `uplink.status: "Broken"` |
| [`33_transferred_uplink_low_signal.json`](tickets/33_transferred_uplink_low_signal.json) (`CRIT33`) | Success · Transferred | Uplink Low Signal | network_good_workflow · uplink_status=Low Signal | Passive-Infra | `uplink.status: "Low Signal"` |
| [`34_transferred_low_dbm.json`](tickets/34_transferred_low_dbm.json) (`CRIT34`) | Success · Transferred | ONU Working + is_low_dbm=true | network_good_workflow · ONU Phase State Working: ONU Low Dbm (IS_TRUE) | LANOps | `onu.is_low_dbm: true` |
| [`35_transferred_swap_onu.json`](tickets/35_transferred_swap_onu.json) (`CRIT35`) | Success · Transferred | ONU Working + swap ONU | network_good_workflow · ONU Phase State=Working → swap_onu | LANOps | `onu.is_swap_onu: true` |
| [`36_transferred_onu_down.json`](tickets/36_transferred_onu_down.json) (`CRIT36`) | Success · Transferred | ONU Working မဟုတ် + EPC outage မဟုတ် | network_good_workflow · ONU Phase State=Default → onu_down | LANOps | `onu.phase_state: "Offline"` · `onu.is_epc_outage: false` |

"All good" response (`10` နှင့် default): `olt.status: "Online"`, `ca1.status` / `uplink.status: "OK"`, `onu.phase_state: "Working"`, `onu.is_low_dbm: false`, `onu.is_swap_onu` / `is_onu_on_off` / `is_port_loss` / `is_bip8_error` / `profile.is_line_profile_mismatch: false`, `cpe.status: "Up"`, `onu["24h_onu_usage_gb"]: 1.5`။

> **`is_low_dbm`:** workflow config (`workflow_engine_configs/network_good_workflow.json`) တွင် `IS_TRUE` ဖြစ်သဖြင့် `is_low_dbm: true` (signal နိမ့်) ဖြစ်လျှင် "ONU has been offline for over 12 hours" ဖြင့် LANOps သို့ ရွှေ့သည် (`34`)။ ကျန် criteria များ၏ body တွင် `false`။
>
> **Node-RED field types (noc_automation `models.NodeRedONU`):** `voltage`, `temperature`, `signal` = number၊ `uptime` = string၊ `profile.id` = int၊ body တွင် `errors: []` ပါသည်။ Type မကိုက်လျှင် noc က parse မလုပ်နိုင်ဘဲ `CPE_FETCH_RETRY_SCHEDULED` ဖြစ်သည်။

Workflow engine (noc_automation) ကို ဤ file များဖြင့် တိုက်ရိုက် run ၍ `target_queue` / status ကို စစ်ပြီး — ၁၆ ခုလုံး အထက်ပါ table အတိုင်း ထွက်သည် (`28` မပါ၊ contract အသစ်ဖြင့် ပြန်မစစ်ရသေး)။

### Manual check (Node-RED CPE status ပြင်)

| File (`cpe_id`) | Report | Criteria | `current_state` | Node-RED response |
|---|---|---|---|---|
| [`40_cpe_not_found.json`](tickets/40_cpe_not_found.json) (`CRIT40`) | Manual check · Failed | Node-RED တွင် CPE မရှိ | `CPE_NOT_FOUND` (retry မလုပ်) | HTTP **404** · `error_code` 4003 ([`cpe_status/40_cpe_not_found.json`](cpe_status/40_cpe_not_found.json)) |
| [`41_invalid_request.json`](tickets/41_invalid_request.json) (`CRIT41`) | Manual check · Failed | Node-RED က request ကို ငြင်း | `FAILED_PERMANENT` · `cpe_invalid_request` (retry မလုပ်) | HTTP **400** · `error_code` 4002 ([`cpe_status/41_invalid_request.json`](cpe_status/41_invalid_request.json)) |

noc_automation သည် Node-RED မှ 404 ရလျှင် `CPE_NOT_FOUND`၊ 400 ရလျှင် `FAILED_PERMANENT` (`cpe_invalid_request`) သို့ ချက်ချင်း ရွှေ့သည် (အခြား error / timeout ဆိုလျှင် retry လုပ်ပြီး ကုန်ဆုံးမှ `FAILED_PERMANENT`)။ `cpe_id` တွင် `-` စသည် ပါလျှင် (ဥပမာ UI တွင် `CRIT-41` ရိုက်) Node-RED ကို မခေါ်ဘဲ `cpe_invalid_request` ဖြစ်သည်။ Mock ကို ပြင်ပြီးလျှင် `docker restart node-red-criteria-mock` လုပ်ပါ (hot-reload မလုပ်)။

### BCS success / fail

Remote resolved ဖြစ်ပြီး (`is_remote_resolved = 1`) rtutil က BCS (Peppermint) ကို ခေါ်သည်။ Node-RED ဘက်တွင် `CRIT11`–`CRIT15` သည် rule မကိုက်သဖြင့် "all good" (default) ရပြီး Customer Support (Resolved) သို့ ရောက်သည်။ BCS mock က CPE id အလိုက် ဖြေသည်:

| File (`cpe_id`) | BCS start (rtutil) | Job status poll (noc sweeper) | Report (BCS) | `pipeline_runs` |
|---|---|---|---|---|
| [`11_bcs_success.json`](tickets/11_bcs_success.json) (`CRIT11`) | 202 · `job_id: ok-…` | `done` | BCS OK | `is_bcs_success = 1` |
| [`12_bcs_failed.json`](tickets/12_bcs_failed.json) (`CRIT12`) | 202 · `job_id: fail-…` | `failed` | BCS failed | `is_bcs_success = 0` · `failed (from status poll, no detail)` |
| [`13_bcs_pending.json`](tickets/13_bcs_pending.json) (`CRIT13`) | 202 · `job_id: run-…` | `running` (နောက်တစ်ခါ ပြန် poll) | BCS pending | `is_bcs_success = NULL` |
| [`14_bcs_rejected.json`](tickets/14_bcs_rejected.json) (`CRIT14`) | **401** (retry မလုပ်) | — (job မရှိ) | BCS failed | `is_bcs_success = 0` · event `bcs_update_failed` |
| [`15_bcs_unavailable.json`](tickets/15_bcs_unavailable.json) (`CRIT15`) | **503** (၃ ကြိမ် retry) | — (job မရှိ) | BCS failed | `is_bcs_success = 0` · event `bcs_update_failed` |

Mock route များ (port 3002၊ Node-RED mock နှင့် အတူတူ):

- `POST /api/v2/cpes/:cpe_id/radios/2.4g:select-best-channel` — `job_id` = prefix (`ok-` / `fail-` / `run-`) + `X-Request-ID` (run_id) ၏ ရှေ့ ၃၀ လုံး (`ref_bcs_process_id` VARCHAR(36) ထဲ ဆံ့ရန်)။ အခြား CPE id များ = `ok-`။
- `GET /api/v2/channel-selections/:job_id` — `job_id` prefix အလိုက် `done` / `failed` / `running`၊ မသိသော job = 404။

Config (mock သုံးရန်):

```bash
# rtutil .rtutil_local.json
"cpems_api": { "base_url": "http://127.0.0.1:3002", ... }
# noc_automation .env (BCS result sweeper)
BCS_API_BASE_URL=http://127.0.0.1:3002
BCS_RESULT_TIMEOUT_MINUTES=1      # run ၁ မိနစ်ကျော်မှ poll
BCS_SWEEP_INTERVAL_MS=15000
```

rtutil နှင့် noc_automation ကို restart လုပ်ပါ။ Poll သည် run ဖွင့်ပြီး `BCS_RESULT_TIMEOUT_MINUTES` ကျော်မှ ဖြစ်သဖြင့် `11` / `12` ၏ result သည် ~၁–၂ မိနစ်ကြာမှ ပေါ်သည်။ `before_bcs_channel` / `target_bcs_channel` ကို webhook (`POST /api/v1/bcs/callback?token=<BCS_WEBHOOK_TOKEN>`) တစ်ခုတည်းက ရေးသဖြင့် poll ဖြင့် မဖြစ်ပါ။

### ဤ folder တွင် မပါသော criteria (JSON ဖြင့် မဖြစ်စေနိုင်)

| Report | ဖြစ်စေပုံ |
|---|---|
| Manual check · API error (unreachable) | noc_automation ကို ရပ်ထား၊ သို့ `RT_WEB_UI_API_URL` ကို မှား |
| Manual check · Failed (`RT_UPDATE_FAILED`) | rtutil ၏ RT REST2 update fail (RT ရပ်ထား စသည်) |
| Manual check · Failed (`FAILED_PERMANENT`) | Node-RED / Kafka publish retry ကုန်ဆုံး (`NODE_RED_BASE_URL` ကို မရနိုင်သော address သို့) |
| Manual check · API error (409) | အသုံးပြုပြီးသား `run_id` ကို ပြန်ပို့ — `make send-criteria C=10_remote_resolved RUN_ID=<ယခင် run_id>` (API က `run_id already in use` ဖြင့် ငြင်း) |
| Manual check · Validation failed (API 400) / API error (5xx) | API ဘက် validation / DB error — rt_web_ui gate များက ကြိုစစ်ထားသဖြင့် ticket JSON ဖြင့် မစမ်းရသေး |
| In progress · Stuck | rtutil consumer ကို မ run ထား (run သည် `PUBLISHED` တွင် ရပ်) |

## Run ပုံ

```bash
cd rt_web_ui

# 0. System များ ready ဖြစ်/မဖြစ် စစ် (read-only; ✘ ရှိရင် exit 1)
make check                                         # NOC_ENV=<noc_automation .env> ဖြင့် noc env ကို ညွှန်
#    ✘ = send မလုပ်နိုင် (MySQL၊ RT External API ...)  ! = feature တချို့သာ မရ (mock၊ Kafka၊ rtutil ...)

#    ✘ ရှိရင် down နေတာကို အလိုအလျောက် start: make up (ပြီးရင် make down)
#    (MySQL/RT container, criteria mock, noc_automation, rtutil · log/pid: .run/)

# 1. Criteria mock ကို port 3002 တွင် run (terminal တစ်ခု သီးသန့်; docker image mockoon/cli)
make criteria-mock
#    သို့ Mockoon desktop တွင် criteria/mockoon-criteria.json ကို "Open environment" ဖြင့် ဖွင့်

# 2. noc_automation ကို ဤ mock သို့ ညွှန် (noc_automation/.env) ပြီး restart
#    NODE_RED_BASE_URL=http://localhost:3002/
#    (မူလ Mockoon "Local" environment သည် port 3001 — ပြီးလျှင် ပြန်ပြောင်းပါ)

# 3. Criteria တစ်ခု ပို့
make send-criteria C=20_kept_olt_offline
make send-criteria C=01_not_eligible_queue
make send-criteria C=30_transferred_ca1_broken ARGS='--set id=385'   # ticket id ပြောင်း

# 3 (UI ဖြင့်). Browser မှ ticket id / criteria / CPE id ရွေး၍ ပို့
make criteria-ui                     # http://127.0.0.1:8090 (UI_ADDR=... ဖြင့် ပြောင်း)
```

UI (`go run . serve`) သည် `make send-criteria C=<criteria> ARGS='--set id=<ticket> [--set custom_fields.cpe_id=<cpe>]'` နှင့် အတူတူ ပို့သည်။ CPE id ကို ကိုယ်တိုင် ထည့်ပါက mock ၏ `CRITxx` rule နှင့် မကိုက်တော့သဖြင့် `10`–`36` criteria result မဖြစ်တော့ပါ (UI တွင် သတိပေးချက် ပြသည်)။ Ticket ကို တကယ် update လုပ်သဖြင့် server ကို default အနေဖြင့် localhost တွင်သာ ဖွင့်ထားသည်။

> **သတိ:** Success criteria များသည် rtutil မှတစ်ဆင့် **RT ticket ကို တကယ် update** လုပ်သည် (queue ပြောင်း၊ comment ထည့်၊ CPE ID custom field ကို `CRITxx` ဟု ရေး)။ Local / test RT ticket ဖြင့်သာ ပို့ပါ (default `id` 384)။
