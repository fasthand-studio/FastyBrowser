// The scripts picture in picture runs inside pages.
package main

import (
	"fmt"
)

// pipInfoJS describes the page's main video: which site, where to find it
// again, and how far it has played.
const pipInfoJS = `(() => {
  const vs = [...document.querySelectorAll('video')].filter(v => v.readyState > 0 || v.currentSrc);
  if (!vs.length) return JSON.stringify({kind: 'none'});
  const area = v => v.clientWidth * v.clientHeight;
  vs.sort((a, b) => (a.paused - b.paused) || (area(b) - area(a)));
  const v = document.querySelector('video[data-fasty-pip-target]') || vs[0];
  const u = new URL(location.href);
  let id = '';
  if (/(^|\.)youtube\.com$/.test(u.hostname)) {
    id = u.searchParams.get('v') || (u.pathname.match(/^\/(?:shorts|live|embed)\/([\w-]{11})/) || [])[1] || '';
  }
  return JSON.stringify({kind: id ? 'youtube' : (/^https?:/.test(v.currentSrc) ? 'direct' : 'other'),
    id: id, src: v.currentSrc, time: v.currentTime || 0, paused: v.paused, loop: v.loop, rate: v.playbackRate || 1,
    volume: v.volume, page: location.href});
})()`

// pauseJS stops the page's own copy while its video plays in the window.
const pauseJS = `(() => { document.querySelectorAll('video').forEach(v => { try { v.pause(); } catch (e) {} }); return 0; })()`

// playJS makes the page's main video play from a time, quietly resuming it.
func resumeJS(t float64, play bool) string {
	return fmt.Sprintf(`(() => {
  // YouTube's player manages its own buffer, and undoes a raw seek past it,
  // so go through its API there.
  const yt = document.getElementById('movie_player');
  if (yt && yt.seekTo) {
    try { yt.seekTo(%f, true); (%t ? yt.playVideo : yt.pauseVideo).call(yt); return 0; } catch (e) {}
  }
  const vs = [...document.querySelectorAll('video')].filter(v => v.readyState > 0 || v.currentSrc);
  vs.sort((a, b) => b.clientWidth * b.clientHeight - a.clientWidth * a.clientHeight);
  const v = document.querySelector('video[data-fasty-pip-target]') || vs[0]; if (!v) return 0;
  try { v.currentTime = %f; } catch (e) {}
  if (%t) v.play().catch(() => {});
  return 0;
})()`, t, play, t, play)
}

// videoJS finds the page's main video (one that is playing, else the
// biggest) and makes it fill the window: everything else on the page is
// hidden, the video is stretched over it and given WebKit's own controls
// (the site's sit on top of it, hidden too). Every box the video is in is
// stretched over the window as well: WebKit draws the picture relative to
// them, so one left lower on the page would push the picture down. Full screen would be simpler,
// but WebKitGTK stops drawing video in full screen when the window itself
// stays as it is. Returns "none" if there is no video.
const videoJS = `(() => {
  const vs = [...document.querySelectorAll('video')].filter(v => v.readyState > 0 || v.currentSrc);
  if (!vs.length) return 'none';
  const area = v => v.clientWidth * v.clientHeight;
  vs.sort((a, b) => (a.paused - b.paused) || (area(b) - area(a)));
  const v = document.querySelector('video[data-fasty-pip-target]') || vs[0];
  if (!v.hasAttribute('data-fasty-pip')) {
    document.querySelectorAll('[data-fasty-pip],[data-fasty-pip-up]').forEach(e => {
      e.removeAttribute('data-fasty-pip'); e.removeAttribute('data-fasty-pip-up'); });
    v.setAttribute('data-fasty-pip', v.controls ? 'controls' : '');
    for (let e = v.parentElement; e; e = e.parentElement) e.setAttribute('data-fasty-pip-up', '');
  }
  v.controls = true;
  let st = document.getElementById('fasty-pip-style');
  if (!st) {
    st = document.createElement('style');
    st.id = 'fasty-pip-style';
    st.textContent = ` + "`" + `
      html, body { overflow: hidden !important; background: #000 !important; }
      body *:not([data-fasty-pip-up]):not([data-fasty-pip]) { visibility: hidden !important; }
      [data-fasty-pip-up] { transform: none !important; filter: none !important; contain: none !important;
        will-change: auto !important; perspective: none !important; backdrop-filter: none !important;
        clip-path: none !important; mask: none !important; container-type: normal !important;
        content-visibility: visible !important; z-index: 2147483647 !important; }
      body[data-fasty-pip-up], body [data-fasty-pip-up] { position: fixed !important; inset: 0 !important;
        left: 0 !important; top: 0 !important; width: 100vw !important; height: 100vh !important;
        min-width: 0 !important; min-height: 0 !important; max-width: none !important; max-height: none !important;
        margin: 0 !important; padding: 0 !important; border: 0 !important; }
      video[data-fasty-pip] { visibility: visible !important; position: fixed !important; inset: 0 !important;
        left: 0 !important; top: 0 !important; width: 100vw !important; height: 100vh !important;
        max-width: none !important; max-height: none !important; margin: 0 !important;
        transform: none !important; object-fit: contain !important; background: #000 !important;
        z-index: 2147483647 !important; }` + "`" + `;
    document.documentElement.appendChild(st);
  }
  return 'ok';
})()`

// unVideoJS puts the page back as it was.
const unVideoJS = `(() => {
  document.getElementById('fasty-pip-style')?.remove();
  const v = document.querySelector('video[data-fasty-pip]');
  if (v) v.controls = v.getAttribute('data-fasty-pip') === 'controls';
  document.querySelectorAll('[data-fasty-pip],[data-fasty-pip-up]').forEach(e => {
    e.removeAttribute('data-fasty-pip'); e.removeAttribute('data-fasty-pip-up'); });
  return 0;
})()`
