// Applies the saved theme and design before first paint (kept in sync with
// src/lib/theme.ts and src/app/design.ts). External file because the CSP
// forbids inline scripts.
;(function () {
  var t = 'system'
  var d = 'triage'
  try {
    t = localStorage.getItem('theme') || 'system'
    var q = new URLSearchParams(location.search).get('design')
    if (q === 'command' || q === 'triage' || q === 'bento') localStorage.setItem('findbugs.design', q)
    d = localStorage.getItem('findbugs.design') || d
  } catch {}
  var dark = t === 'dark' || (t !== 'light' && window.matchMedia('(prefers-color-scheme: dark)').matches)
  document.documentElement.classList.toggle('dark', dark)
  document.documentElement.dataset.design = d
})()
