/**
 * Auto-logout from specialist cabinet after 2 minutes without interaction.
 */
(function () {
  const IDLE_MS = 2 * 60 * 1000;
  const WARN_MS = 15 * 1000;
  const TICK_MS = 500;

  let lastActive = Date.now();
  let loggingOut = false;
  let overlay = null;
  let secondsEl = null;

  function bump() {
    lastActive = Date.now();
  }

  function ensureUI() {
    if (overlay) return;
    overlay = document.createElement("div");
    overlay.className = "admin-idle-overlay";
    overlay.hidden = true;
    overlay.innerHTML = `
      <div class="admin-idle-card" role="dialog" aria-live="assertive">
        <h2>Сессия скоро завершится</h2>
        <p class="admin-idle-count"><span id="admin-idle-seconds">15</span></p>
        <p>Нет действий. Выход из кабинета специалиста…</p>
        <button type="button" class="primary" id="admin-idle-stay">Остаться</button>
      </div>
    `;
    document.body.appendChild(overlay);
    secondsEl = overlay.querySelector("#admin-idle-seconds");
    overlay.querySelector("#admin-idle-stay").addEventListener("click", (e) => {
      e.stopPropagation();
      bump();
      hideWarn();
      fetch("/api/admin/me", { credentials: "same-origin" }).catch(() => {});
    });
  }

  function showWarn(sec) {
    ensureUI();
    overlay.hidden = false;
    if (secondsEl) secondsEl.textContent = String(Math.max(1, sec));
  }

  function hideWarn() {
    if (overlay) overlay.hidden = true;
  }

  async function forceLogout() {
    if (loggingOut) return;
    loggingOut = true;
    try {
      await fetch("/api/admin/logout", { method: "POST", credentials: "same-origin" });
    } catch (_) {}
    location.href = "/admin/";
  }

  function tick() {
    if (loggingOut) return;
    const idle = Date.now() - lastActive;
    if (idle >= IDLE_MS) {
      forceLogout();
      return;
    }
    const left = IDLE_MS - idle;
    if (left <= WARN_MS) {
      showWarn(Math.ceil(left / 1000));
    } else {
      hideWarn();
    }
  }

  ["pointerdown", "keydown", "touchstart", "wheel"].forEach((evt) => {
    document.addEventListener(
      evt,
      () => {
        const wasWarning = overlay && !overlay.hidden;
        bump();
        if (wasWarning) hideWarn();
      },
      { passive: true, capture: true }
    );
  });

  setInterval(tick, TICK_MS);
})();
