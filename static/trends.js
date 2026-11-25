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

function drawChart(container, measurement) {
  const width = 900;
  const height = 260;
  const margin = { top: 18, right: 18, bottom: 38, left: 44 };
  const plotWidth = width - margin.left - margin.right;
  const plotHeight = height - margin.top - margin.bottom;
  const { low, top, ticks } = axis(measurement);
  const count = data.dates.length;
  const svg = svgElement('svg', { viewBox: `0 0 ${width} ${height}`, 'aria-hidden': 'true' });
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
  data.dates.forEach((date, index) => {
    if (index % 5 !== 0 && index !== count - 1) return;
    addText(svg, x(index), height - 12, date.slice(5));
  });

  const stateClass = (value, kind) => {
    if (!hasTarget(measurement)) return `${kind}-neutral`;
    return inTarget(measurement, value) ? `${kind}-goal` : `${kind}-outside`;
  };

  if (measurement.integer) {
    const barWidth = Math.max(5, plotWidth / count - 5);
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
      points.push(svgElement('circle', { cx: x(index), cy: y(value), r: 4, class: stateClass(value, 'point') }));
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

data.measurements.forEach((measurement) => {
  const container = document.querySelector(`[data-chart="${CSS.escape(measurement.key)}"]`);
  if (container) drawChart(container, measurement);
});
fillTable();
