(() => {
  const form = document.getElementById('flows-form');
  const el = document.getElementById('sankey');
  if (!form || !el) return;
  const chart = echarts.init(el, matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : null);
  window.addEventListener('resize', () => chart.resize());
  const textColor = getComputedStyle(document.body).color;
  const fmt = (v) => v.toLocaleString('ru-RU', { maximumFractionDigits: 4 });

  async function load() {
    const q = new URLSearchParams(new FormData(form));
    history.replaceState(null, '', '/flows?' + q);
    const res = await fetch('/flows/data?' + q);
    if (!res.ok) { el.textContent = await res.text(); return; }
    const d = await res.json();
    document.getElementById('flows-summary').textContent =
      t('In: %s · Out: %s', `${fmt(d.in)} ${d.symbol || ''}`, `${fmt(d.out)} ${d.symbol || ''}`);
    const labels = Object.fromEntries((d.nodes || []).map((n) => [n.name, t(n.label)]));
    chart.clear();
    if (!d.links || !d.links.length) {
      chart.setOption({ title: { text: t('No flows in this period'), left: 'center', top: 'middle', textStyle: { color: '#888' } } });
      return;
    }
    chart.setOption({
      backgroundColor: 'transparent',
      textStyle: { color: textColor },
      tooltip: {
        trigger: 'item',
        formatter: (p) => p.dataType === 'edge'
          ? `${labels[p.data.source]} → ${labels[p.data.target]}<br>${fmt(p.data.value)} ${d.symbol} · ${t('%d transfers', p.data.count)}`
          : `${labels[p.name]}: ${fmt(p.value)} ${d.symbol}`,
      },
      series: [{
        type: 'sankey',
        left: 10, right: 160, top: 20, bottom: 20,
        nodeAlign: 'justify',
        emphasis: { focus: 'adjacency' },
        lineStyle: { color: 'gradient', curveness: 0.5, opacity: 0.4 },
        label: { formatter: (p) => labels[p.name], color: textColor, textBorderWidth: 0, fontSize: 13 },
        data: d.nodes.map((n) => ({ name: n.name, itemStyle: n.color ? { color: n.color } : undefined })),
        links: d.links,
      }],
    });
  }

  chart.on('click', (p) => {
    if (p.dataType !== 'edge') return;
    const q = new URLSearchParams(new FormData(form));
    q.delete('min');
    q.set('wallet', p.data.wallet);
    q.set('class', p.data.class);
    location.href = '/transactions?' + q;
  });

  form.addEventListener('change', load);
  load();
})();
