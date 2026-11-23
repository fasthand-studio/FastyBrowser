// A small "Picture in Picture" button that appears over a video while the
// pointer is on it (like Chrome/Edge's hover button). Clicking it asks the
// browser to pop the video out into its own window.
(() => {
  'use strict';
  if (window.__fastyPipButton) return;
  const inFrame = window.top !== window;
  window.__fastyPipButton = true;

  const MIN = 200; // ignore tiny videos (ads, previews, icons)
  let btn, hideTimer, current;

  function make() {
    btn = document.createElement('div');
    btn.setAttribute('data-fasty-pip-btn', '');
    btn.title = 'Picture in Picture';
    // Built with the DOM, not innerHTML: YouTube enforces Trusted Types.
    const NS = 'http://www.w3.org/2000/svg';
    const svg = document.createElementNS(NS, 'svg');
    for (const [k, v] of [['viewBox', '0 0 24 24'], ['width', '18'], ['height', '18'], ['fill', 'none'],
                          ['stroke', '#fff'], ['stroke-width', '2'], ['stroke-linejoin', 'round']]) svg.setAttribute(k, v);
    const big = document.createElementNS(NS, 'rect');
    for (const [k, v] of [['x', '2.5'], ['y', '4.5'], ['width', '19'], ['height', '15'], ['rx', '2']]) big.setAttribute(k, v);
    const small = document.createElementNS(NS, 'rect');
    for (const [k, v] of [['x', '12'], ['y', '11'], ['width', '8'], ['height', '6'], ['rx', '1'], ['fill', '#fff']]) small.setAttribute(k, v);
    svg.append(big, small);
    btn.appendChild(svg);
    btn.style.cssText = 'all:initial;position:fixed;z-index:2147483647;width:34px;height:34px;' +
      'display:none;align-items:center;justify-content:center;border-radius:17px;cursor:pointer;' +
      'background:rgba(20,20,20,.78);box-shadow:none;';
    btn.addEventListener('click', (e) => {
      e.preventDefault(); e.stopPropagation();
      const v = current;
      document.querySelectorAll('video[data-fasty-pip-target]').forEach(e => e.removeAttribute('data-fasty-pip-target'));
      if (v) v.setAttribute('data-fasty-pip-target', '');
      const msg = { _ch: 'pip' };
      if (inFrame && v) {
        // A video inside an embedded frame: the browser re-opens the frame's
        // own page in the pop-up, from where the video is now.
        msg.frame = location.href;
        msg.time = v.currentTime || 0;
        msg.rate = v.playbackRate || 1;
        msg.paused = v.paused;
        try { v.pause(); } catch (err) { /* ignore */ }
      }
      try { window.webkit.messageHandlers.officeVeil.postMessage(msg); } catch (err) { /* ignore */ }
      hide();
    }, true);
    btn.addEventListener('mousedown', (e) => e.stopPropagation(), true);
    btn.addEventListener('mouseenter', () => clearTimeout(hideTimer));
    (document.body || document.documentElement).appendChild(btn);
  }

  function hide() { if (btn) btn.style.display = 'none'; current = null; }

  function videoAt(x, y) {
    // elementsFromPoint, also looking inside shadow roots (custom players).
    const seen = new Set();
    const search = (root) => {
      for (const el of root.elementsFromPoint(x, y)) {
        if (el.tagName === 'VIDEO') return el;
        if (el.shadowRoot && !seen.has(el.shadowRoot)) {
          seen.add(el.shadowRoot);
          const f = search(el.shadowRoot);
          if (f) return f;
        }
      }
      return null;
    };
    return search(document);
  }

  document.addEventListener('mousemove', (e) => {
    if (document.fullscreenElement) return hide();
    const v = videoAt(e.clientX, e.clientY);
    if (!v) {
      if (btn && btn.style.display !== 'none') {
        clearTimeout(hideTimer);
        hideTimer = setTimeout(hide, 600);
      }
      return;
    }
    const r = v.getBoundingClientRect();
    if (r.width < MIN || r.height < MIN * 0.5) return;
    if (!btn) make();
    clearTimeout(hideTimer);
    current = v;
    btn.style.left = Math.max(4, Math.min(innerWidth - 38, r.right - 44)) + 'px';
    btn.style.top = Math.max(4, r.top + 10) + 'px';
    btn.style.display = 'flex';
    hideTimer = setTimeout(hide, 2500);
  }, { capture: true, passive: true });
})();
