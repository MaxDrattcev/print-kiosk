/**
 * Shared kiosk UI helpers. Does not change API contracts.
 */
(function () {
  // Keep vertical touch scrolling for long lists, but prevent kiosk users from
  // navigating away with horizontal swipe gestures or multi-touch zoom.
  let touchStartX = 0;
  let touchStartY = 0;

  document.addEventListener("touchstart", (event) => {
    if (event.touches.length > 1) {
      event.preventDefault();
      return;
    }
    const touch = event.touches[0];
    if (!touch) return;
    touchStartX = touch.clientX;
    touchStartY = touch.clientY;
  }, { passive: false });

  document.addEventListener("touchmove", (event) => {
    if (event.touches.length > 1) {
      event.preventDefault();
      return;
    }
    const touch = event.touches[0];
    if (!touch) return;
    const deltaX = Math.abs(touch.clientX - touchStartX);
    const deltaY = Math.abs(touch.clientY - touchStartY);
    if (deltaX > 12 && deltaX > deltaY) event.preventDefault();
  }, { passive: false });

  for (const eventName of ["gesturestart", "gesturechange", "gestureend"]) {
    document.addEventListener(eventName, (event) => event.preventDefault(), { passive: false });
  }

  const FRIENDLY =
    "Что-то пошло не так. Попробуйте ещё раз. Если ошибка повторится, обратитесь к специалисту.";
  const TECHNICAL =
    /connection refused|econnrefused|econnreset|etimedout|enotfound|status code 5\d\d|\b500\b|\b502\b|\b503\b|scanner error|i\/o timeout|eof|sql:|http:|panic|nil pointer|websocket|tls:|json:|libreoffice|failed to|traceback|exception|stack|powershell|start-process|categoryinfo|exit status|could not be opened|empty/i;

  function isTechnical(msg) {
    const raw = String(msg || "").trim();
    if (!raw) return true;
    if (TECHNICAL.test(raw)) return true;
    if (/^[A-Z_]+$/.test(raw) && raw.length > 8) return true;
    if (/https?:\/\//.test(raw) && /error|failed/i.test(raw)) return true;
    return false;
  }

  function friendlyError(msg) {
    const raw = String(msg || "").trim();
    if (raw) console.error("[kiosk]", raw);
    if (!raw || isTechnical(raw)) return FRIENDLY;
    return raw;
  }

  function setError(el, msg) {
    if (!el) return;
    const text = friendlyError(msg);
    el.textContent = text;
    el.hidden = !msg;
  }

  function bindSuccessCountdown(dialog, options) {
    if (!dialog || dialog.dataset.kioskBound === "1") return;
    dialog.dataset.kioskBound = "1";
    const opts = options || {};
    const seconds = opts.seconds || 20;
    const homeBtn = dialog.querySelector(opts.homeSelector || "#success-home");
    const moreBtn = dialog.querySelector(opts.moreSelector || "#success-more");
    let timer = null;
    let left = seconds;

    function countdownEl() {
      let el = dialog.querySelector("[data-success-countdown]");
      if (!el) {
        el = document.createElement("p");
        el.className = "success-countdown";
        el.setAttribute("data-success-countdown", "1");
        const card = dialog.querySelector(".modal-card, .success-state") || dialog;
        card.appendChild(el);
      }
      return el;
    }

    function stop() {
      if (timer) {
        clearInterval(timer);
        timer = null;
      }
    }

    function moreVisible() {
      return moreBtn && !moreBtn.hidden;
    }

    function homeVisible() {
      return homeBtn && !homeBtn.hidden;
    }

    function start() {
      stop();
      const el = countdownEl();
      if (moreVisible() || !homeVisible()) {
        el.hidden = true;
        return;
      }
      left = seconds;
      el.hidden = false;
      el.textContent = "Возврат в главное меню через " + left + " секунд";
      timer = setInterval(() => {
        left -= 1;
        if (left <= 0) {
          stop();
          location.href = "/";
          return;
        }
        el.textContent = "Возврат в главное меню через " + left + " секунд";
      }, 1000);
    }

    const observer = new MutationObserver(() => {
      if (dialog.open) start();
      else {
        stop();
        const el = dialog.querySelector("[data-success-countdown]");
        if (el) el.hidden = true;
      }
    });
    observer.observe(dialog, { attributes: true, attributeFilter: ["open"] });
    [homeBtn, moreBtn].forEach((btn) => {
      if (!btn) return;
      observer.observe(btn, { attributes: true, attributeFilter: ["hidden", "class"] });
    });
    if (dialog.open) start();
  }

  function bindErrorDialog(dialog) {
    if (!dialog) return;
    const retryBtn = dialog.querySelector("#error-retry");
    if (retryBtn) {
      retryBtn.addEventListener("click", () => dialog.close());
    }
  }

  function showErrorDialog(dialog, rawMsg) {
    if (!dialog) return;
    const detail = dialog.querySelector("#error-detail");
    if (detail) {
      detail.textContent = friendlyError(rawMsg);
    }
    if (typeof dialog.showModal === "function" && !dialog.open) {
      dialog.showModal();
    }
  }

  function init() {
    document.querySelectorAll("#success-modal").forEach((dialog) => {
      bindSuccessCountdown(dialog);
    });
    document.querySelectorAll("#error-modal").forEach((dialog) => {
      bindErrorDialog(dialog);
    });
    document.querySelectorAll("#success-home").forEach((btn) => {
      if (btn.textContent && /на главную/i.test(btn.textContent.trim())) {
        btn.textContent = "Вернуться в главное меню";
      }
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }

  window.KioskUI = {
    friendlyError: friendlyError,
    setError: setError,
    bindSuccessCountdown: bindSuccessCountdown,
    showErrorDialog: showErrorDialog,
  };
})();
