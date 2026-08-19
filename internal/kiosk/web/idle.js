/**
 * Kiosk idle watchdog:
 * - after 50s without interaction → show 10s countdown
 * - at 60s → go to home page
 * Paused while dialogs/loading overlays are open.
 */
(function () {
  const IDLE_LIMIT_MS = 60 * 1000;
  const WARN_BEFORE_MS = 10 * 1000;
  const TICK_MS = 250;

  const path = location.pathname.replace(/\/+$/, "") || "/";
  if (path === "/") return;
  // Waiting for IMAP mail can take up to 2 minutes without touch input.
  if (path === "/print/email/wait") return;

  let lastActive = Date.now();
  let warnVisible = false;
  let overlay = null;
  let secondsEl = null;

  function ensureUI() {
    if (overlay) return;
    overlay = document.createElement("div");
    overlay.className = "idle-overlay";
    overlay.hidden = true;
    overlay.innerHTML = `
      <div class="idle-card" role="dialog" aria-live="assertive" aria-modal="true">
        <h2>Сессия скоро завершится</h2>
        <p class="idle-count"><span id="idle-seconds">10</span></p>
        <p class="muted">Нет действий. Возврат на главную через несколько секунд.</p>
        <button type="button" class="primary-btn" id="idle-stay-btn">Остаться</button>
      </div>
    `;
    document.body.appendChild(overlay);
    secondsEl = overlay.querySelector("#idle-seconds");
    overlay.querySelector("#idle-stay-btn").addEventListener("click", (e) => {
      e.stopPropagation();
      bump();
      hideWarn();
    });
    overlay.addEventListener("click", bump);
  }

  function bump() {
    lastActive = Date.now();
  }

  function isPaused() {
    if (document.querySelector("dialog[open]")) return true;
    const loading = document.querySelector(".loading-overlay:not([hidden])");
    if (loading) return true;
    return false;
  }

  function showWarn(secondsLeft) {
    ensureUI();
    warnVisible = true;
    overlay.hidden = false;
    document.body.classList.add("idle-warn");
    if (secondsEl) secondsEl.textContent = String(Math.max(1, secondsLeft));
  }

  function hideWarn() {
    warnVisible = false;
    if (overlay) overlay.hidden = true;
    document.body.classList.remove("idle-warn");
  }

  function tick() {
    if (isPaused()) {
      lastActive = Date.now();
      if (warnVisible) hideWarn();
      return;
    }

    const idle = Date.now() - lastActive;
    if (idle >= IDLE_LIMIT_MS) {
      location.href = "/";
      return;
    }

    const remaining = IDLE_LIMIT_MS - idle;
    if (remaining <= WARN_BEFORE_MS) {
      showWarn(Math.ceil(remaining / 1000));
    } else if (warnVisible) {
      hideWarn();
    }
  }

  const activityEvents = [
    "pointerdown",
    "pointermove",
    "keydown",
    "touchstart",
    "touchmove",
    "wheel",
    "scroll",
  ];
  activityEvents.forEach((evt) => {
    document.addEventListener(
      evt,
      () => {
        bump();
        if (warnVisible && evt !== "pointermove" && evt !== "touchmove" && evt !== "scroll") {
          hideWarn();
        }
      },
      { passive: true, capture: true }
    );
  });

  // Don't treat tiny pointer jitters as cancel during warn — only real clicks/keys hide via above.
  // pointermove still bumps timer so active user watching screen keeps session.

  setInterval(tick, TICK_MS);
})();
