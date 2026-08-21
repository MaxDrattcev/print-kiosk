/**
 * Home hero headline rotator. One timer per page lifetime;
 * pagehide / pageshow / visibilitychange start or stop it without duplicates.
 */
(function () {
  if (window.__printstartHero) return;

  const INTERVAL_MS = 5000;
  const FADE_MS = 360;
  const PHRASES = [
    { question: "Документы?", before: "", accent: "Готово", after: "\nза пару минут" },
    { question: "Файл в телефоне?", before: "", accent: "Отправьте", after: "\nмы напечатаем" },
    { question: "Цените своё время?", before: "Печатайте ", accent: "без очереди", after: "" },
    { question: "Нужна копия?", before: "Сделайте ее ", accent: "прямо здесь", after: "" },
    { question: "Документ нужен в PDF?", before: "", accent: "Сканируйте", after: "\nза несколько касаний" },
  ];

  const message = document.getElementById("hero-message");
  const questionEl = document.getElementById("hero-question");
  const textEl = document.getElementById("hero-title-text");
  if (!message || !questionEl || !textEl) return;

  let index = 0;
  let timer = null;
  let fading = false;

  function render(phrase) {
    questionEl.textContent = phrase.question;
    textEl.replaceChildren();
    if (phrase.before) textEl.appendChild(document.createTextNode(phrase.before));
    if (phrase.accent) {
      const em = document.createElement("span");
      em.className = "hero-accent";
      em.textContent = phrase.accent;
      textEl.appendChild(em);
    }
    if (phrase.after) {
      const parts = phrase.after.split("\n");
      parts.forEach((part, partIndex) => {
        if (partIndex > 0) textEl.appendChild(document.createElement("br"));
        if (part) textEl.appendChild(document.createTextNode(part));
      });
    }
  }

  function showEnter() {
    message.classList.remove("is-leave");
    message.classList.add("is-enter");
    void message.offsetWidth;
    message.classList.remove("is-enter");
    message.classList.add("is-in");
  }

  function tick() {
    if (fading || document.hidden) return;
    fading = true;
    message.classList.remove("is-in", "is-enter");
    message.classList.add("is-leave");

    window.setTimeout(() => {
      index = (index + 1) % PHRASES.length;
      render(PHRASES[index]);
      showEnter();
      fading = false;
    }, FADE_MS);
  }

  function start() {
    if (timer != null) return;
    timer = window.setInterval(tick, INTERVAL_MS);
  }

  function stop() {
    if (timer == null) return;
    window.clearInterval(timer);
    timer = null;
  }

  render(PHRASES[0]);
  message.classList.add("is-in");

  document.addEventListener("visibilitychange", () => {
    if (document.hidden) stop();
    else start();
  });
  window.addEventListener("pagehide", stop);
  window.addEventListener("pageshow", start);

  start();
  window.__printstartHero = { stop: stop };
})();
