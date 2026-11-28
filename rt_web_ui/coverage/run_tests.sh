#!/bin/bash
# Runs the unit tests of the four repos and saves the go test -json output
# (gt_*.json) that gen_coverage.py and gen_qa.py read for pass/fail.
cd "$(dirname "$0")"
RT=$(cd ../../.. && pwd)
# $3: packages to test (rtdatacore/tests needs a live DB, so only ./pkg/... there)
run() { echo "== $2"; (cd "$RT/$2" && go test -json -count=1 ${3:-./...} > "$OLDPWD/gt_$1.json"); }
run noc    NocAutomationCodeMerge/noc_automation
run rtutil NocAutomationCodeMerge/remote-resolved-queue-transfer
run core   rtdatacore ./pkg/...
run report pipeline_report
python3 - <<'PY'
import json
for n in ('noc', 'rtutil', 'core', 'report'):
    r = {'pass': 0, 'fail': 0}
    for l in open(f'gt_{n}.json'):
        try: e = json.loads(l)
        except ValueError: continue
        if e.get('Test') and '/' not in e['Test'] and e.get('Action') in r: r[e['Action']] += 1
    print(n, r)
PY
