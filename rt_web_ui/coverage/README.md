# Coverage pages

Two pages, built from the same AC coverage data and test results:

| Page | Command (run in this folder) |
|---|---|
| `../coverage_ui.html` (served at `/coverage`): each Story AC and the unit tests that assert it | `python3 gen_coverage.py ../coverage_ui.html` |
| `../[RT] Roadmap Testcase-2/R2026-07_coverage.html`: each QA test case with its ACs and unit tests | `python3 gen_qa.py "../[RT] Roadmap Testcase-2/R2026-07.html" "../[RT] Roadmap Testcase-2/R2026-07_coverage.html"` |

The acceptance page (`/accept`) shows, for each case, the Story ACs it checks and their unit tests. After `gen_coverage.py`, run `python3 gen_accept_acs.py`: it writes `../accept_acs.json`, which the server embeds, so rebuild and restart rt_web_ui (`make up` after stopping it).

After the code or the tests change, run `./run_tests.sh` first. It refreshes the pass/fail results; the repos sit at `../../..` (the RT folder).

## Files
- `cov_A.json`, `cov_B.json`, `cov_C.json`: the coverage entry of each AC (status, how, gap, functions, tests with file:line). These are edited by hand: when a test is added or moves, update its entry here.
- `gt_*.json`: `go test -json` output of noc_automation, rtutil, rtdatacore and pipeline_report (written by `run_tests.sh`).
- `rm/*.json`, `parent.json`: the Redmine Story issues of Epic #22947 (AC text). `gen_stories.py rm ../stories.html` rebuilds the stories page from them.
- `mm.py`: Burmese text of the stories and ACs.
- `accept_map.py`: acceptance case → the Story ACs it checks. A case the server has but this file lacks fails rt_web_ui's tests (`TestAcceptACs_EveryCaseLinked`).
- `needs.py`: what each 🟡/❌ AC still needs to become ✅ (a test to write, a code change, or a PO decision). gen_coverage.py stops if a 🟡/❌ AC has no entry; remove an entry once the AC is ✅.
- `coverage_tpl.html`, `qa_tpl.html`: the page templates.
- QA page only:
  - `parse_r2026.py`: reads the Google Sheets HTML export.
  - `qa_map.py`: sheet row → ACs, tests, status and notes (my/en).
  - `scenario_mm.py`: Burmese text of each Test Scenario. A new scenario in the sheet must be added here.
  - `qa_smoke.js`: an optional check: `node qa_smoke.js <page.html>`.
