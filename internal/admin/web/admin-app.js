(function () {
  const TITLES = {
    overview: "Обзор",
    history: "Выгрузить историю",
    prices: "Цены",
    paper: "Бумага",
    print: "Оборудование",
    email: "Email",
    max: "MAX",
    payment: "Оплата",
    system: "Система",
  };
  const SAVE_SECTIONS = new Set(["prices", "paper", "print", "email", "max", "payment", "system"]);
  const PAPER_CAPACITY = 500;
  const boolFields = new Set([
    "max_enabled",
    "service_print_enabled",
    "service_copy_enabled",
    "service_scan_enabled",
    "source_usb_enabled",
    "source_email_enabled",
    "test_device_mode",
    "test_payment_mode",
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
    test_device_mode: "true",
    test_payment_mode: "true",
  };

  let dirty = false;
  let pendingLeave = null;
  let allowLeave = false;
  let currentSection = "overview";
  let dangerAction = null;
  let lastOverview = null;
  let statsScope = "today";
  let historyReportID = "";
  let historyDefaultName = "";
  let historyMaxSession = "";
  let historyMaxTimer = null;
  let historyEmailSending = false;

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
    const minutes = Math.floor(sec / 60);
    const hours = Math.floor(sec / 3600);
    const days = Math.floor(sec / 86400);
    const months = Math.floor(days / 30);
    const years = Math.floor(days / 365);
    if (years > 0) {
      const restMonths = Math.floor((days % 365) / 30);
      return years + " " + pluralRu(years, "год", "года", "лет") +
        (restMonths ? " " + restMonths + " " + pluralRu(restMonths, "месяц", "месяца", "месяцев") : "");
    }
    if (months > 0) {
      const restDays = days % 30;
      return months + " " + pluralRu(months, "месяц", "месяца", "месяцев") +
        (restDays ? " " + restDays + " " + pluralRu(restDays, "день", "дня", "дней") : "");
    }
    if (days > 0) return days + " " + pluralRu(days, "день", "дня", "дней");
    if (hours > 0) return hours + " " + pluralRu(hours, "час", "часа", "часов");
    const shownMinutes = sec > 0 ? Math.max(1, minutes) : 0;
    return shownMinutes + " " + pluralRu(shownMinutes, "минута", "минуты", "минут");
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
    if (["overview", "print", "paper", "email", "max", "payment"].includes(id)) {
      loadOverview();
    }
    if (id === "max") loadMaxBotIdentity();
    if (id === "history" && !document.getElementById("history-table-wrap").hidden) loadHistory();
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

    const metrics = [
      metricCard("Название терминала", data.kiosk_name || "Не задано"),
      metricCard("ID терминала", data.kiosk_id || "Не задан"),
      metricCard("Местоположение", data.kiosk_location || "Не задано"),
    ];
    if (metricsEl) metricsEl.innerHTML = metrics.join("");

    applyInfra(data);
    await loadStatistics(statsScope);
  }

  async function loadStatistics(scope) {
    statsScope = scope === "total" ? "total" : "today";
    document.querySelectorAll("[data-stats-scope]").forEach((btn) => {
      const active = btn.dataset.statsScope === statsScope;
      btn.classList.toggle("is-active", active);
      btn.setAttribute("aria-selected", active ? "true" : "false");
    });
    const grid = document.getElementById("statistics-grid");
    if (grid) grid.innerHTML = '<div class="admin-stat-loading">Загружаем статистику…</div>';
    const res = await fetch("/api/admin/stats?scope=" + encodeURIComponent(statsScope), { credentials: "same-origin" });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) {
      if (grid) grid.innerHTML = '<div class="error">' + escapeHtml(userError(data, "Не удалось загрузить статистику", res.status)) + "</div>";
      return;
    }
    const total = statsScope === "total";
    document.getElementById("stats-kicker").textContent = total ? "За всё время" : "Текущие сутки";
    document.getElementById("stats-title").textContent = total ? "Общая статистика" : "Статистика за сегодня";
    document.getElementById("stats-period-note").textContent = total
      ? "Накапливается в базе данных между перезапусками"
      : "Новый счётчик начнётся автоматически в 00:00";
    document.getElementById("reset-statistics-btn").textContent = total
      ? "Сбросить общую статистику"
      : "Сбросить статистику за сегодня";
    if (grid) {
      grid.innerHTML = [
        statisticsCard("Время работы", formatUptime(data.uptime_seconds), "clock"),
        statisticsCard(total ? "Выручка всего" : "Выручка за сегодня", money(data.revenue || 0), "revenue"),
        statisticsCard("Потрачено листов", String(data.sheets_used || 0), "paper"),
        statisticsCard("Распечатано / скопировано", String(data.printed_copied || 0), "print"),
        statisticsCard("Сделано сканов", String(data.scans || 0), "scan"),
      ].join("");
    }
  }

  function statisticsCard(title, value, kind) {
    return '<div class="admin-stat-card admin-stat-card--' + kind + '"><span class="admin-stat-icon" aria-hidden="true"></span>' +
      '<span class="admin-stat-label">' + escapeHtml(title) + '</span><strong>' + escapeHtml(value) + "</strong></div>";
  }

  function applyInfra(data) {
    const setVal = (id, v) => {
      const el = document.getElementById(id);
      if (el) el.value = v;
    };
    if (data.printer) {
      setVal("printer-name", data.printer.name || "—");
      setVal("printer-mode", data.printer.label || "—");
      const testMode = document.querySelector('input[name="test_device_mode"]');
      if (testMode) {
        testMode.disabled = data.printer.config_dry_run === true;
        testMode.checked = data.printer.dry_run === true;
        testMode.closest(".admin-row")?.classList.toggle("is-config-locked", data.printer.config_dry_run === true);
      }
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
    if (data.payment) setVal("payment-driver-url", data.payment.driver_url || "—");
    const listen = document.getElementById("listen-addr");
    if (listen) listen.textContent = "Адрес API: " + (data.listen_addr || "—");
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
    return '<div class="admin-card"><h3>' + escapeHtml(title) + '</h3><div class="admin-metric">' + escapeHtml(value) + "</div></div>";
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
      return null;
    }
    resultEl.textContent = "✓ " + (data.message || "Подключение успешно");
    resultEl.className = "success";
    return data;
  }

  async function loadMaxBotIdentity() {
    const field = document.getElementById("max-bot-username");
    if (!field) return;
    field.value = "Проверяем…";
    try {
      const res = await fetch("/api/kiosk/max/info");
      const data = await res.json().catch(() => ({}));
      field.value = data.bot_username ? "@" + data.bot_username : "Не определено — проверьте токен";
    } catch (_) {
      field.value = "Не удалось проверить";
    }
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

  function operationName(name) {
    return ({print:"Печать", copy:"Копирование", scan:"Сканирование", report:"Печать отчёта"})[name] || name;
  }

  async function loadHistory() {
    const days = Math.max(1, Math.min(30, Number(document.getElementById("history-days").value) || 7));
    document.getElementById("history-days").value = days;
    const empty = document.getElementById("history-empty");
    empty.hidden = false; empty.textContent = "Загружаем историю…";
    const res = await fetch("/api/admin/history?days=" + days, {credentials:"same-origin"});
    const data = await res.json().catch(() => ({}));
    if (!res.ok) { empty.textContent = userError(data, "Не удалось загрузить историю", res.status); return; }
    const items = data.items || [];
    document.getElementById("history-rows").innerHTML = items.map((item) => {
      const date = new Date(item.created_at).toLocaleString("ru-RU", {day:"2-digit",month:"2-digit",year:"numeric",hour:"2-digit",minute:"2-digit",second:"2-digit"});
      return '<tr class="' + (item.success ? "" : "is-error") + '"><td>' + escapeHtml(date) + '</td><td><strong>' + escapeHtml(operationName(item.operation)) + '</strong></td><td>' + Number(item.pages||0) + '</td><td>' + Number(item.sheets||0) + '</td><td>' + money(item.amount||0) + '</td><td><span class="history-result ' + (item.success ? "ok" : "bad") + '">' + (item.success ? "Успешно" : "Ошибка") + '</span></td><td class="history-error-cell">' + escapeHtml(item.error_text || "—") + '</td></tr>';
    }).join("");
    const success = items.filter((x) => x.success).length;
    const amount = items.reduce((n,x) => n + Number(x.amount||0), 0);
    const summary = document.getElementById("history-summary");
    summary.innerHTML = '<div><span>Операций</span><strong>'+items.length+'</strong></div><div><span>Успешно</span><strong>'+success+'</strong></div><div><span>Ошибок</span><strong>'+(items.length-success)+'</strong></div><div><span>Оплачено</span><strong>'+money(amount)+'</strong></div>';
    summary.hidden = false; empty.hidden = items.length > 0; empty.textContent = "За выбранный период операций нет.";
    document.getElementById("history-table-wrap").hidden = items.length === 0;
    document.getElementById("history-actions").hidden = false;
  }

  document.getElementById("history-filter").addEventListener("submit", (e) => { e.preventDefault(); loadHistory(); });
  document.getElementById("history-report-btn").addEventListener("click", async () => {
    const btn = document.getElementById("history-report-btn"); btn.disabled=true; btn.textContent="Формируем PDF…";
    const res = await fetch("/api/admin/history/report", {method:"POST",credentials:"same-origin",headers:{"Content-Type":"application/json"},body:JSON.stringify({days:Number(document.getElementById("history-days").value)})});
    const data = await res.json().catch(() => ({})); btn.disabled=false; btn.textContent="Подготовить PDF";
    if (!res.ok) { document.getElementById("history-empty").hidden=false; document.getElementById("history-empty").textContent=userError(data,"Не удалось сформировать PDF",res.status); return; }
    historyReportID=data.report_id; const pages=Number(data.pages)||1;
    historyDefaultName=data.default_name || ("Отчет_" + new Date().toLocaleDateString("ru-RU").replace(/\//g,".") + ".pdf");
    document.querySelectorAll("[data-history-channel]").forEach((channel)=>{channel.hidden=data.delivery&&data.delivery[channel.dataset.historyChannel]===false;});
    document.getElementById("history-report-pages").textContent="В отчёте " + pages + " стр. Можно напечатать весь отчёт или только нужный диапазон.";
    document.getElementById("history-report-preview").src="/api/admin/history/reports/"+encodeURIComponent(historyReportID)+"/preview#page=1&zoom=page-width&toolbar=0&navpanes=0";
    const from=document.getElementById("history-page-from"), to=document.getElementById("history-page-to"); from.value=1; from.max=pages; to.value=pages; to.max=pages;
    document.getElementById("history-print-error").hidden=true;
    document.getElementById("history-print-modal").showModal();
  });
  document.getElementById("history-print-cancel").addEventListener("click",()=>document.getElementById("history-print-modal").close());
  document.getElementById("history-page-from").addEventListener("change",()=>{
    if(!historyReportID)return;
    const page=Math.max(1,Number(document.getElementById("history-page-from").value)||1);
    document.getElementById("history-report-preview").src="/api/admin/history/reports/"+encodeURIComponent(historyReportID)+"/preview#page="+page+"&zoom=page-width&toolbar=0&navpanes=0";
  });
  const historyDeliveryModal=document.getElementById("history-delivery-modal");
  function historyDeliveryShow(step) {
    ["name","channel","usb","email","max"].forEach((id)=>{document.getElementById("history-"+id+"-step").hidden=id!==step;});
    document.getElementById("history-delivery-success").hidden=true;
    document.getElementById("history-delivery-error").hidden=true;
    document.getElementById("history-delivery-close").hidden=false;
  }
  function historyDeliveryError(message){const el=document.getElementById("history-delivery-error");el.textContent=message;el.hidden=false;}
  function historyFileName(){let name=document.getElementById("history-file-name").value.trim();if(name&&!name.toLowerCase().endsWith(".pdf"))name+=".pdf";return name;}
  function historyDeliveryDone(message){["name","channel","usb","email","max"].forEach((id)=>document.getElementById("history-"+id+"-step").hidden=true);document.getElementById("history-delivery-title").hidden=true;document.getElementById("history-delivery-error").hidden=true;document.getElementById("history-delivery-close").hidden=true;document.getElementById("history-delivery-success-text").textContent=message;document.getElementById("history-delivery-success").hidden=false;}
  document.getElementById("history-take-btn").addEventListener("click",()=>{document.getElementById("history-print-modal").close();document.getElementById("history-delivery-title").hidden=false;document.getElementById("history-file-name").value=historyDefaultName;historyDeliveryShow("name");historyDeliveryModal.showModal();});
  document.getElementById("history-name-next").addEventListener("click",()=>{if(!historyFileName()){historyDeliveryError("Укажите название файла");return;}document.getElementById("history-delivery-title").textContent="Как забрать отчёт?";historyDeliveryShow("channel");});
  document.querySelectorAll("[data-history-channel]").forEach((btn)=>btn.addEventListener("click",async()=>{
    const channel=btn.dataset.historyChannel;
    if(channel==="email"){historyDeliveryShow("email");document.getElementById("history-email").focus();return;}
    if(channel==="usb"){
      historyDeliveryShow("usb");const list=document.getElementById("history-drive-list");list.innerHTML="Проверяем USB…";
      const res=await fetch("/api/kiosk/usb/drives");const data=await res.json().catch(()=>({}));
      if(!res.ok||!(data.drives||[]).length){list.innerHTML="";historyDeliveryError("Флешка не найдена. Вставьте накопитель и попробуйте снова.");return;}
      list.innerHTML="";(data.drives||[]).forEach((drive)=>{const b=document.createElement("button");b.type="button";b.className="admin-btn secondary";b.textContent=drive.label||drive.name;b.addEventListener("click",()=>saveHistoryUSB(drive.path));list.appendChild(b);});return;
    }
    historyDeliveryShow("max");startHistoryMAX();
  }));
  async function saveHistoryUSB(drivePath){const res=await fetch("/api/admin/history/deliver/usb",{method:"POST",credentials:"same-origin",headers:{"Content-Type":"application/json"},body:JSON.stringify({report_id:historyReportID,file_name:historyFileName(),drive_path:drivePath})});const data=await res.json().catch(()=>({}));if(!res.ok){historyDeliveryError(userError(data,"Не удалось сохранить отчёт",res.status));return;}historyDeliveryDone("Отчёт «"+historyFileName()+"» сохранён на флешку. Её можно безопасно извлечь.");}
  document.getElementById("history-email-send").addEventListener("click",async()=>{
    if(historyEmailSending)return;
    const email=document.getElementById("history-email").value.trim();
    const button=document.getElementById("history-email-send"),status=document.getElementById("history-email-status");
    historyEmailSending=true;button.disabled=true;button.textContent="Отправляем…";status.hidden=false;document.getElementById("history-delivery-error").hidden=true;
    try {
      const res=await fetch("/api/admin/history/deliver/email",{method:"POST",credentials:"same-origin",headers:{"Content-Type":"application/json"},body:JSON.stringify({report_id:historyReportID,file_name:historyFileName(),email})});
      const data=await res.json().catch(()=>({}));
      if(!res.ok){historyDeliveryError(userError(data,"Не удалось отправить отчёт",res.status));return;}
      historyDeliveryDone(data.already_sent?("Отчёт «"+historyFileName()+"» уже был отправлен на "+email+"."):("Отчёт «"+historyFileName()+"» отправлен на "+email+"."));
    } catch(e) { historyDeliveryError("Ошибка связи с сервером. Повторите попытку после проверки подключения."); }
    finally { historyEmailSending=false;button.disabled=false;button.textContent="Отправить отчёт";status.hidden=true; }
  });
  async function startHistoryMAX(){const res=await fetch("/api/admin/history/deliver/max",{method:"POST",credentials:"same-origin",headers:{"Content-Type":"application/json"},body:JSON.stringify({report_id:historyReportID,file_name:historyFileName()})});const data=await res.json().catch(()=>({}));if(!res.ok){historyDeliveryError(userError(data,"Не удалось подключить MAX",res.status));return;}historyMaxSession=data.session.id;document.getElementById("history-max-code").textContent=data.session.code;document.getElementById("history-max-bot").textContent=data.bot_username?("Бот: @"+data.bot_username):"Откройте настроенного бота MAX";clearInterval(historyMaxTimer);historyMaxTimer=setInterval(pollHistoryMAX,1800);}
  async function pollHistoryMAX(){if(!historyMaxSession)return;const res=await fetch("/api/admin/history/deliver/max/"+encodeURIComponent(historyMaxSession),{credentials:"same-origin"});const data=await res.json().catch(()=>({}));if(!res.ok)return;const status=data.session&&data.session.status;if(status==="found"){clearInterval(historyMaxTimer);const done=await fetch("/api/admin/history/deliver/max/"+encodeURIComponent(historyMaxSession)+"/complete",{method:"POST",credentials:"same-origin",headers:{"Content-Type":"application/json"},body:JSON.stringify({report_id:historyReportID,file_name:historyFileName()})});const result=await done.json().catch(()=>({}));if(!done.ok){historyDeliveryError(userError(result,"Не удалось отправить отчёт",done.status));return;}historyDeliveryDone("Отчёт «"+historyFileName()+"» уже ждёт вас в MAX.");}else if(status==="timeout"||status==="error"){clearInterval(historyMaxTimer);historyDeliveryError((data.session&&data.session.error)||"Время ожидания истекло");}}
  document.getElementById("history-delivery-close").addEventListener("click",()=>{clearInterval(historyMaxTimer);historyDeliveryModal.close();});
  document.getElementById("history-delivery-more").addEventListener("click",()=>{document.getElementById("history-delivery-title").hidden=false;document.getElementById("history-delivery-title").textContent="Как забрать отчёт?";historyDeliveryShow("channel");});
  document.getElementById("history-delivery-finish").addEventListener("click",()=>{clearInterval(historyMaxTimer);historyDeliveryModal.close();});
  document.getElementById("history-print-btn").addEventListener("click", async () => {
    const btn=document.getElementById("history-print-btn"), err=document.getElementById("history-print-error"); btn.disabled=true; btn.textContent="Отправляем…"; err.hidden=true;
    const res=await fetch("/api/admin/history/print",{method:"POST",credentials:"same-origin",headers:{"Content-Type":"application/json"},body:JSON.stringify({report_id:historyReportID,from_page:Number(document.getElementById("history-page-from").value),to_page:Number(document.getElementById("history-page-to").value)})});
    const data=await res.json().catch(()=>({})); btn.disabled=false; btn.textContent="Напечатать бесплатно";
    if(!res.ok){err.textContent=userError(data,"Не удалось напечатать отчёт",res.status);err.hidden=false;return;}
    document.getElementById("history-print-modal").close(); await loadHistory();
  });

  const refreshOverview = document.getElementById("refresh-overview");
  if (refreshOverview) {
    refreshOverview.addEventListener("click", async () => {
      refreshOverview.disabled = true;
      refreshOverview.textContent = "Проверяем…";
      await loadOverview();
      refreshOverview.disabled = false;
      refreshOverview.textContent = "Обновить состояние";
    });
  }

  document.querySelectorAll("[data-stats-scope]").forEach((btn) => {
    btn.addEventListener("click", () => loadStatistics(btn.dataset.statsScope));
  });

  function scheduleMidnightStatisticsRefresh() {
    const now = new Date();
    const next = new Date(now);
    next.setHours(24, 0, 0, 0);
    setTimeout(async () => {
      if (statsScope === "today") await loadStatistics("today");
      scheduleMidnightStatisticsRefresh();
    }, Math.max(1000, next.getTime() - now.getTime() + 150));
  }
  scheduleMidnightStatisticsRefresh();
  setInterval(() => {
    const overview = document.getElementById("panel-overview");
    if (overview && !overview.hidden) loadStatistics(statsScope);
  }, 60000);

  const statsResetModal = document.getElementById("stats-reset-modal");
  document.getElementById("reset-statistics-btn").addEventListener("click", () => {
    const total = statsScope === "total";
    document.getElementById("stats-reset-title").textContent = total
      ? "Сбросить общую статистику?"
      : "Сбросить статистику за сегодня?";
    document.getElementById("stats-reset-text").textContent = total
      ? "Все накопленные показатели за всё время будут обнулены. Статистика за сегодня останется без изменений."
      : "Показатели текущего дня будут обнулены. Общая статистика за всё время останется без изменений.";
    statsResetModal.showModal();
  });
  document.getElementById("stats-reset-no").addEventListener("click", () => statsResetModal.close());
  document.getElementById("stats-reset-yes").addEventListener("click", async () => {
    const button = document.getElementById("stats-reset-yes");
    button.disabled = true;
    const res = await fetch("/api/admin/stats/reset", {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ scope: statsScope }),
    });
    const data = await res.json().catch(() => ({}));
    button.disabled = false;
    if (!res.ok) {
      document.getElementById("stats-reset-text").textContent = userError(data, "Не удалось сбросить статистику", res.status);
      return;
    }
    statsResetModal.close();
    await loadStatistics(statsScope);
  });

  document.getElementById("home-link").addEventListener("click", (e) => {
    e.preventDefault();
    askLeave({ type: "href", href: "/" });
  });
  document.getElementById("minimize-browser").addEventListener("click", async () => {
    const button = document.getElementById("minimize-browser");
    button.disabled = true;
    const res = await fetch("/api/admin/browser/minimize", {method:"POST", credentials:"same-origin"});
    const data = await res.json().catch(()=>({}));
    button.disabled = false;
    if (!res.ok) {
      const error = document.getElementById("error");
      error.textContent = userError(data, "Не удалось свернуть окно", res.status);
      error.hidden = false;
    }
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
    const data = await postTest(
      "/api/admin/max/test",
      {
        max_bot_token: form.elements.namedItem("max_bot_token").value,
        max_admin_id: form.elements.namedItem("max_admin_id").value,
        send_message: false,
      },
      document.getElementById("max-test-result")
    );
    if (data && data.bot_username) document.getElementById("max-bot-username").value = "@" + data.bot_username;
  });

  document.getElementById("max-send-btn").addEventListener("click", async () => {
    const form = document.getElementById("settings-form");
    const data = await postTest(
      "/api/admin/max/test",
      {
        max_bot_token: form.elements.namedItem("max_bot_token").value,
        max_admin_id: form.elements.namedItem("max_admin_id").value,
        send_message: true,
      },
      document.getElementById("max-test-result")
    );
    if (data && data.bot_username) document.getElementById("max-bot-username").value = "@" + data.bot_username;
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
