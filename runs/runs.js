// The Runs pages: pointing at a run lights it in the timeline and the list
// both, a line opens its run, and a run's page shows one tab at a time.
const tip = document.getElementById('tip');

function hot(key, on) {
  for (const el of document.querySelectorAll(`[data-key="${CSS.escape(key)}"]`)) el.classList.toggle('hot', on);
}

document.addEventListener('mouseover', e => {
  const el = e.target.closest('[data-key]');
  if (!el) return;
  hot(el.dataset.key, true);
  if (tip && el.classList.contains('runbar')) {
    tip.textContent = el.dataset.tip;
    const box = tip.parentElement.getBoundingClientRect(), bar = el.getBoundingClientRect();
    tip.style.left = `${Math.min(Math.max(e.clientX - box.left, 200), box.width - 200)}px`;
    tip.style.top = `${bar.top - box.top}px`;
    tip.classList.add('on');
  }
});
document.addEventListener('mouseout', e => {
  const el = e.target.closest('[data-key]');
  if (!el) return;
  hot(el.dataset.key, false);
  if (tip) tip.classList.remove('on');
});

document.addEventListener('click', e => {
  const row = e.target.closest('tr[data-href]');
  if (row && !e.target.closest('a, button, input, form')) location = row.dataset.href;
  const tab = e.target.closest('.tabs a');
  if (tab) {
    e.preventDefault();
    const name = tab.getAttribute('href').slice(1);
    for (const a of tab.parentElement.children) a.toggleAttribute('aria-current', a === tab);
    for (const pane of document.querySelectorAll('.pane')) pane.hidden = pane.dataset.pane !== name;
  }
});

const client = document.getElementById('client');
if (client) client.addEventListener('change', () => { location = client.value; });
