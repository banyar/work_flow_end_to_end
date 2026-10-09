# Report criteria samples

Pipeline report ([pipeline_report](../../pipeline_report/README.md)) ၏ card / bucket တစ်ခုချင်းစီကို ဖြစ်စေရန် ပို့ရမည့် ticket JSON နှင့် Node-RED CPE status JSON များ။

| Folder / file | အသုံး |
|---|---|
| `tickets/<id>.json` | rt_web_ui က ပို့မည့် ticket ([ticket_valid.json](../ticket_valid.json) ကို အခြေခံ၍ ပြင်ထား) |
| `cpe_status/<id>.json` | Node-RED `GET /api/v1/onus/<cpe_id>/status` ၏ response (ရည်ညွှန်းရန်) |
| `mockoon-criteria.json` | Mockoon environment (port **3002**) — `cpe_id` (`CRIT-<number>`) အလိုက် `cpe_status/` ထဲက response ကို ပြန်ပေးသည်။ အခြား CPE id များအတွက် "all good" response (default) |

## ဘယ်လို ဆုံးဖြတ်သလဲ

1. **rt_web_ui gate များ** — ticket JSON ကိုသာ ကြည့်သည်။ Fail ဖြစ်ပါက `NOT_ELIGIBLE` (Not eligible card)။
2. **noc_automation workflow** ([remote_resolve_workflow_engine.json](../../../NocAutomationCodeMerge/noc_automation/remote_resolve_workflow_engine.json)) — Node-RED CPE status ကို `network_good_workflow` → `category_type_workflow` → `final_action` အစီအစဉ်ဖြင့် စစ်ပြီး **ပထမဆုံး ကိုက်သော step** ၏ queue ကို ယူသည်။ ထို့ကြောင့် criteria တစ်ခုချင်းစီတွင် field **တစ်ခု (သို့ နှစ်ခု)** သာ ပြောင်းထားပြီး ကျန် field များသည် "all good" အတိုင်း ဖြစ်သည်။
3. Ticket ၏ category custom field များ (`program`, `ticket_problem`, `tags`, `root_cause`, `plan_start_date`) သည် noc_automation ၏ `REMOTE_RESOLVE_CUSTOM_FIELDS` တွင် မပါသဖြင့် ယခု workflow က မမြင်ပါ — ထို field များဖြင့် criteria မဖန်တီးထားပါ။

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
| [`10_remote_resolved.json`](tickets/10_remote_resolved.json) (`CRIT-10`) | Success · Remote resolved | Network နှင့် CPE အားလုံး good | category_type_workflow.final_action | Customer Support (Resolved) | ပြောင်းစရာ မလို (good data အတိုင်း) |
| [`20_kept_olt_offline.json`](tickets/20_kept_olt_offline.json) (`CRIT-20`) | Success · Kept in NOC | OLT Offline | network_good_workflow · olt_status | Network Operation Center (NOC) | `olt.status: "Offline"` |
| [`21_kept_onu_on_off.json`](tickets/21_kept_onu_on_off.json) (`CRIT-21`) | Success · Kept in NOC | ONU online/offline > 20 ကြိမ် | network_good_workflow · is_onu_onoff | Network Operation Center (NOC) | `onu.is_onu_on_off: true` |
| [`22_kept_port_loss.json`](tickets/22_kept_port_loss.json) (`CRIT-22`) | Success · Kept in NOC | PortLoss > 20 | network_good_workflow · is_port_loss | Network Operation Center (NOC) | `onu.is_port_loss: true` |
| [`23_kept_bip8_error.json`](tickets/23_kept_bip8_error.json) (`CRIT-23`) | Success · Kept in NOC | BIP8 error > 20 | network_good_workflow · is_bip8_error | Network Operation Center (NOC) | `onu.is_bip8_error: true` |
| [`24_kept_line_profile_mismatch.json`](tickets/24_kept_line_profile_mismatch.json) (`CRIT-24`) | Success · Kept in NOC | Line profile (plan) မှား | network_good_workflow · is_line_profile_mismatch | Network Operation Center (NOC) | `onu.profile.is_line_profile_mismatch: true` |
| [`25_kept_cpe_down.json`](tickets/25_kept_cpe_down.json) (`CRIT-25`) | Success · Kept in NOC | ONU Working, CPE Down | network_good_workflow · ONU Phase State=Working → cpe_down | Network Operation Center (NOC) | `cpe.status: "Down"` |
| [`26_kept_epc_outage.json`](tickets/26_kept_epc_outage.json) (`CRIT-26`) | Success · Kept in NOC | ONU Working မဟုတ် + EPC outage | network_good_workflow · ONU Phase State=Default → epc_outage | Network Operation Center (NOC) | `onu.phase_state: "Offline"` · `onu.is_epc_outage: true` |
| [`27_kept_low_usage.json`](tickets/27_kept_low_usage.json) (`CRIT-27`) | Success · Kept in NOC | 24h usage < 1 GB | category_type_workflow · abnormal_usage | Network Operation Center (NOC) | `onu.24h_onu_usage_gb: "0.5"` · `onu.is_usage_over_gb: false` |
| [`30_transferred_ca1_broken.json`](tickets/30_transferred_ca1_broken.json) (`CRIT-30`) | Success · Transferred | CA1 Broken | network_good_workflow · ca1_status=Broken | Passive-Infra | `ca1.status: "Broken"` |
| [`31_transferred_ca1_low_signal.json`](tickets/31_transferred_ca1_low_signal.json) (`CRIT-31`) | Success · Transferred | CA1 Low Signal | network_good_workflow · ca1_status=Low Signal | Passive-Infra | `ca1.status: "Low Signal"` |
| [`32_transferred_uplink_broken.json`](tickets/32_transferred_uplink_broken.json) (`CRIT-32`) | Success · Transferred | Uplink Broken | network_good_workflow · uplink_status=Broken | Passive-Infra | `uplink.status: "Broken"` |
| [`33_transferred_uplink_low_signal.json`](tickets/33_transferred_uplink_low_signal.json) (`CRIT-33`) | Success · Transferred | Uplink Low Signal | network_good_workflow · uplink_status=Low Signal | Passive-Infra | `uplink.status: "Low Signal"` |
| [`34_transferred_not_low_dbm.json`](tickets/34_transferred_not_low_dbm.json) (`CRIT-34`) | Success · Transferred | ONU Working + is_low_dbm=false | network_good_workflow · ONU Phase State=Working → is_low_dbm (IS_FALSE) | LANOps | `onu.is_low_dbm: false` |
| [`35_transferred_swap_onu.json`](tickets/35_transferred_swap_onu.json) (`CRIT-35`) | Success · Transferred | ONU Working + swap ONU | network_good_workflow · ONU Phase State=Working → swap_onu | LANOps | `onu.is_swap_onu: true` |
| [`36_transferred_onu_down.json`](tickets/36_transferred_onu_down.json) (`CRIT-36`) | Success · Transferred | ONU Working မဟုတ် + EPC outage မဟုတ် | network_good_workflow · ONU Phase State=Default → onu_down | LANOps | `onu.phase_state: "Offline"` · `onu.is_epc_outage: false` |

"All good" response (`10` နှင့် default): `olt.status: "Online"`, `ca1.status` / `uplink.status: "OK"`, `onu.phase_state: "Working"`, `onu.is_low_dbm: true`, `onu.is_swap_onu` / `is_onu_on_off` / `is_port_loss` / `is_bip8_error` / `profile.is_line_profile_mismatch: false`, `cpe.status: "Up"`, `onu["24h_onu_usage_gb"]: "1.5"`။

> **`is_low_dbm`:** workflow တွင် `IS_FALSE` ဖြစ်သဖြင့် `is_low_dbm: false` (signal ကောင်း) ဖြစ်လျှင် "ONU has been offline for over 12 hours" ဖြင့် LANOps သို့ ရွှေ့သည် (`34`)။ ရည်ရွယ်ချက်ရှိရှိ ဟုတ်မဟုတ် team နှင့် အတည်ပြုရန်။

Workflow engine (noc_automation) ကို ဤ file များဖြင့် တိုက်ရိုက် run ၍ `target_queue` / status ကို စစ်ပြီး — ၁၆ ခုလုံး အထက်ပါ table အတိုင်း ထွက်သည်။

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

UI (`go run . serve`) သည် `make send-criteria C=<criteria> ARGS='--set id=<ticket> [--set custom_fields.cpe_id=<cpe>]'` နှင့် အတူတူ ပို့သည်။ CPE id ကို ကိုယ်တိုင် ထည့်ပါက mock ၏ `CRIT-xx` rule နှင့် မကိုက်တော့သဖြင့် `10`–`36` criteria result မဖြစ်တော့ပါ (UI တွင် သတိပေးချက် ပြသည်)။ Ticket ကို တကယ် update လုပ်သဖြင့် server ကို default အနေဖြင့် localhost တွင်သာ ဖွင့်ထားသည်။

> **သတိ:** Success criteria များသည် rtutil မှတစ်ဆင့် **RT ticket ကို တကယ် update** လုပ်သည် (queue ပြောင်း၊ comment ထည့်၊ CPE ID custom field ကို `CRIT-xx` ဟု ရေး)။ Local / test RT ticket ဖြင့်သာ ပို့ပါ (default `id` 384)။
