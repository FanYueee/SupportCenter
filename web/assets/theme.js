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
  const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)');
  document.addEventListener('click', event => {
    const button = event.target.closest?.('[data-theme-toggle]');
    if (!button) return;
    preference = current() === 'dark' ? 'light' : 'dark';
    try { localStorage.setItem(storageKey, preference); } catch { /* Switching still works for this page. */ }
    if (!document.startViewTransition || reducedMotion.matches) {
      apply();
      return;
    }
    // The new appearance grows as a circle from the switch (see theme-reveal in app.css).
    const box = button.getBoundingClientRect();
    root.style.setProperty('--theme-x', `${box.left + box.width / 2}px`);
    root.style.setProperty('--theme-y', `${box.top + box.height / 2}px`);
    root.classList.add('theme-transition');
    document.startViewTransition(apply).finished.finally(() => root.classList.remove('theme-transition'));
  });
})();
