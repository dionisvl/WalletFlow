// Copy an address on click.
document.addEventListener('click', (e) => {
  const el = e.target.closest('[data-copy]');
  if (!el) return;
  navigator.clipboard?.writeText(el.dataset.copy);
  const old = el.textContent;
  el.textContent = t('copied');
  setTimeout(() => { el.textContent = old; }, 700);
});

// Inbox: keyboard-first review.
(() => {
  let current = null;
  const items = () => [...document.querySelectorAll('#inbox-list .item')];

  function select(el) {
    current?.classList.remove('current');
    current = el || null;
    if (current) {
      current.classList.add('current');
      current.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
    }
  }

  function move(step) {
    const list = items();
    if (!list.length) return;
    const i = list.indexOf(current);
    select(list[Math.min(Math.max(i + step, 0), list.length - 1)] || list[0]);
  }

  function updateBulk() {
    const n = document.querySelectorAll('#inbox-list .pick:checked').length;
    const c = document.getElementById('bulk-count');
    if (c) c.textContent = n;
  }

  document.addEventListener('change', (e) => { if (e.target.matches('.pick')) updateBulk(); });

  document.addEventListener('click', (e) => {
    const item = e.target.closest('#inbox-list .item');
    if (item && item !== current) select(item);
  });

  document.addEventListener('keydown', (e) => {
    if (!document.getElementById('inbox-list')) return;
    if (e.metaKey || e.ctrlKey || e.altKey) return;
    if (e.target.matches('input:not([type=checkbox]), textarea, select')) {
      if (e.key === 'Escape') e.target.blur();
      return;
    }
    if (!current) select(items()[0]);
    if (!current) return;
    if (e.key === 'j' || e.key === 'ArrowDown') { move(1); e.preventDefault(); }
    else if (e.key === 'k' || e.key === 'ArrowUp') { move(-1); e.preventDefault(); }
    else if (/^[1-9]$/.test(e.key)) {
      const btn = current.querySelectorAll('button.cat')[Number(e.key) - 1];
      if (btn) btn.click();
    } else if (e.key === 'x') {
      const cb = current.querySelector('.pick');
      cb.checked = !cb.checked; updateBulk();
    } else if (e.key === 'r') {
      const cb = current.querySelector('input[name=rule]');
      cb.checked = !cb.checked;
    } else if (e.key === 'c') {
      const d = current.querySelector('details');
      d.open = true;
      d.querySelector('.comment').focus();
      e.preventDefault();
    }
  });

  // Before a row disappears, move the cursor to its neighbour.
  document.addEventListener('htmx:beforeSwap', (e) => {
    const t = e.detail.target;
    if (t && t.classList?.contains('item') && t === current) {
      const list = items();
      const i = list.indexOf(t);
      const next = list[i + 1] || list[i - 1];
      select(null);
      if (next) setTimeout(() => select(next), 0);
    }
  });

  document.addEventListener('htmx:afterSettle', () => {
    updateBulk();
    if (current && !document.body.contains(current)) select(null);
    const list = items();
    if (!current && list.length) select(list[0]);
    // Ran out of loaded rows but more are waiting: load the next batch.
    if (!list.length && document.querySelector('[data-more]')) location.reload();
  });

  if (document.getElementById('inbox-list')) select(items()[0]);
})();
