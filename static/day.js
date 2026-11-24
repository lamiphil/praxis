// Load the chosen day immediately, so the page never shows one date
// while the habits (and the save target) belong to another.
const picker = document.querySelector('.date-picker');
const dateInput = picker?.querySelector('input[type="date"]');

if (picker && dateInput) {
  dateInput.addEventListener('change', () => {
    if (dateInput.value && dateInput.checkValidity()) picker.requestSubmit();
  });
}

const stateLabels = {
  missing: 'Non enregistré',
  goal: 'Dans l’objectif',
  outside: 'Hors objectif',
};

function updateTracker(card) {
  const input = card.querySelector('input');
  const raw = input.value;
  let state = 'missing';
  if (raw !== '' && input.checkValidity()) {
    const value = Number(raw);
    state = card.dataset.tracker === 'sleep'
      ? (value >= 7 && value <= 9 ? 'goal' : 'outside')
      : (value <= 3 ? 'goal' : 'outside');
  }
  card.dataset.state = state;
  card.querySelector('.tracker-status').textContent = stateLabels[state];
}

document.querySelectorAll('.tracker-card').forEach((card) => {
  const input = card.querySelector('input');
  card.querySelectorAll('[data-step]').forEach((button) => {
    button.addEventListener('click', () => {
      const step = Number(button.dataset.step);
      let value;
      if (input.value === '') {
        value = card.dataset.tracker === 'sleep' ? 8 : (step > 0 ? 1 : 0);
      } else {
        value = Number(input.value) + step;
      }
      const min = Number(input.min);
      const max = Number(input.max);
      input.value = String(Math.min(max, Math.max(min, Math.round(value * 4) / 4)));
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
