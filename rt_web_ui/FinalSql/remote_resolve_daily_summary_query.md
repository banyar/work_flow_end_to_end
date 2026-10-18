# Remote Resolve Daily Summary Query ရှင်းလင်းချက်

ဒီ query က Remote Resolve pipeline ကို **တစ်ရက်အတွင်း ဝင်လာတဲ့ ticket run တွေ** ကို state အလိုက် ရေတွက်ပေးပါတယ်။ ရလဒ်က **row တစ်ကြောင်းတည်း** ထွက်ပြီး column တစ်ခုစီက အရေအတွက် (count) တစ်ခုစီ ဖြစ်ပါတယ်။

ဥပမာ query မှာ `2026-10-14` ရက်နေ့ (`00:00:00` ကနေ `2026-10-15 00:00:00` မတိုင်ခင်အထိ) ဝင်လာတဲ့ run တွေကို ရေတွက်ထားပါတယ်။

---

## ၁။ Pipeline အကြောင်း အတိုချုပ်

Ticket တစ်ခုကို NOC queue ထဲ assign လုပ်လိုက်တိုင်း `pipeline_runs` table ထဲမှာ **run** တစ်ခု (row တစ်ကြောင်း) ဖွင့်ပါတယ်။ အဲဒီ run ကို service ၃ ခုက အဆင့်လိုက် ဆက်လုပ်ပါတယ်:

```
rt_web_ui  ──►  RT External API (noc_automation)  ──►  Kafka  ──►  rtutil (remote-resolved-queue-transfer)
 ticket စစ်      CPE status ယူ၊ workflow တွက်               BCS update + RT ticket update
```

| Table | ဘာသိမ်းလဲ |
|---|---|
| `pipeline_runs` (`pr`) | Run တစ်ခုကို row တစ်ကြောင်း။ `current_state` က လက်ရှိ ရောက်နေတဲ့ အဆင့်။ `completed_at` က run ပြီးဆုံးသွားတဲ့ အချိန် (မပြီးသေးရင် `NULL`) |
| `pipeline_run_events` (`e`) | Run တစ်ခုရဲ့ state ပြောင်းတိုင်း event တစ်ကြောင်း။ `detail` (JSON) ထဲမှာ အသေးစိတ် (ဥပမာ `http_status`) ပါတယ် |

---

## ၂။ State တွေရဲ့ အဓိပ္ပာယ်

| State | ဘယ်သူ ရေးလဲ | အဓိပ္ပာယ် |
|---|---|---|
| `NOT_ELIGIBLE` | rt_web_ui | Ticket က စည်းကမ်းချက် မပြည့်လို့ process မလုပ်ပါ (CPE ID / Local Service ID မရှိ၊ OPI customer၊ Service Type မကိုက်) |
| `ALREADY_PROCESSED` | rt_web_ui | 24 နာရီအတွင်း process လုပ်ပြီးသား ticket (လောလောဆယ် ရေးတဲ့ code မရှိသေးပါ) |
| `API_REJECTED` | rt_web_ui | RT External API က 4xx/5xx နဲ့ ငြင်းလိုက်တယ် |
| `API_UNREACHABLE` | rt_web_ui | RT External API ကို ချိတ်မရ / timeout |
| `CPE_FETCH_RETRY_SCHEDULED` | noc_automation | CPE status ယူတာ မအောင်မြင်လို့ ပြန်ကြိုးစားဖို့ စီစဉ်ထားတယ် |
| `CPE_NOT_FOUND` | noc_automation | CPE ကို ရှာမတွေ့ |
| `KAFKA_PUBLISH_RETRY_SCHEDULED` | noc_automation | Kafka ကို ပို့တာ မအောင်မြင်လို့ ပြန်ကြိုးစားဖို့ စီစဉ်ထားတယ် |
| `FAILED_PERMANENT` | noc_automation | Retry တွေ ကုန်သွားပြီ၊ အပြီးတိုင် fail |
| `NORMALIZE_FAILED` | noc_automation (version ဟောင်း) | Data normalize မလုပ်နိုင်လို့ fail။ အခု code မှာ မသုံးတော့ပေမယ့် data ဟောင်းတွေအတွက် ထည့်ရေတွက်ထားတယ် |
| `CONSUME_VALIDATION_FAILED` | rtutil | Kafka message ကို စစ်ဆေးတဲ့အခါ မှားနေတယ် (queue / root cause မမှန် စသည်) |
| `SKIPPED` | rtutil | Ticket က Pending/Resolved/Closed ဖြစ်နေပြီးသားမို့ ဘာမှ မပြောင်းဘဲ ကျော်လိုက်တယ် |
| `RT_UPDATE_FAILED` | rtutil | RT ticket ကို update လုပ်တာ fail |
| `COMPLETED` | rtutil | RT ticket update အောင်မြင်တယ်။ **အောင်မြင်စွာ ပြီးဆုံးတယ်** |

---

## ၃။ Column တစ်ခုစီရဲ့ အဓိပ္ပာယ်

### (က) အထွေထွေ အခြေအနေ

| Column | ရေတွက်ပုံ | အဓိပ္ပာယ် |
|---|---|---|
| `received` | Run အားလုံး | အဲဒီနေ့ ဝင်လာတဲ့ run စုစုပေါင်း |
| `not_eligible` | `NOT_ELIGIBLE` + `ALREADY_PROCESSED` | Process လုပ်စရာ မလိုတဲ့ ticket |
| `in_progress` | `completed_at IS NULL` | မပြီးသေးတဲ့ run အားလုံး (`retrying` နဲ့ `stuck` ပါ ပါဝင်တယ်) |
| `retrying` | မပြီးသေး + `*_RETRY_SCHEDULED` | Retry စောင့်နေတဲ့ run။ ပုံမှန်ပါ၊ ခဏနေရင် ဆက်သွားမယ် |
| `stuck` | မပြီးသေး + retry မဟုတ် + **cutoff အချိန်ထက် ကြာနေပြီ** | တစ်နေရာမှာ ပိတ်မိနေတဲ့ run။ **စစ်ဆေးဖို့ လိုတယ်** (အောက်က မှတ်ချက် ကြည့်ပါ) |
| `finished` | Pipeline ထဲ ဝင်ပြီး ပြီးဆုံးသွားတဲ့ state တွေ | `COMPLETED`, `SKIPPED`, `CONSUME_VALIDATION_FAILED`, `FAILED_PERMANENT`, `CPE_NOT_FOUND`, `RT_UPDATE_FAILED`, `NORMALIZE_FAILED` |

> **`stuck` ရဲ့ cutoff အချိန်:** `'2026-10-14 10:07:36'` က query ကို run တဲ့ program က ထည့်ပေးတဲ့ အချိန်ပါ (လက်ရှိအချိန် ထဲက threshold တစ်ခု နုတ်ထားတာ)။ နောက်ဆုံး update (`updated_at`၊ မရှိရင် `created_at`) က ဒီအချိန်ထက် စောနေရင် "ကြာလွန်းနေပြီ" လို့ သတ်မှတ်ပါတယ်။ Query ကို ကိုယ်တိုင် run ရင် ဒီတန်ဖိုးကို လိုသလို ပြောင်းပါ။

### (ခ) အောင်မြင်တဲ့ run တွေ (`COMPLETED`)

| Column | အဓိပ္ပာယ် |
|---|---|
| `success` | `COMPLETED` ဖြစ်တဲ့ run အားလုံး |
| `remote_resolved` | Remote ကနေ ဖြေရှင်းလိုက်တယ် (`is_remote_resolved = 1`) |
| `not_remote_resolved` | Remote resolve မလုပ်ဖြစ်ဘူး (`is_remote_resolved` က `0` သို့မဟုတ် `NULL`) |
| `kept_in_noc` | Remote resolve မလုပ်ဖြစ်ဘဲ ticket က NOC queue ထဲမှာပဲ ဆက်ရှိနေတယ် (မူလ queue ရော ပစ်မှတ် queue ရော `Network Operation Center (NOC)`) |

`success = remote_resolved + not_remote_resolved` ဖြစ်ရပါမယ်။

### (ဂ) BCS ရလဒ် (Remote resolved ticket တွေအတွက်ပဲ)

| Column | အဓိပ္ပာယ် |
|---|---|
| `bcs_ok` | BCS update အောင်မြင်တယ် (`is_bcs_success = 1`) |
| `bcs_failed` | BCS update fail (`is_bcs_success = 0`) |
| `bcs_pending` | BCS ကို ပို့ပြီးပြီ (`ref_bcs_process_id` ရှိတယ်)၊ ရလဒ် (webhook / status poll) ကို စောင့်နေဆဲ |

> BCS fail ဖြစ်လည်း run က `COMPLETED` ဖြစ်နိုင်ပါတယ်။ BCS ရလဒ်ကို state အနေနဲ့ မဟုတ်ဘဲ `is_bcs_success` column မှာ သီးသန့် မှတ်ပါတယ်။

### (ဃ) API ဘက်က ပြဿနာ (rt_web_ui → RT External API)

| Column | အဓိပ္ပာယ် |
|---|---|
| `validation_api_400` | API က `400 Bad Request` နဲ့ ငြင်းတယ်။ ပို့လိုက်တဲ့ data မှားနေတယ် |
| `validation_rt_util` | rtutil က Kafka message ကို validate လုပ်တာ fail (`CONSUME_VALIDATION_FAILED`) |
| `api_error_409` | API က `409 Conflict` နဲ့ ငြင်းတယ်။ ပုံမှန်အားဖြင့် တူညီတဲ့ request ထပ်ဝင်လာလို့ |
| `api_error_other` | API က 400/409 မဟုတ်တဲ့ တခြား error (5xx စသည်) နဲ့ ငြင်းတယ်၊ ဒါမှမဟုတ် http_status မသိရဘူး |
| `api_unreachable` | API ကို ချိတ်ဆက်လို့ မရဘူး |

### (င) Pipeline အတွင်း fail ဖြစ်တာတွေ

| Column | အဓိပ္ပာယ် |
|---|---|
| `failed_permanent` | `FAILED_PERMANENT` + `NORMALIZE_FAILED` |
| `cpe_not_found` | CPE ရှာမတွေ့ |
| `rt_update_failed` | RT ticket update fail |
| `skipped` | Ticket က ပြီးပြီးသား ဖြစ်နေလို့ ကျော်လိုက်တယ် |

### (စ) စစ်ဆေးရန်

| Column | အဓိပ္ပာယ် |
|---|---|
| `unclassified` | ပြီးဆုံးသွားပြီ (`completed_at` ရှိတယ်) ဒါပေမယ့် state က အထက်ပါ ဘယ်အုပ်စုထဲမှာမှ မပါဘူး။ **`0` ဖြစ်ရပါမယ်**။ `0` မဟုတ်ရင် state အသစ် ပေါ်လာတာ ဒါမှမဟုတ် data မှားနေတာ ဖြစ်နိုင်လို့ စစ်ဆေးပါ |

---

## ၄။ Query ရဲ့ နည်းပညာပိုင်း အသေးစိတ်

### `SUM(condition)` နဲ့ ရေတွက်ပုံ

MySQL/MariaDB မှာ condition တစ်ခုက မှန်ရင် `1`၊ မှားရင် `0` ပြန်ပေးပါတယ်။ ဒါကြောင့် `SUM(pr.current_state = 'COMPLETED')` ဆိုတာ "`COMPLETED` ဖြစ်တဲ့ row ဘယ်နှစ်ကြောင်းလဲ" ကို ရေတွက်တာပါ။ ဒီနည်းနဲ့ table ကို **တစ်ခါတည်း scan** လုပ်ပြီး count အားလုံးကို တပြိုင်နက် ရပါတယ်။

### `COALESCE(SUM(...), 0)`

အဲဒီနေ့မှာ run တစ်ခုမှ မရှိရင် `SUM` က `NULL` ပြန်ပါတယ်။ `COALESCE` က `NULL` ကို `0` အဖြစ် ပြောင်းပေးပါတယ်။

### `COALESCE(column, default)` ကို condition ထဲမှာ သုံးတာ

`NULL` နဲ့ နှိုင်းယှဉ်ရင် ရလဒ်က `NULL` ဖြစ်ပြီး ရေတွက်ခြင်း မခံရပါဘူး။ ဥပမာ `is_remote_resolved = 0` လို့ ရေးရင် `NULL` row တွေ ကျန်ခဲ့ပါမယ်။ `COALESCE(pr.is_remote_resolved, 0) = 0` လို့ ရေးတော့ `NULL` ကိုပါ `0` အဖြစ် ထည့်တွက်ပါတယ်။

### `rej` subquery (API_REJECTED run တွေရဲ့ HTTP status)

`pipeline_runs` မှာ HTTP status column မရှိပါဘူး။ ဒါကြောင့် `pipeline_run_events` ထဲက `api_rejected` event ရဲ့ `detail` JSON ထဲကနေ `$.http_status` ကို ထုတ်ယူပါတယ်။

- `JSON_VALUE(e.detail, '$.http_status')`: JSON ထဲက `http_status` တန်ဖိုးကို ယူတယ်
- `CAST(... AS UNSIGNED)`: ဂဏန်း အဖြစ် ပြောင်းတယ်
- `MAX(...)` + `GROUP BY e.run_id`: Run တစ်ခုမှာ event အများကြီး ရှိနေရင်တောင် run တစ်ခုကို row တစ်ကြောင်းပဲ ထွက်အောင် လုပ်တယ်
- Subquery ထဲမှာလည်း ရက်စွဲနဲ့ `API_REJECTED` ကို filter လုပ်ထားလို့ event table တစ်ခုလုံးကို မ scan ရပါဘူး

`LEFT JOIN` ကို သုံးထားလို့ reject မဖြစ်တဲ့ run တွေလည်း ရလဒ်ထဲ ပါပါတယ် (သူတို့အတွက် `rej.http_status` က `NULL` ဖြစ်ပါတယ်)။

### ရက်စွဲ filter

```sql
WHERE pr.created_at >= '2026-10-14 00:00:00' AND pr.created_at < '2026-10-15 00:00:00'
```

`BETWEEN` အစား `>=` နဲ့ `<` ကို သုံးထားလို့ ညသန်းခေါင် `00:00:00` အချိန်ပါ run တွေကို နှစ်ရက်မှာ ထပ်မရေတွက်မိပါဘူး။ တခြားရက်ကို ကြည့်ချင်ရင် main query နဲ့ subquery **နှစ်နေရာလုံးမှာ** ရက်စွဲ ပြောင်းပါ။

---

## ၅။ ရလဒ် စစ်ဆေးတဲ့အခါ အဓိကကြည့်ရမယ့် အချက်များ

1. **`stuck > 0`** ဆိုရင် ပိတ်မိနေတဲ့ run ရှိတယ်။ Service log တွေ စစ်ပါ။
2. **`unclassified > 0`** ဆိုရင် မသိတဲ့ state ရှိနေတယ်။ Dev team ကို အကြောင်းကြားပါ။
3. **`api_unreachable` / `api_error_other`** များနေရင် RT External API service ကျနေတာ ဖြစ်နိုင်တယ်။
4. **`bcs_failed`** များနေရင် BCS (CPEMS) ဘက် စစ်ပါ။
5. **`bcs_pending`** ကြာကြာ မကျရင် BCS webhook / status poll အလုပ်မလုပ်တာ ဖြစ်နိုင်တယ်။
