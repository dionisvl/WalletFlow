(() => {
  const form = document.getElementById('graph-form');
  const el = document.getElementById('graph');
  const panel = document.getElementById('graph-panel');
  if (!form || !el) return;

  const css = (v) => getComputedStyle(document.documentElement).getPropertyValue(v).trim();
  const colors = {
    mine: css('--mine'), exchange: css('--exchange'), external: css('--external'),
    in: css('--in'), out: css('--out'), text: css('--text'), muted: css('--muted'), panel: css('--panel'),
  };
  const classColor = {
    internal: colors.mine, cex_deposit: colors.exchange, cex_withdrawal: colors.exchange,
    inflow: colors.in, outflow: colors.out, unknown: colors.muted,
  };
  const classLabel = {
    internal: 'Внутренний', cex_deposit: 'На биржу', cex_withdrawal: 'С биржи',
    inflow: 'Входящий', outflow: 'Исходящий', unknown: 'Неизвестно',
  };
  const kindLabel = { mine: 'Мой', exchange: 'Биржа', external: 'Чужой', unknown: 'Неизвестный адрес', more: 'Свёрнутые адреса' };
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
      const rank = { mine: 4, exchange: 3, external: 2, unknown: 1, more: 0 };
      return { ...base, concentric: (n) => rank[n.data('kind')] ?? 0, levelWidth: () => 1, minNodeSpacing: 40 };
    }
    return { ...base, directed: true, spacingFactor: 1.2, grid: false,
      roots: cy.nodes().filter((n) => n.indegree(false) === 0) };
  }

  function relayout(clear) {
    if (clear) savePositions({});
    cy.layout(layoutOptions(document.getElementById('graph-layout').value)).run();
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
      const base = n.kind === 'mine' ? 34 : n.kind === 'unknown' || n.kind === 'more' ? 16 : 26;
      return base + 22 * Math.sqrt(n.count / maxNode);
    };
    cy.elements().remove();
    cy.add(nodes.map((n) => ({ group: 'nodes', data: { ...n, color: nodeColor(n), size: size(n) } })));
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
        // Place new nodes around their neighbours, keep the rest where the user left them.
        missing.forEach((n) => {
          const nb = n.neighborhood('node').filter((m) => saved[m.id()]);
          const c = nb.length ? nb[0].position() : { x: 0, y: 0 };
          n.position({ x: c.x + (Math.random() - 0.5) * 160, y: c.y + (Math.random() - 0.5) * 160 });
        });
        rememberAll();
      }
      cy.fit(undefined, 40);
    }

    const unknownCount = nodes.filter((n) => n.kind === 'unknown').length;
    document.getElementById('graph-summary').textContent =
      `Узлов: ${nodes.length}, связей: ${edges.length}` +
      (unknownCount ? `, неизвестных адресов: ${unknownCount}` : '') +
      (g.hiddenUnknown ? ` (ещё ${g.hiddenUnknown} свёрнуто)` : '');
    if (!nodes.length) panel.innerHTML = '<p class="muted">Нет связей за выбранный период.</p>';
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
    html += `<div>Связей: ${edges.length} · входящих переводов: ${inCount} · исходящих: ${outCount}</div>`;
    if (d.ref) html += `<div class="row"><a class="button" href="${txLink({ wallet: d.ref })}">Транзакции</a></div>`;
    if (d.kind === 'unknown') {
      html += `<form class="row" id="graph-add">
        <input name="name" placeholder="имя, напр. Bybit" required>
        <select name="kind"><option value="external">Чужой</option><option value="exchange">Биржа</option><option value="mine">Мой</option></select>
        <button class="primary">В адресную книгу</button></form>`;
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
      <div class="muted">${classLabel[d.class] || d.class} · ${d.count} перевод(ов)</div>
      <div class="muted">${day(d.first)} … ${day(d.last)}</div><ul>` +
      (d.totals || []).map((t) => `<li><b>${fmt(t.amount)}</b> ${esc(t.symbol)} <span class="muted">${esc(t.chain)}</span></li>`).join('') + '</ul>';
    if (src.ref && dst.ref) html += `<div class="row"><a class="button" href="${txLink({ src: src.ref, dst: dst.ref })}">Транзакции этой связи</a></div>`;
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
