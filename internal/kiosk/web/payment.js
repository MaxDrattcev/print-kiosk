/**
 * Shared kiosk payment UI for print, copy, and scan.
 * Does not change API contracts or recalculate amounts.
 */
(function () {
  const CARD_ICON =
    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' +
    '<rect x="2.5" y="5" width="19" height="14" rx="2.5"/>' +
    '<path d="M2.5 10h19"/>' +
    '<path d="M6.5 15h4"/>' +
    "</svg>";

  const NFC_ICON =
    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" aria-hidden="true">' +
    '<path d="M6.2 8.2c3.2-3.1 8.4-3.1 11.6 0"/>' +
    '<path d="M8.1 11c2.1-2 5.7-2 7.8 0"/>' +
    '<path d="M10.2 13.6c.9-.9 2.7-.9 3.6 0"/>' +
    '<circle cx="12" cy="16.4" r="1.15" fill="currentColor" stroke="none"/>' +
    "</svg>";

  const QR_ICON = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="3" y="3" width="6" height="6" rx="1"/><rect x="15" y="3" width="6" height="6" rx="1"/><rect x="3" y="15" width="6" height="6" rx="1"/><path d="M15 15h3v3h3v3h-6v-3h-3v-3M12 3v6M3 12h6M12 12h6M21 12v3"/></svg>';

  const METHOD_HTML =
    '<dialog class="modal payment-screen" id="method-modal">' +
    '<div class="payment-card">' +
    '<h2 class="payment-title">Выберите способ оплаты</h2>' +
    '<p class="payment-due-label">К оплате:</p>' +
    '<p class="payment-amount" id="pay-sum">—</p>' +
    '<div class="payment-methods">' +
    '<button type="button" class="payment-method" id="pay-terminal-btn">' +
    '<span class="payment-method__top">' +
    '<span class="payment-method__icon">' +
    CARD_ICON +
    "</span>" +
    '<span class="payment-method__copy">' +
    '<span class="payment-method__title">Оплатить картой</span>' +
    '<span class="payment-method__subtitle">Нажмите, чтобы перейти к оплате</span>' +
    "</span>" +
    "</span>" +
    '<span class="payment-method__hint">' +
    '<span class="payment-method__hint-icon">' +
    NFC_ICON +
    "</span>" +
    "После нажатия приложите карту или телефон к терминалу" +
    "</span>" +
    "</button>" +
    '<button type="button" class="payment-method payment-method--sbp" id="pay-qr-btn" hidden>' +
    '<span class="payment-method__top"><span class="payment-method__icon">' + QR_ICON + '</span>' +
    '<span class="payment-method__copy"><span class="payment-method__title">Оплатить по QR-коду СБП</span>' +
    '<span class="payment-method__subtitle">Нажмите, чтобы перейти к оплате</span></span></span>' +
    '<span class="payment-method__hint"><span class="payment-method__hint-icon">' + QR_ICON + '</span>' +
    'Отсканируйте код телефоном и оплатите в приложении банка</span></button>' +
    '</div>' +
    '<button type="button" class="payment-cancel" id="method-cancel">← Отмена</button>' +
    '<p class="payment-secure"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><rect x="5" y="10" width="14" height="10" rx="3"/><path d="M8 10V7a4 4 0 0 1 8 0v3"/><path d="m10 15 1.5 1.5L15 13"/></svg>Безопасная оплата</p>' +
    "</div>" +
    "</dialog>";

  const WAIT_HTML =
    '<dialog class="modal payment-screen" id="terminal-modal">' +
    '<div class="payment-card payment-card--waiting">' +
    '<div class="payment-tap-scene" aria-hidden="true">' +
    '<span class="payment-tap-glow"></span>' +
    '<span class="payment-tap-sparkles"></span>' +
    '<span class="payment-terminal-art"><i class="payment-terminal-screen"></i><i class="payment-terminal-keypad"></i></span>' +
    '<span class="payment-tap-waves"><i></i><i></i><i></i></span>' +
    '<span class="payment-card-hand"><i class="payment-bank-card"><b></b></i><i class="payment-hand-palm"></i><i class="payment-hand-wrist"></i></span>' +
    "</div>" +
    '<p class="payment-waiting-kicker">Платёжное волшебство</p>' +
    '<h2 class="payment-title">Поднесите карту к терминалу</h2>' +
    '<p class="payment-waiting-text">Приложите карту или телефон и подержите, пока терминал не подаст сигнал</p>' +
    '<div class="dots-loader" aria-hidden="true"><span></span><span></span><span></span></div>' +
    "</div>" +
    "</dialog>";

  const QR_HTML = '<dialog class="modal payment-screen" id="qr-payment-modal"><div class="payment-card payment-card--sbp">' +
    '<h2 class="payment-title">Оплата по QR-коду СБП</h2><p class="payment-amount" id="qr-payment-amount">—</p>' +
    '<div class="sbp-qr-frame"><img id="qr-payment-image" alt="QR-код для оплаты СБП" width="768" height="768" hidden><p id="qr-payment-loading">Готовим QR-код…</p></div>' +
    '<p id="qr-payment-status" aria-live="polite">Отсканируйте код камерой телефона и подтвердите оплату в приложении банка</p>' +
    '<p id="qr-payment-countdown"></p><p id="qr-payment-test" hidden>Тестовый режим — деньги не списываются. Этот QR-код не предназначен для оплаты.</p>' +
    '<button type="button" class="primary-btn" id="qr-payment-confirm" hidden>Подтвердить тестовую оплату</button>' +
    '<button type="button" class="payment-cancel" id="qr-payment-cancel">Отменить оплату</button></div></dialog>';
  let selectedMethod = 'terminal';
  let qrAttempt = '';
  let qrTimer = null;
  let cancelRequested = false;
  let qrGeneration = 0;
  let qrBusy = false;

  async function pollQR(generation) {
    if (generation !== qrGeneration || qrBusy || !qrAttempt) return;
    qrBusy = true;
    try {
      const attempt = qrAttempt;
      if (cancelRequested) await fetch('/api/kiosk/payment/qr/' + attempt + '/cancel', {method:'POST'});
      const res = await fetch('/api/kiosk/payment/qr/' + attempt, {cache:'no-store'});
      if (!res.ok) return;
      const data = await res.json();
      if (generation !== qrGeneration) return;
      $('qr-payment-test').hidden = !data.test;
      $('qr-payment-confirm').hidden = !data.test || data.state !== 'pending' || cancelRequested;
      $('qr-payment-amount').textContent = Number(data.amount).toLocaleString('ru-RU') + ' ₽';
      const left = Math.max(0, data.expires - Math.floor(Date.now()/1000));
      $('qr-payment-countdown').textContent = 'Осталось ' + Math.floor(left/60) + ':' + String(left%60).padStart(2,'0');
      const image = $('qr-payment-image');
      if (data.qr && !cancelRequested && image.dataset.ready !== attempt) {
        image.src = data.qr;
        await image.decode();
        if (generation !== qrGeneration || cancelRequested) return;
        image.dataset.ready = attempt; image.hidden = false; $('qr-payment-loading').hidden = true;
      }
      if (data.state !== 'pending' || cancelRequested || left === 0) {
        image.hidden = true; $('qr-payment-loading').hidden = false;
        $('qr-payment-loading').textContent = data.state === 'paid' ? 'Оплата получена' : 'Проверяем результат…';
        $('qr-payment-status').textContent = cancelRequested ? 'Проверяем отмену. Дождитесь ответа, чтобы избежать повторной оплаты.' : 'Проверяем результат оплаты…';
      }
    } catch (_) {
      if (generation === qrGeneration) $('qr-payment-status').textContent = 'Проверяем связь и результат оплаты…';
    } finally { qrBusy = false; }
  }
  function showQR() {
    cancelRequested = false;
    const generation = ++qrGeneration;
    const image = $('qr-payment-image'); image.hidden = true; image.removeAttribute('src'); image.dataset.ready = '';
    $('qr-payment-loading').hidden = false; $('qr-payment-loading').textContent = 'Готовим QR-код…';
    $('qr-payment-status').textContent = 'Отсканируйте код камерой телефона и подтвердите оплату в приложении банка';
    $('qr-payment-confirm').hidden = true; $('qr-payment-test').hidden = true;
    $('qr-payment-cancel').disabled = false; $('qr-payment-countdown').textContent = '';
    $('qr-payment-amount').textContent = $('pay-sum').textContent;
    $('qr-payment-modal').showModal();
    qrTimer = setInterval(() => pollQR(generation),1000);
  }

  function $(id) {
    return document.getElementById(id);
  }

  function mount() {
    if (!$("method-modal")) {
      document.body.insertAdjacentHTML("beforeend", METHOD_HTML);
    }
    if (!$("terminal-modal")) {
      document.body.insertAdjacentHTML("beforeend", WAIT_HTML);
    }
    if (!$('qr-payment-modal')) {
      document.body.insertAdjacentHTML('beforeend', QR_HTML);
      $('qr-payment-modal').addEventListener('cancel', e => e.preventDefault());
      document.body.insertAdjacentHTML('beforeend', '<dialog class="modal payment-screen" id="qr-cancel-confirm"><div class="payment-card"><h2 class="payment-title">Отменить оплату?</h2><p>Вы вернётесь к выбору способа оплаты.</p><button type="button" class="primary-btn" id="qr-keep-paying">Продолжить оплату</button><button type="button" class="payment-cancel" id="qr-confirm-cancel">Да, отменить оплату</button></div></dialog>');
      $('qr-payment-cancel').addEventListener('click', () => $('qr-cancel-confirm').showModal());
      $('qr-keep-paying').addEventListener('click', () => $('qr-cancel-confirm').close());
      $('qr-confirm-cancel').addEventListener('click', () => {
        $('qr-cancel-confirm').close();
        cancelRequested = true; $('qr-payment-cancel').disabled = true;
        $('qr-payment-image').hidden = true; $('qr-payment-confirm').hidden = true;
        pollQR(qrGeneration);
      });
      $('qr-payment-confirm').addEventListener('click', async () => {
        $('qr-payment-confirm').disabled = true;
        try { await fetch('/api/kiosk/payment/qr/' + qrAttempt + '/test-confirm',{method:'POST'}); }
        finally { $('qr-payment-confirm').disabled = false; }
      });
      $('pay-qr-btn').addEventListener('click', () => {
        qrAttempt = crypto.randomUUID(); selectedMethod = 'qr:' + qrAttempt;
        $('pay-terminal-btn').click();
      });
      $('pay-terminal-btn').addEventListener('click', e => {
        if (e.isTrusted) { selectedMethod = 'terminal'; qrAttempt = ''; }
      }, true);
      fetch('/api/kiosk/info').then(r => r.json()).then(info => { $('pay-qr-btn').hidden = !info.payment_qr; }).catch(() => {});
    }
    const cancel = $("method-cancel");
    const method = $("method-modal");
    if (cancel && method && cancel.dataset.kioskBound !== "1") {
      cancel.dataset.kioskBound = "1";
      cancel.addEventListener("click", () => {
        close();
        if (window.KioskStages) window.KioskStages.reset();
      });
    }
    [method, $("terminal-modal")].forEach((dialog) => {
      if (!dialog || dialog.dataset.kioskCancelBound === "1") return;
      dialog.dataset.kioskCancelBound = "1";
      dialog.addEventListener("cancel", (e) => e.preventDefault());
    });
  }

  function setAmount(text) {
    const el = $("pay-sum");
    if (el) el.textContent = text;
  }

  function open(amountText) {
    selectedMethod = "terminal"; qrAttempt = "";
    if (amountText != null && amountText !== "") setAmount(amountText);
    if (window.KioskStages) window.KioskStages.payment();
    const d = $("method-modal");
    if (d && typeof d.showModal === "function" && !d.open) d.showModal();
  }

  function close() {
    const d = $("method-modal");
    if (d && d.open) d.close();
  }

  function showWaiting() {
    close();
    if (selectedMethod.startsWith("qr:")) { showQR(); return; }
    const d = $("terminal-modal");
    if (d && typeof d.showModal === "function" && !d.open) d.showModal();
  }

  function closeWaiting() {
    const confirmation = $('qr-cancel-confirm'); if (confirmation?.open) confirmation.close();
    ++qrGeneration; if (qrTimer) clearInterval(qrTimer); qrTimer = null;
    const qrDialog = $('qr-payment-modal'); if (qrDialog && qrDialog.open) qrDialog.close();
    const d = $("terminal-modal");
    if (d && d.open) d.close();
  }

  window.addEventListener('pagehide', () => {
    if (qrAttempt && $('qr-payment-modal')?.open) navigator.sendBeacon('/api/kiosk/payment/qr/' + qrAttempt + '/cancel');
  });
  if (document.body) mount();
  else document.addEventListener("DOMContentLoaded", mount);

  async function resumeAfterCancel() {
    if (!cancelRequested || !qrAttempt) return false;
    try {
      const response = await fetch('/api/kiosk/payment/qr/' + qrAttempt);
      const data = await response.json();
      if (!response.ok || data.state !== 'cancelled') return false;
      cancelRequested = false;
      open();
      return true;
    } catch (_) { return false; }
  }

  window.KioskPayment = {
    resumeAfterCancel: resumeAfterCancel,
    method: () => selectedMethod,
    mount: mount,
    setAmount: setAmount,
    open: open,
    close: close,
    showWaiting: showWaiting,
    closeWaiting: closeWaiting,
  };
})();
