(() => {
  const form = document.getElementById('graph-form');
  const el = document.getElementById('graph');
  const panel = document.getElementById('graph-panel');
  if (!form || !el) return;

  const css = (v) => getComputedStyle(document.documentElement).getPropertyValue(v).trim();
  const colors = {
    mine: css('--mine'), exchange: css('--exchange'), external: css('--external'), watch: css('--watch'), shared: css('--out'),
    in: css('--in'), out: css('--out'), text: css('--text'), muted: css('--muted'), panel: css('--panel'),
  };
  const classColor = {
    internal: colors.mine, cex_deposit: colors.exchange, cex_withdrawal: colors.exchange,
    inflow: colors.in, outflow: colors.out, unknown: colors.watch,
  };
  const classLabel = {
    internal: t('Internal'), cex_deposit: t('To exchange'), cex_withdrawal: t('From exchange'),
    inflow: t('Incoming'), outflow: t('Outgoing'), unknown: t('Unknown'),
  };
  const kindLabel = {
    mine: t('Mine'), exchange: t('Exchange'), external: t('External'), watch: t('Watched'),
    unknown: t('Unknown address'), shared: t('Shared counterparty: linked to several wallets'), more: t('Folded addresses'),
  };
  const fmt = (v) => Number(v).toLocaleString('ru-RU', { maximumFractionDigits: 6 });
  const day = (ts) => new Date(ts * 1000).toISOString().slice(0, 10);
  const esc = (s) => String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

  const POS_KEY = 'walletflow.graph.positions';
  const loadPositions = () => { try { return JSON.parse(localStorage.getItem(POS_KEY)) || {}; } catch { return {}; } };
  const savePositions = (p) => { try { localStorage.setItem(POS_KEY, JSON.stringify(p)); } catch {} };

  const cy = cytoscape({
    container: el,
    wheelSensitivity: 0.3,
    minZoom: 0.1,
    maxZoom: 4,
    style: [
      { selector: 'node', style: {
        label: 'data(label)', color: colors.text, 'font-size': 11, 'text-valign': 'bottom', 'text-margin-y': 5,
        'text-wrap': 'ellipsis', 'text-max-width': 140, 'text-background-color': colors.panel,
        'text-background-opacity': 0.7, 'text-background-padding': 2, 'text-background-shape': 'roundrectangle',
        width: 'data(size)', height: 'data(size)', 'background-color': 'data(color)',
        'border-width': 2, 'border-color': colors.panel,
      } },
      { selector: 'node[kind = "mine"]', style: { shape: 'ellipse', 'font-weight': 600, 'font-size': 12 } },
      { selector: 'node[kind = "exchange"]', style: { shape: 'round-rectangle' } },
      { selector: 'node[kind = "external"]', style: { shape: 'diamond' } },
      { selector: 'node[kind = "watch"]', style: { shape: 'hexagon', 'font-weight': 600, 'font-size': 12 } },
      { selector: 'node[kind = "shared"]', style: { shape: 'ellipse', 'background-opacity': 0.85, 'font-size': 10 } },
      { selector: 'node[kind = "unknown"]', style: {
        shape: 'ellipse', 'background-opacity': 0.15, 'border-style': 'dashed', 'border-color': colors.external,
        'font-size': 9, 'min-zoomed-font-size': 8, 'text-background-opacity': 0,
      } },
      { selector: 'node[kind = "more"]', style: { shape: 'round-octagon', 'background-opacity': 0.2, 'border-style': 'dotted', 'border-color': colors.external } },
      { selector: 'edge', style: {
        width: 'data(w)', 'line-color': 'data(color)', 'target-arrow-color': 'data(color)', 'target-arrow-shape': 'triangle',
        'arrow-scale': 0.9, 'curve-style': 'bezier', opacity: 0.75,
      } },
      { selector: 'edge[?unknown]', style: { 'line-style': 'dashed', opacity: 0.5 } },
      { selector: 'edge.labeled[!unknown]', style: {
        label: 'data(count)', 'font-size': 9, color: colors.muted, 'text-rotation': 'autorotate',
        'text-background-color': colors.panel, 'text-background-opacity': 0.8, 'text-background-padding': 1,
      } },
      { selector: '.faded', style: { opacity: 0.1 } },
      { selector: 'node:selected', style: { 'border-color': colors.text, 'border-width': 3 } },
      { selector: 'edge:selected', style: { opacity: 1, 'z-index': 10 } },
    ],
  });

  function layoutOptions(name, fit = true) {
    const base = { name, animate: false, fit, padding: 40 };
    if (name === 'cose') {
      return { ...base, randomize: true, numIter: 2500, gravity: 0.25, componentSpacing: 120, nodeOverlap: 30,
        nodeRepulsion: (n) => (n.data('kind') === 'unknown' ? 60000 : 120000),
        idealEdgeLength: (e) => (e.data('unknown') ? 170 : 140) };
    }
    if (name === 'concentric') {
      const rank = { mine: 4, watch: 4, shared: 3, exchange: 3, external: 2, unknown: 1, more: 0 };
      return { ...base, concentric: (n) => rank[n.data('kind')] ?? 0, levelWidth: () => 1, minNodeSpacing: 40 };
    }
    return { ...base, directed: true, spacingFactor: 1.2, grid: false,
      roots: cy.nodes().filter((n) => n.indegree(false) === 0) };
  }

  // fit shows everything but never zooms a small graph into giant nodes.
  function fit() {
    cy.fit(undefined, 40);
    if (cy.zoom() > 1.2) {
      cy.zoom(1.2);
      cy.center();
    }
  }

  function relayout(clear) {
    if (clear) savePositions({});
    cy.layout({ ...layoutOptions(document.getElementById('graph-layout').value), fit: false }).run();
    fit();
    rememberAll();
  }

  function rememberAll() {
    const p = loadPositions();
    cy.nodes().forEach((n) => { p[n.id()] = n.position(); });
    savePositions(p);
  }

  async function load() {
    const q = new URLSearchParams(new FormData(form));
    if (!form.group.checked) q.set('group', '0');
    history.replaceState(null, '', '/graph?' + q);
    const res = await fetch('/graph/data?' + q);
    if (!res.ok) { panel.textContent = await res.text(); return; }
    const g = await res.json();
    render(g);
  }

  function render(g) {
    const nodes = g.nodes || [], edges = g.edges || [];
    const maxCount = Math.max(1, ...edges.map((e) => e.count));
    const maxNode = Math.max(1, ...nodes.map((n) => n.count));
    const nodeColor = (n) => n.color || colors[n.kind] || colors.external;
    const size = (n) => {
      const base = n.kind === 'mine' || n.kind === 'watch' ? 34 : n.kind === 'unknown' || n.kind === 'more' ? 16 : 24;
      return base + 22 * Math.sqrt(n.count / maxNode);
    };
    cy.elements().remove();
    const label = (n) => (n.kind === 'more' ? `${t(n.label)} (${n.folded || 0})` : n.label);
    cy.add(nodes.map((n) => ({ group: 'nodes', data: { ...n, label: label(n), color: nodeColor(n), size: size(n) } })));
    cy.add(edges.map((e) => ({ group: 'edges', data: {
      ...e, color: classColor[e.class] || colors.muted, w: 1.5 + 8 * Math.log1p(e.count) / Math.log1p(maxCount),
    } })));
    if (edges.length <= 80) cy.edges().addClass('labeled');

    const saved = loadPositions();
    const missing = cy.nodes().filter((n) => !saved[n.id()]);
    if (missing.length === cy.nodes().length) {
      relayout(false);
    } else {
      cy.nodes().forEach((n) => { if (saved[n.id()]) n.position(saved[n.id()]); });
      if (missing.length) {
        // Seed new nodes next to a neighbour, then let the force layout settle
        // from the current positions, so what the user arranged stays recognisable.
        missing.forEach((n) => {
          const nb = n.neighborhood('node').filter((m) => saved[m.id()]);
          const c = nb.length ? nb[0].position() : { x: 0, y: 0 };
          n.position({ x: c.x + (Math.random() - 0.5) * 200, y: c.y + (Math.random() - 0.5) * 200 });
        });
        if (document.getElementById('graph-layout').value === 'cose') {
          cy.layout({ ...layoutOptions('cose', false), randomize: false, numIter: 800 }).run();
        }
        rememberAll();
      }
      fit();
    }

    const unknownCount = nodes.filter((n) => n.kind === 'unknown').length;
    const sharedCount = nodes.filter((n) => n.kind === 'shared').length;
    document.getElementById('graph-summary').textContent =
      t('Nodes: %d, links: %d', nodes.length, edges.length) +
      (sharedCount ? ', ' + t('shared counterparties: %d', sharedCount) : '') +
      (unknownCount ? ', ' + t('unknown addresses: %d', unknownCount) : '') +
      (g.hiddenUnknown ? ' ' + t('(%d more folded)', g.hiddenUnknown) : '') +
      (nodes.length && !edges.length ? '. ' + t('No links found: turn on “shared counterparties” or widen the period.') : '');
    if (!nodes.length) panel.innerHTML = `<p class="muted">${t('No links in this period.')}</p>`;
  }

  function txLink(params) {
    const q = new URLSearchParams();
    for (const k of ['from', 'to', 'asset']) if (form[k].value) q.set(k, form[k].value);
    for (const [k, v] of Object.entries(params)) q.set(k, v);
    return '/transactions?' + q;
  }

  function showNode(n) {
    const d = n.data();
    const edges = n.connectedEdges();
    const inCount = n.incomers('edge').reduce((s, e) => s + e.data('count'), 0);
    const outCount = n.outgoers('edge').reduce((s, e) => s + e.data('count'), 0);
    let html = `<h3>${esc(d.label)}</h3><div class="muted">${kindLabel[d.kind] || d.kind}</div>`;
    if (d.addresses && d.addresses.length) {
      html += '<ul>' + d.addresses.map((a) => `<li><code class="addr" data-copy="${esc(a)}">${esc(a)}</code></li>`).join('') + '</ul>';
    }
    html += `<div>${t('Links: %d · incoming transfers: %d · outgoing: %d', edges.length, inCount, outCount)}</div>`;
    if (d.ref) html += `<div class="row"><a class="button" href="${txLink({ wallet: d.ref })}">${t('Transactions')}</a></div>`;
    if (d.kind === 'unknown' || d.kind === 'shared') {
      html += `<form class="row" id="graph-add">
        <input name="name" placeholder="${t('name, e.g. Bybit')}" required>
        <select name="kind"><option value="external">${t('External')}</option><option value="exchange">${t('Exchange')}</option><option value="watch">${t('Watched')}</option><option value="mine">${t('Mine')}</option></select>
        <button class="primary">${t('Add to the book')}</button></form>`;
    }
    panel.innerHTML = html;
    const addForm = document.getElementById('graph-add');
    if (addForm) addForm.addEventListener('submit', async (e) => {
      e.preventDefault();
      const body = new URLSearchParams(new FormData(addForm));
      body.set('address', d.ref);
      body.set('from', 'graph');
      const res = await fetch('/counterparty', { method: 'POST', body });
      if (res.ok) load(); else panel.insertAdjacentHTML('beforeend', `<p class="error">${esc(await res.text())}</p>`);
    });
  }

  function showEdge(e) {
    const d = e.data();
    const src = e.source().data(), dst = e.target().data();
    let html = `<h3>${esc(src.label)} → ${esc(dst.label)}</h3>
      <div class="muted">${classLabel[d.class] || d.class} · ${t('%d transfers', d.count)}</div>
      <div class="muted">${day(d.first)} … ${day(d.last)}</div><ul>` +
      (d.totals || []).map((t) => `<li><b>${fmt(t.amount)}</b> ${esc(t.symbol)} <span class="muted">${esc(t.chain)}</span></li>`).join('') + '</ul>';
    if (src.ref && dst.ref) html += `<div class="row"><a class="button" href="${txLink({ src: src.ref, dst: dst.ref })}">${t('Transactions of this link')}</a></div>`;
    panel.innerHTML = html;
  }

  cy.on('tap', 'node', (ev) => showNode(ev.target));
  cy.on('tap', 'edge', (ev) => showEdge(ev.target));
  cy.on('mouseover', 'node', (ev) => {
    const keep = ev.target.closedNeighborhood();
    cy.elements().not(keep).addClass('faded');
  });
  cy.on('mouseout', 'node', () => cy.elements().removeClass('faded'));
  cy.on('dragfree', 'node', rememberAll);

  document.getElementById('graph-relayout').addEventListener('click', () => relayout(true));
  document.getElementById('graph-layout').addEventListener('change', () => relayout(true));
  document.getElementById('graph-png').addEventListener('click', () => {
    const a = document.createElement('a');
    a.href = cy.png({ full: true, scale: 2, bg: colors.panel });
    a.download = 'walletflow-graph.png';
    a.click();
  });
  // While a sync runs, redraw every few seconds as history arrives, and once more at the end.
  let lastReload = 0, wasRunning = false;
  document.body.addEventListener('htmx:afterSettle', () => {
    const st = document.getElementById('sync-status');
    if (!st) return;
    const running = st.hasAttribute('hx-get'); // the status polls itself only while running
    if ((running && Date.now() - lastReload > 4000) || (wasRunning && !running)) {
      lastReload = Date.now();
      load();
    }
    wasRunning = running;
  });

  form.addEventListener('change', (e) => { if (e.target.id !== 'graph-layout') load(); });

  // Restore filters from the URL.
  const q = new URLSearchParams(location.search);
  for (const [k, v] of q) {
    const f = form.elements[k];
    if (!f) continue;
    if (f.type === 'checkbox') f.checked = v !== '0';
    else f.value = v;
  }
  if (q.get('group') === '0') form.group.checked = false;
  load();
})();
