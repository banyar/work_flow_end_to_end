# Remote Resolve Daily Summary Query

This query counts the **ticket runs that entered the Remote Resolve pipeline on one day**, grouped by state. It returns **a single row**, and each column is one count.

The example query counts runs created on `2026-10-14`, from `00:00:00` up to (but not including) `2026-10-15 00:00:00`.

---

## 1. Pipeline overview

Each time a ticket is assigned to the NOC queue, a **run** (one row) is opened in the `pipeline_runs` table. Three services then move that run forward in stages:

```
rt_web_ui  ──►  RT External API (noc_automation)  ──►  Kafka  ──►  rtutil (remote-resolved-queue-transfer)
 checks ticket   fetches CPE status, evaluates workflow           BCS update + RT ticket update
```

| Table | What it holds |
|---|---|
| `pipeline_runs` (`pr`) | One row per run. `current_state` is the stage the run is at now. `completed_at` is when the run finished (`NULL` while it is still running) |
| `pipeline_run_events` (`e`) | One row per state change of a run. `detail` (JSON) holds extra information, such as `http_status` |

---

## 2. What each state means

| State | Written by | Meaning |
|---|---|---|
| `NOT_ELIGIBLE` | rt_web_ui | The ticket does not meet the conditions, so it is not processed (missing CPE ID / Local Service ID, OPI customer, or Service Type not eligible) |
| `ALREADY_PROCESSED` | rt_web_ui | The ticket was already processed in the last 24 hours (no code writes this state yet) |
| `API_REJECTED` | rt_web_ui | The RT External API rejected the request with a 4xx/5xx status |
| `API_UNREACHABLE` | rt_web_ui | The RT External API could not be reached or timed out |
| `CPE_FETCH_RETRY_SCHEDULED` | noc_automation | Fetching the CPE status failed and a retry is scheduled |
| `CPE_NOT_FOUND` | noc_automation | The CPE could not be found |
| `KAFKA_PUBLISH_RETRY_SCHEDULED` | noc_automation | Publishing to Kafka failed and a retry is scheduled |
| `FAILED_PERMANENT` | noc_automation | Retries ran out; the run failed for good |
| `NORMALIZE_FAILED` | noc_automation (older versions) | The data could not be normalized. Current code no longer uses it, but it is still counted for older data |
| `CONSUME_VALIDATION_FAILED` | rtutil | The Kafka message failed validation (wrong queue, wrong root cause, etc.) |
| `SKIPPED` | rtutil | The ticket was already Pending/Resolved/Closed, so it was left unchanged |
| `RT_UPDATE_FAILED` | rtutil | Updating the RT ticket failed |
| `COMPLETED` | rtutil | The RT ticket update succeeded. **The run finished successfully** |

---

## 3. What each column means

### (a) Overall status

| Column | Counts | Meaning |
|---|---|---|
| `received` | All runs | Total runs that came in that day |
| `not_eligible` | `NOT_ELIGIBLE` + `ALREADY_PROCESSED` | Tickets that did not need processing |
| `in_progress` | `completed_at IS NULL` | All runs not finished yet (includes `retrying` and `stuck`) |
| `retrying` | Not finished + `*_RETRY_SCHEDULED` | Runs waiting for a retry. This is normal; they will move on shortly |
| `stuck` | Not finished + not retrying + **older than the cutoff time** | Runs that are stuck somewhere. **Needs investigation** (see the note below) |
| `finished` | States that entered the pipeline and then ended | `COMPLETED`, `SKIPPED`, `CONSUME_VALIDATION_FAILED`, `FAILED_PERMANENT`, `CPE_NOT_FOUND`, `RT_UPDATE_FAILED`, `NORMALIZE_FAILED` |

> **The `stuck` cutoff time:** `'2026-10-14 10:07:36'` is filled in by the program that runs the query (the current time minus some threshold). A run whose last update (`updated_at`, or `created_at` if that is empty) is earlier than this time is treated as "taking too long". If you run the query by hand, change this value as needed.

### (b) Successful runs (`COMPLETED`)

| Column | Meaning |
|---|---|
| `success` | All runs in `COMPLETED` |
| `remote_resolved` | Resolved remotely (`is_remote_resolved = 1`) |
| `not_remote_resolved` | Not resolved remotely (`is_remote_resolved` is `0` or `NULL`) |
| `kept_in_noc` | Not resolved remotely, and the ticket stayed in the NOC queue (both the original and the target queue are `Network Operation Center (NOC)`) |

`success` should equal `remote_resolved + not_remote_resolved`.

### (c) BCS result (remote-resolved tickets only)

| Column | Meaning |
|---|---|
| `bcs_ok` | The BCS update succeeded (`is_bcs_success = 1`) |
| `bcs_failed` | The BCS update failed (`is_bcs_success = 0`) |
| `bcs_pending` | Sent to BCS (`ref_bcs_process_id` is set), still waiting for the result (webhook / status poll) |

> A run can be `COMPLETED` even when BCS failed. The BCS result is not a state; it is recorded separately in the `is_bcs_success` column.

### (d) API-side problems (rt_web_ui → RT External API)

| Column | Meaning |
|---|---|
| `validation_api_400` | The API rejected the request with `400 Bad Request`: the data sent was invalid |
| `validation_rt_util` | rtutil failed to validate the Kafka message (`CONSUME_VALIDATION_FAILED`) |
| `api_error_409` | The API rejected the request with `409 Conflict`, usually because the same request came in again |
| `api_error_other` | The API rejected the request with some other error (5xx, etc.), or the http_status is unknown |
| `api_unreachable` | The API could not be reached |

### (e) Failures inside the pipeline

| Column | Meaning |
|---|---|
| `failed_permanent` | `FAILED_PERMANENT` + `NORMALIZE_FAILED` |
| `cpe_not_found` | The CPE was not found |
| `rt_update_failed` | The RT ticket update failed |
| `skipped` | The ticket was already finished, so it was skipped |

### (f) To check

| Column | Meaning |
|---|---|
| `unclassified` | Finished (`completed_at` is set), but the state is in none of the groups above. **This should be `0`**. If it is not, a new state may have appeared or the data may be wrong, so investigate |

---

## 4. How the query works

### Counting with `SUM(condition)`

In MySQL/MariaDB a condition returns `1` when true and `0` when false. So `SUM(pr.current_state = 'COMPLETED')` counts how many rows are `COMPLETED`. This way the table is **scanned only once** and every count comes out together.

### `COALESCE(SUM(...), 0)`

If there are no runs that day, `SUM` returns `NULL`. `COALESCE` turns that `NULL` into `0`.

### `COALESCE(column, default)` inside a condition

Comparing with `NULL` gives `NULL`, so the row is not counted. For example, `is_remote_resolved = 0` would miss rows where the value is `NULL`. Writing `COALESCE(pr.is_remote_resolved, 0) = 0` treats `NULL` as `0` so those rows are counted too.

### The `rej` subquery (HTTP status of API_REJECTED runs)

`pipeline_runs` has no HTTP status column. So the query reads `$.http_status` from the `detail` JSON of the `api_rejected` event in `pipeline_run_events`.

- `JSON_VALUE(e.detail, '$.http_status')`: reads the `http_status` value from the JSON
- `CAST(... AS UNSIGNED)`: converts it to a number
- `MAX(...)` + `GROUP BY e.run_id`: returns one row per run even if the run has several events
- The subquery also filters by date and `API_REJECTED`, so it does not scan the whole event table

Because it uses a `LEFT JOIN`, runs that were not rejected are still included (their `rej.http_status` is `NULL`).

### Date filter

```sql
WHERE pr.created_at >= '2026-10-14 00:00:00' AND pr.created_at < '2026-10-15 00:00:00'
```

Using `>=` and `<` instead of `BETWEEN` means a run at exactly midnight (`00:00:00`) is not counted on two days. To look at another day, change the dates in **both places**: the main query and the subquery.

---

## 5. What to watch in the result

1. **`stuck > 0`**: some runs are stuck. Check the service logs.
2. **`unclassified > 0`**: there is an unknown state. Tell the dev team.
3. **`api_unreachable` / `api_error_other`** high: the RT External API service may be down.
4. **`bcs_failed`** high: check the BCS (CPEMS) side.
5. **`bcs_pending`** not going down for a long time: the BCS webhook or status poll may not be working.
