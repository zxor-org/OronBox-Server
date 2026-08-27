package web

// ThemeJS runs before first paint so a stored theme preference does not flash
// the opposite palette. Login still loads it from /assets/theme.js.
const ThemeJS = `(() => {
  const savedTheme = localStorage.getItem('oronbox_server_theme');
  if (savedTheme === 'dark' || savedTheme === 'light') {
    document.documentElement.classList.add(savedTheme + '-theme');
  }
})();
`

// TransitionJS drives the automatic redirect on OAuth transition pages. The
// target arrives through a data attribute and is re-checked here so a
// scheme that never passes server-side validation cannot execute either.
const TransitionJS = `(() => {
  const holder = document.querySelector('[data-transition-target]');
  if (!holder) return;
  const target = holder.dataset.transitionTarget;
  if (!target) return;
  if (/^\s*(javascript|data|vbscript):/i.test(target)) return;
  window.setTimeout(() => { location.replace(target); }, 900);
})();
`
