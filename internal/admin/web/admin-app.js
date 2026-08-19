(function () {
  const TITLES = {
    overview: "Обзор",
    prices: "Цены",
    paper: "Бумага",
    print: "Печать",
    scan: "Сканирование",
    email: "Email",
    max: "MAX",
    payment: "Оплата",
    system: "Система",
    journal: "Журнал",
  };
  const SAVE_SECTIONS = new Set(["prices", "paper", "print", "email", "max", "system"]);
  const PAPER_CAPACITY = 500;
  const boolFields = new Set([
    "max_enabled",
    "service_print_enabled",
    "service_copy_enabled",
    "service_scan_enabled",
    "source_usb_enabled",
    "source_email_enabled",
  ]);
  const PASSWORD_MASK = "********";
  const DEFAULTS = {
    price_bw: "5",
    price_color: "15",
    price_copy: "10",
    price_scan: "10",
    paper_remaining: "500",
    paper_alert_threshold: "50",
    session_timeout_sec: "120",
    support_text: "Обратитесь к администратору",
    service_print_enabled: "true",
    service_copy_enabled: "true",
    service_scan_enabled: "true",
    source_usb_enabled: "true",
    source_email_enabled: "true",
    max_enabled: "false",
    email_poll_interval_sec: "30",
    email_max_file_size_mb: "20",
  };

  let dirty = false;
  let pendingLeave = null;
  let allowLeave = false;
  let currentSection = "overview";
  let dangerAction = null;
  let lastOverview = null;

  KioskKeyboard.bind(document);

  function statusHTML(kind, label) {
    return '<span class="admin-status ' + kind + '"><i></i> ' + escapeHtml(label) + "</span>";
  }

  function escapeHtml(s) {
    return String(s || "")
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  function money(n) {
    return Number(n).toLocaleString("ru-RU", { maximumFractionDigits: 2 }) + " ₽";
  }

  function formatUptime(sec) {
    sec = Math.max(0, Number(sec) || 0);
    const h = Math.floor(sec / 3600);
    const m = Math.floor((sec % 3600) / 60);
    if (h > 0) return h + " ч " + m + " мин";
    return m + " мин";
  }

  function userError(data, fallback, status) {
    const raw = (data && data.error) || "";
    if (status >= 500 || /SQLITE|database locked|busy|sql/i.test(raw)) {
      return fallback;
    }
    return raw || fallback;
  }

  function showSection(id) {
    currentSection = id;
    document.querySelectorAll(".admin-panel").forEach((el) => {
      el.hidden = el.id !== "panel-" + id;
    });
    document.querySelectorAll(".admin-nav-item").forEach((btn) => {
      btn.classList.toggle("is-active", btn.dataset.section === id);
    });
    document.getElementById("section-title").textContent = TITLES[id] || id;
    document.getElementById("savebar").hidden = !SAVE_SECTIONS.has(id);
    if (id === "overview" || id === "print" || id === "journal" || id === "paper") {
      loadOverview();
    }
    location.hash = id;
  }

  async function requireAuth() {
    const res = await fetch("/api/admin/me", { credentials: "same-origin" });
    if (!res.ok) {
      location.href = "/admin/";
      return null;
    }
    return res.json();
  }

  function pluralRu(n, one, few, many) {
    const abs = Math.abs(Number(n)) % 100;
    const n1 = abs % 10;
    if (abs > 10 && abs < 20) return many;
    if (n1 > 1 && n1 < 5) return few;
    if (n1 === 1) return one;
    return many;
  }

  function paperThreshold() {
    const el = document.querySelector('input[name="paper_alert_threshold"]');
    const n = parseInt(String(el && el.value), 10);
    return Number.isNaN(n) ? 50 : n;
  }

  function updatePaperStatus(raw) {
    const box = document.getElementById("paper-status");
    const valueEl = document.getElementById("paper-status-value");
    const meterLabel = document.getElementById("paper-meter-label");
    const warnEl = document.getElementById("paper-meter-warn");
    const fill = document.getElementById("paper-bar-fill");
    const bar = document.getElementById("paper-bar");
    if (raw === undefined || raw === null || raw === "") {
      raw = document.querySelector('input[name="paper_remaining"]')?.value;
    }
    const n = parseInt(String(raw), 10);
    const low = !Number.isNaN(n) && paperThreshold() > 0 && n < paperThreshold();
    if (box && valueEl) {
      if (Number.isNaN(n)) {
        valueEl.textContent = "—";
        box.classList.remove("low");
      } else {
        valueEl.textContent = n + " " + pluralRu(n, "лист", "листа", "листов");
        box.classList.toggle("low", low);
      }
    }
    if (meterLabel) {
      meterLabel.textContent = Number.isNaN(n) ? "—" : n + " / " + PAPER_CAPACITY + " листов";
    }
    if (warnEl) warnEl.hidden = !low;
    if (fill && bar) {
      const pct = Number.isNaN(n) ? 0 : Math.max(0, Math.min(100, (n / PAPER_CAPACITY) * 100));
      fill.style.width = pct + "%";
      bar.classList.toggle("low", low);
    }
  }

  function fillForm(settings) {
    const form = document.getElementById("settings-form");
    for (const [key, value] of Object.entries(settings)) {
      const el = form.elements.namedItem(key);
      if (!el) continue;
      if (boolFields.has(key)) {
        el.checked = value === "true";
      } else {
        el.value = value;
      }
    }
    const tokenState = document.getElementById("max-token-state");
    if (tokenState) {
      tokenState.textContent =
        settings.max_bot_token === PASSWORD_MASK ? "Токен бота: задан" : "Токен бота: не задан";
    }
    updatePaperStatus(settings.paper_remaining);
    updateMailHosts(settings.email_address);
    markClean();
  }

  function updateMailHosts(address) {
    const imap = document.getElementById("email-imap");
    const smtp = document.getElementById("email-smtp");
    if (lastOverview && lastOverview.email) {
      const e = lastOverview.email;
      if (imap) imap.value = e.imap_host ? e.imap_host + ":" + e.imap_port : "—";
      if (smtp) smtp.value = e.smtp_host ? e.smtp_host + ":" + e.smtp_port : "—";
    }
    if (!address) {
      if (imap) imap.value = "—";
      if (smtp) smtp.value = "—";
    }
  }

  function collectForm(form) {
    const data = {};
    for (const el of form.elements) {
      if (!el.name) continue;
      if (boolFields.has(el.name)) {
        data[el.name] = el.checked ? "true" : "false";
      } else {
        data[el.name] = el.value;
      }
    }
    return data;
  }

  function markClean() {
    dirty = false;
    updateUnsavedHint();
  }

  function markDirty() {
    dirty = true;
    updateUnsavedHint();
  }

  function updateUnsavedHint() {
    const hint = document.getElementById("unsaved-hint");
    if (hint) hint.hidden = !dirty;
  }

  async function saveSettings() {
    const errorEl = document.getElementById("error");
    const successEl = document.getElementById("success");
    errorEl.hidden = true;
    successEl.hidden = true;
    const form = document.getElementById("settings-form");
    const payload = collectForm(form);
    const res = await fetch("/api/admin/settings", {
      method: "PUT",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) {
      errorEl.textContent = userError(data, "Не удалось сохранить настройки", res.status);
      errorEl.hidden = false;
      return false;
    }
    if (payload.email_password && payload.email_password !== PASSWORD_MASK) {
      form.elements.namedItem("email_password").value = PASSWORD_MASK;
    }
    if (payload.max_bot_token && payload.max_bot_token !== PASSWORD_MASK) {
      form.elements.namedItem("max_bot_token").value = PASSWORD_MASK;
      document.getElementById("max-token-state").textContent = "Токен бота: задан";
    }
    updatePaperStatus(payload.paper_remaining);
    markClean();
    successEl.textContent = "✓ Настройки сохранены";
    successEl.hidden = false;
    return true;
  }

  async function loadOverview() {
    const statusEl = document.getElementById("overview-status");
    const metricsEl = document.getElementById("overview-metrics");
    const res = await fetch("/api/admin/overview", { credentials: "same-origin" });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) {
      if (statusEl) {
        statusEl.innerHTML = '<div class="admin-card"><p class="error">Не удалось загрузить обзор</p></div>';
      }
      return;
    }
    lastOverview = data;

    const cards = [];
    if (data.paper && data.paper.known) {
      const cap = data.paper.capacity || PAPER_CAPACITY;
      const n = data.paper.remaining;
      const pct = Math.max(0, Math.min(100, (Number(n) / cap) * 100));
      const warn = data.paper.low
        ? '<div class="admin-status warn" style="margin-top:8px"><i></i> Ниже порога уведомления</div>'
        : "";
      cards.push(
        '<div class="admin-card"><h3>Бумага</h3><div class="admin-metric">' +
          n +
          " / " +
          cap +
          ' листов</div><div class="paper-bar' +
          (data.paper.low ? " low" : "") +
          '"><span style="width:' +
          pct +
          '%"></span></div>' +
          warn +
          "</div>"
      );
    }
    if (data.printer) {
      cards.push(wrapCard("Принтер", statusHTML(data.printer.status || "warn", data.printer.label || "—")));
    }
    if (data.copy) {
      cards.push(wrapCard("Копирование", statusHTML(data.copy.status, data.copy.label)));
    }
    if (data.scanner) {
      cards.push(wrapCard("Сканирование", statusHTML(data.scanner.status, data.scanner.label)));
    }
    if (data.payment) {
      cards.push(wrapCard("Оплата", statusHTML(data.payment.status, data.payment.label)));
    }
    if (data.email) {
      cards.push(wrapCard("Email", statusHTML(data.email.status, data.email.label)));
    }
    if (data.max) {
      cards.push(wrapCard("MAX", statusHTML(data.max.status, data.max.label)));
    }
    if (data.usb && data.usb.known) {
      cards.push(
        wrapCard(
          "USB",
          data.usb.available ? statusHTML("ok", "Доступен") : statusHTML("off", "Не обнаружена")
        )
      );
    }
    if (statusEl) statusEl.innerHTML = cards.join("");

    const metrics = [];
    if (data.kiosk_name) metrics.push(metricCard("Терминал", data.kiosk_name));
    if (data.kiosk_location) metrics.push(metricCard("Точка", data.kiosk_location));
    if (data.uptime_sec != null) metrics.push(metricCard("Время работы", formatUptime(data.uptime_sec)));
    if (data.today) {
      metrics.push(metricCard("Выручка за сегодня", money(data.today.revenue || 0)));
      metrics.push(metricCard("Листов сегодня", String(data.today.sheets_used || 0)));
      metrics.push(
        metricCard("Копий / сканов сегодня", String(Number(data.today.scans || 0) + Number(data.today.copies || 0)))
      );
    }
    if (metricsEl) metricsEl.innerHTML = metrics.join("");

    applyInfra(data);
    const journalEl = document.getElementById("journal-metrics");
    if (journalEl) journalEl.innerHTML = metrics.join("");
  }

  function applyInfra(data) {
    const setVal = (id, v) => {
      const el = document.getElementById(id);
      if (el) el.value = v;
    };
    if (data.printer) {
      setVal("printer-name", data.printer.name || "—");
      setVal("printer-mode", data.printer.label || "—");
    }
    setVal("sumatra-status", data.sumatra_found ? "Найден" : "Не найден");
    setVal("libreoffice-status", data.libreoffice_found ? "Найден" : "Не найден");
    function setDeviceStatus(id, info) {
      const el = document.getElementById(id);
      if (!el || !info) return;
      el.className = "admin-status " + (info.status || "off");
      el.innerHTML = "<i></i> " + (info.label || "—");
    }
    setDeviceStatus("scan-device-status", data.scanner);
    setDeviceStatus("copy-device-status", data.copy);
    const listen = document.getElementById("listen-addr");
    if (listen) listen.textContent = "Адрес API: " + (data.listen_addr || "—");
    const logPath = document.getElementById("log-path");
    if (logPath) logPath.textContent = "Файл: " + (data.log_path || "logs/kiosk.log");
    if (data.email) {
      const imap = document.getElementById("email-imap");
      const smtp = document.getElementById("email-smtp");
      if (imap) imap.value = data.email.imap_host ? data.email.imap_host + ":" + data.email.imap_port : "—";
      if (smtp) smtp.value = data.email.smtp_host ? data.email.smtp_host + ":" + data.email.smtp_port : "—";
    }
  }

  function wrapCard(title, inner) {
    return '<div class="admin-card"><h3>' + title + "</h3>" + inner + "</div>";
  }

  function metricCard(title, value) {
    return '<div class="admin-card"><h3>' + title + '</h3><div class="admin-metric">' + value + "</div></div>";
  }

  async function leaveNow() {
    const action = pendingLeave;
    pendingLeave = null;
    allowLeave = true;
    if (!action) return;
    if (action.type === "logout") {
      await fetch("/api/admin/logout", { method: "POST", credentials: "same-origin" });
      location.href = "/admin/";
      return;
    }
    if (action.type === "section") {
      allowLeave = false;
      showSection(action.id);
      return;
    }
    location.href = action.href || "/";
  }

  function askLeave(action) {
    if (action.type === "section" && action.id === currentSection) return;
    if (!dirty) {
      pendingLeave = action;
      leaveNow();
      return;
    }
    pendingLeave = action;
    document.getElementById("unsaved-modal").showModal();
  }

  async function resetSettings() {
    const form = document.getElementById("settings-form");
    for (const [key, value] of Object.entries(DEFAULTS)) {
      const el = form.elements.namedItem(key);
      if (!el) continue;
      if (boolFields.has(key)) el.checked = value === "true";
      else el.value = value;
    }
    markDirty();
    return saveSettings();
  }

  async function postTest(url, body, resultEl) {
    resultEl.textContent = "Проверка…";
    resultEl.className = "admin-muted";
    const res = await fetch(url, {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) {
      resultEl.textContent = userError(data, "Проверка не удалась", res.status);
      resultEl.className = "error";
      return;
    }
    resultEl.textContent = "✓ " + (data.message || "Подключение успешно");
    resultEl.className = "success";
  }

  (async () => {
    const me = await requireAuth();
    if (!me) return;
    document.getElementById("username").textContent = me.username || "Администратор";

    const res = await fetch("/api/admin/settings", { credentials: "same-origin" });
    const data = await res.json();
    if (!res.ok) {
      document.getElementById("error").textContent = userError(data, "Не удалось загрузить настройки", res.status);
      document.getElementById("error").hidden = false;
      return;
    }
    fillForm(data.settings);

    const form = document.getElementById("settings-form");
    form.addEventListener("input", markDirty);
    form.addEventListener("change", markDirty);
    const paperInput = document.querySelector('input[name="paper_remaining"]');
    if (paperInput) {
      paperInput.addEventListener("input", () => updatePaperStatus(paperInput.value));
    }
    const threshInput = document.querySelector('input[name="paper_alert_threshold"]');
    if (threshInput) {
      threshInput.addEventListener("input", () => updatePaperStatus());
    }

    const hash = (location.hash || "#overview").replace("#", "");
    showSection(TITLES[hash] ? hash : "overview");
  })();

  document.querySelectorAll(".admin-nav-item").forEach((btn) => {
    btn.addEventListener("click", () => askLeave({ type: "section", id: btn.dataset.section }));
  });

  document.getElementById("home-link").addEventListener("click", (e) => {
    e.preventDefault();
    askLeave({ type: "href", href: "/" });
  });
  document.getElementById("logout-btn").addEventListener("click", (e) => {
    e.preventDefault();
    askLeave({ type: "logout" });
  });

  document.getElementById("unsaved-save-btn").addEventListener("click", async () => {
    const ok = await saveSettings();
    if (!ok) {
      document.getElementById("unsaved-modal").close();
      pendingLeave = null;
      return;
    }
    document.getElementById("unsaved-modal").close();
    await leaveNow();
  });
  document.getElementById("unsaved-discard-btn").addEventListener("click", async () => {
    const res = await fetch("/api/admin/settings", { credentials: "same-origin" });
    const data = await res.json().catch(() => ({}));
    if (res.ok && data.settings) fillForm(data.settings);
    else markClean();
    document.getElementById("unsaved-modal").close();
    await leaveNow();
  });
  document.getElementById("unsaved-cancel-btn").addEventListener("click", () => {
    pendingLeave = null;
    document.getElementById("unsaved-modal").close();
  });

  window.addEventListener("beforeunload", (e) => {
    if (allowLeave || !dirty) return;
    e.preventDefault();
    e.returnValue = "";
  });

  document.getElementById("paper-refilled-btn").addEventListener("click", () => {
    document.getElementById("paper-confirm-modal").showModal();
  });
  document.getElementById("paper-confirm-no").addEventListener("click", () => {
    document.getElementById("paper-confirm-modal").close();
  });
  document.getElementById("paper-confirm-yes").addEventListener("click", async () => {
    document.getElementById("paper-confirm-modal").close();
    const errorEl = document.getElementById("error");
    const successEl = document.getElementById("success");
    errorEl.hidden = true;
    successEl.hidden = true;
    const res = await fetch("/api/admin/settings", {
      method: "PUT",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ paper_remaining: String(PAPER_CAPACITY) }),
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) {
      errorEl.textContent = userError(data, "Не удалось сохранить настройки", res.status);
      errorEl.hidden = false;
      return;
    }
    const input = document.querySelector('input[name="paper_remaining"]');
    if (input) input.value = String(PAPER_CAPACITY);
    updatePaperStatus(PAPER_CAPACITY);
    successEl.textContent = "Бумага загружена: остаток " + PAPER_CAPACITY + " листов";
    successEl.hidden = false;
  });

  document.getElementById("settings-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    await saveSettings();
  });

  document.querySelectorAll("[data-reveal]").forEach((btn) => {
    btn.addEventListener("click", () => {
      const el = document.querySelector('input[name="' + btn.dataset.reveal + '"]');
      if (!el) return;
      el.value = "";
      el.focus();
      markDirty();
    });
  });

  document.getElementById("email-test-btn").addEventListener("click", async () => {
    const form = document.getElementById("settings-form");
    await postTest(
      "/api/admin/email/test",
      {
        email_address: form.elements.namedItem("email_address").value,
        email_login: form.elements.namedItem("email_login").value,
        email_password: form.elements.namedItem("email_password").value,
      },
      document.getElementById("email-test-result")
    );
  });

  document.getElementById("max-test-btn").addEventListener("click", async () => {
    const form = document.getElementById("settings-form");
    await postTest(
      "/api/admin/max/test",
      {
        max_bot_token: form.elements.namedItem("max_bot_token").value,
        max_admin_id: form.elements.namedItem("max_admin_id").value,
        send_message: false,
      },
      document.getElementById("max-test-result")
    );
  });

  document.getElementById("max-send-btn").addEventListener("click", async () => {
    const form = document.getElementById("settings-form");
    await postTest(
      "/api/admin/max/test",
      {
        max_bot_token: form.elements.namedItem("max_bot_token").value,
        max_admin_id: form.elements.namedItem("max_admin_id").value,
        send_message: true,
      },
      document.getElementById("max-test-result")
    );
  });

  const dangerModal = document.getElementById("danger-modal");
  document.querySelectorAll("[data-danger]").forEach((btn) => {
    btn.addEventListener("click", () => {
      dangerAction = btn.dataset.danger;
      document.getElementById("danger-title").textContent = "Сбросить настройки?";
      document.getElementById("danger-text").textContent =
        "Цены и флаги услуг вернутся к значениям по умолчанию. Токены не трогаем.";
      dangerModal.showModal();
    });
  });
  document.getElementById("danger-no").addEventListener("click", () => {
    dangerAction = null;
    dangerModal.close();
  });
  document.getElementById("danger-yes").addEventListener("click", async () => {
    const action = dangerAction;
    dangerAction = null;
    dangerModal.close();
    const errorEl = document.getElementById("error");
    const successEl = document.getElementById("success");
    successEl.hidden = true;
    if (action === "reset") {
      const ok = await resetSettings();
      if (ok) {
        errorEl.hidden = true;
        successEl.textContent = "✓ Настройки сброшены";
        successEl.hidden = false;
      }
    }
  });
})();
