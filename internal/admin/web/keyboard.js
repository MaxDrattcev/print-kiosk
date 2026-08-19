/**
 * On-screen keyboard for touch kiosk (no physical keyboard).
 * Attaches to text/password/number/textarea fields.
 */
(function () {
  const LAYOUTS = {
    en: {
      label: "EN",
      rows: [
        ["1", "2", "3", "4", "5", "6", "7", "8", "9", "0", "-", "_"],
        ["q", "w", "e", "r", "t", "y", "u", "i", "o", "p"],
        ["a", "s", "d", "f", "g", "h", "j", "k", "l"],
        ["z", "x", "c", "v", "b", "n", "m", ".", "@"],
      ],
    },
    ru: {
      label: "RU",
      rows: [
        ["1", "2", "3", "4", "5", "6", "7", "8", "9", "0", "-", "_"],
        ["й", "ц", "у", "к", "е", "н", "г", "ш", "щ", "з", "х", "ъ"],
        ["ф", "ы", "в", "а", "п", "р", "о", "л", "д", "ж", "э"],
        ["я", "ч", "с", "м", "и", "т", "ь", "б", "ю", "."],
      ],
    },
  };

  let activeInput = null;
  let layout = "en";
  let shifted = false;
  let root = null;

  function isEditable(el) {
    if (!el) return false;
    if (el.tagName === "TEXTAREA") return !el.disabled && !el.readOnly;
    if (el.tagName !== "INPUT") return false;
    const type = (el.type || "text").toLowerCase();
    return (
      !el.disabled &&
      ["text", "password", "email", "search", "tel", "url", "number"].includes(type)
    );
  }

  function ensureRoot() {
    if (root) return root;
    root = document.createElement("div");
    root.className = "osk";
    root.id = "osk";
    root.hidden = true;
    root.innerHTML = `
      <div class="osk-bar">
        <span class="osk-hint">Экранная клавиатура</span>
        <button type="button" class="osk-hide" data-osk="hide">Скрыть</button>
      </div>
      <div class="osk-keys" id="osk-keys"></div>
    `;
    document.body.appendChild(root);
    root.addEventListener("mousedown", (e) => e.preventDefault());
    root.addEventListener("click", onKeyClick);
    renderKeys();
    return root;
  }

  function renderKeys() {
    const box = document.getElementById("osk-keys");
    if (!box) return;
    const cfg = LAYOUTS[layout];
    const rowsHtml = cfg.rows
      .map((row) => {
        const keys = row
          .map((ch) => {
            const shown = shifted ? ch.toUpperCase() : ch;
            return `<button type="button" class="osk-key" data-insert="${escapeAttr(shown)}">${escapeHtml(shown)}</button>`;
          })
          .join("");
        return `<div class="osk-row">${keys}</div>`;
      })
      .join("");

    box.innerHTML =
      rowsHtml +
      `<div class="osk-row osk-row-actions">
        <button type="button" class="osk-key osk-wide" data-osk="shift">${shifted ? "⇧ ON" : "⇧"}</button>
        <button type="button" class="osk-key osk-wide" data-osk="lang">${cfg.label}</button>
        <button type="button" class="osk-key osk-space" data-insert=" ">Пробел</button>
        <button type="button" class="osk-key osk-wide" data-osk="back">⌫</button>
        <button type="button" class="osk-key osk-wide" data-osk="clear">Очистить</button>
        <button type="button" class="osk-key osk-wide osk-enter" data-osk="enter">Ввод</button>
      </div>`;
  }

  function escapeHtml(s) {
    return String(s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;");
  }

  function escapeAttr(s) {
    return String(s).replace(/"/g, "&quot;");
  }

  function show(input) {
    ensureRoot();
    activeInput = input;
    root.hidden = false;
    document.body.classList.add("osk-open");
    document.querySelectorAll(".osk-target").forEach((el) => el.classList.remove("osk-target"));
    input.classList.add("osk-target");
    input.scrollIntoView({ block: "center", behavior: "smooth" });
  }

  function hide() {
    const input = activeInput;
    if (root) root.hidden = true;
    document.body.classList.remove("osk-open");
    document.querySelectorAll(".osk-target").forEach((el) => el.classList.remove("osk-target"));
    activeInput = null;
    if (input && document.activeElement === input) {
      input.blur();
    }
  }

  function insertText(text) {
    if (!activeInput) return;
    const el = activeInput;
    const start = el.selectionStart ?? el.value.length;
    const end = el.selectionEnd ?? el.value.length;
    const value = el.value || "";
    el.value = value.slice(0, start) + text + value.slice(end);
    const pos = start + text.length;
    try {
      el.setSelectionRange(pos, pos);
    } catch (_) {
      /* number inputs may not support selection */
    }
    el.dispatchEvent(new Event("input", { bubbles: true }));
    if (shifted && text.length === 1 && /[a-zа-яё]/i.test(text)) {
      shifted = false;
      renderKeys();
    }
  }

  function backspace() {
    if (!activeInput) return;
    const el = activeInput;
    const start = el.selectionStart ?? el.value.length;
    const end = el.selectionEnd ?? el.value.length;
    if (start !== end) {
      el.value = el.value.slice(0, start) + el.value.slice(end);
      try {
        el.setSelectionRange(start, start);
      } catch (_) {}
    } else if (start > 0) {
      el.value = el.value.slice(0, start - 1) + el.value.slice(start);
      try {
        el.setSelectionRange(start - 1, start - 1);
      } catch (_) {}
    }
    el.dispatchEvent(new Event("input", { bubbles: true }));
  }

  function onKeyClick(e) {
    const btn = e.target.closest("button");
    if (!btn || !root.contains(btn)) return;

    const action = btn.dataset.osk;
    if (action === "hide") {
      hide();
      return;
    }
    if (action === "shift") {
      shifted = !shifted;
      renderKeys();
      return;
    }
    if (action === "lang") {
      layout = layout === "en" ? "ru" : "en";
      shifted = false;
      renderKeys();
      return;
    }
    if (action === "back") {
      backspace();
      return;
    }
    if (action === "clear") {
      if (activeInput) {
        activeInput.value = "";
        activeInput.dispatchEvent(new Event("input", { bubbles: true }));
      }
      return;
    }
    if (action === "enter") {
      pressEnter();
      return;
    }
    if (btn.dataset.insert != null) {
      insertText(btn.dataset.insert);
    }
  }

  function pressEnter() {
    if (!activeInput) return;
    const el = activeInput;

    if (el.tagName === "TEXTAREA") {
      insertText("\n");
      return;
    }

    const opts = { key: "Enter", code: "Enter", keyCode: 13, which: 13, bubbles: true, cancelable: true };
    const down = new KeyboardEvent("keydown", opts);
    const press = new KeyboardEvent("keypress", opts);
    const up = new KeyboardEvent("keyup", opts);
    el.dispatchEvent(down);
    if (!down.defaultPrevented) {
      el.dispatchEvent(press);
    }
    el.dispatchEvent(up);

    const form = el.form || el.closest("form");
    if (form && !down.defaultPrevented) {
      if (typeof form.requestSubmit === "function") {
        form.requestSubmit();
      } else {
        const submitBtn =
          form.querySelector('button[type="submit"], input[type="submit"]') ||
          form.querySelector("button.primary-btn, button.button-primary, button.primary");
        if (submitBtn) {
          submitBtn.click();
        } else {
          form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
        }
      }
      hide();
      return;
    }

    // Pages without <form>: click the main action near the field.
    const card = el.closest(".prompt-card, .card, .screen, form, body");
    const actionBtn =
      card &&
      (card.querySelector("button.primary-btn:not([disabled])") ||
        card.querySelector('button[type="submit"]:not([disabled])'));
    if (actionBtn) {
      actionBtn.click();
      hide();
    }
  }

  function bind(rootEl) {
    const scope = rootEl || document;
    scope.addEventListener(
      "focusin",
      (e) => {
        if (isEditable(e.target)) {
          e.target.setAttribute("inputmode", "none");
          e.target.setAttribute("autocomplete", e.target.autocomplete || "off");
          show(e.target);
        }
      },
      true
    );

    document.addEventListener("pointerdown", (e) => {
      if (!root || root.hidden) return;
      if (root.contains(e.target)) return;
      if (e.target === activeInput) return;
      if (isEditable(e.target)) return;
      // Keep open when tapping labels of current field
      if (activeInput && activeInput.closest("label")?.contains(e.target)) return;
      hide();
    });
  }

  window.KioskKeyboard = { bind, show, hide };
})();
