(() => {
  const form = document.getElementById('balances-form');
  const el = document.getElementById('balances-chart');
  if (!form || !el) return;
  const chart = echarts.init(el, matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : null);
  window.addEventListener('resize', () => chart.resize());
  const textColor = getComputedStyle(document.body).color;
  const fmt = (v) => v.toLocaleString('ru-RU', { maximumFractionDigits: 4 });

  async function load() {
    const q = new URLSearchParams(new FormData(form));
    history.replaceState(null, '', '/balances?' + q);
    const res = await fetch('/balances/data?' + q);
    if (!res.ok) { el.textContent = await res.text(); return; }
    const d = await res.json();
    chart.clear();
    const series = d.series || [];
    const total = series.reduce((s, x) => s + (x.values.at(-1) || 0), 0);
    document.getElementById('balances-summary').textContent =
      series.length ? t('Total now: %s', `${fmt(total)} ${d.symbol}`) : t('No movements in your wallets');
    chart.setOption({
      backgroundColor: 'transparent',
      textStyle: { color: textColor },
      tooltip: { trigger: 'axis', valueFormatter: (v) => `${fmt(v)} ${d.symbol}` },
      legend: { top: 0, textStyle: { color: textColor } },
      grid: { left: 60, right: 30, top: 40, bottom: 70 },
      xAxis: { type: 'category', data: d.days, boundaryGap: false, axisLabel: { color: textColor } },
      yAxis: { type: 'value', axisLabel: { color: textColor } },
      dataZoom: [{ type: 'inside' }, { type: 'slider' }],
      series: series.map((s) => ({
        name: s.name, type: 'line', stack: 'total', step: 'end', symbol: 'none',
        areaStyle: { opacity: 0.5 }, lineStyle: { width: 1 },
        itemStyle: s.color ? { color: s.color } : undefined,
        data: s.values,
      })),
    });
  }

  form.addEventListener('change', load);
  load();
})();
