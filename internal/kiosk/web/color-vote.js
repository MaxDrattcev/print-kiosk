(() => {
  const button = document.getElementById('color-vote-btn');
  if (!button) return;
  const status = document.getElementById('color-vote-status');
  const key = 'color-vote:' + location.pathname + location.search;
  const reloaded = performance.getEntriesByType('navigation')[0]?.type === 'reload';
  let state;
  try { state = reloaded ? JSON.parse(sessionStorage.getItem(key) || 'null') : null; } catch (_) {}
  if (!state) state = {id: crypto.randomUUID(), voted: false};
  const save = () => { try { sessionStorage.setItem(key, JSON.stringify(state)); } catch (_) {} };
  function markVoted() {
    button.classList.add('is-voted');
    button.setAttribute('aria-pressed', 'true');
    button.textContent = '✓ Спасибо, ваш голос учтён!';
    button.disabled = true;
  }
  save();
  if (state.voted) markVoted();
  button.addEventListener('click', async () => {
    button.disabled = true;
    status.hidden = true;
    try {
      const res = await fetch('/api/kiosk/color-print/vote', {
        method: 'POST', headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({session_id: state.id, source: document.body.classList.contains('copy-setup-page') ? 'copy' : 'print'}),
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error || 'Не удалось сохранить голос');
      state.voted = true;
      save();
      markVoted();
    } catch (error) {
      status.textContent = error.message || 'Не удалось сохранить голос. Попробуйте ещё раз.';
      status.hidden = false;
      button.disabled = false;
    }
  });
})();
