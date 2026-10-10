/* Create the MAX session before navigation to avoid flashing an intermediate page. */
(function () {
  const link = document.getElementById('source-max');
  if (!link) return;
  let busy = false;
  link.addEventListener('click', async event => {
    event.preventDefault();
    if (busy) return;
    busy = true;
    const origin = new URL(link.href, location.href).searchParams.get('origin');
    const overlay = document.createElement('div');
    overlay.className = 'loading-overlay max-entry-overlay';
    overlay.hidden = true;
    overlay.innerHTML = '<div class="loading-card" role="status"><div class="spinner" aria-hidden="true"></div><h2>Подключаем MAX…</h2><p>Готовим QR-код для вашего документа</p><button type="button" class="secondary-btn" hidden>Закрыть</button></div>';
    document.body.appendChild(overlay);
    const showLoading = setTimeout(() => { overlay.hidden = false; }, 400);
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 20000);
    try {
      const res = await fetch('/api/kiosk/max/print/sessions', {method:'POST', signal:controller.signal});
      const data = await res.json();
      if (!res.ok || !data.session?.id) throw new Error(data.error || 'Не удалось начать ожидание файла');
      clearTimeout(showLoading);
      location.href = '/print/max/wait/?session=' + encodeURIComponent(data.session.id) + (origin === 'home' ? '&origin=home' : '');
    } catch (error) {
      clearTimeout(showLoading);
      overlay.hidden = false;
      overlay.querySelector('.spinner').hidden = true;
      overlay.querySelector('h2').textContent = 'Не удалось подключить MAX';
      overlay.querySelector('p').textContent = error.name === 'AbortError' ? 'Сервер не ответил. Попробуйте ещё раз.' : error.message;
      const close = overlay.querySelector('button');
      close.hidden = false;
      close.addEventListener('click', () => {overlay.remove(); busy = false;});
    } finally { clearTimeout(timeout); clearTimeout(showLoading); }
  });
})();
