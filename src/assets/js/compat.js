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

  // GitHub's tab bars (the repo's Code / Issues / Pull requests row, action
  // bars) find out whether they overflow with a scroll-driven CSS animation
  // (animation-timeline: scroll(self)) that flips a custom property when the
  // bar becomes scrollable, to show a "More" button. WebKit keeps that
  // animation busy on every frame, which halved the frame rate of the whole
  // page while scrolling (about 35 ms a frame instead of 16). Stop the
  // animation and do the same measuring from script, only when the size or
  // the contents change.
  if (location.hostname === 'github.com') {
    const BARS = [
      ['[class*="-UnderlineWrapper-"]', '--UnderlineNav_moreButton-display'],
      ['[class*="ActionBar-List-"]', '--morebutton-display']
    ];
    const style = document.createElement('style');
    style.textContent = BARS.map(([sel]) => sel).join(',') +
      ' { animation: none !important; animation-timeline: auto !important; }';
    (document.head || document.documentElement).appendChild(style);

    const ro = new ResizeObserver((entries) => { for (const e of entries) measure(e.target); });
    const seen = new WeakSet();
    function measure(el) {
      const prop = el.__fastyBarProp;
      if (!prop) return;
      if (el.scrollWidth > el.clientWidth + 1) el.style.setProperty(prop, 'flex');
      else el.style.removeProperty(prop);
    }
    function scan() {
      for (const [sel, prop] of BARS) {
        for (const el of document.querySelectorAll(sel)) {
          if (!seen.has(el)) { seen.add(el); el.__fastyBarProp = prop; ro.observe(el); }
          measure(el);
        }
      }
    }
    let scanQueued = false;
    const later = () => { if (!scanQueued) { scanQueued = true; setTimeout(() => { scanQueued = false; scan(); }, 300); } };
    new MutationObserver(later).observe(document.documentElement, { childList: true, subtree: true });
    document.addEventListener('DOMContentLoaded', later);
    later();
  }
})();
