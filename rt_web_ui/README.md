# rt_web_ui

NOC automation ၏ RT Web UI အပိုင်းကို စမ်းဖို့ Go program ဖြစ်သည်။ Ticket JSON ကို RT extension လိုပဲ RT External API (`noc_automation`) သို့ တိုက်ရိုက် ပို့ပြီး API ၏ အဖြေကို ပြသည်။ Ticket ကို စစ်တာ (cooldown၊ active run၊ eligibility rule)၊ run ဖန်တီးတာနဲ့ `run_id` ထုတ်တာကို **API တစ်ခုတည်းက** လုပ်သည်။ rt_web_ui က `pipeline_runs` / `pipeline_run_events` ထဲ ဘာမှ မရေးပါ — ကိုယ်ပိုင် run row ဖွင့်ရင် ticket ၏ active-run lock ကို ကိုင်ထားပြီး API က ticket ကို skip လုပ်သွားမည်ဖြစ်သည်။

## Run ပုံ

```bash
cp .env.example .env   # သို့မဟုတ် --env noc_automation/.env ကို တိုက်ရိုက်သုံး
go run . send --file ticket.json --env .env

# Field override (test အတွက်)
go run . send --file criteria/tickets/30_transferred_ca1_broken.json --env .env \
    --set id=97 --set custom_fields.service_type="MNet Plus" --set custom_fields.opi_site_code=
```

| Flag | အဓိပ္ပာယ် |
|---|---|
| `--file` | RT Web UI ပို့မည့် ticket JSON (`-` = stdin) |
| `--env` | Config file (default `.env`) |
| `--set` | Field override — `queue=…` သို့မဟုတ် `custom_fields.<key>=…` (အကြိမ်ကြိမ် သုံးနိုင်) |

Payload ကို `id` (string) နဲ့ ပို့သည်။ `run_id` မပါ (sample file ထဲမှာ ပါရင်လည်း ဖယ်ပြီးမှ ပို့သည်)။

Exit code: `0` accepted · `1` not eligible / not started · `2` API rejected / unreachable · `3` usage / config / DB error

## API ၏ အဖြေ

| API ၏ အဖြေ | Outcome | ပြသမည့်စာ |
|---|---|---|
| 202 + `run_id` | `ACCEPTED` — API ၏ run ကို ~3s အထိ ဖတ်ပြီး state ပြ | Ticket Processing started |
| 202 + `run_id`၊ run က `NOT_ELIGIBLE` ဖြစ်သွား | `NOT_ELIGIBLE` | No Process: <API ၏ rule reason> |
| 202၊ `run_id` မပါ | `SKIPPED` — active run ရှိနေ (သို့) ၂၄ နာရီ cooldown ထဲ | Not started … |
| 4xx / 5xx | `REJECTED` | API ၏ error message |
| Connection error / timeout | `UNREACHABLE` | RT External API unreachable |

Eligibility rule (queue၊ status၊ CPE/local service ID၊ OPI၊ service type) ကို API ၏ `main.json` `ticket_eligible_criterias` က ဆုံးဖြတ်သည်။ `gates.go` ၏ `CheckEligibility` ကို acceptance page က info အဖြစ်သာ ပြသည်။

## Acceptance cases (`/accept`)

[noc_remote_resolve_tdd_acceptance.md](acceptance/noc_remote_resolve_tdd_acceptance.md) ၏ case **၈၁** ခုကို group ၇ ခု (Admission · MariaDB/Kafka/restart · BCS/RT · Stale work · BCS ရလဒ်/report · Sweeper/capacity · V2) အလိုက် browser ကနေ run သည်။

```bash
make up                      # API · rtutil · mock · criteria UI ကို start (UI က /accept ပါ serve)
open http://127.0.0.1:8091/accept
```

- Case တစ်ခုချင်း (checkbox) သို့မဟုတ် group တစ်ခုလုံးကို run နိုင်သည်။ Run နေစဉ် step timeline ကို live ပြသည်။ Job တစ်ကြိမ်မှာ တစ်ခုပဲ run သည် (case တွေက stack တစ်ခုတည်းကို မျှသုံး)။
- **Runnable (၂၃ ခု)** — real stack ကို အပြင်ကနေ (RT extension လို API ကို တိုက်ရိုက် POST) စမ်းပြီး system တစ်ခုချင်း ကိုယ်တိုင်သိမ်းထားတာကို စစ်သည်: `pipeline_runs` / events၊ RT ၏ `Tickets` / comment (run marker)၊ mock ၏ request log (BCS ခေါ်တဲ့ အကြိမ်)။ Case တိုင်း RT ticket အသစ် ဖန်တီးသည်။ Fault case (RCV-02, ACT-02) က `.run/rtutil.pid` ကို SIGKILL ပြီး `make up` ဖြင့် ပြန် start သည်။ Sweeper case (RCV-05, OPS-06, OPS-13) က synthetic row ထည့်ပြီး ပြီးရင် ပြန်ဖျက်သည်။
- **Skip (၅၈ ခု)** — API/rtutil ထဲက checkpoint crash၊ race barrier၊ Peppermint/RT contract၊ U/V profile စတာတွေမို့ ဒီကနေ မ run နိုင်။ Row ကို ဖွင့်ရင် အကြောင်းပြချက်နဲ့ တခြား repo က cover လုပ်တဲ့ test နာမည်ကို ပြသည်။
- Mock ၏ admin API token: `CRITERIA_MOCK_ADMIN_TOKEN` (default `rt-web-ui-accept`)။ Mock ဟောင်းကို token မပါဘဲ start ထားရင် `make criteria-mock-stop criteria-mock-up`။
- Spec file ကို ပြင်ရင် `accept_cases.go` ၏ `caseDefs` နဲ့ ကိုက်ရမည် (`go test -run TestAcceptCases`)။
