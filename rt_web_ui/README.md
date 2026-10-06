# rt_web_ui

NOC automation ၏ RT Web UI အပိုင်း ([final.html](./final.html) section 1, step 1–5) ကို လုပ်ဆောင်သော Go program ဖြစ်သည်။ NOC queue သို့ ရောက်လာသော ticket ၏ JSON ကို section 1 gate များဖြင့် စစ်ပြီး RT External API (`noc_automation`) သို့ ပို့ကာ run ၏ state နှင့် event များကို `pipeline_runs` / `pipeline_run_events` တွင် မှတ်တမ်းတင်သည်။

## Run ပုံ

```bash
cp rt_web_ui/.env.example rt_web_ui/.env   # သို့မဟုတ် --env noc_automation/.env ကို တိုက်ရိုက်သုံး
go run ./rt_web_ui send --file ticket.json --env rt_web_ui/.env

# Field override (test အတွက်)
go run ./rt_web_ui send --file rt_web_ui/rt_web_api_payload_end_to_end.json --env noc_automation/.env \
    --set id=97 --set custom_fields.service_type="MNet Plus" --set custom_fields.opi_site_code=
```

| Flag | အဓိပ္ပာယ် |
|---|---|
| `--file` | RT Web UI ပို့မည့် ticket JSON (`-` = stdin) |
| `--env` | Config file (default `.env`) |
| `--run-id` | UUID အသစ်အစား သတ်မှတ်ထားသော `run_id` ကို သုံး |
| `--set` | Field override — `queue=…` သို့မဟုတ် `custom_fields.<key>=…` (အကြိမ်ကြိမ် သုံးနိုင်) |

Exit code: `0` accepted · `1` not eligible · `2` API rejected / unreachable · `3` usage / config / DB error

## Flow

| အဆင့် | စစ်ဆေးချက် | Fail ဖြစ်ပါက |
|---|---|---|
| 1 | Ticket queue = `RT_WEB_UI_NOC_QUEUE` | `NOT_ELIGIBLE` · `not_in_noc_queue` |
| 2 | Ticket `status` သည် `RT_WEB_UI_ALLOWED_TICKET_STATUSES` (default `new`, `in_progress`, `re-open`, `re-open-1` … `re-open-5`) ထဲတွင် ပါရမည် | `NOT_ELIGIBLE` · `ticket_status_not_allowed` |
| 3 | `cpe_id` နှင့် `local_service_id` ပါဝင်ရမည် | `NOT_ELIGIBLE` · `cpe_or_local_service_id_missing` |
| 4 | OPI field (`RT_WEB_UI_OPI_FIELDS`) အလွတ်ဖြစ်ရမည် | `NOT_ELIGIBLE` · `opi_customer_excluded` |
| 5 | `service_type` သည် `RT_WEB_UI_ELIGIBLE_SERVICE_TYPES` ထဲတွင် ပါရမည် | `NOT_ELIGIBLE` · `service_type_not_eligible` |
| 6 | Payload (`id` string, `run_id`) ကို RT External API သို့ POST | အောက်ပါ table |

| API ၏ အဖြေ | State | Event | ပြသမည့်စာ |
|---|---|---|---|
| 200 | API က `RECEIVED` သို့ ဆက်ယူသည် (rt_web_ui က state မရေး) | `api_accepted` | Ticket Processing started |
| 400 / 409 / 500 | `API_REJECTED` | `api_rejected` | API ၏ error message |
| Connection error / timeout | `API_UNREACHABLE` | `api_unreachable` | RT External API unreachable |

## State များ (rtdatacore, component `rt_web_ui`)

```
"" ─┬─▶ NOT_ELIGIBLE ■
    └─▶ SUBMITTED_TO_API ─┬─▶ API_REJECTED ■
                          ├─▶ API_UNREACHABLE ■
                          └─▶ RECEIVED (RT External API) ─▶ FETCHING_CPE_STATUS ─▶ … ─▶ COMPLETED ■
```

- Run ကို rt_web_ui က state မပါဘဲ (`""`) ဖွင့်ပြီး ပထမ event သည် `""` ─▶ `NOT_ELIGIBLE` သို့မဟုတ် `""` ─▶ `SUBMITTED_TO_API` ဖြစ်သည်။ RT External API သည် run ကို `SUBMITTED_TO_API` မှ `RECEIVED` သို့ ဆက်ယူသည် (event `api_received`)။ ထို့ကြောင့် ticket တစ်ခု၏ journey တစ်ခုလုံးကို `run_id` တစ်ခုတည်းဖြင့် ကြည့်နိုင်သည်။
- API ကို 200 ဖြင့် ပြန်လာချိန်တွင် API ၏ background processing သည် ရှေ့ရောက်နေနိုင်သဖြင့် rt_web_ui က state ကို မရေးဘဲ event သာ ထည့်သည်။
- `ALREADY_PROCESSED` (step 9–10 Redis 24h) ကို state အဖြစ် ကြိုသတ်မှတ်ထားသော်လည်း writer မရှိသေးပါ။
