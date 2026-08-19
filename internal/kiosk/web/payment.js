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

  const METHOD_HTML =
    '<dialog class="modal payment-screen" id="method-modal">' +
    '<div class="payment-card">' +
    '<h2 class="payment-title">Выберите способ оплаты</h2>' +
    '<p class="payment-due-label">К оплате:</p>' +
    '<p class="payment-amount" id="pay-sum">—</p>' +
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
    '<button type="button" class="payment-cancel" id="method-cancel">← Отмена</button>' +
    '<p class="payment-secure">🔒 Безопасная оплата</p>' +
    "</div>" +
    "</dialog>";

  const WAIT_HTML =
    '<dialog class="modal payment-screen" id="terminal-modal">' +
    '<div class="payment-card payment-card--waiting">' +
    '<div class="payment-waiting-icon" aria-hidden="true">' +
    CARD_ICON +
    "</div>" +
    '<h2 class="payment-title">Завершите оплату</h2>' +
    '<p class="payment-waiting-text">Приложите карту или телефон к платёжному терминалу</p>' +
    '<div class="dots-loader" aria-hidden="true"><span></span><span></span><span></span></div>' +
    "</div>" +
    "</dialog>";

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
    const cancel = $("method-cancel");
    const method = $("method-modal");
    if (cancel && method && cancel.dataset.kioskBound !== "1") {
      cancel.dataset.kioskBound = "1";
      cancel.addEventListener("click", close);
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
    if (amountText != null && amountText !== "") setAmount(amountText);
    const d = $("method-modal");
    if (d && typeof d.showModal === "function" && !d.open) d.showModal();
  }

  function close() {
    const d = $("method-modal");
    if (d && d.open) d.close();
  }

  function showWaiting() {
    close();
    const d = $("terminal-modal");
    if (d && typeof d.showModal === "function" && !d.open) d.showModal();
  }

  function closeWaiting() {
    const d = $("terminal-modal");
    if (d && d.open) d.close();
  }

  if (document.body) mount();
  else document.addEventListener("DOMContentLoaded", mount);

  window.KioskPayment = {
    mount: mount,
    setAmount: setAmount,
    open: open,
    close: close,
    showWaiting: showWaiting,
    closeWaiting: closeWaiting,
  };
})();
