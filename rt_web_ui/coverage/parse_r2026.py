import sys, json
from html.parser import HTMLParser
class P(HTMLParser):
    def __init__(s): super().__init__(); s.rows=[]; s.row=None; s.cell=None
    def handle_starttag(s,t,a):
        a=dict(a)
        if t=='tr': s.row=[]
        elif t=='td' and s.row is not None:
            s.cell=[]; s.meta=(int(a.get('colspan',1)),int(a.get('rowspan',1)),a.get('class',''))
        elif t=='br' and s.cell is not None: s.cell.append('\n')
    def handle_endtag(s,t):
        if t=='td' and s.cell is not None:
            s.row.append((''.join(s.cell).strip(),)+s.meta); s.cell=None
        elif t=='tr' and s.row is not None: s.rows.append(s.row); s.row=None
    def handle_data(s,d):
        if s.cell is not None: s.cell.append(d)
p=P(); p.feed(open(sys.argv[1],encoding='utf-8').read())
# build grid with rowspan carry
grid=[]; carry={}
for r in p.rows:
    out=[]; col=0; it=iter(r)
    cells=list(r); i=0
    while i<len(cells) or col in carry:
        if col in carry:
            v,left,cls=carry[col]; out.append((v,cls))
            if left<=1: del carry[col]
            else: carry[col]=(v,left-1,cls)
            col+=1; continue
        v,cs,rs,cls=cells[i]; i+=1
        for k in range(cs):
            out.append((v if k==0 else '',cls))
            if rs>1: carry[col]=(v,rs-1,cls)
            col+=1
    grid.append(out)
hdr=None; rows=[]
for g in grid:
    vals=[v for v,_ in g]
    if 'Test Item' in vals: hdr=vals; continue
    if hdr and any(vals): rows.append(g)
print(hdr, file=sys.stderr)
recs=[]
for g in rows:
    d={hdr[i]:g[i][0] for i in range(min(len(hdr),len(g))) if hdr[i]}
    d['_cls']=[c for _,c in g][:len(hdr)]
    recs.append(d)
json.dump({'title':[v for v,_ in grid[1] if v][0] if len(grid)>1 else '','header':hdr,'rows':recs},open(sys.argv[2],'w'),ensure_ascii=False,indent=1)
