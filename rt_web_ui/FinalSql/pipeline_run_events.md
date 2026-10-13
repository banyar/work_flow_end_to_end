# `pipeline_run_events` table

[`pipeline_run_events.sql`](pipeline_run_events.sql) က `pipeline_run_events` table ကို ဖန်တီးပါတယ်။ Pipeline run တစ်ခုချင်းစီမှာ ဖြစ်ခဲ့တဲ့ အဆင့်တိုင်းကို **မှတ်တမ်း (history)** အဖြစ် သိမ်းတဲ့ table ဖြစ်ပါတယ်။

## Table ရဲ့ ရည်ရွယ်ချက်

- **Append-only table ပါ။** Row အသစ်ကို ထည့်ရုံပဲ ထည့်ပြီး ရှိပြီးသား row ကို ဘယ်တော့မှ update မလုပ်ပါ။
- Row အသစ် ဝင်လာတာ ၂ မျိုး ရှိပါတယ်:
  1. **State ပြောင်းတိုင်း** — ဥပမာ `SUBMITTED_TO_API` → `RECEIVED`
  2. **State မပြောင်းပေမဲ့ မှတ်ထားသင့်တဲ့ အဆင့်တွေ** — ဥပမာ `api_accepted`၊ retry တစ်ကြိမ် fail ဖြစ်တာ
- Run ရဲ့ **လက်ရှိ state** ကို ဒီ table မှာ မကြည့်ရပါ၊ [`pipeline_runs`](pipeline_runs.sql) မှာ ကြည့်ရပါမယ်။ ဒီ table က **"ဘယ်လို ရောက်လာခဲ့လဲ"** ဆိုတဲ့ timeline ကို ပြတာပါ။

## Schema ကို ဘယ်သူ ထိန်းလဲ

- Schema ကို [`pipeline_run_events.sql`](pipeline_run_events.sql) file က သတ်မှတ်ပါတယ်။ **DBA (သို့) release အဆင့်မှာ RT database ပေါ် ကိုယ်တိုင် apply လုပ်ရပါမယ်။**
- ဘယ် service ကမှ table ကို အလိုအလျောက် create / alter မလုပ်ပါ (**AutoMigrate မရှိပါ**)။
- rtdatacore ရဲ့ `entities.PipelineRunEvent` struct tag တွေက ဒီ column တွေနဲ့ ကိုက်ရပါမယ်။ **SQL file ကို ပြင်ရင် struct tag ကိုပါ လိုက်ပြင်ရပါမယ်။**

## Column တစ်ခုချင်းစီ

| Column | Type | အဓိပ္ပာယ် |
|---|---|---|
| `id` | `bigint`, auto increment | Internal primary key ဖြစ်ပါတယ်။ Millisecond တစ်ခုတည်းမှာ event ၂ ခု ဝင်လာရင် ဘယ်ဟာ အရင်လဲဆိုတာကို ဒါနဲ့ စီပါတယ် |
| `run_id` | `char(36)` | Pipeline run ရဲ့ UUID ပါ (`pipeline_runs.run_id`)။ Foreign key မရှိပါ၊ logical reference သက်သက်ပါ |
| `from_state` | `varchar(40)` | Event မဖြစ်ခင် state ပါ။ rt_web_ui ဖွင့်တဲ့ run ရဲ့ ပထမ transition မှာ `""` (ဗလာ) ဖြစ်ပါတယ် |
| `to_state` | `varchar(40)` | Event ဖြစ်ပြီးနောက် state ပါ။ State မပြောင်းတဲ့ event ဆိုရင် `from_state` နဲ့ အတူတူပါ |
| `event` | `varchar(80)` | ဘာဖြစ်ခဲ့လဲ ပြပါတယ်။ ဥပမာ `payload_submitted`, `api_received`, `node_red_fetch_started`, `cpe_not_found`, `retry_attempt_failed` |
| `component` | `varchar(64)` | ဘယ်သူ ရေးခဲ့လဲ ပြပါတယ်: `rt_web_ui`, `remote_resolve_service`, `retry_worker`, `publish_timeout_sweeper`, `rtutil_consumer` |
| `detail` | `longtext` (JSON) | Event နဲ့ ဆိုင်တဲ့ data ပါ။ ဥပမာ `{"http_status":200}`, `{"error":"...","attempt":2,"max_attempts":3}`။ မရှိရင် `NULL` ပါ။ `CHECK (json_valid(...))` ကြောင့် JSON မှန်မှသာ ထည့်လို့ရပါတယ် |
| `occurred_at` | `datetime(3)` | Event ဖြစ်ခဲ့တဲ့ အချိန်ပါ (millisecond အထိ) |

## Index

```sql
KEY idx_pipeline_run_events_run_occurred (run_id, occurred_at)
```

Run တစ်ခုရဲ့ event တွေကို အချိန်စဉ်အတိုင်း မြန်မြန် ဆွဲထုတ်နိုင်ဖို့ပါ။ pipeline_report ရဲ့ run detail page နဲ့ debug လုပ်တဲ့အခါ သုံးပါတယ်:

```sql
SELECT * FROM pipeline_run_events
WHERE run_id = '<run_id>'
ORDER BY occurred_at, id;
```

## Charset

- Table တစ်ခုလုံးက **`latin1`** ပါ။ `event`, `component`, state name တွေက ASCII စာသားတွေပဲမို့ အဆင်ပြေပါတယ်။
- **`detail`** column တစ်ခုတည်းကိုတော့ **`utf8mb4_bin`** နဲ့ သီးသန့် သတ်မှတ်ထားပါတယ်။ Error message တွေနဲ့ Node-RED response ထဲမှာ မြန်မာစာ (သို့) Unicode စာလုံး ပါလာနိုင်လို့ ဖြစ်ပါတယ်။

## ဥပမာ timeline (criteria 25)

| `from_state` | `to_state` | `event` | `component` |
|---|---|---|---|
| `""` | `SUBMITTED_TO_API` | `payload_submitted` | `rt_web_ui` |
| `SUBMITTED_TO_API` | `RECEIVED` | `api_received` | `remote_resolve_service` |
| `RECEIVED` | `RECEIVED` | `api_accepted` | `rt_web_ui` |
| `RECEIVED` | `FETCHING_CPE_STATUS` | `node_red_fetch_started` | `remote_resolve_service` |
| `FETCHING_CPE_STATUS` | `WORKFLOW_EVALUATED` | `workflow_evaluated` | `remote_resolve_service` |
| `WORKFLOW_EVALUATED` | `PUBLISHED` | `kafka_published` | `remote_resolve_service` |
| … | `COMPLETED` | `rt_updated` | `rtutil_consumer` |

> **မှတ်ချက်:** `api_accepted` row မှာ `from_state` နဲ့ `to_state` တူနေပါတယ်။ State မပြောင်းဘဲ မှတ်တမ်းသက်သက် ထည့်တဲ့ event ဖြစ်လို့ပါ။
>
> Event အစီအစဉ်ကတော့ အချိန်ပေါ် မူတည်ပြီး ပြောင်းနိုင်ပါတယ်။ `api_accepted` ကို rt_web_ui က 200 response ပြန်ရမှ ထည့်တာဖြစ်ပြီး အဲ့အချိန်မှာ API ရဲ့ background processing က ရှေ့ရောက်နေနိုင်ပါတယ်။ ဒါကြောင့် `api_accepted` row ရဲ့ state က `RECEIVED` မဟုတ်ဘဲ `FETCHING_CPE_STATUS` (သို့) နောက်ပိုင်း state တစ်ခု ဖြစ်နိုင်ပါတယ်။
