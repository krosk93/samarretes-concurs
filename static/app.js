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

/* --- Virtual scroll: només la finestra visible (+ overscan) viu al DOM.
   La llista manté l'alçada total amb paddingTop/Bottom, així l'scroll de la
   finestra no salta. Alçades variables mesurades per fila amb cache. --- */
let items = [];
let heights = [];
let estH = 170;
let gapPx = 12;
let rev = 0;
let winStart = -1;
let winEnd = -1;
let scrollRaf = 0;
let measureRaf = 0;
const OVERSCAN_PX = 900;
const MIN_WINDOW = 12;

function readGap() {
  try {
    const g = parseFloat(getComputedStyle(list).rowGap);
    if (Number.isFinite(g) && g >= 0) gapPx = g;
  } catch { /* manté fallback */ }
}

function virtualTotal() {
  let t = 0;
  for (let i = 0; i < heights.length; i++) t += heights[i];
  if (heights.length > 1) t += gapPx * (heights.length - 1);
  return t;
}

function virtualOffset(idx) {
  let o = 0;
  for (let i = 0; i < idx; i++) o += heights[i] + gapPx;
  return o;
}

function listTopAbs() {
  return list.getBoundingClientRect().top + window.scrollY;
}

function computeRange() {
  const n = items.length;
  if (n === 0) return [0, 0];
  const topAbs = listTopAbs();
  const relTop = window.scrollY - topAbs;
  const relBottom = relTop + window.innerHeight;
  const lo = relTop - OVERSCAN_PX;
  const hi = relBottom + OVERSCAN_PX;
  let start = 0;
  let off = 0;
  for (let i = 0; i < n; i++) {
    const h = heights[i];
    if (off + h < lo) { off += h + gapPx; start = i + 1; continue; }
    break;
  }
  if (start >= n) start = Math.max(0, n - MIN_WINDOW);
  let end = start;
  off = virtualOffset(start);
  while (end < n && off < hi) { off += heights[end] + gapPx; end++; }
  if (end - start < MIN_WINDOW) end = Math.min(n, start + MIN_WINDOW);
  if (end - start < MIN_WINDOW) start = Math.max(0, end - MIN_WINDOW);
  return [start, end];
}

function applyPadding(start, end) {
  list.style.paddingTop = `${virtualOffset(start)}px`;
  list.style.paddingBottom = `${Math.max(0, virtualTotal() - virtualOffset(end))}px`;
}

function buildEditorForm(person) {
  const row = person.row;
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
  return form;
}

function remeasureRow(row) {
  const idx = items.findIndex((p) => p.row === row);
  if (idx < 0) return;
  const li = list.querySelector(`li.card[data-row="${row}"]`);
  if (!li) return;
  const h = li.offsetHeight;
  if (Number.isFinite(h) && h > 0 && Math.abs(h - heights[idx]) > 1) {
    heights[idx] = h;
    applyPadding(winStart, winEnd);
  }
}

function scheduleMeasure() {
  if (measureRaf) return;
  measureRaf = requestAnimationFrame(() => {
    measureRaf = 0;
    const nodes = list.querySelectorAll('li.card[data-row]');
    let changed = false;
    nodes.forEach((li, k) => {
      const idx = winStart + k;
      if (idx < 0 || idx >= heights.length) return;
      const h = li.offsetHeight;
      if (Number.isFinite(h) && h > 0 && Math.abs(h - heights[idx]) > 2) {
        heights[idx] = h;
        changed = true;
      }
    });
    if (changed) {
      const measured = heights.filter((h) => Math.abs(h - estH) > 1);
      if (measured.length > 0) {
        estH = Math.round(measured.reduce((a, b) => a + b, 0) / measured.length);
      }
      applyPadding(winStart, winEnd);
      renderWindow(false);
    }
  });
}

function renderWindow(force) {
  const total = people.length;
  if (items.length === 0 && total > 0) {
    winStart = 0; winEnd = 0;
    list.style.paddingTop = '0px';
    list.style.paddingBottom = '0px';
    list.innerHTML = '';
    list.appendChild(emptyCard(
      amagaEl.checked
        ? 'Tots els resultats són acompanyants. Desmarca «Amaga acompanyants» per veure\'ls.'
        : 'Cap resultat.',
    ));
    countEl.textContent = countText(0, total);
    return;
  }
  if (items.length === 0) {
    winStart = 0; winEnd = 0;
    list.style.paddingTop = '0px';
    list.style.paddingBottom = '0px';
    list.innerHTML = '';
    countEl.textContent = countText(0, total);
    return;
  }
  const [start, end] = computeRange();
  if (!force && start === winStart && end === winEnd) return;
  winStart = start; winEnd = end;
  const frag = document.createDocumentFragment();
  for (let i = start; i < end; i++) {
    const li = card(items[i]);
    if (items[i].row === openSizeRow) {
      li.querySelector('.top').appendChild(buildEditorForm(items[i]));
      const toggle = li.querySelector('[data-action="edittalla"]');
      if (toggle) toggle.setAttribute('aria-expanded', 'true');
    }
    frag.appendChild(li);
  }
  list.innerHTML = '';
  list.appendChild(frag);
  applyPadding(start, end);
  countEl.textContent = countText(items.length, total);
  scheduleMeasure();
}

// Reconstrueix el dataset visible. resetScroll=true en cerques noves.
function render(resetScroll) {
  readGap();
  const prevItems = items;
  const prevHeights = heights;
  const cache = new Map();
  for (let i = 0; i < prevItems.length; i++) {
    const h = prevHeights[i];
    if (Number.isFinite(h) && h > 0) cache.set(prevItems[i].row, h);
  }
  items = visiblePeople();
  heights = items.map((p) => {
    const h = cache.get(p.row);
    return Number.isFinite(h) && h > 0 ? h : estH;
  });
  rev++;
  winStart = -1; winEnd = -1;
  if (resetScroll) window.scrollTo(0, 0);
  renderWindow(true);
}

function refreshRow(updated, focusAction) {
  people = people.map((p) => (p.row === updated.row ? updated : p));
  const idx = items.findIndex((p) => p.row === updated.row);
  if (idx >= 0) items[idx] = updated;
  renderWindow(true);
  if (focusAction) {
    const li = list.querySelector(`li.card[data-row="${updated.row}"]`);
    const btn = li && li.querySelector(`[data-action="${focusAction}"]`);
    if (btn) btn.focus();
  } else {
    scheduleMeasure();
  }
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
    openSizeRow = null;
    render(true);
  } catch (e) {
    showStatus(`No s'ha pogut carregar: ${e.message}`, true);
  }
}

/* --- Inline size editor: open/close, no apply until "Desa" --- */

function closeSizeEditor(stealFocus) {
  const ed = list.querySelector('.size-editor');
  const li = ed && ed.closest('li.card');
  const row = li ? Number(li.dataset.row) : openSizeRow;
  if (ed) ed.remove();
  const btn = list.querySelector('[data-action="edittalla"][aria-expanded="true"]');
  if (btn) btn.setAttribute('aria-expanded', 'false');
  openSizeRow = null;
  if (row) remeasureRow(row);
  if (stealFocus === false) return;
}

function openSizeEditor(li) {
  const row = Number(li.dataset.row);
  const wasOpen = openSizeRow === row;
  closeSizeEditor(false);
  if (wasOpen) { remeasureRow(row); return; } // second tap closes

  const person = people.find((p) => p.row === row);
  if (!person) return;

  li.querySelector('.top').appendChild(buildEditorForm(person));
  openSizeRow = row;
  const toggle = li.querySelector('[data-action="edittalla"]');
  if (toggle) toggle.setAttribute('aria-expanded', 'true');
  remeasureRow(row);
  li.querySelector('.size-select').focus();
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
    openSizeRow = null; // badge updates, editor closes
    refreshRow(updated, 'edittalla');
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
    refreshRow(updated, recollida ? 'unmark' : 'mark');
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

amagaEl.addEventListener('change', () => render(false));

function syncStuck() {
  toolbar.classList.toggle('stuck', window.scrollY > 4);
}
function onScroll() {
  if (scrollRaf) return;
  scrollRaf = requestAnimationFrame(() => {
    scrollRaf = 0;
    syncStuck();
    renderWindow(false);
  });
}
window.addEventListener('scroll', onScroll, { passive: true });
window.addEventListener('resize', () => {
  readGap();
  renderWindow(true);
}, { passive: true });
window.addEventListener('load', () => {
  readGap();
  renderWindow(true);
});

readGap();
search();
syncStuck();
