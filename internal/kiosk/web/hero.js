/**
 * Home hero headline rotator. One timer per page lifetime;
 * pagehide / pageshow / visibilitychange start or stop it without duplicates.
 */
(function () {
  if (window.__printstartHero) return;

  const INTERVAL_MS = 4500;
  const FADE_MS = 360;
  const PHRASES = [
    { before: "Распечатайте документ за пару ", accent: "минут", after: "" },
    { before: "Печать, копирование и сканирование — ", accent: "без очереди", after: "" },
    { before: "Отправьте файл с ", accent: "телефона", after: " и заберите распечатку" },
    { before: "Нужна ", accent: "копия", after: "? Сделайте её прямо здесь" },
    { before: "", accent: "Сканируйте", after: " документы и отправляйте их себе" },
  ];

  const title = document.getElementById("hero-title");
  const textEl = document.getElementById("hero-title-text");
  if (!title || !textEl) return;

  let index = 0;
  let timer = null;
  let fading = false;

  function render(phrase) {
    textEl.replaceChildren();
    if (phrase.before) textEl.appendChild(document.createTextNode(phrase.before));
    if (phrase.accent) {
      const em = document.createElement("span");
      em.className = "hero-accent";
      em.textContent = phrase.accent;
      textEl.appendChild(em);
    }
    if (phrase.after) textEl.appendChild(document.createTextNode(phrase.after));
  }

  function showEnter() {
    textEl.classList.remove("is-leave");
    textEl.classList.add("is-enter");
    void textEl.offsetWidth;
    textEl.classList.remove("is-enter");
    textEl.classList.add("is-in");
  }

  function tick() {
    if (fading || document.hidden) return;
    fading = true;
    textEl.classList.remove("is-in", "is-enter");
    textEl.classList.add("is-leave");

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
  textEl.classList.add("is-in");

  document.addEventListener("visibilitychange", () => {
    if (document.hidden) stop();
    else start();
  });
  window.addEventListener("pagehide", stop);
  window.addEventListener("pageshow", start);

  start();
  window.__printstartHero = { stop: stop };
})();
