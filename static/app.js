const q = document.getElementById('q');
const list = document.getElementById('list');
const countEl = document.getElementById('count');
const statusEl = document.getElementById('status');
const amagaEl = document.getElementById('amaga');
const toolbar = document.getElementById('toolbar');

const NO_SHIRT = 'Ja tinc una samarreta dels Xiquets del Serrallo';

// Stored values (option value = what goes to the API) and their display labels.
const SIZE_OPTIONS = [
  { value: 'S', label: 'S' },
  { value: 'M', label: 'M' },
  { value: 'L', label: 'L' },
  { value: 'XL', label: 'XL' },
  { value: 'XXL', label: 'XXL' },
  { value: NO_SHIRT, label: 'No vull samarreta' },
];

let timer = null;
let people = [];
let openSizeRow = null; // row with the inline size editor open (only one at a time)

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

function isNoShirt(t) {
  return String(t ?? '').trim().toLowerCase() === NO_SHIRT.toLowerCase();
}

function sizeBadge(t) {
  if (!t) return '';
  if (isNoShirt(t)) {
    return `<span class="size none" title="${escapeHtml(t)}">No vull samarreta</span>`;
  }
  return `<span class="size">${escapeHtml(t)}</span>`;
}

function sizeOptionsHtml(current) {
  const known = SIZE_OPTIONS.some((o) => o.value === current);
  let html = SIZE_OPTIONS.map((o) => (
    `<option value="${escapeHtml(o.value)}"${o.value === current ? ' selected' : ''}>${escapeHtml(o.label)}</option>`
  )).join('');
  if (current && !known) {
    // Unknown value already in the sheet: keep it visible and preselected.
    html = `<option value="${escapeHtml(current)}" selected>${escapeHtml(current)} (actual)</option>` + html;
  }
  return html;
}

function normAlias(s) {
  return String(s ?? '').normalize('NFC').trim().toLowerCase().replace(/\s+/g, ' ');
}

// "Alies final" row: hidden when empty; when it equals the current alias we show
// a quiet confirmation chip instead of repeating the same text.
function aliesFinalHtml(p) {
  const final = String(p.aliesFinal ?? '').trim();
  if (!final) return { meta: '', line: '' };
  if (normAlias(final) === normAlias(p.alies)) {
    return {
      meta: '<span class="final-ok" title="Coincideix amb l\u2019àlies actual">✓ Alies final</span>',
      line: '',
    };
  }
  return {
    meta: '',
    line: `<div class="aliesfinal"><span class="aliesfinal-k">Alies final</span>${escapeHtml(final)}</div>`,
  };
}

function card(p) {
  const li = document.createElement('li');
  li.className = 'card' + (p.recollida ? ' done' : '') + (p.acompanya ? ' acompanya' : '');
  li.dataset.row = p.row;

  const af = aliesFinalHtml(p);
  const metaBits = [p.alies, p.colla].filter(Boolean).map(escapeHtml);
  if (af.meta) metaBits.push(af.meta);
  const meta = metaBits.join(' · ');
  const status = p.recollida
    ? '<span class="pill ok">✓ Recollida</span>'
    : '<span class="pill pending">Pendent</span>';
  const acomp = p.acompanya
    ? `<span class="pill acomp" title="${escapeHtml(p.modalitat || '')}">⚠ Acompanyant — sense pinya</span>`
    : '';
  // External-system flag: read-only. Missing/undefined flag renders nothing.
  const apps = p.enAppsistencia
    ? `<span class="pill apps" title="${escapeHtml('Aquesta persona ja és a Appsistència')}">${escapeHtml('A Appsistència')}</span>`
    : '';
  const btn = p.recollida
    ? '<button class="btn undo" data-action="unmark">Desmarca</button>'
    : '<button class="btn" data-action="mark">Marca recollida</button>';

  li.innerHTML = `
    <div class="top">
      <div class="who">
        <div class="nom">${escapeHtml(p.nom || '(sense nom)')}</div>
        ${meta ? `<div class="meta">${meta}</div>` : ''}
        ${af.line}
      </div>
      <div class="sizebox">
        ${sizeBadge(p.talla)}
        <button type="button" class="linkbtn" data-action="edittalla"
                aria-expanded="false">Canvia talla</button>
      </div>
    </div>
    <div class="bottom">
      <div class="pills">${status}${acomp}${apps}</div>
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
  openSizeRow = null; // rebuild closes any open editor
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

/* --- Inline size editor: open/close, no apply until "Desa" --- */

function closeSizeEditor() {
  const ed = list.querySelector('.size-editor');
  if (ed) ed.remove();
  const btn = list.querySelector('[data-action="edittalla"][aria-expanded="true"]');
  if (btn) btn.setAttribute('aria-expanded', 'false');
  openSizeRow = null;
}

function openSizeEditor(li) {
  const row = Number(li.dataset.row);
  const wasOpen = openSizeRow === row;
  closeSizeEditor();
  if (wasOpen) return; // second tap closes

  const person = people.find((p) => p.row === row);
  if (!person) return;
  const current = person.talla || '';

  const form = document.createElement('form');
  form.className = 'size-editor';
  form.id = `editor-${row}`;
  form.setAttribute('aria-label', `Canvia la talla de ${person.nom || 'aquesta persona'}`);
  form.innerHTML = `
    <label class="size-label" for="size-${row}">Talla</label>
    <select class="size-select" id="size-${row}" name="talla">${sizeOptionsHtml(current)}</select>
    <div class="editor-actions">
      <button type="submit" class="btn sm">Desa</button>
      <button type="button" class="btn ghost sm" data-action="canceltalla">Cancel·la</button>
    </div>
    <p class="editor-msg" hidden></p>`;

  li.querySelector('.top').appendChild(form);
  openSizeRow = row;
  const toggle = li.querySelector('[data-action="edittalla"]');
  if (toggle) toggle.setAttribute('aria-expanded', 'true');
  form.querySelector('.size-select').focus();
}

async function saveSize(form) {
  const li = form.closest('li.card');
  if (!li) return;
  const row = Number(li.dataset.row);
  const select = form.querySelector('.size-select');
  const msg = form.querySelector('.editor-msg');
  const saveBtn = form.querySelector('button[type="submit"]');

  msg.hidden = true;
  msg.textContent = '';
  saveBtn.disabled = true;
  select.disabled = true;
  try {
    const updated = await fetchJSON('/api/size', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ row, talla: select.value }),
    });
    people = people.map((p) => (p.row === updated.row ? updated : p));
    render(); // badge updates, editor closes
  } catch (e) {
    msg.textContent = `No s'ha pogut desar: ${e.message}`;
    msg.hidden = false;
  } finally {
    saveBtn.disabled = false;
    select.disabled = false;
  }
}

list.addEventListener('click', async (ev) => {
  const btn = ev.target.closest('button[data-action]');
  if (!btn) return;
  const action = btn.dataset.action;
  const li = btn.closest('li.card');
  if (!li) return;

  if (action === 'edittalla') {
    openSizeEditor(li);
    return;
  }
  if (action === 'canceltalla') {
    closeSizeEditor();
    return;
  }
  if (action !== 'mark' && action !== 'unmark') return;

  const row = Number(li.dataset.row);
  const recollida = action === 'mark';
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

list.addEventListener('submit', (ev) => {
  const form = ev.target;
  if (!(form instanceof HTMLFormElement) || !form.classList.contains('size-editor')) return;
  ev.preventDefault();
  saveSize(form);
});

document.addEventListener('keydown', (ev) => {
  if (ev.key !== 'Escape' || openSizeRow === null) return;
  const li = list.querySelector(`li.card[data-row="${openSizeRow}"]`);
  closeSizeEditor();
  const btn = li && li.querySelector('[data-action="edittalla"]');
  if (btn) btn.focus();
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
