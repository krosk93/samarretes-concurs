const q = document.getElementById('q');
const list = document.getElementById('list');
const countEl = document.getElementById('count');
const statusEl = document.getElementById('status');
const amagaEl = document.getElementById('amaga');
const toolbar = document.getElementById('toolbar');

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

function plural(n, one, many) {
  return n === 1 ? one : many;
}

function card(p) {
  const li = document.createElement('li');
  li.className = 'card' + (p.recollida ? ' done' : '') + (p.acompanya ? ' acompanya' : '');
  li.dataset.row = p.row;

  const badge = p.talla ? `<span class="size">${escapeHtml(p.talla)}</span>` : '';
  const meta = [p.alies, p.colla].filter(Boolean).map(escapeHtml).join(' · ');
  const status = p.recollida
    ? '<span class="pill ok">✓ Recollida</span>'
    : '<span class="pill pending">Pendent</span>';
  const acomp = p.acompanya
    ? `<span class="pill acomp" title="${escapeHtml(p.modalitat || '')}">⚠ Acompanyant — sense pinya</span>`
    : '';
  const btn = p.recollida
    ? '<button class="btn undo" data-action="unmark">Desmarca</button>'
    : '<button class="btn" data-action="mark">Marca recollida</button>';

  li.innerHTML = `
    <div class="top">
      <div class="who">
        <div class="nom">${escapeHtml(p.nom || '(sense nom)')}</div>
        ${meta ? `<div class="meta">${meta}</div>` : ''}
      </div>
      ${badge}
    </div>
    <div class="bottom">
      <div class="pills">${status}${acomp}</div>
      ${btn}
    </div>`;
  return li;
}

function emptyCard(msg) {
  const li = document.createElement('li');
  li.className = 'card empty';
  li.textContent = msg;
  return li;
}

function countText(shown, total) {
  if (shown === total) return `${total} ${plural(total, 'persona', 'persones')}`;
  const hidden = total - shown;
  return `${shown} de ${total} ${plural(total, 'persona', 'persones')} · ${hidden} ${plural(hidden, 'amagat', 'amagats')}`;
}

function visiblePeople() {
  return amagaEl.checked ? people.filter((p) => !p.acompanya) : people;
}

function render() {
  const total = people.length;
  const items = visiblePeople();
  list.innerHTML = '';
  if (items.length === 0 && total > 0) {
    list.appendChild(emptyCard(
      amagaEl.checked
        ? 'Tots els resultats són acompanyants. Desmarca «Amaga acompanyants» per veure\'ls.'
        : 'Cap resultat.',
    ));
  } else {
    const frag = document.createDocumentFragment();
    for (const p of items) frag.appendChild(card(p));
    list.appendChild(frag);
  }
  countEl.textContent = countText(items.length, total);
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
    render();
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
    render();
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

amagaEl.addEventListener('change', render);

function syncStuck() {
  toolbar.classList.toggle('stuck', window.scrollY > 4);
}
window.addEventListener('scroll', syncStuck, { passive: true });

search();
syncStuck();
