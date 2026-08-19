/**
 * Kiosk idle watchdog:
 * after session_timeout_sec without interaction → home,
 * with a 10s countdown near the end.
 * Paused while dialogs/loading overlays are open.
 * Wait screens (email/MAX) skip the timer so the visitor can receive a file.
 */
(function () {
  const WARN_BEFORE_MS = 10 * 1000;
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
    const warnMs = Math.min(WARN_BEFORE_MS, Math.max(3000, idleLimitMs / 6));
    if (remaining <= warnMs) {
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

  setInterval(tick, TICK_MS);
})();
