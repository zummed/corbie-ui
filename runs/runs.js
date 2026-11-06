// The Runs pages: a line stays unfolded as the store changes, a line or a
// row opens its run, the button at a line's start unfolds it, and a panel
// shows one tab at a time.
const unfolded = new Set();

document.addEventListener('toggle', e => {
  const key = e.target.matches?.('details.req') && e.target.querySelector(':scope > summary')?.dataset.key;
  if (key) e.target.open ? unfolded.add(key) : unfolded.delete(key);
}, true);

document.addEventListener('part', e => {
  for (const line of e.detail.querySelectorAll('details.req')) {
    if (unfolded.has(line.querySelector(':scope > summary').dataset.key)) line.open = true;
  }
});

document.addEventListener('click', e => {
  const unfold = e.target.closest('.req > summary > button.unfold');
  if (unfold) {
    e.preventDefault();
    unfold.closest('details').toggleAttribute('open');
    return;
  }
  const row = e.target.closest('[data-href]');
  if (row && !e.target.closest('a, button, input, form')) {
    e.preventDefault();
    location = row.dataset.href;
  }
  const tab = e.target.closest('.tabs a');
  if (tab) {
    e.preventDefault();
    const name = tab.getAttribute('href').slice(1);
    for (const a of tab.parentElement.children) a.toggleAttribute('aria-current', a === tab);
    for (const pane of tab.closest('.tabbed').querySelectorAll(':scope > .pane')) pane.hidden = pane.dataset.pane !== name;
    if (tab.closest('#details')) detailTab = name;
  }
});

// A run's Steps. Each child run is a group under the row that started it:
// an ended child is folded and a running one open, until the person folds
// or opens it. A send or a call to a run the UI may read is a group too,
// folded, whose rows are asked for when it is opened. One row is selected; the drawing and the details beside the
// list show its run as of its step. The selection follows the newest row,
// or the row of the folded group it is in, until the person moves it; on a
// running run, Follow then selects the newest row and follows again.
const split = document.querySelector('.split[data-page]');
const folds = new Map();
let selected = null, moved = false, details = false, detailTab = 'what', seen = null, asked = null, base = null;

const listOf = () => document.querySelector('#steps .steplist');
const visible = () => [...(listOf()?.querySelectorAll('.step') ?? [])].filter(r => r.offsetParent !== null);
const rowOf = key => key && [...(listOf()?.querySelectorAll('.step') ?? [])].find(r => keyOf(r) === key);

// keyOf gives a row's key on the page: the keys of the send and call groups
// it is in, then its own. A run's rows can be on the page more than once,
// in the groups of two sends to it, and each copy has its own key.
function keyOf(row) {
  const keys = [row.dataset.row];
  for (let g = groupOf(row); g; g = groupOf(g.firstElementChild)) if (g.matches('.trace')) keys.unshift(g.dataset.group);
  return keys.join(' ');
}

// The group a row's run is in: the row's own li, not the group a head row starts.
function groupOf(row) {
  const li = row.parentElement;
  return (li.matches('li.group') ? li.parentElement : li).closest('li.group');
}

function applyFolds() {
  for (const g of listOf()?.querySelectorAll('li.group') ?? []) {
    const open = folds.has(g.dataset.group) ? folds.get(g.dataset.group) : !g.hasAttribute('data-ended');
    g.classList.toggle('folded', !open);
    if (open) load(g);
  }
}

// load asks the UI for the rows of a send's or a call's group, once each
// time the group is drawn. A group the UI could not read says why, and is
// asked for again when it is next opened.
async function load(g) {
  if (!g.dataset.load || g.dataset.loaded) return;
  g.dataset.loaded = '1';
  const ol = g.querySelector(':scope > ol');
  try {
    const res = await fetch(`${g.dataset.load}&page=${encodeURIComponent(split.dataset.page)}`);
    const t = document.createElement('template');
    if (!res.ok) {
      delete g.dataset.loaded;
      const none = document.createElement('li');
      none.className = 'dim none';
      none.textContent = (await res.text()).trim() || res.statusText;
      ol.replaceChildren(none);
      return;
    }
    t.innerHTML = (await res.text()).trim();
    if (!t.content.firstElementChild) t.innerHTML = '<li class="dim none">nothing yet</li>';
    ol.replaceChildren(t.content);
    for (const r of ol.querySelectorAll('.step')) seen.add(keyOf(r));
    applyFolds();
    const row = rowOf(selected);
    if (row && ol.contains(row)) select(row, false);
  } catch (err) {
    delete g.dataset.loaded;
    console.error(err);
  }
}

// shownOf gives the row that shows a row: itself, or the first row of the
// outermost folded group it is in.
function shownOf(row) {
  let at = row;
  for (let g = row && groupOf(row); g; g = groupOf(g.firstElementChild)) if (g.classList.contains('folded')) at = g.firstElementChild;
  return at;
}

// newestRow gives the newest row of the run and the runs under it; the rows
// of a send's or a call's group are of other runs, and are left out.
function newestRow() {
  let last = null;
  for (const r of listOf()?.querySelectorAll('.step') ?? []) {
    if (r.parentElement.parentElement.closest('li.trace')) continue;
    if (!last || +r.dataset.at >= +last.dataset.at) last = r;
  }
  return last;
}

// showPath lights the selected row's run in the path. A run off the path,
// below this page's run, is shown by the groups down to it.
function showPath(row) {
  const path = document.getElementById('path');
  if (!path || base === null) return;
  path.innerHTML = base;
  const id = row.dataset.run;
  let a = path.querySelector(`a[data-run="${CSS.escape(id)}"]`);
  if (!a) {
    const here = path.querySelector('a[aria-current]');
    while (here?.nextSibling) here.nextSibling.remove();
    const chain = [];
    for (let g = groupOf(row); g; g = groupOf(g.firstElementChild)) chain.unshift(g);
    for (const g of chain) {
      const sep = document.createElement('span');
      sep.className = g.matches('.trace') ? 'sep send' : 'sep';
      sep.textContent = g.matches('.trace') ? '⇢' : '›';
      const link = document.createElement('a');
      link.href = '/run/' + g.dataset.run;
      link.dataset.run = g.dataset.run;
      link.title = g.dataset.tip;
      const icon = g.querySelector(':scope > ol .step svg.g');
      link.append(g.dataset.machine);
      if (icon) link.prepend(icon.cloneNode(true));
      path.append(sep, link);
    }
    a = path.querySelector(`a[data-run="${CSS.escape(id)}"]`);
  }
  a?.classList.add('sel');
}

// show asks the UI for the drawing and the details of a row's run as of the
// row's step, and puts them in their places.
async function show(row) {
  asked?.abort();
  const ask = asked = new AbortController();
  try {
    const res = await fetch(`/run/${row.dataset.run}/at?row=${encodeURIComponent(row.dataset.row)}&page=${encodeURIComponent(split.dataset.page)}`, { signal: ask.signal });
    if (!res.ok) return;
    for (const part of await res.json()) {
      const old = document.getElementById(part.id);
      const t = document.createElement('template');
      t.innerHTML = part.html.trim();
      const next = t.content.firstElementChild;
      if (part.id === 'details') {
        next.hidden = !details;
        for (const a of next.querySelectorAll('.tabs a')) a.toggleAttribute('aria-current', a.getAttribute('href') === '#' + detailTab);
        for (const pane of next.querySelectorAll(':scope > .pane')) pane.hidden = pane.dataset.pane !== detailTab;
      }
      old?.replaceWith(next);
      if (part.id === 'view') showMarked(next);
    }
  } catch (err) {
    if (err.name !== 'AbortError') console.error(err);
  }
}

// showMarked scrolls a drawing to the first card marked as going, held or
// failed.
function showMarked(view) {
  const drawn = view.querySelector('.drawn'), card = drawn?.querySelector('g.now:not(.holder), g.held, g.failed:not(.holder)');
  if (!card) return;
  const a = drawn.getBoundingClientRect(), b = card.getBoundingClientRect();
  drawn.scrollTop += b.top - a.top - 24;
  drawn.scrollLeft += b.left - a.left - 24;
}

function select(row, fetchToo = true) {
  if (!row) return;
  for (let g = groupOf(row); g; g = groupOf(g.firstElementChild)) fold(g, true);
  for (const r of listOf().querySelectorAll('.step.sel')) r.classList.remove('sel');
  row.classList.add('sel');
  const changed = selected !== keyOf(row);
  selected = keyOf(row);
  row.scrollIntoView({ block: 'nearest' });
  showPath(row);
  if (fetchToo && changed) show(row);
}

// showFollow shows Follow, which a running run's Steps have, while the
// selection does not follow the newest row.
function showFollow() {
  const follow = document.querySelector('#steps [data-follow]');
  if (follow) follow.hidden = !moved;
}

function fold(group, open) {
  folds.set(group.dataset.group, open);
  group.classList.toggle('folded', !open);
  if (open) load(group);
}

if (split && listOf()) {
  base = document.getElementById('path')?.innerHTML ?? null;
  seen = new Set([...listOf().querySelectorAll('.step')].map(keyOf));
  applyFolds();
  select(newestRow(), false);
  const sel = rowOf(selected), view = document.getElementById('view');
  if (sel && sel.dataset.row !== view?.dataset.row) show(sel);
  else if (view) showMarked(view);

  document.addEventListener('part', e => {
    if (e.detail.id === 'path') {
      base = e.detail.innerHTML;
      const row = rowOf(selected);
      if (row) showPath(row);
    }
    if (e.detail.id !== 'steps') return;
    applyFolds();
    for (const r of listOf().querySelectorAll('.step')) if (!seen.has(keyOf(r))) { r.classList.add('new'); seen.add(keyOf(r)); }
    const row = rowOf(selected);
    if (!moved || !row) select(shownOf(newestRow()));
    else select(row, false);
    showFollow();
  });

  document.addEventListener('click', e => {
    if (!e.target.closest('#steps')) return;
    if (e.target.closest('[data-follow]')) {
      moved = false;
      select(shownOf(newestRow()));
      showFollow();
      return;
    }
    const all = e.target.closest('[data-fold]');
    if (all) {
      for (const g of listOf().querySelectorAll('li.group')) fold(g, all.dataset.fold === 'open');
      let top = null;
      for (let g = rowOf(selected) && groupOf(rowOf(selected)); g; g = groupOf(g.firstElementChild)) top = g;
      if (top && all.dataset.fold === 'fold') { moved = true; select(top.firstElementChild); showFollow(); }
      return;
    }
    const button = e.target.closest('button.fold');
    if (button) {
      const g = button.closest('li.group'), row = rowOf(selected);
      fold(g, g.classList.contains('folded'));
      if (row && shownOf(row) !== row) select(shownOf(row));
      return;
    }
    const row = e.target.closest('.step');
    if (!row || e.target.closest('a')) return;
    moved = true;
    details = true;
    document.getElementById('details')?.removeAttribute('hidden');
    select(row);
    showFollow();
  });

  document.addEventListener('keydown', e => {
    if (e.target !== document.body && !e.target.closest('#steps .steplist') || e.altKey || e.ctrlKey || e.metaKey) return;
    if (e.key === 'Enter' && e.target.closest('a, button')) return;
    const row = rowOf(selected);
    if (!row || listOf().offsetParent === null) return;
    const rows = visible();
    const at = rows.indexOf(shownOf(row));
    const head = row.parentElement.matches('li.group') ? row.parentElement : null;
    let next = null;
    switch (e.key) {
      case 'ArrowDown': next = rows[at + 1]; break;
      case 'ArrowUp': next = rows[at - 1]; break;
      case 'ArrowLeft':
        if (head && !head.classList.contains('folded')) fold(head, false);
        else if (groupOf(row)) { next = groupOf(row).firstElementChild; fold(groupOf(row), false); }
        break;
      case 'ArrowRight': if (head) fold(head, true); break;
      case 'Enter': details = true; document.getElementById('details')?.removeAttribute('hidden'); break;
      default: return;
    }
    e.preventDefault();
    if (next) { moved = true; select(next); showFollow(); }
  });
}
