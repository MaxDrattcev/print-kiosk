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

  const FLOW_STAGES = {
    print: ["Документ", "Настройки", "Оплата", "Печать"],
    copy: ["Оригинал", "Настройки", "Оплата", "Копирование"],
    scan: ["Оригинал", "Оплата", "Получение", "Готово"],
  };

  let stageState = null;
  const standaloneTimers = new WeakMap();

  function routeStage() {
    const path = location.pathname.replace(/\/+$/, "") || "/";
    if (path === "/" || path.startsWith("/admin")) return null;
    if (path.startsWith("/print")) {
      return { flow: "print", current: path === "/print/setup" ? 2 : 1 };
    }
    if (path.startsWith("/copy")) {
      return { flow: "copy", current: path === "/copy/setup" ? 2 : 1 };
    }
    if (path.startsWith("/scan")) {
      const delivery = /\/scan\/(name|delivery|email|max|usb)$/.test(path);
      return { flow: "scan", current: delivery ? 3 : 1 };
    }
    return null;
  }

  function renderStages(current) {
    if (!stageState || !stageState.element) return;
    const safeCurrent = Math.max(1, Math.min(4, Number(current) || 1));
    stageState.current = safeCurrent;
    stageState.element.dataset.current = String(safeCurrent);
    stageState.element.querySelectorAll("li").forEach((item, index) => {
      const step = index + 1;
      item.classList.toggle("is-complete", step < safeCurrent);
      item.classList.toggle("is-current", step === safeCurrent);
      item.setAttribute("aria-current", step === safeCurrent ? "step" : "false");
    });
  }

  function initStages() {
    const initial = routeStage();
    if (!initial) return;
    const anchor = document.querySelector(".screen > .top-nav") || document.querySelector(".screen > .app-header");
    if (!anchor || document.querySelector(".kiosk-progress")) return;
    const labels = FLOW_STAGES[initial.flow];
    const progress = document.createElement("nav");
    progress.className = "kiosk-progress kiosk-progress--" + initial.flow;
    progress.setAttribute("aria-label", "Этапы операции");
    const list = document.createElement("ol");
    labels.forEach((label, index) => {
      const item = document.createElement("li");
      const marker = document.createElement("span");
      marker.className = "kiosk-progress-marker";
      marker.textContent = String(index + 1);
      const text = document.createElement("strong");
      text.textContent = label;
      item.append(marker, text);
      list.appendChild(item);
    });
    progress.appendChild(list);
    anchor.insertAdjacentElement("afterend", progress);
    stageState = { flow: initial.flow, current: initial.current, routeCurrent: initial.current, element: progress };
    renderStages(initial.current);
  }

  function setStage(current) {
    renderStages(current);
  }

  function resetStage() {
    if (stageState) renderStages(stageState.routeCurrent);
  }

  function paymentStage() {
    if (!stageState) return;
    renderStages(stageState.flow === "scan" ? 2 : 3);
  }

  function normalizeStateComponents() {
    document.querySelectorAll(".loading-overlay").forEach((overlay) => {
      overlay.classList.add("kiosk-state-layer", "kiosk-state-layer--loading");
      overlay.setAttribute("role", "status");
      overlay.setAttribute("aria-live", "polite");
      overlay.setAttribute("aria-busy", "true");
      const card = overlay.firstElementChild;
      if (card) card.classList.add("kiosk-state-card");
    });
    document.querySelectorAll("#success-modal").forEach((dialog) => {
      dialog.classList.add("kiosk-state-dialog", "kiosk-state-dialog--success");
    });
    document.querySelectorAll("#error-modal").forEach((dialog) => {
      dialog.classList.add("kiosk-state-dialog", "kiosk-state-dialog--error");
    });
  }

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
    if (dialog.querySelector(".print-magic-countdown")) return;
    dialog.dataset.kioskBound = "1";
    const opts = options || {};
    const seconds = opts.seconds || 15;
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

    function homeVisible() {
      return homeBtn && !homeBtn.hidden;
    }

    function start() {
      stop();
      const el = countdownEl();
      if (!homeVisible()) {
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

  function startAutoReturn(container, seconds) {
    if (!container) return;
    const previous = standaloneTimers.get(container);
    if (previous) clearInterval(previous);
    let left = Math.max(1, Number(seconds) || 15);
    let el = container.querySelector("[data-success-countdown]");
    if (!el) {
      el = document.createElement("p");
      el.className = "success-countdown";
      el.setAttribute("data-success-countdown", "1");
      container.appendChild(el);
    }
    const render = () => { el.textContent = "Возврат в главное меню через " + left + " секунд"; };
    render();
    const timer = setInterval(() => {
      left -= 1;
      if (left <= 0) {
        clearInterval(timer);
        standaloneTimers.delete(container);
        location.href = "/";
        return;
      }
      render();
    }, 1000);
    standaloneTimers.set(container, timer);
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
    initStages();
    normalizeStateComponents();
    document.querySelectorAll("#success-modal").forEach((dialog) => {
      bindSuccessCountdown(dialog);
      const observer = new MutationObserver(() => {
        if (dialog.open) setStage(4);
      });
      observer.observe(dialog, { attributes: true, attributeFilter: ["open"] });
    });
    document.querySelectorAll("#error-modal").forEach((dialog) => {
      bindErrorDialog(dialog);
    });
    document.querySelectorAll("#success-home").forEach((btn) => {
      if (btn.textContent && /на главную/i.test(btn.textContent.trim())) {
        btn.textContent = "Вернуться в главное меню";
      }
    });
    document.addEventListener("pointerdown", (event) => {
      if (!event.target.closest("a,button")) return;
      document.body.classList.add("kiosk-interacting");
      window.setTimeout(() => document.body.classList.remove("kiosk-interacting"), 420);
    }, { passive: true });
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
    startAutoReturn: startAutoReturn,
  };
  window.KioskStages = {
    set: setStage,
    reset: resetStage,
    payment: paymentStage,
    flow: function () { return stageState ? stageState.flow : ""; },
  };
})();
