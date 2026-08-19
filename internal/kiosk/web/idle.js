/**
 * Kiosk idle watchdog:
 * after session_timeout_sec without interaction → home,
 * with a 15s countdown warning on top of any screen (including dialogs).
 * Paused only while a loading overlay is visible.
 * Wait screens (email/MAX) skip the timer so the visitor can receive a file.
 */
(function () {
  const WARN_BEFORE_MS = 15 * 1000;
  const TICK_MS = 250;
  const DEFAULT_LIMIT_MS = 120 * 1000;

  const path = location.pathname.replace(/\/+$/, "") || "/";
  if (path === "/") return;
  if (path === "/print/email/wait") return;
  if (path === "/print/max/wait") return;
  if (path === "/scan/max") return;

  let idleLimitMs = DEFAULT_LIMIT_MS;
  let lastActive = Date.now();
  let warnVisible = false;
  let overlay = null;
  let secondsEl = null;
  let leaving = false;

  fetch("/api/kiosk/info")
    .then((r) => r.json())
    .then((info) => {
      const sec = Number(info && info.session_timeout_sec);
      if (sec === 0) {
        idleLimitMs = 0;
        return;
      }
      if (Number.isFinite(sec) && sec > 0) {
        idleLimitMs = Math.max(15, sec) * 1000;
      }
    })
    .catch(() => {});

  function warnWindowMs() {
    // Always warn for 15s when the session is long enough.
    // Never shorter than 10s unless the whole timeout is under 10s.
    const minWarn = Math.min(10000, idleLimitMs);
    return Math.min(WARN_BEFORE_MS, Math.max(minWarn, idleLimitMs - 5000));
  }

  function ensureUI() {
    if (overlay) return;
    overlay = document.createElement("dialog");
    overlay.className = "idle-overlay";
    overlay.setAttribute("aria-modal", "true");
    overlay.setAttribute("aria-live", "assertive");
    overlay.innerHTML =
      '<div class="idle-card" role="document">' +
      "<h2>Сессия скоро завершится</h2>" +
      '<p class="idle-count"><span id="idle-seconds">15</span></p>' +
      '<p class="muted">Нет действий. Возврат на главную через несколько секунд.</p>' +
      '<button type="button" class="primary-btn" id="idle-stay-btn">Остаться</button>' +
      "</div>";
    document.body.appendChild(overlay);
    secondsEl = overlay.querySelector("#idle-seconds");
    overlay.querySelector("#idle-stay-btn").addEventListener("click", (e) => {
      e.stopPropagation();
      stay();
    });
    overlay.addEventListener("click", (e) => {
      if (e.target === overlay) stay();
    });
    overlay.addEventListener("cancel", (e) => {
      e.preventDefault();
      stay();
    });
  }

  function bump() {
    lastActive = Date.now();
  }

  function stay() {
    bump();
    hideWarn();
  }

  function isPaused() {
    if (document.querySelector(".loading-overlay:not([hidden])")) return true;
    return false;
  }

  function showWarn(secondsLeft) {
    ensureUI();
    warnVisible = true;
    document.body.classList.add("idle-warn");
    if (secondsEl) secondsEl.textContent = String(Math.max(1, secondsLeft));
    overlay.hidden = false;
    if (typeof overlay.showModal === "function") {
      if (!overlay.open) overlay.showModal();
    }
  }

  function hideWarn() {
    warnVisible = false;
    document.body.classList.remove("idle-warn");
    if (!overlay) return;
    if (overlay.open) overlay.close();
    overlay.hidden = true;
  }

  function sessionPayload() {
    const params = new URLSearchParams(location.search);
    const job = params.get("job") || "";
    const session = params.get("session") || "";
    const body = {};
    if (path.indexOf("/scan") === 0 && job) body.scan_job_id = job;
    else if (path.indexOf("/copy") === 0 && job) body.copy_job_id = job;
    else if (job) body.print_job_id = job;
    if (path.indexOf("/print/max") === 0 && session) body.max_session_id = session;
    else if (path.indexOf("/scan/max") === 0 && session) body.max_scan_session_id = session;
    else if (session) body.email_session_id = session;
    return body;
  }

  function goHome() {
    if (leaving) return;
    leaving = true;
    hideWarn();
    const body = sessionPayload();
    const hasIds = Object.keys(body).length > 0;
    const finish = () => {
      location.href = "/";
    };
    if (!hasIds) {
      finish();
      return;
    }
    fetch("/api/kiosk/session/end", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
      keepalive: true,
    }).finally(finish);
    setTimeout(finish, 800);
  }

  function tick() {
    if (idleLimitMs === 0) return;
    if (isPaused()) {
      lastActive = Date.now();
      if (warnVisible) hideWarn();
      return;
    }

    const idle = Date.now() - lastActive;
    if (idle >= idleLimitMs) {
      goHome();
      return;
    }

    const remaining = idleLimitMs - idle;
    const warnMs = warnWindowMs();
    if (remaining <= warnMs) {
      showWarn(Math.ceil(remaining / 1000));
    } else if (warnVisible) {
      hideWarn();
    }
  }

  ["pointerdown", "keydown", "touchstart"].forEach((evt) => {
    document.addEventListener(
      evt,
      (e) => {
        if (overlay && overlay.contains(e.target)) return;
        bump();
        if (warnVisible) hideWarn();
      },
      { passive: true, capture: true }
    );
  });

  setInterval(tick, TICK_MS);
})();
