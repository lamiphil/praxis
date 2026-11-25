// Drag-and-drop ordering shared by the habits and measurements pages.
const list = document.querySelector('[data-sortable]');
const orderInput = document.querySelector('[data-sort-order]');
const saveButton = document.querySelector('[data-sort-save]');

if (list && orderInput && saveButton) {
  let initialOrder = '';

  const syncOrder = () => {
    const items = [...list.querySelectorAll('[data-sort-id]')];
    orderInput.value = items.map((item) => item.dataset.sortId).join(',');
    items.forEach((item, index) => {
      const position = item.querySelector('[name="position"]');
      if (position) position.value = index + 1;
    });
    saveButton.disabled = orderInput.value === initialOrder;
  };

  syncOrder();
  initialOrder = orderInput.value;
  saveButton.disabled = true;

  Sortable.create(list, {
    animation: 150,
    chosenClass: 'dragging',
    ghostClass: 'drag-ghost',
    handle: '.drag-handle',
    onEnd: syncOrder,
  });

  list.querySelectorAll('.drag-handle').forEach((handle) => {
    handle.addEventListener('keydown', (event) => {
      const item = handle.closest('[data-sort-id]');
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
