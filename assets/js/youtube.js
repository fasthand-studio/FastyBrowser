// YouTube's video ads are scheduled from the player response itself, not
// from a separate ad URL. Strip the ad keys from the inline player response
// and from player/watch/playlist responses so no ad is ever requested.
// (Same mechanism uBlock Origin and AdGuard use.)
(() => {
  'use strict';
  const host = location.hostname;
  if (host !== 'www.youtube.com' && host !== 'youtube.com' && host !== 'm.youtube.com') return;
  if (window.__fastyNoAds) return;
  window.__fastyNoAds = true;

  const KEYS = ['adPlacements', 'adSlots', 'playerAds', 'bkaEnforcementMessageViewModel'];

  function strip(o) {
    if (!o || typeof o !== 'object') return false;
    let changed = false;
    for (const k of KEYS) {
      if (Object.prototype.hasOwnProperty.call(o, k)) {
        delete o[k];
        changed = true;
      }
    }
    if (o.playerResponse && typeof o.playerResponse === 'object' && strip(o.playerResponse)) changed = true;
    return changed;
  }

  function isPlayer(url) {
    return /\/youtubei\/v1\/(player|get_watch)|\/watch\?|\/playlist\?/.test(String(url || ''));
  }

  // Inline response in the document source.
  let inline;
  Object.defineProperty(window, 'ytInitialPlayerResponse', {
    configurable: true,
    get() { return inline; },
    set(v) { strip(v); inline = v; }
  });

  // fetch() responses.
  const realFetch = window.fetch;
  window.fetch = async function (...args) {
    const response = await realFetch.apply(this, args);
    const url = typeof args[0] === 'string' ? args[0] : (args[0] && args[0].url) || '';
    if (!isPlayer(url) || !response.ok) return response;
    try {
      const json = await response.clone().json();
      if (!strip(json)) return response;
      return new Response(JSON.stringify(json), {
        status: response.status,
        statusText: response.statusText,
        headers: response.headers
      });
    } catch (e) {
      return response;
    }
  };

  // XHR responses (YouTube still uses these for some player calls).
  const realOpen = XMLHttpRequest.prototype.open;
  XMLHttpRequest.prototype.open = function (method, url, ...rest) {
    this.__fastyURL = String(url || '');
    return realOpen.call(this, method, url, ...rest);
  };
  const realSend = XMLHttpRequest.prototype.send;
  XMLHttpRequest.prototype.send = function (...args) {
    this.addEventListener('loadend', () => {
      if (!isPlayer(this.__fastyURL)) return;
      try {
        const text = this.responseText;
        const json = JSON.parse(text);
        if (!strip(json)) return;
        const out = JSON.stringify(json);
        Object.defineProperty(this, 'responseText', { value: out, configurable: true });
        if (this.responseType === '' || this.responseType === 'text') {
          Object.defineProperty(this, 'response', { value: out, configurable: true });
        } else if (this.responseType === 'json') {
          Object.defineProperty(this, 'response', { value: json, configurable: true });
        }
      } catch (e) { /* leave the original response alone */ }
    });
    return realSend.apply(this, args);
  };
})();
