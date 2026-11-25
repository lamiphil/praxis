const data = JSON.parse(document.querySelector('#trend-data').textContent);
const svgNS = 'http://www.w3.org/2000/svg';

function svgElement(name, attributes = {}) {
  const element = document.createElementNS(svgNS, name);
  Object.entries(attributes).forEach(([key, value]) => element.setAttribute(key, value));
  return element;
}

function addText(svg, x, y, text, anchor = 'middle') {
  const label = svgElement('text', { x, y, 'text-anchor': anchor, class: 'chart-label' });
  label.textContent = text;
  svg.append(label);
}

function baseChart(container, maxY, ticks) {
  const width = 900;
  const height = 260;
  const margin = { top: 18, right: 18, bottom: 38, left: 44 };
  const plotWidth = width - margin.left - margin.right;
  const plotHeight = height - margin.top - margin.bottom;
  const svg = svgElement('svg', { viewBox: `0 0 ${width} ${height}`, 'aria-hidden': 'true' });
  const x = (index) => margin.left + (index / (data.length - 1)) * plotWidth;
  const y = (value) => margin.top + plotHeight - (value / maxY) * plotHeight;

  ticks.forEach((tick) => {
    svg.append(svgElement('line', { x1: margin.left, y1: y(tick), x2: width - margin.right, y2: y(tick), class: 'chart-grid' }));
    addText(svg, margin.left - 8, y(tick) + 4, String(tick), 'end');
  });
  data.forEach((point, index) => {
    if (index % 5 !== 0 && index !== data.length - 1) return;
    addText(svg, x(index), height - 12, point.date.slice(5));
  });
  container.replaceChildren(svg);
  return { svg, x, y, width, height, margin, plotHeight };
}

function drawSleep() {
  const values = data.flatMap((point) => point.sleepHours == null ? [] : [point.sleepHours]);
  const maxY = Math.max(12, Math.ceil(Math.max(0, ...values)));
  const chart = baseChart(document.querySelector('#sleep-chart'), maxY, [0, 3, 6, 9, 12].filter((tick) => tick <= maxY));
  chart.svg.insertBefore(svgElement('rect', {
    x: chart.margin.left,
    y: chart.y(9),
    width: chart.width - chart.margin.left - chart.margin.right,
    height: chart.y(7) - chart.y(9),
    class: 'goal-band',
  }), chart.svg.firstChild);

  let segment = [];
  const flush = () => {
    if (!segment.length) return;
    const path = segment.map((point, index) => `${index ? 'L' : 'M'} ${point[0]} ${point[1]}`).join(' ');
    chart.svg.append(svgElement('path', { d: path, class: 'sleep-line' }));
    segment = [];
  };
  data.forEach((point, index) => {
    if (point.sleepHours == null) {
      flush();
      return;
    }
    const cx = chart.x(index);
    const cy = chart.y(point.sleepHours);
    segment.push([cx, cy]);
    chart.svg.append(svgElement('circle', { cx, cy, r: 4, class: point.sleepHours >= 7 && point.sleepHours <= 9 ? 'point-goal' : 'point-outside' }));
  });
  flush();
}

function drawCoffee() {
  const values = data.flatMap((point) => point.coffee == null ? [] : [point.coffee]);
  const maxY = Math.max(6, Math.ceil(Math.max(0, ...values)));
  const ticks = Array.from(new Set([0, 3, Math.ceil(maxY / 2), maxY])).sort((a, b) => a - b);
  const chart = baseChart(document.querySelector('#coffee-chart'), maxY, ticks);
  chart.svg.append(svgElement('line', {
    x1: chart.margin.left,
    y1: chart.y(3),
    x2: chart.width - chart.margin.right,
    y2: chart.y(3),
    class: 'goal-line',
  }));
  const barWidth = Math.max(5, (chart.width - chart.margin.left - chart.margin.right) / data.length - 5);
  data.forEach((point, index) => {
    if (point.coffee == null) return;
    chart.svg.append(svgElement('rect', {
      x: chart.x(index) - barWidth / 2,
      y: chart.y(point.coffee),
      width: barWidth,
      height: chart.y(0) - chart.y(point.coffee),
      rx: 3,
      class: point.coffee <= 3 ? 'bar-goal' : 'bar-outside',
    }));
  });
}

function fillTable() {
  const body = document.querySelector('#trend-table tbody');
  data.slice().reverse().forEach((point) => {
    const row = document.createElement('tr');
    [point.date, point.sleepHours == null ? '—' : `${point.sleepHours} h`, point.coffee == null ? '—' : point.coffee]
      .forEach((value) => {
        const cell = document.createElement('td');
        cell.textContent = value;
        row.append(cell);
      });
    body.append(row);
  });
}

drawSleep();
drawCoffee();
fillTable();
