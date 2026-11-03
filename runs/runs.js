// The Runs pages: a line stays unfolded as the store changes, a row opens
// its run, and a run's page shows one tab at a time.
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
