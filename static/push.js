function vapidKey(value) {
  const padding = '='.repeat((4 - value.length % 4) % 4);
  const binary = atob((value + padding).replace(/-/g, '+').replace(/_/g, '/'));
  return Uint8Array.from(binary, (character) => character.charCodeAt(0));
}

async function postSubscription(path, subscription) {
  const response = await fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(subscription),
  });
  if (response.status !== 204) throw new Error(`HTTP ${response.status}`);
}

async function configurePush() {
  const card = document.querySelector('.notification-card');
  const button = document.querySelector('#push-toggle');
  const status = document.querySelector('#push-status');
  if (!button) return;
  if (!('serviceWorker' in navigator) || !('PushManager' in window) || !('Notification' in window)) {
    button.disabled = true;
    status.textContent = 'Ce navigateur ne prend pas en charge Web Push.';
    return;
  }

  const registration = await navigator.serviceWorker.register('/sw.js', { scope: '/' });
  let subscription = await registration.pushManager.getSubscription();
  let syncError = '';
  if (subscription) {
    try {
      await postSubscription('/push/subscribe', subscription.toJSON());
    } catch (error) {
      syncError = `Abonnement local actif, mais non synchronisé: ${error.message}`;
    }
  }
  const render = () => {
    button.textContent = subscription ? 'Désactiver sur cet appareil' : 'Activer sur cet appareil';
    status.textContent = syncError || (subscription ? 'Rappel actif sur cet appareil.' : 'Rappel inactif sur cet appareil.');
  };
  render();

  button.addEventListener('click', async () => {
    button.disabled = true;
    try {
      if (subscription) {
        await postSubscription('/push/unsubscribe', { endpoint: subscription.endpoint });
        await subscription.unsubscribe();
        subscription = null;
        syncError = '';
      } else {
        const permission = await Notification.requestPermission();
        if (permission !== 'granted') throw new Error('permission refusée');
        const created = await registration.pushManager.subscribe({
          userVisibleOnly: true,
          applicationServerKey: vapidKey(card.dataset.vapidKey),
        });
        try {
          await postSubscription('/push/subscribe', created.toJSON());
          subscription = created;
          syncError = '';
        } catch (error) {
          await created.unsubscribe();
          throw error;
        }
      }
      render();
    } catch (error) {
      status.textContent = `Impossible de modifier le rappel: ${error.message}`;
    } finally {
      button.disabled = false;
    }
  });
}

configurePush().catch((error) => {
  const status = document.querySelector('#push-status');
  if (status) status.textContent = `Notifications indisponibles: ${error.message}`;
});
