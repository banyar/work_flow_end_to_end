const fs = require('fs');
const html = fs.readFileSync(process.argv[2], 'utf8');
const js = html.slice(html.indexOf('<script>') + 8, html.indexOf('</script>'));
global.els = {};
const el = id => els[id] || (els[id] = { id, innerHTML: '', textContent: '', hidden: false, placeholder: '', querySelectorAll: () => [], set onclick(f) {}, set oninput(f) {} });
global.document = { getElementById: el, querySelectorAll: () => [], documentElement: {} };
global.localStorage = { getItem: () => null, setItem() {} };
const run = new Function(js + `
  ;render();
  for (const l of ['my','en']) { lang = l; tab = 'all'; DATA.items.forEach(i => open.add(i.n)); render();
    for (let g = 0; g < DATA.groups.length; g++) { tab = String(g); render(); }
    for (const s of ST) { tab = 'all'; filt = s; render(); } filt = ''; q = 'AC11'; render(); q = ''; }
  return els;`);
const out = run();
const body = out.tbody.innerHTML;
console.log('rows rendered (last):', (body.match(/class="row/g) || []).length, 'summary ok:', out.summary.innerHTML.includes('QA'), 'no undefined:', !/undefined/.test(out.tbody.innerHTML + out.summary.innerHTML + out.tabs.innerHTML));
