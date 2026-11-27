const dataElement = document.querySelector('#trend-data');
const data = dataElement ? JSON.parse(dataElement.textContent) : { dates: [], measurements: [] };
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

const formatNumber = (value) => String(Number(value.toFixed(2))).replace('.', ',');

function inTarget(measurement, value) {
  return (measurement.targetMin == null || value >= measurement.targetMin)
    && (measurement.targetMax == null || value <= measurement.targetMax);
}

function hasTarget(measurement) {
  return measurement.targetMin != null || measurement.targetMax != null;
}

// Axis from the measurement minimum to a rounded top covering values and targets.
function axis(measurement) {
  const values = measurement.values.filter((value) => value != null);
  const low = measurement.min;
  let high = Math.max(low + 1, ...values, measurement.targetMax ?? measurement.targetMin ?? low + 1);
  high = Math.min(measurement.max, low + (high - low) * 1.25);
  const rawStep = (high - low) / 4;
  const magnitude = 10 ** Math.floor(Math.log10(rawStep));
  const tickStep = [1, 2, 2.5, 5, 10].map((factor) => factor * magnitude).find((step) => step >= rawStep);
  const top = Math.max(low + tickStep, Math.ceil((high - low) / tickStep) * tickStep + low);
  const ticks = [];
  for (let tick = low; tick <= top + 1e-9; tick += tickStep) ticks.push(Number(tick.toFixed(6)));
  return { low, top, ticks };
}

const months = ['janv.', 'févr.', 'mars', 'avr.', 'mai', 'juin', 'juil.', 'août', 'sept.', 'oct.', 'nov.', 'déc.'];
const shortDate = (iso) => `${Number(iso.slice(8))} ${months[Number(iso.slice(5, 7)) - 1]}`;

// Drawn at the container's pixel width so text keeps its CSS size on phones.
function drawChart(container, measurement) {
  const width = Math.max(260, Math.round(container.clientWidth || 900));
  const height = width < 520 ? 190 : 240;
  const margin = { top: 14, right: 12, bottom: 30, left: 36 };
  const plotWidth = width - margin.left - margin.right;
  const plotHeight = height - margin.top - margin.bottom;
  const { low, top, ticks } = axis(measurement);
  const count = data.dates.length;
  const svg = svgElement('svg', { viewBox: `0 0 ${width} ${height}`, width, height, 'aria-hidden': 'true' });
  const x = (index) => margin.left + (index / (count - 1)) * plotWidth;
  const y = (value) => margin.top + plotHeight - ((Math.min(value, top) - low) / (top - low)) * plotHeight;

  if (hasTarget(measurement)) {
    const bandTop = y(measurement.targetMax ?? top);
    svg.append(svgElement('rect', {
      x: margin.left, y: bandTop, width: plotWidth, height: y(measurement.targetMin ?? low) - bandTop, class: 'goal-band',
    }));
  }
  ticks.forEach((tick) => {
    svg.append(svgElement('line', { x1: margin.left, y1: y(tick), x2: width - margin.right, y2: y(tick), class: 'chart-grid' }));
    addText(svg, margin.left - 8, y(tick) + 4, formatNumber(tick), 'end');
  });
  [measurement.targetMin, measurement.targetMax].forEach((target) => {
    if (target == null) return;
    svg.append(svgElement('line', { x1: margin.left, y1: y(target), x2: width - margin.right, y2: y(target), class: 'goal-line' }));
  });
  // One label per ~64px, always ending on the last day.
  const every = Math.max(1, Math.ceil(count / Math.floor(plotWidth / 64)));
  data.dates.forEach((date, index) => {
    const last = index === count - 1;
    if (!last && ((count - 1 - index) % every !== 0 || count - 1 - index < every)) return;
    addText(svg, x(index), height - 10, shortDate(date), last ? 'end' : 'middle');
  });

  const stateClass = (value, kind) => {
    if (!hasTarget(measurement)) return `${kind}-neutral`;
    return inTarget(measurement, value) ? `${kind}-goal` : `${kind}-outside`;
  };

  if (measurement.integer) {
    const barWidth = Math.max(3, Math.min(18, plotWidth / count * 0.6));
    measurement.values.forEach((value, index) => {
      if (value == null) return;
      svg.append(svgElement('rect', {
        x: x(index) - barWidth / 2, y: y(value), width: barWidth, height: Math.max(1, y(low) - y(value)), rx: 3, class: stateClass(value, 'bar'),
      }));
    });
  } else {
    let segment = [];
    const flush = () => {
      if (!segment.length) return;
      const path = segment.map((point, index) => `${index ? 'L' : 'M'} ${point[0]} ${point[1]}`).join(' ');
      svg.append(svgElement('path', { d: path, class: 'value-line' }));
      segment = [];
    };
    const points = [];
    measurement.values.forEach((value, index) => {
      if (value == null) {
        flush();
        return;
      }
      segment.push([x(index), y(value)]);
      points.push(svgElement('circle', { cx: x(index), cy: y(value), r: width < 520 ? 3 : 4, class: stateClass(value, 'point') }));
    });
    flush();
    svg.append(...points);
  }
  container.replaceChildren(svg);
}

function fillTable() {
  const body = document.querySelector('#trend-table tbody');
  if (!body) return;
  data.dates.map((date, index) => index).reverse().forEach((index) => {
    const row = document.createElement('tr');
    const cells = [data.dates[index], ...data.measurements.map((measurement) => {
      const value = measurement.values[index];
      if (value == null) return '—';
      return measurement.unit ? `${formatNumber(value)} ${measurement.unit}` : formatNumber(value);
    })];
    cells.forEach((value) => {
      const cell = document.createElement('td');
      cell.textContent = value;
      row.append(cell);
    });
    body.append(row);
  });
}

const ratingChartModel = () => ({
  key: '__rating__', name: 'Note', unit: '', integer: true,
  min: 1, max: 13, targetMin: null, targetMax: null,
  values: data.rating.values.map((value) => (value == null ? null : Number(value))),
});

function drawAll() {
  data.measurements.forEach((measurement) => {
    const container = document.querySelector(`[data-chart="${CSS.escape(measurement.key)}"]`);
    if (container) drawChart(container, measurement);
  });
  if (data.rating) {
    const container = document.querySelector('[data-chart="__rating__"]');
    if (container) drawChart(container, ratingChartModel());
  }
}

drawAll();
fillTable();
let lastWidth = document.documentElement.clientWidth;
window.addEventListener('resize', () => {
  const widthNow = document.documentElement.clientWidth;
  if (widthNow === lastWidth) return;
  lastWidth = widthNow;
  requestAnimationFrame(drawAll);
});
