# Builds the QA test case coverage page: each row of the R2026-07 sheet with
# the Story ACs it checks, the unit tests that assert it and their last result.
# usage: gen_qa.py <sheet.html> <out.html>
import json, os, re, sys, subprocess, datetime, html as H
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from qa_map import M
from scenario_mm import S as SCEN_MM
HERE = os.path.dirname(os.path.abspath(__file__))
RT = os.path.abspath(os.path.join(HERE, '..', '..', '..'))
sheet, out = sys.argv[1], sys.argv[2]

subprocess.check_call([sys.executable, os.path.join(HERE, 'parse_r2026.py'), sheet, os.path.join(HERE, 'r2026.json')])
d = json.load(open(os.path.join(HERE, 'r2026.json')))

# ACs: title (stories.html) and coverage entry (cov_*.json)
stories = open(os.path.join(HERE, '..', 'stories.html'), encoding='utf-8').read()
ac_title = dict(re.findall(r'acid">(AC\d+)</span><span class="act">([^<]*)', stories))
cov = {}
for g in 'ABC':
    for c in json.load(open(os.path.join(HERE, f'cov_{g}.json'))):
        cov[c['ac']] = c

# last go test -json results
passed, failed = set(), set()
for n in ('noc', 'rtutil', 'core', 'report'):
    for l in open(os.path.join(HERE, f'gt_{n}.json')):
        try: e = json.loads(l)
        except ValueError: continue
        if e.get('Test') and e.get('Action') in ('pass', 'fail'):
            (passed if e['Action'] == 'pass' else failed).add(e['Test'])
def tstate(name):
    for k in (name.replace(' ', '_'), name.split('/')[0]):
        if k in failed: return 'fail'
        if k in passed: return 'pass'
    return 'unknown'

# where each test function lives
REPOS = [('noc_automation', 'NocAutomationCodeMerge/noc_automation'), ('rtutil', 'NocAutomationCodeMerge/remote-resolved-queue-transfer'),
         ('rtdatacore', 'rtdatacore'), ('pipeline_report', 'pipeline_report')]
where = {}
for repo, p in REPOS:
    o = subprocess.run(['grep', '-rn', '--include=*_test.go', '-E', r'^func Test\w+\(', os.path.join(RT, p)], capture_output=True, text=True).stdout
    for line in o.splitlines():
        f, ln, rest = line.split(':', 2)
        name = re.match(r'func (Test\w+)', rest).group(1)
        where.setdefault(name, dict(repo=repo, file=os.path.relpath(f, RT), line=int(ln)))

RANK = {'none': 0, 'partial': 1, 'covered': 2}
items, groups = [], []
for i, r in enumerate(d['rows']):
    if not (r.get('Description') or r.get('Expected Result')): continue
    m = M.get(i)
    if m is None: sys.exit(f'row {i} has no mapping: {r["Test Scenario"][:60]}')
    acs = [dict(id=a, title=ac_title.get(a, ''), status=cov[a]['status']) for a in m['acs']]
    names = m.get('tests')
    if names is None:
        tests = []
        for a in m['acs']:
            for t in cov[a]['tests']:
                if t['name'] not in [x['name'] for x in tests]:
                    tests.append(dict(repo=t['repo'], file=t['file'], line=t['line'], name=t['name'], asserts=t.get('asserts', ''), ac=a))
    else:
        tests = []
        for n in names:
            loc = where.get(n.split('/')[0])
            if not loc: sys.exit(f'row {i}: test {n} not found')
            asserts = next((t.get('asserts', '') for a in cov for t in cov[a]['tests'] if t['name'] == n), '')
            tests.append(dict(loc, name=n, asserts=asserts, ac=''))
    for t in tests: t['state'] = tstate(t['name'])
    status = m.get('status') or (min((a['status'] for a in acs), key=lambda s: RANK.get(s, 0)) if acs else 'none')
    fns = []
    for a in m['acs']:
        for f in cov[a].get('functions', []):
            if (f['file'], f['name']) not in [(x['file'], x['name']) for x in fns]: fns.append(f)
    group = ' '.join(r['Test Item'].split())
    scen = ' '.join(l.strip() for l in r['Test Scenario'].split('\n') if l.strip())
    if scen not in SCEN_MM: sys.exit(f'row {i}: no Burmese scenario in scenario_mm.py for: {scen}')
    if group not in groups: groups.append(group)
    items.append(dict(n=len(items) + 1, row=i, group=groups.index(group), scenario=r['Test Scenario'], scenario_mm=SCEN_MM[scen], desc=r['Description'],
        pre=r.get('Preconditions', ''), steps=r.get('Test Step', ''), data=r.get('Test Data', ''), expected=r.get('Expected Result', ''),
        notes=r.get('Notes', ''), actual=r.get('Actual Result', ''), exec=r.get('Execution Status', ''), env=r.get('Test Env', ''),
        flagged='s7' in r['_cls'], acs=acs, status=status, mm=m.get('mm', ''), en=m.get('en', ''), tests=tests, functions=fns[:10]))

title = d['title']
data = dict(generated=datetime.datetime.now().strftime('%Y-%m-%d %H:%M'), sheet=os.path.basename(sheet),
            meta=[l.strip() for l in title.split('\n') if l.strip()], groups=groups, items=items)
tpl = open(os.path.join(HERE, 'qa_tpl.html'), encoding='utf-8').read()
open(out, 'w', encoding='utf-8').write(tpl.replace('/*DATA*/null', json.dumps(data, ensure_ascii=False).replace('</', '<\\/')))
from collections import Counter
print(Counter(i['status'] for i in items), len(items))
