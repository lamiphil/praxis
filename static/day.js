// Load the chosen day immediately, so the page never shows one date
// while the habits (and the save target) belong to another.
const picker = document.querySelector('.date-picker');
const dateInput = picker?.querySelector('input[type="date"]');

if (picker && dateInput) {
  dateInput.addEventListener('change', () => {
    if (dateInput.value && dateInput.checkValidity()) picker.requestSubmit();
  });
  // The input covers the title. Desktop browsers only open the picker from
  // their small icon, so open it on any click.
  dateInput.addEventListener('click', () => {
    try { dateInput.showPicker?.(); } catch { /* already open or not allowed */ }
  });
}

// Keep "x / n faites" in step with the checkboxes.
const doneCount = document.querySelector('[data-done-count]');
const habitBoxes = document.querySelectorAll('input[name="habit"]');
const updateDoneCount = () => {
  if (doneCount) doneCount.textContent = String([...habitBoxes].filter((box) => box.checked).length);
};
habitBoxes.forEach((box) => box.addEventListener('change', updateDoneCount));

const stateLabels = {
  missing: 'Non enregistré',
  goal: 'Dans l’objectif',
  outside: 'Hors objectif',
  recorded: 'Enregistré',
};

const optionalNumber = (raw) => (raw === '' || raw === undefined ? null : Number(raw));

function updateTracker(card) {
  const input = card.querySelector('input[type="number"]');
  const targetMin = optionalNumber(card.dataset.targetMin);
  const targetMax = optionalNumber(card.dataset.targetMax);
  let state = 'missing';
  if (input.value !== '' && input.checkValidity()) {
    const value = Number(input.value);
    if (targetMin === null && targetMax === null) {
      state = 'recorded';
    } else {
      const ok = (targetMin === null || value >= targetMin) && (targetMax === null || value <= targetMax);
      state = ok ? 'goal' : 'outside';
    }
  }
  card.dataset.state = state;
  card.querySelector('.tracker-status').textContent = stateLabels[state];
}

// First tap on an empty tracker starts in the target zone when there is one.
function startValue(card, input, direction) {
  const min = Number(input.min);
  const step = Number(card.dataset.step);
  const targetMin = optionalNumber(card.dataset.targetMin);
  const targetMax = optionalNumber(card.dataset.targetMax);
  if (targetMin !== null && targetMax !== null) return (targetMin + targetMax) / 2;
  return direction > 0 ? min + step : min;
}

document.querySelectorAll('.tracker-card').forEach((card) => {
  const input = card.querySelector('input[type="number"]');
  const step = Number(card.dataset.step);
  card.querySelectorAll('[data-direction]').forEach((button) => {
    button.addEventListener('click', () => {
      const direction = Number(button.dataset.direction);
      const raw = input.value === '' ? startValue(card, input, direction) : Number(input.value) + direction * step;
      const snapped = Math.round(raw / step) * step;
      const clamped = Math.min(Number(input.max), Math.max(Number(input.min), snapped));
      input.value = String(Number(clamped.toFixed(6)));
      updateTracker(card);
    });
  });
  card.querySelector('.clear-tracker').addEventListener('click', () => {
    input.value = '';
    updateTracker(card);
    input.focus();
  });
  input.addEventListener('input', () => updateTracker(card));
  updateTracker(card);
});
