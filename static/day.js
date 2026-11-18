// Load the chosen day immediately, so the page never shows one date
// while the habits (and the save target) belong to another.
const picker = document.querySelector('.date-picker');
const dateInput = picker?.querySelector('input[type="date"]');

if (picker && dateInput) {
  dateInput.addEventListener('change', () => {
    if (dateInput.value && dateInput.checkValidity()) picker.requestSubmit();
  });
}
