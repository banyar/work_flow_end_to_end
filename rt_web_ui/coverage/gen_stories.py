import json, re, html, sys, os
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import mm
src, out = sys.argv[1], sys.argv[2]
parent = json.load(open(os.path.join(src, '..', 'parent.json')))['issue']
BASE = 'https://redmine-dev.frontiir.net/issues/'

def inline(s):
    s = html.escape(s, quote=False)
    s = re.sub(r'`([^`]+)`', r'<code>\1</code>', s)
    s = re.sub(r'\*\*([^*]+)\*\*', r'<strong>\1</strong>', s)
    s = re.sub(r'(https?://[^\s)<]+)', r'<a href="\1" target="_blank" rel="noopener">\1</a>', s)
    s = re.sub(r'(?<![\w/>#])#(2\d{4})\b', lambda m: f'<a href="{BASE}{m.group(1)}" target="_blank" rel="noopener">#{m.group(1)}</a>', s)
    return s

def md(text):
    outl, inlist = [], False
    for line in (text or '').replace('\r', '').split('\n'):
        t = line.strip()
        if t.startswith('- '):
            if not inlist: outl.append('<ul>'); inlist = True
            outl.append(f'<li>{inline(t[2:])}</li>')
            continue
        if inlist: outl.append('</ul>'); inlist = False
        if t: outl.append(f'<p>{inline(t)}</p>')
    if inlist: outl.append('</ul>')
    return '\n'.join(outl)

AC_RE = re.compile(r'^- `(RT-NOC-AUTO-AC\d+)` ([^:]+): (.*)$')
def section(desc, head):
    m = re.search(r'\*\*' + re.escape(head) + r'[^*]*\*\*(.*?)(?=\n\*\*[A-Z][^*\n]*:\*\*|\Z)', desc, re.S)
    return m.group(1).strip() if m else ''

items = []
for f in sorted(os.listdir(src)):
    d = json.load(open(os.path.join(src, f)))['issue']
    desc = (d.get('description') or '').replace('\r', '')
    acs = []
    for line in desc.split('\n'):
        m = AC_RE.match(line.strip())
        if m: acs.append(m.groups())
    rest = desc
    if acs:  # drop the AC block from the detail text; ACs get their own list
        rest = re.sub(r'\*\*ACs \(\d+\):\*\*\s*\n(\s*\n|\s*- `RT-NOC-AUTO-AC.*\n?)+', '', rest)
    sn = re.match(r'NOC-AUTO (S\d+)', desc)
    rest = re.sub(r'^NOC-AUTO S\d+\s*\n', '', rest)
    stmt = section(rest, 'Statement:')
    if stmt: rest = re.sub(r'\*\*Statement:\*\*.*?(?=\n\n|\Z)', '', rest, count=1, flags=re.S)
    items.append(dict(id=d['id'], type=d['tracker']['name'], subject=d['subject'], status=d['status']['name'],
        assignee=(d.get('assigned_to') or {}).get('name', '—'), done=d.get('done_ratio', 0),
        updated=d['updated_on'][:10], s=sn.group(1) if sn else '', stmt=stmt, acs=acs, rest=md(rest.strip())))

stories = sorted([i for i in items if i['type'] == 'Story'], key=lambda i: int(i['s'][1:] or 0))
tasks = sorted([i for i in items if i['type'] != 'Story'], key=lambda i: i['id'])
nac = sum(len(i['acs']) for i in stories)

def card(i):
    tag = i['s'] or i['type']
    acs = ''
    if i['acs']:
        acs = '<ol class="acs">' + ''.join(
            f'<li><span class="acid">{html.escape(a[0].replace("RT-NOC-AUTO-", ""))}</span>'
            f'<span class="act">{inline(a[1])}</span><span class="acm">{mm.AC.get(a[0].replace("RT-NOC-AUTO-", ""), "")}</span><span class="acd en">{inline(a[2])}</span></li>' for a in i['acs']) + '</ol>'
    search = html.escape(' '.join([str(i['id']), i['s'], i['subject'], i['stmt'], mm.TITLE.get(i['id'],''), mm.STORY.get(i['id'],''), ' '.join(mm.AC.get(a[0].replace('RT-NOC-AUTO-',''),'') for a in i['acs']), ' '.join(' '.join(a) for a in i['acs'])]).lower())
    return f'''<details class="card" data-kind="{i['type'].lower()}" data-q="{search}">
  <summary>
    <span class="tag {i['type'].lower()}">{tag}</span>
    <span class="title">{mm.TITLE.get(i['id'], '')}<span class="en t-en">{inline(i['subject'])}</span></span>
    <span class="meta"><a href="{BASE}{i['id']}" target="_blank" rel="noopener" onclick="event.stopPropagation()">#{i['id']}</a>
      <span class="status">{html.escape(i['status'])}</span>
      {f'<span class="count">{len(i["acs"])} AC</span>' if i['acs'] else ''}
      <span class="who">{html.escape(i['assignee'])}</span></span>
  </summary>
  <div class="body">
    <div class="mm"><b class="lbl">ရိုးရှင်းစွာ ပြောရရင်</b><p>{mm.STORY.get(i['id'], '')}</p></div>
    {f'<p class="stmt en">{inline(i["stmt"])}</p>' if i['stmt'] else ''}
    {acs}
    <details class="rest-wrap"><summary>Redmine မူရင်း အသေးစိတ် (Notes, Open points, Implementation status)</summary><div class="rest">{i['rest']}</div></details>
    <p class="foot">ပြီးစီးမှု {i['done']}% · Assignee {html.escape(i['assignee'])} · နောက်ဆုံး update {i['updated']}</p>
  </div>
</details>'''

page = f'''<!doctype html>
<html lang="my"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>NOC-AUTO Stories (မြန်မာ)</title>
<style>
:root {{ --bg:#f6f7f9; --card:#fff; --fg:#1d2330; --muted:#5d6678; --line:#dde1e8; --accent:#2457c5;
  --story:#e6eefc; --story-fg:#1e4aa8; --task:#eef0f3; --task-fg:#4a5262; --code:#f0f2f5; }}
@media (prefers-color-scheme: dark) {{ :root:not([data-theme="light"]) {{ --bg:#14171d; --card:#1c2028; --fg:#e4e7ee;
  --muted:#9aa3b5; --line:#2c323d; --accent:#7ea6ff; --story:#22304d; --story-fg:#a9c3ff; --task:#2a2f39; --task-fg:#c1c7d3; --code:#262b35; }} }}
:root[data-theme="dark"] {{ --bg:#14171d; --card:#1c2028; --fg:#e4e7ee; --muted:#9aa3b5; --line:#2c323d; --accent:#7ea6ff;
  --story:#22304d; --story-fg:#a9c3ff; --task:#2a2f39; --task-fg:#c1c7d3; --code:#262b35; }}
* {{ box-sizing:border-box }}
body {{ margin:0; background:var(--bg); color:var(--fg); font:15px/1.55 system-ui,-apple-system,"Segoe UI",sans-serif }}
main {{ max-width:980px; margin:0 auto; padding:28px 16px 60px }}
a {{ color:var(--accent) }}
h1 {{ font-size:1.5rem; margin:0 0 4px }}
h2 {{ font-size:1.05rem; margin:30px 0 10px; color:var(--muted); text-transform:uppercase; letter-spacing:.04em }}
.sub {{ color:var(--muted); margin:0 0 18px }}
.stats {{ display:flex; gap:10px; flex-wrap:wrap; margin-bottom:16px }}
.stat {{ background:var(--card); border:1px solid var(--line); border-radius:10px; padding:10px 14px; min-width:110px }}
.stat b {{ display:block; font-size:1.4rem }} .stat span {{ color:var(--muted); font-size:.85rem }}
.bar {{ display:flex; gap:8px; flex-wrap:wrap; margin-bottom:6px; position:sticky; top:0; background:var(--bg); padding:8px 0; z-index:1 }}
.bar input {{ flex:1; min-width:200px; padding:8px 12px; border:1px solid var(--line); border-radius:8px; background:var(--card); color:var(--fg); font:inherit }}
.bar button {{ padding:8px 12px; border:1px solid var(--line); border-radius:8px; background:var(--card); color:var(--fg); cursor:pointer; font:inherit }}
.bar button.on {{ border-color:var(--accent); color:var(--accent) }}
.card {{ background:var(--card); border:1px solid var(--line); border-radius:10px; margin-bottom:8px }}
.card summary {{ list-style:none; cursor:pointer; padding:12px 14px; display:grid; grid-template-columns:auto 1fr; gap:4px 12px; align-items:start }}
.card summary::-webkit-details-marker {{ display:none }}
.tag {{ font-weight:600; font-size:.8rem; padding:2px 8px; border-radius:6px; min-width:46px; text-align:center; margin-top:2px }}
.tag.story {{ background:var(--story); color:var(--story-fg) }} .tag.task {{ background:var(--task); color:var(--task-fg) }}
.title {{ font-weight:600 }}
.meta {{ grid-column:2; display:flex; gap:12px; flex-wrap:wrap; color:var(--muted); font-size:.85rem }}
.body {{ padding:0 14px 14px; border-top:1px solid var(--line) }}
.stmt {{ font-style:italic; margin:12px 0; padding-left:10px; border-left:3px solid var(--accent) }}
.acs {{ list-style:none; padding:0; margin:12px 0 }}
.acs li {{ display:grid; grid-template-columns:52px 1fr; gap:0 10px; padding:8px 0; border-bottom:1px dashed var(--line) }}
.acid {{ font:600 .8rem ui-monospace,monospace; color:var(--accent); grid-row:span 2; padding-top:2px }}
.act {{ font-weight:600 }} .acd {{ color:var(--muted) }}
.rest p, .rest li {{ margin:6px 0 }} .rest ul {{ padding-left:20px }}
code {{ background:var(--code); padding:1px 5px; border-radius:4px; font-size:.88em; word-break:break-word }}
.foot {{ color:var(--muted); font-size:.82rem; margin:10px 0 0 }}
.body a {{ word-break:break-all }}
.hidden {{ display:none }}
.en {{ display:block; color:var(--muted); font-size:.86rem }}
body.hide-en .en {{ display:none }}
.title .en {{ font-weight:400; margin-top:2px }}
.mm {{ margin:12px 0; padding:10px 12px; background:var(--story); border-radius:8px }}
.mm p {{ margin:4px 0 0 }} .lbl {{ font-size:.82rem; color:var(--story-fg) }}
.acm {{ display:block }}
.acs li {{ grid-template-columns:52px 1fr }} .acid {{ grid-row:span 3 }}
.rest-wrap {{ margin-top:10px }} .rest-wrap > summary {{ cursor:pointer; color:var(--accent); font-size:.88rem }}
.intro {{ background:var(--card); border:1px solid var(--line); border-radius:10px; padding:12px 16px; margin-bottom:16px }}
.intro p {{ margin:6px 0 }} .intro ol {{ margin:6px 0; padding-left:22px }}

</style></head><body><main>
<h1>NOC Ticket Auto-Processing — Story များ</h1>
<p class="sub">Epic ရဲ့ Subtask များ — <a href="{BASE}{parent['id']}" target="_blank" rel="noopener">#{parent['id']} {html.escape(parent['subject'])}</a> · Redmine snapshot (Epic update {parent['updated_on'][:10]})</p>
<div class="intro">
  <p><b>ဒီ Epic က ဘာလဲ။</b> {mm.EPIC}</p>
  <p><b>ဘယ်လို ဖတ်ရမလဲ။</b></p>
  <ol>
    <li><b>Story</b> (S1–S11) — user လိုအပ်ချက်တစ်ခု။ နှိပ်ရင် မြန်မာ ရှင်းပြချက်၊ AC စာရင်းနဲ့ Redmine မူရင်း အသေးစိတ်ကို ပြတယ်။</li>
    <li><b>AC</b> (Acceptance Criteria, RT-NOC-AUTO-ACxx) — Story ပြီးပြီလို့ ပြောနိုင်ဖို့ ဖြစ်ရမယ့် အချက်။ QA က ဒါကနေ test case ထုတ်တယ်။</li>
    <li><b>Task</b> — Story မဟုတ်တဲ့ အလုပ် (design review၊ contract အတည်ပြု၊ QA၊ ticket အဟောင်း ရှင်း)။</li>
    <li>အစဉ် — S1 + S2 က walking skeleton (အရင်ဆောက်)၊ ပြီးမှ S4 နဲ့ S9၊ ပြီးမှ ကျန်တာ။</li>
  </ol>
</div>
<div class="stats">
  <div class="stat"><b>{len(stories)}</b><span>Story</span></div>
  <div class="stat"><b>{nac}</b><span>Acceptance criteria (AC)</span></div>
  <div class="stat"><b>{len(tasks)}</b><span>Task</span></div>
  <div class="stat"><b>{len([i for i in items if i['status']=='New'])}/{len(items)}</b><span>Status “New” ဖြစ်နေ</span></div>
</div>
<div class="bar">
  <input id="q" type="search" placeholder="ID၊ S နံပါတ်၊ AC နံပါတ် ဒါမှမဟုတ် စာသားနဲ့ ရှာပါ…">
  <button data-f="all" class="on">အားလုံး</button><button data-f="story">Story</button><button data-f="task">Task</button>
  <button id="toggle">အားလုံး ဖွင့်</button><button id="en">English မူရင်း ဖျောက်</button>
</div>
<h2 id="h-story">Story များ (S1–S{len(stories)})</h2>
{''.join(card(i) for i in stories)}
<h2 id="h-task">Task များ</h2>
{''.join(card(i) for i in tasks)}
</main>
<script>
const cards=[...document.querySelectorAll('.card')], q=document.getElementById('q');
let f='all';
function apply(){{const s=q.value.trim().toLowerCase();
  cards.forEach(c=>c.classList.toggle('hidden',(f!=='all'&&c.dataset.kind!==f)||(s&&!c.dataset.q.includes(s))));
  ['story','task'].forEach(k=>document.getElementById('h-'+k).classList.toggle('hidden',!cards.some(c=>c.dataset.kind===k&&!c.classList.contains('hidden'))));}}
q.addEventListener('input',apply);
document.querySelectorAll('[data-f]').forEach(b=>b.onclick=()=>{{f=b.dataset.f;document.querySelectorAll('[data-f]').forEach(x=>x.classList.toggle('on',x===b));apply();}});
const t=document.getElementById('toggle');
t.onclick=()=>{{const open=t.textContent==='အားလုံး ဖွင့်';cards.forEach(c=>c.open=open);t.textContent=open?'အားလုံး ပိတ်':'အားလုံး ဖွင့်';}};
const e=document.getElementById('en');
e.onclick=()=>{{const h=document.body.classList.toggle('hide-en');e.textContent=h?'English မူရင်း ပြ':'English မူရင်း ဖျောက်';}};
</script></body></html>'''
open(out, 'w').write(page)
print(len(stories), 'stories', nac, 'ACs', len(tasks), 'tasks')
for i in stories: print(i['s'], i['id'], len(i['acs']), bool(i['stmt']))
