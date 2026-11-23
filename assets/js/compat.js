// Site fixes for things WebKitGTK lacks.
(() => {
  'use strict';

  // GitHub's sign-in page puts "Continue with Google / Apple" (and the
  // passkey divider) in a fragment that it only starts loading once it has
  // found WebAuthn support in the browser (window.PublicKeyCredential).
  // WebKitGTK has none, so the fragment never loads and the buttons never
  // appear. Load it anyway: the buttons are ordinary links to Google and Apple.
  if (location.hostname === 'github.com') {
    const SELECTOR = 'include-fragment[data-target="webauthn-status.fragment"]:not([src])';
    let queued = false;
    const fix = () => {
      queued = false;
      for (const f of document.querySelectorAll(SELECTOR)) {
        const src = f.getAttribute('data-src');
        if (src) f.setAttribute('src', src);
      }
    };
    const schedule = () => { if (!queued) { queued = true; setTimeout(fix, 300); } };
    // GitHub moves between pages without reloading, so keep watching.
    new MutationObserver(schedule).observe(document.documentElement, { childList: true, subtree: true });
    document.addEventListener('DOMContentLoaded', schedule);
    schedule();
  }
})();
