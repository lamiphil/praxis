const list = document.querySelector('#habit-list');
const orderInput = document.querySelector('#habit-order');
const saveButton = document.querySelector('#save-order');

if (list && orderInput && saveButton) {
  let initialOrder = '';

  const syncOrder = () => {
    const items = [...list.querySelectorAll('[data-habit-id]')];
    orderInput.value = items.map((item) => item.dataset.habitId).join(',');
    items.forEach((item, index) => {
      item.querySelector('[name="position"]').value = index + 1;
    });
    saveButton.disabled = orderInput.value === initialOrder;
  };

  syncOrder();
  initialOrder = orderInput.value;

  Sortable.create(list, {
    animation: 150,
    chosenClass: 'dragging',
    ghostClass: 'drag-ghost',
    handle: '.drag-handle',
    onEnd: syncOrder,
  });

  list.querySelectorAll('.drag-handle').forEach((handle) => {
    handle.addEventListener('keydown', (event) => {
      const item = handle.closest('[data-habit-id]');
      if (event.key === 'ArrowUp' && item.previousElementSibling) {
        list.insertBefore(item, item.previousElementSibling);
      } else if (event.key === 'ArrowDown' && item.nextElementSibling) {
        list.insertBefore(item.nextElementSibling, item);
      } else {
        return;
      }
      event.preventDefault();
      syncOrder();
      handle.focus();
    });
  });
}
