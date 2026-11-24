self.addEventListener('push', (event) => {
  const payload = event.data?.json() || {};
  event.waitUntil(self.registration.showNotification(payload.title || 'Praxis', {
    body: payload.body || 'Il reste des mesures à enregistrer.',
    tag: 'praxis-daily-reminder',
    data: { url: payload.url || '/day' },
  }));
});

self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const target = new URL(event.notification.data?.url || '/day', self.location.origin).href;
  event.waitUntil((async () => {
    const windows = await clients.matchAll({ type: 'window', includeUncontrolled: true });
    const existing = windows.find((client) => new URL(client.url).origin === self.location.origin);
    if (existing) {
      await existing.navigate(target);
      return existing.focus();
    }
    return clients.openWindow(target);
  })());
});
