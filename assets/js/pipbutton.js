// A small, quiet Picture in Picture button, like the ones in a video's own
// controls: an icon in the corner of the video that shows while the pointer
// is on it and fades with it. Clicking it pops the video out into its own
// window; while it is out, the page's video area says so, and clicking that
// brings the video back.
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
    btn.setAttribute('role', 'button');
    btn.setAttribute('aria-label', 'Picture in Picture');
    // Built with the DOM, not innerHTML: YouTube enforces Trusted Types.
    const NS = 'http://www.w3.org/2000/svg';
    const svg = document.createElementNS(NS, 'svg');
    for (const [k, v] of [['viewBox', '0 0 24 24'], ['width', '16'], ['height', '16'], ['fill', 'none'],
                          ['stroke', '#fff'], ['stroke-width', '2'], ['stroke-linejoin', 'round']]) svg.setAttribute(k, v);
    const big = document.createElementNS(NS, 'rect');
    for (const [k, v] of [['x', '2.5'], ['y', '4.5'], ['width', '19'], ['height', '15'], ['rx', '2']]) big.setAttribute(k, v);
    const small = document.createElementNS(NS, 'rect');
    for (const [k, v] of [['x', '12'], ['y', '11'], ['width', '8'], ['height', '6'], ['rx', '1'], ['fill', '#fff']]) small.setAttribute(k, v);
    svg.append(big, small);
    btn.appendChild(svg);
    btn.style.cssText = 'all:initial;position:fixed;z-index:2147483647;width:30px;height:30px;box-sizing:border-box;' +
      'display:none;align-items:center;justify-content:center;border-radius:6px;cursor:pointer;opacity:0;' +
      'background:rgba(0,0,0,.45);transition:opacity .15s,background .15s;';
    btn.addEventListener('mouseenter', () => { btn.style.background = 'rgba(0,0,0,.75)'; });
    btn.addEventListener('mouseleave', () => { btn.style.background = 'rgba(0,0,0,.45)'; });
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
        showOverlay(v);
      }
      try { window.webkit.messageHandlers.officeVeil.postMessage(msg); } catch (err) { /* ignore */ }
      hide();
    }, true);
    btn.addEventListener('mousedown', (e) => e.stopPropagation(), true);
    btn.addEventListener('mouseenter', () => clearTimeout(hideTimer));
    (document.body || document.documentElement).appendChild(btn);
  }

  function hide() {
    if (!btn) return;
    btn.style.opacity = '0';
    setTimeout(() => { if (btn.style.opacity === '0') btn.style.display = 'none'; }, 160);
    current = null;
  }

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
        hideTimer = setTimeout(hide, 400);
      }
      return;
    }
    const r = v.getBoundingClientRect();
    if (r.width < MIN || r.height < MIN * 0.5) return;
    if (!btn) make();
    clearTimeout(hideTimer);
    current = v;
    btn.style.display = 'flex';
    // Tucked into the video's top-right corner.
    btn.style.left = Math.max(4, Math.min(innerWidth, r.right) - 38) + 'px';
    btn.style.top = Math.max(4, r.top + 8) + 'px';
    btn.style.opacity = '1';
    hideTimer = setTimeout(hide, 2000);
  }, { capture: true, passive: true });

  // While a video is out in its pop-up window, its place in the page says so.
  let overlay, overlayTimer, overlayVideo;
  function placeOverlay() {
    if (!overlay || !overlayVideo || !overlayVideo.isConnected) return;
    const r = overlayVideo.getBoundingClientRect();
    overlay.style.left = r.left + 'px'; overlay.style.top = r.top + 'px';
    overlay.style.width = r.width + 'px'; overlay.style.height = r.height + 'px';
  }
  function showOverlay(v) {
    hideOverlay();
    if (!v) return;
    overlayVideo = v;
    overlay = document.createElement('div');
    overlay.setAttribute('data-fasty-pip-overlay', '');
    overlay.style.cssText = 'all:initial;position:fixed;z-index:2147483646;display:flex;flex-direction:column;' +
      'align-items:center;justify-content:center;gap:6px;background:#1c1b22;color:#fbfbfe;' +
      'font:15px system-ui,sans-serif;text-align:center;cursor:pointer;';
    const a = document.createElement('div');
    a.textContent = 'This video is playing in Picture-in-Picture';
    a.style.cssText = 'all:initial;color:#fbfbfe;font:600 16px system-ui,sans-serif;';
    const b = document.createElement('div');
    b.textContent = 'Click here to bring it back';
    b.style.cssText = 'all:initial;color:#9e9ea6;font:13px system-ui,sans-serif;';
    overlay.append(a, b);
    overlay.addEventListener('click', (e) => {
      e.preventDefault(); e.stopPropagation();
      hideOverlay();
      try { window.webkit.messageHandlers.officeVeil.postMessage({ _ch: 'pip' }); } catch (err) { /* ignore */ }
    }, true);
    (document.body || document.documentElement).appendChild(overlay);
    placeOverlay();
    overlayTimer = setInterval(placeOverlay, 250);
  }
  function hideOverlay() {
    clearInterval(overlayTimer);
    if (overlay) overlay.remove();
    overlay = overlayVideo = null;
  }
  // The browser calls these when the pop-up window opens and closes.
  window.__fastyPipUI = {
    on() {
      const vs = [...document.querySelectorAll('video')].filter(v => v.readyState > 0 || v.currentSrc);
      vs.sort((a, b) => b.clientWidth * b.clientHeight - a.clientWidth * a.clientHeight);
      showOverlay(document.querySelector('video[data-fasty-pip-target]') || vs[0]);
    },
    off: hideOverlay
  };
  window.__fastyPipFrameOverlay = showOverlay;
})();
