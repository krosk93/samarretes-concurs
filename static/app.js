const q = document.getElementById('q');
const list = document.getElementById('list');
const countEl = document.getElementById('count');
const statusEl = document.getElementById('status');

let timer = null;
let people = [];

function showStatus(msg, isError) {
  statusEl.textContent = msg;
  statusEl.hidden = !msg;
  statusEl.classList.toggle('error', !!isError);
}

function escapeHtml(s) {
  return String(s ?? '').replace(/[&<>"']/g, (c) => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
  }[c]));
}

function card(p) {
  const li = document.createElement('li');
  li.className = 'card' + (p.recollida ? ' done' : '');
  li.dataset.row = p.row;

  const badge = p.talla ? `<span class="size">${escapeHtml(p.talla)}</span>` : '';
  const meta = [p.alies, p.colla].filter(Boolean).map(escapeHtml).join(' · ');
  const status = p.recollida
    ? '<span class="pill ok">✓ Recollida</span>'
    : '<span class="pill pending">Pendent</span>';
  const btn = p.recollida
    ? '<button class="btn undo" data-action="unmark">Desmarca</button>'
    : '<button class="btn" data-action="mark">Marca recollida</button>';

  li.innerHTML = `
    <div class="top">
      <div>
        <div class="nom">${escapeHtml(p.nom || '(sense nom)')}</div>
        ${meta ? `<div class="meta">${meta}</div>` : ''}
      </div>
      ${badge}
    </div>
    <div class="bottom">${status}${btn}</div>`;
  return li;
}

function render(items) {
  list.innerHTML = '';
  const frag = document.createDocumentFragment();
  for (const p of items) frag.appendChild(card(p));
  list.appendChild(frag);
  countEl.textContent = items.length === 1 ? '1 persona' : `${items.length} persones`;
}

async function fetchJSON(url, opts) {
  const res = await fetch(url, opts);
  if (!res.ok) {
    let msg = `Error ${res.status}`;
    try {
      const data = await res.json();
      if (data.error) msg = data.error;
    } catch { /* ignore */ }
    throw new Error(msg);
  }
  return res.json();
}

async function search() {
  const query = q.value.trim();
  showStatus('', false);
  try {
    const url = query ? `/api/search?q=${encodeURIComponent(query)}` : '/api/people';
    people = await fetchJSON(url);
    render(people);
  } catch (e) {
    showStatus(`No s'ha pogut carregar: ${e.message}`, true);
  }
}

list.addEventListener('click', async (ev) => {
  const btn = ev.target.closest('button[data-action]');
  if (!btn) return;
  const li = btn.closest('li.card');
  const row = Number(li.dataset.row);
  const recollida = btn.dataset.action === 'mark';
  btn.disabled = true;
  try {
    const updated = await fetchJSON('/api/toggle', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ row, recollida }),
    });
    people = people.map((p) => (p.row === updated.row ? updated : p));
    render(people);
  } catch (e) {
    showStatus(`No s'ha pogut desar: ${e.message}`, true);
  } finally {
    btn.disabled = false;
  }
});

q.addEventListener('input', () => {
  clearTimeout(timer);
  timer = setTimeout(search, 200);
});

search();
