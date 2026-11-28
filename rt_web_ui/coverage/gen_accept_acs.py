# Writes ../accept_acs.json: for each acceptance case, the Story ACs it
# checks (accept_map.py) with their coverage status and unit tests, taken
# from the generated ../coverage_ui.html so both pages show the same thing.
# Run after gen_coverage.py.  usage: python3 gen_accept_acs.py
import json, os, sys
HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
from accept_map import A

page = open(os.path.join(HERE, '..', 'coverage_ui.html'), encoding='utf-8').read()
start = page.index('const DATA = ') + len('const DATA = ')
data = json.loads(page[start:page.index('\n', start)].rstrip(';'))
items = {i['ac']: i for i in data['items']}

missing = sorted({a for acs in A.values() for a in acs} - set(items))
if missing:
    sys.exit(f'accept_map.py names ACs the coverage page does not have: {missing}')

cases = {}
for case_id, acs in A.items():
    cases[case_id] = [dict(
        ac=a, story=items[a]['story'], title=items[a]['title'], mm=items[a]['mm'], status=items[a]['status'],
        tests=[dict(name=t.get('name', ''), repo=t.get('repo', ''), file=t.get('file', ''), line=t.get('line'),
                    state=t.get('state', 'unknown')) for t in items[a]['tests']],
    ) for a in acs]

out = os.path.join(HERE, '..', 'accept_acs.json')
json.dump(dict(generated=data['generated'], cases=cases), open(out, 'w', encoding='utf-8'), ensure_ascii=False, indent=1)
print(f'{len(cases)} cases, {sum(1 for v in cases.values() if v)} with ACs -> {os.path.relpath(out)}')
