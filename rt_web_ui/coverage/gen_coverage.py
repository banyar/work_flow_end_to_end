import json, re, os, sys, html, subprocess, datetime
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import mm
from needs import NEEDS
HERE = os.path.dirname(os.path.abspath(__file__))
RT = os.path.abspath(os.path.join(HERE, '..', '..', '..'))
out = sys.argv[1]

# stories and ACs from the Redmine snapshot
AC_RE = re.compile(r'^- `RT-NOC-AUTO-(AC\d+)` ([^:]+): (.*)$')
stories, acs = [], {}
for f in sorted(os.listdir(os.path.join(HERE, 'rm'))):
    d = json.load(open(os.path.join(HERE, 'rm', f)))['issue']
    if d['tracker']['name'] != 'Story': continue
    desc = d['description'].replace('\r', '')
    s = re.match(r'NOC-AUTO (S\d+)', desc).group(1)
    ids = []
    for line in desc.split('\n'):
        m = AC_RE.match(line.strip())
        if m:
            acs[m.group(1)] = dict(id=m.group(1), title=m.group(2), text=m.group(3), story=s, issue=d['id'])
            ids.append(m.group(1))
    stories.append(dict(s=s, id=d['id'], subject=d['subject'], mm=mm.TITLE[d['id']], acs=ids))
stories.sort(key=lambda x: int(x['s'][1:]))

cov = {}
for g in 'ABC':
    for c in json.load(open(os.path.join(HERE, f'cov_{g}.json'))):
        cov[c['ac']] = c
missing = [a for a in acs if a not in cov]
if missing: sys.exit(f'no coverage data for {missing}')

# test results from the go test -json runs
passed, failed = set(), set()
runs = {}
for n, repo in [('noc', 'noc_automation'), ('rtutil', 'remote-resolved-queue-transfer'), ('core', 'rtdatacore'), ('report', 'pipeline_report')]:
    p = f_ = 0
    for l in open(os.path.join(HERE, f'gt_{n}.json')):
        try: e = json.loads(l)
        except ValueError: continue
        t = e.get('Test')
        if not t or e.get('Action') not in ('pass', 'fail'): continue
        (passed if e['Action'] == 'pass' else failed).add(t)
        if '/' not in t:
            if e['Action'] == 'pass': p += 1
            else: f_ += 1
    runs[repo] = (p, f_)

def tstate(name):
    full = name.replace(' ', '_')
    if full in failed: return 'fail'
    if full in passed: return 'pass'
    top = name.split('/')[0].split(' ')[0]
    if name in failed or top in failed: return 'fail'
    if name in passed or top in passed: return 'pass'
    return 'unknown'

def gitinfo(path):
    try:
        b = subprocess.check_output(['git', '-C', path, 'rev-parse', '--abbrev-ref', 'HEAD'], text=True).strip()
        c = subprocess.check_output(['git', '-C', path, 'rev-parse', '--short', 'HEAD'], text=True).strip()
        dirty = subprocess.check_output(['git', '-C', path, 'status', '--porcelain', '--untracked-files=no'], text=True).strip()
        return f'{b}@{c}' + (' (modified)' if dirty else '')
    except Exception:
        return '?'
repos = [('noc_automation', 'NocAutomationCodeMerge/noc_automation'), ('rtutil', 'NocAutomationCodeMerge/remote-resolved-queue-transfer'),
         ('rtdatacore', 'rtdatacore'), ('pipeline_report', 'pipeline_report')]
env = [dict(name=n, git=gitinfo(os.path.join(RT, p)), run=runs.get(p.split('/')[-1], (0, 0))) for n, p in repos]

items = []
for st in stories:
    for a in st['acs']:
        c, x = cov[a], acs[a]
        items.append(dict(ac=a, story=st['s'], issue=st['id'], title=x['title'], text=x['text'], mm=mm.AC.get(a, ''),
            status=c['status'], how=c.get('how_mm', ''), gap=c.get('gap_mm', ''), note=c.get('note_en', ''),
            functions=c.get('functions', []), needs=NEEDS.get(a, []),
            tests=[dict(t, state=tstate(t.get('name', ''))) for t in c.get('tests', [])]))

missing_needs = [i['ac'] for i in items if i['status'] != 'covered' and not i['needs']]
if missing_needs: sys.exit(f'needs.py has nothing for {missing_needs}')
data = dict(generated=datetime.datetime.now().strftime('%Y-%m-%d %H:%M'), env=env,
            stories=[dict(s=s['s'], id=s['id'], subject=s['subject'], mm=s['mm']) for s in stories], items=items)
tpl = open(os.path.join(HERE, 'coverage_tpl.html')).read()
open(out, 'w').write(tpl.replace('/*DATA*/null', json.dumps(data, ensure_ascii=False).replace('</', '<\\/')))
from collections import Counter
print(Counter(i['status'] for i in items), len(items))
