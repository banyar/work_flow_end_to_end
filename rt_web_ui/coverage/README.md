# Coverage pages

Two pages, built from the same AC coverage data and test results:

| Page | Command (run in this folder) |
|---|---|
| `../coverage_ui.html` (served at `/coverage`): each Story AC and the unit tests that assert it | `python3 gen_coverage.py ../coverage_ui.html` |
| `../[RT] Roadmap Testcase-2/R2026-07_coverage.html`: each QA test case with its ACs and unit tests | `python3 gen_qa.py "../[RT] Roadmap Testcase-2/R2026-07.html" "../[RT] Roadmap Testcase-2/R2026-07_coverage.html"` |

After the code or the tests change, run `./run_tests.sh` first. It refreshes the pass/fail results; the repos sit at `../../..` (the RT folder).

## Files
- `cov_A.json`, `cov_B.json`, `cov_C.json`: the coverage entry of each AC (status, how, gap, functions, tests with file:line). These are edited by hand: when a test is added or moves, update its entry here.
- `gt_*.json`: `go test -json` output of noc_automation, rtutil, rtdatacore and pipeline_report (written by `run_tests.sh`).
- `rm/*.json`, `parent.json`: the Redmine Story issues of Epic #22947 (AC text). `gen_stories.py rm ../stories.html` rebuilds the stories page from them.
- `mm.py`: Burmese text of the stories and ACs.
- `coverage_tpl.html`, `qa_tpl.html`: the page templates.
- QA page only:
  - `parse_r2026.py`: reads the Google Sheets HTML export.
  - `qa_map.py`: sheet row → ACs, tests, status and notes (my/en).
  - `scenario_mm.py`: Burmese text of each Test Scenario. A new scenario in the sheet must be added here.
  - `qa_smoke.js`: an optional check: `node qa_smoke.js <page.html>`.
