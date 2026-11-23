// Picture in picture, the way Firefox does it: the video stays where it is,
// playing in its own page, and the picture is copied out to the pop-up window.
// Each frame is drawn small onto a canvas, encoded as a JPEG and handed to the
// browser as a string. The window's controls call back into this to play,
// pause and seek the page's own video.
//
// Returns 'ok' once copying has started, 'none' if there is no video, and
// 'tainted' if the page's rules forbid reading the video's pixels (then the
// browser uses another way).
(() => {
  'use strict';
  if (window.__fastyPipCap) { try { window.__fastyPipCap.stop(); } catch (e) { /* ignore */ } }

  const post = (s) => window.webkit.messageHandlers.officeVeil.postMessage(s);
  const pick = () => document.querySelector('video[data-fasty-pip-target]') ||
    [...document.querySelectorAll('video')].filter((x) => x.readyState > 0 || x.currentSrc)
      .sort((a, b) => (a.paused - b.paused) || (b.clientWidth * b.clientHeight - a.clientWidth * a.clientHeight))[0];
  const v = pick();
  if (!v) return 'none';

  const canvas = document.createElement('canvas');
  const ctx = canvas.getContext('2d', { alpha: false });
  try {
    canvas.width = 2; canvas.height = 2;
    ctx.drawImage(v, 0, 0, 2, 2);
    ctx.getImageData(0, 0, 1, 1);
  } catch (e) { return 'tainted'; }

  let maxW = __MAXW__, stopped = false, busy = false, last = 0, rvfc = 0, beat = 0, tick = 0;
  const QUALITY = 0.65, MIN_GAP = 40; // about 25 frames a second at most

  function state() {
    return (v.currentTime || 0).toFixed(2) + '|' + (isFinite(v.duration) ? v.duration : 0).toFixed(2) + '|' + (v.paused ? 1 : 0);
  }

  function frame(force) {
    if (stopped || busy) return;
    const now = performance.now();
    if (!force && now - last < MIN_GAP) return;
    const vw = v.videoWidth, vh = v.videoHeight;
    if (!vw || !vh || v.readyState < 2) return;
    const w = Math.max(2, Math.min(maxW, vw)), h = Math.max(2, Math.round(w * vh / vw));
    if (canvas.width !== w || canvas.height !== h) { canvas.width = w; canvas.height = h; }
    try { ctx.drawImage(v, 0, 0, w, h); } catch (e) { return; }
    last = now;
    busy = true;
    canvas.toBlob((blob) => {
      if (!blob || stopped) { busy = false; return; }
      const fr = new FileReader();
      fr.onload = () => {
        const d = String(fr.result);
        post('PIPF|' + state() + '|' + w + '|' + h + '|' + d.slice(d.indexOf(',') + 1));
        busy = false;
      };
      fr.onerror = () => { busy = false; };
      fr.readAsDataURL(blob);
    }, 'image/jpeg', QUALITY);
  }

  const onVideoFrame = () => {
    frame(false);
    if (!stopped && v.requestVideoFrameCallback) rvfc = v.requestVideoFrameCallback(onVideoFrame);
  };
  if (v.requestVideoFrameCallback) rvfc = v.requestVideoFrameCallback(onVideoFrame);
  // A page that isn't on screen gets no video-frame callbacks: a timer covers it.
  tick = setInterval(() => frame(false), 33);
  // Paused or seeking: nothing new arrives by itself, so send what changed.
  const nudge = () => frame(true);
  const events = ['pause', 'play', 'seeked', 'loadeddata', 'ratechange', 'resize'];
  events.forEach((e) => v.addEventListener(e, nudge));
  beat = setInterval(() => { if (!stopped) post('PIPS|' + state()); }, 500);
  frame(true);

  const api = {
    stop() {
      stopped = true;
      clearInterval(tick); clearInterval(beat);
      if (rvfc && v.cancelVideoFrameCallback) v.cancelVideoFrameCallback(rvfc);
      events.forEach((e) => v.removeEventListener(e, nudge));
      try { delete window.__fastyPipCap; } catch (e) { window.__fastyPipCap = undefined; }
    },
    toggle() { if (v.paused) v.play().catch(() => {}); else v.pause(); },
    play() { v.play().catch(() => {}); },
    pause() { v.pause(); },
    seek(t) {
      const yt = document.getElementById('movie_player');
      if (yt && yt.seekTo) { try { yt.seekTo(t, true); return; } catch (e) { /* fall through */ } }
      try { v.currentTime = t; } catch (e) { /* ignore */ }
    },
    skip(d) { api.seek(Math.max(0, (v.currentTime || 0) + d)); },
    width(w) { maxW = Math.max(160, Math.min(1920, Math.round(w))); frame(true); }
  };
  Object.defineProperty(window, '__fastyPipCap', { value: api, configurable: true, writable: true });
  return 'ok';
})()
