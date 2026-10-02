// Applies the saved theme before first paint (kept in sync with src/lib/theme.ts).
// External file because the CSP forbids inline scripts.
;(function () {
  var t = 'system'
  try {
    t = localStorage.getItem('theme') || 'system'
  } catch {}
  var dark = t === 'dark' || (t !== 'light' && window.matchMedia('(prefers-color-scheme: dark)').matches)
  document.documentElement.classList.toggle('dark', dark)
})()
