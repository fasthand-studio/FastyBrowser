/* Shared by the browser's own pages: theme, calls to the app, and the browser's shortcut keys. */
(function () {
  const q = new URLSearchParams(location.search);
  document.documentElement.dataset.theme = q.get('t') || (matchMedia('(prefers-color-scheme:dark)').matches ? 'dark' : 'light');
  const post = o => { try { window.chrome.webview.postMessage(JSON.stringify(o)); } catch (e) {} };
  let seq = 0; const pending = {};
  window.fastyReply = (id, res) => { const f = pending[id]; if (f) { delete pending[id]; f(res); } };
  window.rpc = (m, p) => new Promise(resolve => { const id = ++seq; pending[id] = resolve; post({ a: 'rpc', id, m, p: p || {} }); });
  window.setTitle = t => { document.title = t; post({ a: 'title', t }); };
  window.fastyPost = post;
  const FWD = new Set(['ctrl+t','ctrl+shift+t','ctrl+shift+n','ctrl+w','ctrl+l','ctrl+k','ctrl+d','ctrl+y','ctrl+alt+b','ctrl+shift+b','ctrl+shift+a','ctrl+shift+h','ctrl+shift+u','ctrl+shift+j','ctrl+,','ctrl+r','f5','ctrl+shift+r','ctrl+shift+s','ctrl+s','ctrl+tab','ctrl+shift+tab','alt+arrowleft','alt+arrowright','ctrl+bracketleft','ctrl+bracketright','ctrl+shift+bracketleft','ctrl+shift+bracketright','f11','ctrl+1','ctrl+2','ctrl+3','ctrl+4','ctrl+5','ctrl+6','ctrl+7','ctrl+8','ctrl+9']);
  addEventListener('keydown', e => {
    const k = /^Bracket/.test(e.code) ? e.code.toLowerCase() : e.key.toLowerCase();
    const c = (e.ctrlKey ? 'ctrl+' : '') + (e.altKey ? 'alt+' : '') + (e.shiftKey ? 'shift+' : '') + k;
    if (!window.__noFwd && FWD.has(c)) { e.preventDefault(); post({ a: 'key', k: c }); }
  });
  window.icon = {
    search: '<svg class="ico" viewBox="0 0 16 16"><circle cx="7" cy="7" r="4.6"/><path d="m10.6 10.6 3 3"/></svg>',
    mic: '<svg class="ico" viewBox="0 0 16 16"><rect x="5.6" y="1.4" width="4.8" height="8.4" rx="2.4"/><path d="M3 7.4a5 5 0 0 0 10 0M8 12.4V15"/></svg>',
    globe: '<svg class="ico" viewBox="0 0 16 16"><circle cx="8" cy="8" r="6"/><path d="M2 8h12M8 2c2 2 2 10 0 12M8 2c-2 2-2 10 0 12"/></svg>',
    star: '<svg class="ico" viewBox="0 0 16 16"><path d="m8 1.8 1.9 3.9 4.3.6-3.1 3 .7 4.3L8 11.6l-3.8 2 .7-4.3-3.1-3 4.3-.6z"/></svg>',
    clock: '<svg class="ico" viewBox="0 0 16 16"><circle cx="8" cy="8" r="6"/><path d="M8 4.6V8l2.4 1.6"/></svg>',
    tab: '<svg class="ico" viewBox="0 0 16 16"><rect x="2" y="3.5" width="12" height="9" rx="2"/><path d="M2 6.5h12"/></svg>',
    x: '<svg class="ico" viewBox="0 0 16 16"><path d="m3.5 3.5 9 9m0-9-9 9"/></svg>',
    lock: '<svg class="ico" viewBox="0 0 16 16"><rect x="3.4" y="7" width="9.2" height="6.4" rx="1.6"/><path d="M5.4 7V5.2a2.6 2.6 0 0 1 5.2 0V7"/></svg>'
  };
})();
