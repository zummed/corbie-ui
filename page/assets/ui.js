// A page that follows the store opens a WebSocket to the UI, and puts each
// part it is sent in its place. What changed in a part is lit, as the look
// says, and what a person was typing stays as it was.
const live = document.getElementById('live');
const main = document.querySelector('main[data-stream]');

// lit are the classes that light a line, which the line was not sent with.
const lit = ['new', 'moved', 'ended'];

// sent gives a line as it was sent, without the classes that lit it.
function sent(el) {
  const copy = el.cloneNode(true);
  copy.classList.remove(...lit);
  if (!copy.classList.length) copy.removeAttribute('class');
  return copy.outerHTML;
}

// mark lights what changed between a part as shown and as sent: a line
// that is new, a line that changed, a run that ended, a count that moved.
// A line is found as it was anywhere on the page, since a run that ends
// moves from one part to another.
function mark(old, next) {
  for (const el of next.querySelectorAll('[data-key]')) {
    const was = document.querySelector(`${el.tagName}[data-key="${CSS.escape(el.dataset.key)}"]`);
    if (!was) el.classList.add('new');
    else if (was.dataset.status !== el.dataset.status && el.dataset.ended === 'true') el.classList.add('ended');
    else if (sent(was) !== el.outerHTML) el.classList.add('moved');
  }
  const counts = new Map([...old.querySelectorAll('[data-count]')].map(el => [el.dataset.count, el.textContent]));
  for (const el of next.querySelectorAll('[data-count]')) {
    if (counts.has(el.dataset.count) && counts.get(el.dataset.count) !== el.textContent) el.classList.add('bump');
  }
}

// keep carries into a part what was typed into it, which field had the
// keyboard, and the answer a command gave, form by form.
function keep(old, next) {
  const action = el => el.closest('form')?.getAttribute('action') ?? '';
  for (const field of old.querySelectorAll('input[name]:not([type=hidden]), textarea[name]')) {
    const twin = [...next.querySelectorAll(`[name="${CSS.escape(field.name)}"]`)].find(t => action(t) === action(field));
    if (!twin) continue;
    if (field.value !== field.defaultValue) twin.value = field.value;
    if (document.activeElement === field) queueMicrotask(() => { twin.focus(); twin.setSelectionRange?.(field.selectionStart, field.selectionEnd); });
  }
  for (const said of old.querySelectorAll('form .said')) {
    const twin = [...next.querySelectorAll('form .said')].find(t => action(t) === action(said));
    if (twin && said.textContent) { twin.textContent = said.textContent; twin.className = said.className; }
  }
}

function place(id, html) {
  const old = document.getElementById(id);
  if (!old) return;
  const t = document.createElement('template');
  t.innerHTML = html.trim();
  const next = t.content.firstElementChild;
  mark(old, next);
  keep(old, next);
  old.replaceWith(next);
  document.dispatchEvent(new CustomEvent('part', { detail: next }));
}

function follow() {
  const ws = new WebSocket(`${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}${main.dataset.stream}`);
  ws.onopen = () => live.classList.remove('lost');
  ws.onmessage = e => {
    const m = JSON.parse(e.data);
    if (m.id) place(m.id, m.html);
    if (m.store) live.classList.toggle('lost', m.store === 'lost');
  };
  ws.onclose = () => { live.classList.add('lost'); setTimeout(follow, 2000); };
}
if (main) follow();

// A form that runs a command shows the command's answer beside its button,
// or its error, untouched; while the command waits, the form says so. The
// ID of a run the command started links to the run's page.
document.addEventListener('submit', async e => {
  const form = e.target;
  if (!form.matches('form[data-act]')) return;
  e.preventDefault();
  const said = form.querySelector('.said');
  const button = form.querySelector('button[type=submit], button:not([type])');
  if (button) button.disabled = true;
  if (said) { said.textContent = 'waiting for the answer'; said.className = 'said waiting'; }
  try {
    const res = await fetch(form.action, { method: 'POST', body: new URLSearchParams(new FormData(form)) });
    const text = await res.text();
    if (said) { said.textContent = text.trim(); said.className = res.ok ? 'said' : 'said bad'; }
    const started = res.ok && said && /^started (\S+\/\S+)$/.exec(text.trim());
    if (started) {
      said.textContent = 'started ';
      const a = said.appendChild(document.createElement('a'));
      a.href = encodeURI('/run/' + started[1]);
      a.textContent = started[1];
    }
    if (res.ok && form.dataset.act === 'reset') for (const f of form.querySelectorAll('input:not([type=hidden]), textarea')) f.value = '';
  } catch (err) {
    if (said) { said.textContent = String(err); said.className = 'said bad'; }
  } finally {
    if (button) button.disabled = false;
  }
});

// A handle between two panels resizes them as a person drags it. It sets
// the size of the panel before it, or for a row the panel after it, on the
// element that holds both, and the browser keeps the size for every page.
const sizeOf = name => `--${name}`;
for (const h of document.querySelectorAll('.handle[data-size]')) {
  const kept = localStorage.getItem('size:' + h.dataset.size);
  if (kept) h.parentElement.style.setProperty(sizeOf(h.dataset.size), kept);
}
document.addEventListener('pointerdown', e => {
  const h = e.target.closest('.handle[data-size]');
  if (!h || e.button !== 0) return;
  e.preventDefault();
  const holder = h.parentElement, row = h.classList.contains('row');
  h.setPointerCapture(e.pointerId);
  h.classList.add('dragging');
  const move = m => {
    const r = holder.getBoundingClientRect();
    const whole = row ? r.height : r.width;
    const at = row ? r.bottom - m.clientY : m.clientX - r.left;
    const size = Math.round(Math.min(Math.max(at, 160), whole - 160)) + 'px';
    holder.style.setProperty(sizeOf(h.dataset.size), size);
    localStorage.setItem('size:' + h.dataset.size, size);
  };
  h.addEventListener('pointermove', move);
  h.addEventListener('lostpointercapture', () => { h.removeEventListener('pointermove', move); h.classList.remove('dragging'); }, { once: true });
});
