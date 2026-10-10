/* Shared scan exit warning and delivery recovery actions. */
(function () {
  const scanID = new URLSearchParams(location.search).get('job');
  if (!scanID) return;
  let cached = null;
  let pendingLink = null;
  let bypass = false;
  const dialog = document.createElement('dialog');
  dialog.className = 'modal kiosk-sheet';
  dialog.id = 'scan-exit-dialog';
  dialog.innerHTML = '<div class="name-form"><h2>Вы ещё не сохранили документ</h2><p>Заберите скан перед завершением работы. При выходе документ будет удалён.</p><button type="button" class="primary-btn" id="scan-exit-get">Забрать скан</button><button type="button" class="secondary-btn" id="scan-exit-leave">Завершить без сохранения</button></div>';
  document.body.appendChild(dialog);
  const confirmExit = document.createElement('dialog');
  confirmExit.className = 'modal kiosk-sheet';
  confirmExit.id = 'scan-discard-dialog';
  confirmExit.setAttribute('aria-labelledby', 'scan-discard-title');
  confirmExit.innerHTML = '<div class="name-form"><h2 id="scan-discard-title">Завершить без сохранения?</h2><p>Отсканированный документ будет удалён. Оплата за выполненное сканирование не возвращается — деньги на карту не вернутся.</p><button type="button" class="primary-btn" id="scan-discard-back">Вернуться к документу</button><button type="button" class="secondary-btn" id="scan-discard-confirm">Всё равно завершить</button></div>';
  document.body.appendChild(confirmExit);
  const recovery = document.createElement('dialog');
  recovery.className = 'modal kiosk-sheet';
  recovery.id = 'scan-recovery-dialog';
  recovery.innerHTML = '<div class="name-form"><h2>Не удалось получить документ</h2><p id="scan-recovery-message">Выберите другой способ получения или отмените операцию с возвратом оплаты.</p><button type="button" class="primary-btn" id="scan-recovery-other">Другой способ получения</button><button type="button" class="secondary-btn" id="scan-recovery-cancel">Отменить и вернуть оплату</button><button type="button" class="secondary-btn" id="scan-recovery-stay">Попробовать ещё раз</button></div>';
  document.body.appendChild(recovery);
  async function refresh() {
    const res = await fetch('/api/kiosk/scan/jobs/' + encodeURIComponent(scanID));
    const data = await res.json();
    if (!res.ok) throw new Error(data.error || 'Не удалось проверить документ');
    cached = data.job;
    return cached;
  }
  function unsaved(job) {
    return job && !job.refund_required && (job.page_count > 0 || job.paid) && !job.saved_path && !job.sent_to_email;
  }
  let emptyPaidExit = false;
  function showDiscard(empty) {
    emptyPaidExit = empty;
    confirmExit.querySelector('h2').textContent = empty ? 'Выйти из сканирования?' : 'Завершить без сохранения?';
    confirmExit.querySelector('p').textContent = empty
      ? 'Все отсканированные страницы удалены. Вы можете продолжить сканирование. Если выйти сейчас, оплата за выполненное сканирование не возвращается — деньги на карту не вернутся.'
      : 'Отсканированный документ будет удалён. Оплата за выполненное сканирование не возвращается — деньги на карту не вернутся.';
    confirmExit.querySelector('#scan-discard-back').textContent = empty ? 'Продолжить сканирование' : 'Вернуться к документу';
    confirmExit.querySelector('#scan-discard-confirm').textContent = empty ? 'Да, выйти без возврата денежных средств' : 'Всё равно завершить';
    if (!confirmExit.open) confirmExit.showModal();
  }
  function finish() {
    navigator.sendBeacon('/api/kiosk/session/end', new Blob([JSON.stringify({scan_job_id: scanID})], {type:'application/json'}));
    bypass = true;
    if (pendingLink) pendingLink.click(); else location.href = '/';
  }
  document.addEventListener('click', async (event) => {
    if (bypass) return;
    const link = event.target.closest('a[href]');
    if (!link) return;
    const target = new URL(link.href, location.href);
    if (target.origin !== location.origin) return;
    if (target.hash && target.pathname === location.pathname) return;
    if (target.pathname.startsWith('/scan/') && target.searchParams.get('job') === scanID) return;
    event.preventDefault();
    event.stopImmediatePropagation();
    pendingLink = link;
    try {
      const job = await refresh();
      if (!unsaved(job)) { finish(); return; }
      if (Number(job.page_count || 0) === 0 && job.paid) { showDiscard(true); return; }
      if (!dialog.open) dialog.showModal();
    } catch (_) {
      // A failed state check must not silently discard the visitor's scan.
      if (!dialog.open) dialog.showModal();
    }
  }, true);
  dialog.querySelector('#scan-exit-get').addEventListener('click', () => {
    dialog.close();
    location.href = (cached && cached.page_count > 0 ? '/scan/delivery/' : '/scan/') + '?job=' + encodeURIComponent(scanID);
  });
  dialog.querySelector('#scan-exit-leave').addEventListener('click', () => {
    dialog.close();
    showDiscard(false);
  });
  confirmExit.querySelector('#scan-discard-back').addEventListener('click', () => {
    confirmExit.close();
    pendingLink = null;
    if (emptyPaidExit) location.href = '/scan/?job=' + encodeURIComponent(scanID);
  });
  confirmExit.querySelector('#scan-discard-confirm').addEventListener('click', finish);
  confirmExit.addEventListener('cancel', event => {
    event.preventDefault();
    confirmExit.close();
    pendingLink = null;
  });
  dialog.addEventListener('cancel', event => {event.preventDefault();dialog.close();});
  recovery.querySelector('#scan-recovery-other').addEventListener('click', () => {
    location.href = '/scan/delivery/?job=' + encodeURIComponent(scanID);
  });
  recovery.querySelector('#scan-recovery-stay').addEventListener('click', () => recovery.close());
  recovery.querySelector('#scan-recovery-cancel').addEventListener('click', async () => {
    const button = recovery.querySelector('#scan-recovery-cancel');
    button.disabled = true;
    try {
      const res = await fetch('/api/kiosk/scan/jobs/' + encodeURIComponent(scanID) + '/cancel-refund', {method:'POST'});
      const data = await res.json();
      const message = recovery.querySelector('#scan-recovery-message');
      message.textContent = data.message || data.error || 'Не удалось отменить операцию';
      if (res.ok) {
        recovery.querySelector('#scan-recovery-other').hidden = true;
        button.hidden = true;
        const done = recovery.querySelector('#scan-recovery-stay');
        done.textContent = 'Завершить';
        done.addEventListener('click', () => {pendingLink = null;finish();}, {once:true});
      }
    } catch (_) { recovery.querySelector('#scan-recovery-message').textContent = 'Нет связи. Запросите отмену ещё раз после восстановления соединения.'; }
    finally { button.disabled = false; }
  });
  window.ScanDeliveryRecovery = {
    show: async () => {
      try {await refresh();} catch (_) {}
      recovery.querySelector('#scan-recovery-cancel').hidden = !cached?.delivery_failed;
      if (!recovery.open) recovery.showModal();
    },
    unsaved: () => unsaved(cached),
    refresh,
  };
  refresh().catch(() => {});
})();
