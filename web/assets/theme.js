(() => {
  const root = document.documentElement;
  const storageKey = 'support-appearance';
  const system = window.matchMedia('(prefers-color-scheme: dark)');
  let preference;
  function readPreference() {
    try {
      const value = localStorage.getItem(storageKey);
      return value === 'light' || value === 'dark' ? value : null;
    } catch { return null; }
  }
  const current = () => preference || (system.matches ? 'dark' : 'light');
  function apply() {
    if (preference) root.dataset.theme = preference;
    else root.removeAttribute('data-theme');
    for (const button of document.querySelectorAll('[data-theme-toggle]')) {
      const label = current() === 'dark' ? button.dataset.themeLightLabel : button.dataset.themeDarkLabel;
      button.setAttribute('aria-label', label);
      button.title = label;
    }
  }
  preference = readPreference();
  root.classList.add('theme-enabled');
  apply();
  document.addEventListener('DOMContentLoaded', apply);
  system.addEventListener('change', apply);
  window.addEventListener('storage', event => {
    if (event.key !== storageKey && event.key !== null) return;
    preference = readPreference();
    apply();
  });
  document.addEventListener('click', event => {
    if (!event.target.closest?.('[data-theme-toggle]')) return;
    preference = current() === 'dark' ? 'light' : 'dark';
    try { localStorage.setItem(storageKey, preference); } catch { /* Switching still works for this page. */ }
    apply();
  });
})();
