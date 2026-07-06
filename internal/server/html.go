package server

import "net/http"

// handleForm serves the browser annotation form. It is deliberately minimal (no
// styling framework): a locus input, the known annotations as checkboxes fetched
// from /ui/annotations (defaults pre-checked), and vanilla JS that submits a job,
// polls its status, and renders the result as a tall (long) table.
func (s *Server) handleForm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(formHTML))
}

const formHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>cganno — annotate a locus</title>
<style>
  body { font-family: system-ui, sans-serif; margin: 2rem; max-width: 900px; }
  h1 { font-size: 1.3rem; }
  fieldset { margin: 1rem 0; }
  label.ann { display: block; font-size: 0.9rem; }
  .src { margin: 0.5rem 0; }
  .src > strong { font-size: 0.85rem; color: #444; }
  input[type=text] { width: 22rem; padding: 0.3rem; font-family: monospace; }
  button { padding: 0.4rem 0.9rem; }
  table { border-collapse: collapse; margin-top: 1rem; }
  th, td { border: 1px solid #ccc; padding: 0.3rem 0.6rem; text-align: left; vertical-align: top; }
  th { background: #f4f4f4; }
  #status { margin-top: 1rem; font-style: italic; color: #555; }
  .err { color: #b00; }
  code { background: #f4f4f4; padding: 0 0.2rem; }
</style>
</head>
<body>
<h1>cganno — annotate a locus</h1>
<p>Enter a variant locus as <code>chrom:pos:ref:alt</code> (POS is 1-based), choose annotations, and submit.</p>

<form id="form">
  <input type="text" id="locus" name="locus" placeholder="chr1:115256529:T:C" autofocus>
  <button type="submit">Annotate</button>
  <fieldset id="annset">
    <legend>Annotations</legend>
    <div id="anns">loading…</div>
  </fieldset>
</form>

<div id="status"></div>
<div id="results"></div>

<script>
const $ = (id) => document.getElementById(id);

async function loadAnnotations() {
  const r = await fetch('/ui/annotations');
  const data = await r.json();
  const box = $('anns');
  box.innerHTML = '';
  for (const src of data.sources) {
    if (!src.annotations.length) continue;
    const div = document.createElement('div');
    div.className = 'src';
    const v = src.version ? (':' + src.version) : '';
    div.innerHTML = '<strong>' + src.name + v + ' (' + src.type + ')</strong>';
    for (const a of src.annotations) {
      const label = document.createElement('label');
      label.className = 'ann';
      const cb = document.createElement('input');
      cb.type = 'checkbox';
      cb.value = a.name;
      cb.className = 'annbox';
      cb.checked = a.default;
      label.appendChild(cb);
      const desc = a.description ? (' — ' + a.description) : '';
      label.appendChild(document.createTextNode(' ' + a.name + desc));
      div.appendChild(label);
    }
    box.appendChild(div);
  }
}

function selectedAnnotations() {
  return Array.from(document.querySelectorAll('.annbox'))
    .filter(cb => cb.checked).map(cb => cb.value);
}

function renderTable(variants) {
  const box = $('results');
  box.innerHTML = '';
  for (const v of variants) {
    const h = document.createElement('h3');
    h.textContent = v.chrom + ':' + v.pos + ':' + v.ref + ':' + v.alt;
    box.appendChild(h);
    const table = document.createElement('table');
    table.innerHTML = '<tr><th>annotation</th><th>value</th></tr>';
    const keys = Object.keys(v.annotations);
    if (!keys.length) {
      const tr = document.createElement('tr');
      tr.innerHTML = '<td colspan="2"><em>no annotations selected</em></td>';
      table.appendChild(tr);
    }
    for (const k of keys) {
      const tr = document.createElement('tr');
      const val = v.annotations[k];
      const cell = (val === null || val === undefined) ? '' : String(val);
      const td1 = document.createElement('td'); td1.textContent = k;
      const td2 = document.createElement('td'); td2.textContent = cell;
      tr.appendChild(td1); tr.appendChild(td2);
      table.appendChild(tr);
    }
    box.appendChild(table);
  }
}

async function poll(jobId) {
  for (;;) {
    const r = await fetch('/ui/jobs/' + jobId);
    const job = await r.json();
    if (job.status === 'done') {
      $('status').textContent = 'Done (' + job.n_variants + ' variant(s)).';
      const rr = await fetch('/ui/jobs/' + jobId + '/results');
      renderTable(await rr.json());
      return;
    }
    if (job.status === 'error') {
      $('status').innerHTML = '<span class="err">Job failed: ' + (job.error || 'unknown error') + '</span>';
      return;
    }
    $('status').textContent = 'Status: ' + job.status + '…';
    await new Promise(res => setTimeout(res, 500));
  }
}

$('form').addEventListener('submit', async (e) => {
  e.preventDefault();
  $('results').innerHTML = '';
  $('status').textContent = 'Submitting…';
  const body = { locus: $('locus').value.trim(), annotations: selectedAnnotations() };
  const r = await fetch('/ui/submit', {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
  });
  const data = await r.json();
  if (!r.ok) {
    $('status').innerHTML = '<span class="err">' + (data.error || 'submit failed') + '</span>';
    return;
  }
  poll(data.job_id);
});

loadAnnotations();
</script>
</body>
</html>
`
