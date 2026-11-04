// The Machines page: a file chosen is shown again when it, or what Corbie
// found in it, changed on disk, and a card's link in a drawing unfolds it
// where it is.
const view = document.querySelector('.view[data-file]');

if (view) {
  document.addEventListener('part', e => {
    if (e.detail.id === 'disk' && e.detail.dataset.base !== view.dataset.base) location.reload();
  });
  const drawing = document.getElementById('drawing');
  drawing?.addEventListener('click', async e => {
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
