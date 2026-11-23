// YouTube's video ads are scheduled from the player response itself, not
// from a separate ad URL, so this works the way Brave's and uBlock Origin's
// YouTube scriptlets do (brave/adblock-resources + brave-main-list.txt):
//
//  1. set-constant      ytInitialPlayerResponse never carries ad keys
//  2. json-prune        JSON.parse / Response.json results are pruned
//  3. replace-response  fetch + XHR for player, get_watch, next, playlist and
//                       reel_watch_sequence have ad keys renamed to "no_ads"
//  4. SSAP guard        server-side-inserted ads (baked into the stream, so
//                       they can't be pruned) are seeked past immediately
//  5. cosmetic          exact selectors for ad slots, hidden from inside the page
(() => {
  'use strict';
  const host = location.hostname;
  if (!/(^|\.)youtube(-nocookie)?\.com$/.test(host)) return;
  if (window.__fastyNoAds) return;
  window.__fastyNoAds = true;

  const AD_KEYS = ['adPlacements', 'adSlots', 'playerAds', 'bkaEnforcementMessageViewModel'];
  const URL_RE = /\/youtubei\/v1\/(player|get_watch|next|reel\/reel_watch_sequence|browse)|\/watch\?[tv]=|\/playlist\?list=/;

  const isAdEntry = (v) => !!v && typeof v === 'object' && (
    v.adSlotRenderer ||
    (v.command && v.command.reelWatchEndpoint && v.command.reelWatchEndpoint.adClientParams &&
      v.command.reelWatchEndpoint.adClientParams.isAd)
  );

  // Rename ad keys to "no_ads" (what Brave does) and drop ad entries from
  // lists (Shorts feed, mobile get_watch). Returns true if anything changed.
  function strip(o, depth) {
    depth = depth || 0;
    if (!o || typeof o !== 'object' || depth > 12) return false;
    let changed = false;
    if (Array.isArray(o)) {
      for (let i = o.length - 1; i >= 0; i--) {
        if (isAdEntry(o[i])) { o.splice(i, 1); changed = true; }
        else if (strip(o[i], depth + 1)) changed = true;
      }
      return changed;
    }
    for (const k of AD_KEYS) {
      if (Object.prototype.hasOwnProperty.call(o, k)) {
        o.no_ads = o[k];
        delete o[k];
        changed = true;
      }
    }
    // Only walk the branches that can hold ads, to keep big responses cheap.
    for (const k of ['playerResponse', 'response', 'contents', 'entries', 'reelWatchSequenceResponse',
                     'results', 'twoColumnWatchNextResults', 'secondaryResults', 'itemSectionRenderer', 'sectionListRenderer']) {
      if (o[k] && typeof o[k] === 'object' && strip(o[k], depth + 1)) changed = true;
    }
    return changed;
  }

  // 1. Inline response in the document source.
  let inline;
  try {
    Object.defineProperty(window, 'ytInitialPlayerResponse', {
      configurable: true,
      get() { return inline; },
      set(v) { strip(v); inline = v; }
    });
  } catch (e) { /* already defined */ }

  // 2. JSON.parse / Response.json pruning (json-prune).
  const realParse = JSON.parse;
  JSON.parse = function (text, reviver) {
    const v = realParse.call(this, text, reviver);
    try { if (v && typeof v === 'object' && !reviver) strip(v); } catch (e) { /* ignore */ }
    return v;
  };
  const realJson = Response.prototype.json;
  Response.prototype.json = function () {
    const p = realJson.call(this);
    return p.then((v) => { try { strip(v); } catch (e) { /* ignore */ } return v; });
  };

  // 3a. fetch() responses.
  const realFetch = window.fetch;
  window.fetch = async function (...args) {
    const response = await realFetch.apply(this, args);
    const url = typeof args[0] === 'string' ? args[0] : (args[0] && args[0].url) || String(args[0] || '');
    if (!URL_RE.test(url) || !response.ok) return response;
    try {
      const json = await response.clone().json();
      if (!strip(json)) return response;
      return new Response(JSON.stringify(json), {
        status: response.status, statusText: response.statusText, headers: response.headers
      });
    } catch (e) {
      return response;
    }
  };

  // 3b. XHR responses.
  const realOpen = XMLHttpRequest.prototype.open;
  XMLHttpRequest.prototype.open = function (method, url, ...rest) {
    this.__fastyURL = String(url || '');
    return realOpen.call(this, method, url, ...rest);
  };
  const realSend = XMLHttpRequest.prototype.send;
  XMLHttpRequest.prototype.send = function (...args) {
    this.addEventListener('readystatechange', () => {
      if (this.readyState !== 4 || !URL_RE.test(this.__fastyURL || '')) return;
      try {
        const rt = this.responseType;
        if (rt !== '' && rt !== 'text' && rt !== 'json') return;
        const json = rt === 'json' ? this.response : realParse(this.responseText);
        if (!strip(json)) return;
        const out = JSON.stringify(json);
        Object.defineProperty(this, 'responseText', { value: out, configurable: true });
        Object.defineProperty(this, 'response', { value: rt === 'json' ? json : out, configurable: true });
      } catch (e) { /* leave the original response alone */ }
    });
    return realSend.apply(this, args);
  };

  // 5. Cosmetic layer — Brave's/EasyList's exact selectors, plus the sidebar
  // companion banner and the in-player ad containers.
  const HIDE = [
    '#masthead-ad', '#player-ads', 'ytd-companion-slot-renderer',
    'ytd-ad-slot-renderer', 'ytd-display-ad-renderer', 'ytd-in-feed-ad-layout-renderer',
    'ytd-action-companion-ad-renderer', 'ytd-promoted-sparkles-web-renderer',
    'ytd-promoted-video-renderer', 'ytd-search-pyv-renderer', 'ytd-banner-promo-renderer',
    'ytd-statement-banner-renderer', 'ytd-primetime-promo-renderer',
    'ytd-brand-video-singleton-renderer', 'ytd-brand-video-shelf-renderer',
    'ytd-merch-shelf-renderer', '#shopping-timely-shelf', 'yt-overlay-product-sticker',
    'ad-slot-renderer', 'ytm-companion-slot[data-content-type]', 'ytm-companion-ad-renderer',
    '.ytp-ad-module', '.ytp-ad-overlay-container', '.ytp-paid-content-overlay',
    '.ytp-suggested-action > .ytp-suggested-action-badge',
    'ytd-rich-item-renderer:has(> #content > ytd-ad-slot-renderer)',
    'ytd-rich-item-renderer:has(ytd-ad-slot-renderer)',
    '#shorts-inner-container > .ytd-shorts:has(> .ytd-reel-video-renderer > ytd-ad-slot-renderer)',
    'ytd-rich-section-renderer:has(ytd-statement-banner-renderer)',
    'ytd-engagement-panel-section-list-renderer[target-id="engagement-panel-ads"]',
    // The "ad blockers violate YouTube's terms" popup and its backdrop.
    'ytd-enforcement-message-view-model', 'tp-yt-paper-dialog:has(ytd-enforcement-message-view-model)',
    'tp-yt-iron-overlay-backdrop.opened'
  ];
  const style = document.createElement('style');
  style.textContent = HIDE.join(',\n') + ' { display: none !important; }';
  (document.documentElement || document).appendChild(style);

  // 4. Server-side-inserted / leftover ads: YouTube marks them in the player's
  // stats ("SSAP, AD") and with .ad-showing. Jump to the end, press Skip.
  function adPlaying(player) {
    if (player.classList.contains('ad-showing')) return true;
    try {
      const s = player.getStatsForNerds && player.getStatsForNerds();
      return !!(s && s.debug_info && String(s.debug_info).startsWith('SSAP, AD'));
    } catch (e) { return false; }
  }

  let wasPaused = false;
  function guard() {
    const player = document.getElementById('movie_player');
    if (!player) return;

    // If the anti-adblock popup paused the video, resume it.
    const popup = document.querySelector('ytd-enforcement-message-view-model');
    if (popup) {
      popup.remove();
      document.querySelectorAll('tp-yt-iron-overlay-backdrop').forEach((e) => e.remove());
      const v = player.querySelector('video');
      if (v && v.paused) v.play().catch(() => {});
    }

    if (!adPlaying(player)) {
      if (wasPaused) { wasPaused = false; }
      return;
    }
    const video = player.querySelector('video.html5-main-video') || player.querySelector('video');
    if (video) {
      video.muted = true;
      if (isFinite(video.duration) && video.duration > 0) {
        if (player.seekTo) { try { player.seekTo(video.duration); } catch (e) { /* ignore */ } }
        video.currentTime = video.duration;
      }
    }
    const btn = document.querySelector(
      '.ytp-skip-ad-button, .ytp-ad-skip-button, .ytp-ad-skip-button-modern, .ytp-ad-skip-button-container button');
    if (btn) btn.click();
  }

  // Run on DOM changes (throttled to one frame) instead of a tight timer; a
  // slow heartbeat covers the case where the class flips without a mutation.
  let queued = false;
  const schedule = () => {
    if (queued) return;
    queued = true;
    requestAnimationFrame(() => { queued = false; guard(); });
  };
  const start = () => {
    new MutationObserver(schedule).observe(document.documentElement, {
      childList: true, subtree: true, attributes: true, attributeFilter: ['class']
    });
    setInterval(guard, 1000);
  };
  if (document.documentElement) start();
  else document.addEventListener('DOMContentLoaded', start, { once: true });
})();
