# `next_retry_at` အလုပ်လုပ်ပုံ

`next_retry_at` ဆိုတာ **"ဒီ job ကို ဒီအချိန်မတိုင်ခင် ထပ်မကြိုးစားရ"** ဆိုတဲ့ အချိန်မှတ်ပါ။ Retry Worker က ဒီ column ကို ကြည့်ပြီး ဘယ် job က လုပ်ဖို့ **အချိန်ရောက်ပြီ (due)** လဲ ဆုံးဖြတ်ပါတယ်။

သူ့ကို **ရေးတဲ့နေရာ ၂ ခု** နဲ့ **ဖတ်တဲ့နေရာ ၁ ခု** ပဲ ရှိပါတယ်။

---

## ၁။ ပထမဆုံး ရေးတာ: Job ဆောက်တဲ့အချိန်

မူလ ကြိုးစားမှု fail ပြီး `retry_jobs` row ဆောက်တဲ့အချိန်မှာ ရေးပါတယ် ([retry_job_builder.go:47](retry_job_builder.go#L47)):

```go
NextRetryAt: time.Now().Add(NextRetryDelay(1))   // now + schedule[0] = now + 1m
```

Run `fc33d17b-…` မှာ:

```
created_at    = 01:52:05.316
next_retry_at = 01:53:05.314   ← +1m
```

## ၂။ ဖတ်တာ: Worker က due job ရှာတဲ့အချိန်

Worker က `RETRY_WORKER_POLL_INTERVAL_MS` (ဥပမာ `60000` = ၁ မိနစ်) တစ်ခါ နိုးလာပြီး ဒီ query ကို run ပါတယ် ([retry_job_repository.go:34-49](../../../../rtdatacore/pkg/driven/rtdb/retry_job_repository.go#L34-L49)):

```sql
SELECT ... FROM retry_jobs
WHERE status IN ('PENDING','RETRYING')
  AND next_retry_at <= <now>                              -- ① အချိန်ရောက်ပြီ
  AND attempt_count < max_attempts                        -- ② အကြိမ်ရေ မကုန်သေး
  AND (locked_by IS NULL OR locked_at < <now - lockTTL>)  -- ③ တခြား worker မကိုင်ထား
ORDER BY next_retry_at ASC                                -- ④ အရင်ဆုံး due ဖြစ်တာ အရင်
LIMIT <batch_size>
FOR UPDATE
```

အရေးကြီးတာက `next_retry_at <= now` **တစ်ခုတည်းနဲ့ မလုံလောက်ပါဘူး**။ ①②③ အကုန် မှန်မှ claim လုပ်ပါတယ်။ `ORDER BY next_retry_at ASC` ကြောင့် စောင့်နေတာ အကြာဆုံး job ကို အရင်လုပ်ပါတယ်။

**Claim လုပ်တဲ့အခါ `next_retry_at` ကို မပြောင်းပါဘူး**။ `status=RETRYING`, `locked_by`, `locked_at` ပဲ ပြောင်းပါတယ်။

## ၃။ ဒုတိယ ရေးတာ: Retry fail ပြီး ထပ်စီစဉ်တဲ့အချိန်

Retry fail ပြီး အကြိမ်ရေ မကုန်သေးရင် ([retry_worker.go:313](retry_worker.go#L313)):

```go
nextRetryAt := time.Now().Add(NextRetryDelay(nextAttempt + 1))   // now + 3m
w.repo.Reschedule(job.ID, err, nextRetryAt)
```

`Reschedule` က `next_retry_at` အသစ်ရေးပြီး `attempt_count +1`, `status=PENDING` ပြောင်းကာ lock ကို ဖြုတ်ပါတယ်။

> **သတိပြုရန်:** `now` ဆိုတာ **retry fail ပြီးသွားတဲ့အချိန်** ပါ။ အရင် `next_retry_at` ကနေ ပေါင်းတာ **မဟုတ်** ပါဘူး။ ဒါကြောင့် retry တစ်ခုက ကြာရင် (ဥပမာ Kafka timeout 5s) နောက် retry လည်း အဲဒီလောက် နောက်ကျပါတယ်။

---

## Timeline (`1m,3m`, poll ၁ မိနစ်, Kafka down)

```
01:52:05  Job ဆောက်          next_retry_at = 01:53:05   (+1m)
01:52:30  Worker poll        01:53:05 > now → မလုပ်သေး
01:53:30  Worker poll        01:53:05 <= now → claim, retry #1
01:53:35  Retry #1 fail      next_retry_at = 01:56:35   (01:53:35 + 3m)
01:54:30  Worker poll        မရောက်သေး
01:55:30  Worker poll        မရောက်သေး
01:56:30  Worker poll        01:56:35 > 01:56:30 → ၅ စက္ကန့် ကွာလို့ မလုပ်သေး!
01:57:30  Worker poll        claim, retry #2
01:57:35  Retry #2 fail      → FAILED_PERMANENT
```

ဒီဥပမာမှာ retry #2 က `3m` သတ်မှတ်ထားပေမယ့် တကယ်တမ်း **၄ မိနစ်** နောက်ကျပါတယ်။ ဘာလို့လဲဆိုတော့ poll နဲ့ ၅ စက္ကန့် လွဲသွားလို့ နောက် poll (၁ မိနစ်) ထိ စောင့်ရလို့ပါ။ ဒါကြောင့်:

> **တကယ်စောင့်ရတဲ့ချိန် = delay + (0 ~ poll interval) + retry ကြာချိန်**

---

## Job ဆုံးသွားရင် `next_retry_at` ဘာဖြစ်လဲ

| ရလဒ် | `next_retry_at` | ဘာလို့ ပြဿနာ မရှိလဲ |
|---|---|---|
| `RESOLVED` | တန်ဖိုးအဟောင်း **ကျန်နေ** | `status` က `PENDING/RETRYING` မဟုတ်တော့လို့ query က မရွေးတော့ |
| `FAILED_PERMANENT` | တန်ဖိုးအဟောင်း **ကျန်နေ** | အတူတူပါ |

ဒါကြောင့် ဆုံးသွားတဲ့ job ရဲ့ `next_retry_at` ကို ကြည့်ပြီး "ထပ် retry လုပ်ဦးမယ်" လို့ မထင်ပါနဲ့။ **`status` ကို အရင်ကြည့်ပါ**။

## Worker crash ဖြစ်ရင်

Job ကို claim လုပ်ပြီး worker process ကျသွားရင် `status=RETRYING`, `locked_by=<host-pid>` နဲ့ ကျန်ခဲ့ပါတယ်။ `next_retry_at` က အတိတ်အချိန် ဖြစ်နေပြီးသားမို့ ① မှန်နေပါတယ်။ ဒါပေမယ့် ③ ကြောင့် `locked_at` က `RETRY_WORKER_LOCK_TTL_MS` (default ၅ မိနစ်) ကျော်မှ တခြား worker (ဒါမှမဟုတ် restart ပြီးသား worker) က ပြန်ယူ (reclaim) နိုင်ပါတယ်။

---

## Test လုပ်တဲ့အခါ အသုံးဝင်တာ

**Job ကို ချက်ချင်း retry လုပ်စေချင်ရင်** (စောင့်မနေချင်ရင်):

```sql
UPDATE retry_jobs SET next_retry_at = NOW()
WHERE run_id = '<run_id>' AND status = 'PENDING';
```

နောက် worker poll (အများဆုံး poll interval တစ်ခု) မှာ ကောက်ယူပါမယ်။

**ဘယ်အချိန် due ဖြစ်မလဲ ကြည့်ချင်ရင်:**

```sql
SELECT id, status, attempt_count, max_attempts, next_retry_at,
       TIMESTAMPDIFF(SECOND, NOW(), next_retry_at) AS seconds_left
FROM retry_jobs WHERE status IN ('PENDING','RETRYING');
```

`seconds_left` က အနုတ် ဖြစ်နေရင် due ဖြစ်ပြီး နောက် poll မှာ လုပ်ပါမယ်။

> **အချိန် (timezone) သတိပြုရန်:** `next_retry_at` ကို ရေးတာရော နှိုင်းယှဉ်တာရော Go server ရဲ့ `time.Now()` နဲ့ပဲ လုပ်တာမို့ app အတွင်းမှာ ကိုက်ညီပါတယ်။ ဒါပေမယ့် MySQL ရဲ့ `NOW()` က DB server ရဲ့ အချိန်ကို သုံးတာမို့ app server နဲ့ DB server timezone/နာရီ မတူရင် အပေါ်က SQL နှစ်ခုရဲ့ ရလဒ်က အနည်းငယ် လွဲနိုင်ပါတယ်။

---

## Production မှာ `RETRY_WORKER_POLL_INTERVAL_MS` ဘယ်လောက်ထားမလဲ

**15 စက္ကန့် (`15000`)** ထားဖို့ အကြံပြုပါတယ်။ ၁၀–၁၅ စက္ကန့်ကြား ဘယ်တန်ဖိုး ထားထား ရပါတယ်။

```env
RETRY_WORKER_POLL_INTERVAL_MS=15000
```

### ဘာလို့ ၁ မိနစ်က များလဲ

ပထမ retry delay က `1m` ပါ။ Poll ကလည်း ၁ မိနစ်ဆိုရင် job က due ဖြစ်ပြီး နောက် poll ထိ **အများဆုံး ၁ မိနစ်** ထပ်စောင့်ရပါတယ်။ ဒါကြောင့် `1m` ဆိုပြီး သတ်မှတ်ထားပေမယ့် တကယ်တမ်း **၁–၂ မိနစ်** ဖြစ်သွားပါတယ်၊ နောက်ကျတာက delay ရဲ့ **100%** အထိ ရောက်ပါတယ်။

| Poll interval | `1m` delay ရဲ့ တကယ်စောင့်ရချိန် | နောက်ကျမှု အများဆုံး |
|---|---|---|
| 60s | 1m – 2m | 100% |
| 15s | 1m – 1m15s | 25% |
| 10s | 1m – 1m10s | 17% |

လက်တွေ့သုံးလို့ရတဲ့ စည်းမျဉ်းက **poll interval ≈ backoff schedule ထဲက အတိုဆုံး delay ÷ 4~6** ပါ။ `1m,3m` ဆိုရင် ၁၀–၁၅ စက္ကန့် ကျပါတယ်။ Schedule ကို ပြောင်းရင် poll ကိုပါ လိုက်ညှိပါ။

### ဘာလို့ ၁၅ စက္ကန့်ဆို DB load မပူရလဲ

- Poll တစ်ခါ = query တစ်ခုပါ။ ဒီ query အတွက် `retry_jobs` မှာ `idx_retry_jobs_claim (status, next_retry_at)` index ရှိပြီးသားမို့ table ကြီးလာလည်း မြန်ပါတယ်။
- Instance တစ်ခုကို တစ်မိနစ် ၄ ကြိမ်ပဲ ဖြစ်တာမို့ သိသာတဲ့ load မဟုတ်ပါဘူး။

### ဘာလို့ ၅ စက္ကန့်အောက် မလျှော့သင့်လဲ

- [`FetchClaimable`](../../../../rtdatacore/pkg/driven/rtdb/retry_job_repository.go#L34) က `SELECT … FOR UPDATE` ကို **`SKIP LOCKED` မပါဘဲ** သုံးပါတယ် (MariaDB အဟောင်းနဲ့ ကိုက်အောင် ရည်ရွယ်ချက်ရှိရှိ ဖြုတ်ထားတာပါ)။ ဒါကြောင့် noc_automation instance အများကြီး run ထားရင် တစ်ခုက row lock ကိုင်ထားစဉ် ကျန်တာတွေ **တန်းစီ စောင့်** ရပါတယ်။ Poll မြန်လေ ဒီလို တိုက်မိတာ များလေပါပဲ။
- `1m` delay အတွက် ၅ စက္ကန့်နဲ့ ၁၅ စက္ကန့်ကြား ကွာခြားချက်က user မသိသာပါဘူး။

### သတိပြုရန်: Poll interval က အနိမ့်ဆုံးချိန်ပဲ ဖြစ်ပါတယ်

[`runOnce()`](retry_worker.go#L104) က claim လုပ်ထားတဲ့ job တွေကို **တစ်ခုပြီးမှ တစ်ခု** (sequential) လုပ်ပါတယ်။ Batch မပြီးမချင်း နောက် poll မစပါဘူး။

ဒီအချက်က `RETRY_WORKER_LOCK_TTL_MS` နဲ့ ပတ်သက်ပြီး ပြဿနာ ဖြစ်နိုင်ပါတယ်:

| | တန်ဖိုး |
|---|---|
| Node-RED job တစ်ခု အဆိုးဆုံး ကြာချိန် | 30s × 3 ကြိမ် + delay ≈ **~၉၂ စက္ကန့်** |
| `RETRY_WORKER_BATCH_SIZE` | 20 |
| Batch တစ်ခုလုံး အဆိုးဆုံး ကြာချိန် | 20 × 92s ≈ **~၃၀ မိနစ်** |
| `RETRY_WORKER_LOCK_TTL_MS` | **၅ မိနစ်** |

Node-RED down နေပြီး job ၂၀ တစ်ပြိုင်နက် due ဖြစ်ရင် batch အနောက်ပိုင်းက job တွေရဲ့ lock က ၅ မိနစ်ကျော်ပြီး **stale** ဖြစ်သွားပါမယ်။ အဲဒီမှာ instance နှစ်ခုထက် ပိုရှိရင် တခြား instance က အဲဒီ job တွေကို reclaim လုပ်ပြီး **job တစ်ခုတည်းကို နှစ်နေရာက တစ်ပြိုင်နက် run** မိနိုင်ပါတယ်။ Kafka publish job ဆိုရင် message **နှစ်ခါ ထွက်** သွားနိုင်ပါတယ်။ ဒါကို rtutil ရဲ့ redelivery guard က အပိုင်းလိုက်ပဲ ကာကွယ်ပေးနိုင်ပါတယ်။

Production အတွက် အကြံပြုချက်တွေက:

```env
RETRY_WORKER_POLL_INTERVAL_MS=15000
RETRY_WORKER_BATCH_SIZE=3            # 3 × 92s ≈ 4.6m < lock TTL
RETRY_WORKER_LOCK_TTL_MS=300000
```

ဒါမှမဟုတ် batch ကို 20 ဆက်ထားချင်ရင် lock TTL ကို `batch × ~92s` ထက် ကြီးအောင် တိုးပါ။ ဥပမာ `2400000` (၄၀ မိနစ်) ပါ။ ဒါပေမယ့် worker တကယ် crash ဖြစ်ရင် job တွေ ၄၀ မိနစ်လောက် ပိတ်မိနေမှာမို့ **batch ကို လျှော့တာက ပိုကောင်းပါတယ်**။

Instance **တစ်ခုတည်း**နဲ့ run မယ်ဆိုရင်တော့ ဒီ double-run ပြဿနာ မဖြစ်ပါဘူး၊ `15000` ပြောင်းရုံနဲ့ လုံလောက်ပါတယ်။
