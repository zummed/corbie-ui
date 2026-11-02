// The Machines page: a definition as text, its keys, values and expressions
// set apart, checked a moment after each change, saved only if the file is
// as it was read, and tested or started through Corbie's commands.
const editor = document.querySelector('.editor[data-file]');

if (editor) {
  const file = editor.dataset.file, kind = editor.dataset.kind;
  const text = document.getElementById('text'), lines = document.getElementById('lines');
  const savedWord = document.getElementById('saved'), said = document.getElementById('said');
  const conflict = document.getElementById('conflict');
  let base = editor.dataset.base, saved = text.value, bad = new Set(), timer, problems = 0;

  const esc = s => s.replace(/[&<>]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' }[c]));
  const paint = () => {
    lines.innerHTML = text.value.split('\n').map((l, i) => {
      let h = esc(l)
        .replace(/^(\s*(?:-\s+)?)([A-Za-z_][\w.-]*)(:)(?=\s|$)/, '$1<span class="k">$2</span>$3')
        .replace(/([{,]\s*)([A-Za-z_][\w.-]*)(:)(?=\s)/g, '$1<span class="k">$2</span>$3')
        .replace(/\$\{[^}]*\}/g, m => `<span class="x">${m}</span>`)
        .replace(/(^|\s)(#.*)$/, '$1<span class="c">$2</span>');
      return `<span class="l${bad.has(i + 1) ? ' err' : ''}">${h || ' '}</span>`;
    }).join('');
    lines.scrollTop = text.scrollTop;
    lines.scrollLeft = text.scrollLeft;
  };
  const dirty = () => text.value !== saved;
  const mark = () => {
    if (!text.readOnly) savedWord.textContent = dirty() ? 'edited' : 'saved';
    document.querySelector(`#files a[aria-current] .edited`)?.toggleAttribute('hidden', !dirty());
  };
  const cursorLine = () => text.value.slice(0, text.selectionStart).split('\n').length;
  const offer = () => {
    for (const id of ['test', 'start']) {
      const b = document.getElementById(id);
      if (b) { b.disabled = problems > 0; b.title = problems > 0 ? 'the definition has problems' : ''; }
    }
  };

  async function check() {
    if (!kind) return;
    const res = await fetch('/machines/check', { method: 'POST', body: new URLSearchParams({ file, text: text.value, line: cursorLine() }) });
    if (!res.ok) return;
    const c = await res.json();
    document.getElementById('problems').outerHTML = c.problems;
    document.getElementById('secrets').outerHTML = c.secrets;
    document.getElementById('fields').outerHTML = c.fields;
    if (c.drawing) document.getElementById('drawing').innerHTML = c.drawing;
    bad = new Set(c.lines);
    problems = c.count;
    paint();
    offer();
  }
  const soon = () => { clearTimeout(timer); timer = setTimeout(check, 250); };

  async function save() {
    if (!dirty()) return true;
    const res = await fetch('/machines/save', { method: 'POST', body: new URLSearchParams({ file, text: text.value, base }) });
    const a = await res.json();
    if (res.ok) {
      base = a.base;
      saved = text.value;
      conflict.hidden = true;
      said.textContent = '';
      mark();
      return true;
    }
    said.textContent = a.error;
    said.className = 'said bad';
    if (res.status === 409) {
      conflict.querySelector('pre').textContent = a.text;
      conflict.hidden = false;
      base = a.base;
    }
    return false;
  }

  // A file changed on disk while it has no unsaved edits is read again.
  async function reread() {
    const res = await fetch('/machines/raw?file=' + encodeURIComponent(file));
    if (!res.ok) return;
    const f = await res.json();
    text.value = saved = f.text;
    base = f.base;
    mark();
    paint();
    check();
  }
  document.addEventListener('part', e => {
    if (e.detail.id === 'disk' && e.detail.dataset.base !== base && !dirty()) reread();
    if (e.detail.id === 'files') mark();
  });

  text.addEventListener('input', () => { paint(); mark(); soon(); });
  text.addEventListener('scroll', () => { lines.scrollTop = text.scrollTop; lines.scrollLeft = text.scrollLeft; });
  text.addEventListener('click', soon);
  text.addEventListener('keyup', e => { if (e.key.startsWith('Arrow') || e.key === 'Enter') soon(); });
  text.addEventListener('keydown', e => {
    if (text.readOnly) return;
    if (e.key === 'Tab' && !e.shiftKey) {
      e.preventDefault();
      text.setRangeText('  ', text.selectionStart, text.selectionEnd, 'end');
      paint(); mark(); soon();
    }
    if (e.key === 's' && (e.ctrlKey || e.metaKey)) { e.preventDefault(); save(); }
  });
  document.getElementById('save')?.addEventListener('click', save);
  document.getElementById('take')?.addEventListener('click', () => {
    text.value = saved = conflict.querySelector('pre').textContent;
    conflict.hidden = true;
    said.textContent = '';
    mark(); paint(); check();
  });
  window.addEventListener('beforeunload', e => { if (dirty()) e.preventDefault(); });

  const form = () => Object.fromEntries(new FormData(document.getElementById('run')));
  document.getElementById('test')?.addEventListener('click', async () => {
    if (!(await save())) return;
    const out = document.getElementById('test-out');
    out.hidden = false;
    out.replaceChildren();
    const { input, secrets } = form();
    const q = new URLSearchParams({ file, input, secrets });
    const ws = new WebSocket(`${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/machines/test?${q}`);
    ws.onmessage = e => {
      const li = document.createElement('li');
      li.className = 'new';
      li.textContent = e.data;
      out.append(li);
      li.scrollIntoView({ block: 'nearest' });
    };
  });
  document.getElementById('start')?.addEventListener('click', async () => {
    if (!(await save())) return;
    const res = await fetch('/machines/start', { method: 'POST', body: new URLSearchParams({ file, ...form() }) });
    if (res.ok) { location = (await res.json()).href; return; }
    said.textContent = (await res.text()).trim();
    said.className = 'said bad';
  });

  // Set keeps a secret on the controller, then the check runs again and
  // the row reads set. The Secrets section is drawn again at each check,
  // so the form is found as it is then.
  document.addEventListener('submit', async e => {
    const form = e.target;
    if (!form.closest('#secrets')) return;
    e.preventDefault();
    const res = await fetch(form.action, { method: 'POST', body: new URLSearchParams(new FormData(form)) });
    if (res.ok) { check(); return; }
    const word = form.querySelector('.said');
    word.textContent = (await res.text()).trim();
    word.className = 'said bad';
  });

  paint();
  check();
}

// The drawing of a machine: a box's link unfolds it where it is, and a file
// changed on disk is drawn again.
const view = document.querySelector('.view[data-file]');

if (view) {
  const drawing = document.getElementById('drawing');
  document.addEventListener('part', e => {
    if (e.detail.id === 'disk' && e.detail.dataset.base !== view.dataset.base) location.reload();
  });
  drawing.addEventListener('click', async e => {
    const link = e.target.closest('a');
    if (!link) return;
    e.preventDefault();
    const href = link.getAttribute('href');
    const res = await fetch(href);
    if (!res.ok) { location = href; return; }
    const page = new DOMParser().parseFromString(await res.text(), 'text/html');
    drawing.innerHTML = page.getElementById('drawing').innerHTML;
    history.pushState(null, '', href);
  });
  window.addEventListener('popstate', () => location.reload());
}
