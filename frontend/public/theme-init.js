// Applies the persisted theme before first paint so there is no flash of the
// default one. A separate file rather than an inline <script> so the CSP can
// stay on script-src 'self' without 'unsafe-inline' or per-build hashes.
;(function () {
  const t = localStorage.getItem('taillight-theme')
  if (t && t !== 'tokyonight') document.documentElement.dataset.theme = t
})()
