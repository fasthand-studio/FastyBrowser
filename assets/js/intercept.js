// Request interception, page side. Only injected while the user has agent
// control on and an intercept rule or hold is set. It wraps fetch,
// XMLHttpRequest and sendBeacon so a request the page makes can be changed,
// blocked, answered with a fake response, or held until the browser decides.
// Config comes from window.__fastyIcptCfg: {rules:[], hold:{match,method,timeout}|null}.
// Limits: only requests the page's script makes; the browser's own loads
// (navigation, images, CSS) are not touched, and forbidden headers (Cookie,
// Host, Origin, Referer, User-Agent...) cannot be set from page script.
(() => {
  if (window.__fastyIcpt) return;
  window.__fastyIcpt = true;
  const cfg = () => window.__fastyIcptCfg || { rules: [], hold: null };
  const post = (m) => {
    try {
      window.webkit.messageHandlers.officeVeil.postMessage(
        Object.assign({ _ch: 'icpt', frame: location.href }, m));
    } catch (e) {}
  };
  const abs = (u) => { try { return new URL(u, location.href).href; } catch (e) { return String(u); } };
  const test = (match, url) => {
    if (!match) return false;
    if (match.startsWith('re:')) { try { return new RegExp(match.slice(3)).test(url); } catch (e) { return false; } }
    return url.indexOf(match) >= 0;
  };
  const hit = (r, method, url) => test(r.match, url) && (!r.method || r.method.toUpperCase() === method);
  const headerObj = (h) => { const o = {}; try { new Headers(h).forEach((v, k) => { o[k] = v; }); } catch (e) {} return o; };
  const MAX = 65536;
  function bodyText(b) {
    if (b == null) return null;
    if (typeof b === 'string') return b;
    if (b instanceof URLSearchParams) return b.toString();
    if (typeof FormData !== 'undefined' && b instanceof FormData) return null;
    return null; // binary, blobs, forms: passed through untouched
  }
  function setPath(o, path, val) {
    const p = path.split('.'); let c = o;
    for (let i = 0; i < p.length - 1; i++) { if (c[p[i]] == null || typeof c[p[i]] !== 'object') c[p[i]] = {}; c = c[p[i]]; }
    c[p[p.length - 1]] = val;
  }
  // Applies one set of edits (a rule, or what the browser sent back for a held request).
  function edit(req, e) {
    if (e.method) req.method = String(e.method).toUpperCase();
    if (e.url) req.url = abs(e.url);
    if (e.url_replace) req.url = req.url.split(e.url_replace.from).join(e.url_replace.to);
    for (const k of (e.remove_headers || [])) delete req.headers[String(k).toLowerCase()];
    for (const k in (e.set_headers || {})) req.headers[k.toLowerCase()] = String(e.set_headers[k]);
    if (typeof e.body === 'string') req.body = e.body;
    if (e.body_replace && req.body != null) req.body = req.body.split(e.body_replace.from).join(e.body_replace.to);
    if (e.json_set && req.body != null) {
      try { const j = JSON.parse(req.body); for (const p in e.json_set) setPath(j, p, e.json_set[p]); req.body = JSON.stringify(j); } catch (x) {}
    }
    if (e.resp_json_set || e.resp_body_replace || e.resp_status || e.resp_body != null) {
      (req.resp = req.resp || []).push({ json_set: e.resp_json_set, replace: e.resp_body_replace, status: e.resp_status, body: e.resp_body });
    }
    if (e.resp_json_set || e.resp_body_replace || e.resp_status || e.resp_body != null) {
      (req.resp = req.resp || []).push({ json_set: e.resp_json_set, replace: e.resp_body_replace, status: e.resp_status, body: e.resp_body });
    }
    if (e.block) req.block = true;
    if (e.mock) req.mock = e.mock;
    if (e.delay_ms) req.delay = (req.delay || 0) + e.delay_ms;
  }
  let seq = 0;
  const waiting = {};
  window.__fastyIcptDecide = (id, d) => { const w = waiting[id]; if (w) { delete waiting[id]; w(d || {}); } };
  async function plan(kind, method, url, headers, body) {
    const c = cfg();
    const req = { method, url, headers, body };
    for (const r of (c.rules || [])) {
      if (!hit(r, req.method, req.url)) continue;
      edit(req, r);
      post({ ev: 'applied', rule: r.id, method: req.method, url: req.url, block: !!req.block });
    }
    const h = c.hold;
    if (h && hit(h, req.method, req.url) && !req.block && !req.mock) {
      const id = 'h' + Date.now().toString(36) + (++seq);
      const d = await new Promise((res) => {
        waiting[id] = res;
        post({ ev: 'held', id, kind, method: req.method, url: req.url, headers: req.headers,
               body: req.body == null ? null : (req.body.length > MAX ? req.body.slice(0, MAX) : req.body) });
        setTimeout(() => { if (waiting[id]) { delete waiting[id]; res({ timeout: true }); } }, (h.timeout || 60) * 1000);
      });
      if (d.drop) req.block = true;
      else edit(req, d);
    }
    return req;
  }
  const relevant = (method, url) => {
    const c = cfg();
    return (c.rules || []).some((r) => hit(r, method, url)) || (c.hold && hit(c.hold, method, url));
  };
  const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

  const of = window.fetch;
  if (of) {
    window.fetch = async function (input, init) {
      let rq = null;
      try { if (typeof Request !== 'undefined' && input instanceof Request) rq = input; } catch (e) {}
      const url = abs(rq ? rq.url : input);
      const method = String((init && init.method) || (rq && rq.method) || 'GET').toUpperCase();
      if (!relevant(method, url)) return of.apply(this, arguments);
      let body = null;
      if (init && init.body !== undefined) body = bodyText(init.body);
      else if (rq && method !== 'GET' && method !== 'HEAD') { try { body = await rq.clone().text(); } catch (e) {} }
      const req = await plan('fetch', method, url, headerObj((init && init.headers) || (rq && rq.headers) || {}), body);
      if (req.delay) await sleep(req.delay);
      if (req.block) throw new TypeError('Failed to fetch (blocked by Fasty intercept)');
      if (req.mock) {
        return new Response(req.mock.body == null ? '' : String(req.mock.body),
          { status: req.mock.status || 200, headers: req.mock.headers || { 'content-type': 'application/json' } });
      }
      const ni = Object.assign({}, init || {});
      ni.method = req.method;
      ni.headers = req.headers;
      if (req.body != null && req.method !== 'GET' && req.method !== 'HEAD') ni.body = req.body;
      else if (body != null || (init && init.body)) { if (req.method === 'GET' || req.method === 'HEAD') delete ni.body; }
      if (rq) { ni.credentials = ni.credentials || rq.credentials; ni.mode = ni.mode || rq.mode; }
      const res = await of.call(this, req.url, ni);
      if (!req.resp) return res;
      // Rewrite what the server said, to see how the page (and then the server) reacts.
      let text = await res.text();
      let status = res.status;
      for (const e of req.resp) {
        if (e.body != null) text = String(e.body);
        if (e.replace) text = text.split(e.replace.from).join(e.replace.to);
        if (e.json_set) { try { const j = JSON.parse(text); for (const p in e.json_set) setPath(j, p, e.json_set[p]); text = JSON.stringify(j); } catch (x) {} }
        if (e.status) status = e.status;
      }
      const h = new Headers(res.headers); h.delete('content-length'); h.delete('content-encoding');
      post({ ev: 'applied', rule: 'response-edit', method: req.method, url: req.url });
      return new Response(text, { status, statusText: res.statusText, headers: h });
    };
  }

  const xo = XMLHttpRequest.prototype.open, xs = XMLHttpRequest.prototype.send, xh = XMLHttpRequest.prototype.setRequestHeader;
  XMLHttpRequest.prototype.open = function (m, u, a, user, pw) {
    this.__i = { method: String(m).toUpperCase(), url: abs(u), async: a !== false, user, pw, headers: {} };
    return xo.apply(this, arguments);
  };
  XMLHttpRequest.prototype.setRequestHeader = function (k, v) {
    if (this.__i) this.__i.headers[String(k).toLowerCase()] = v;
    return xh.apply(this, arguments);
  };
  XMLHttpRequest.prototype.send = function (body) {
    const i = this.__i, self = this, args = arguments;
    if (!i || !i.async || !relevant(i.method, i.url)) return xs.apply(this, args);
    plan('xhr', i.method, i.url, i.headers, bodyText(body)).then(async (req) => {
      if (req.delay) await sleep(req.delay);
      if (req.block) {
        try { xo.call(self, 'GET', 'about:blank'); } catch (e) {}
        self.dispatchEvent(new ProgressEvent('error'));
        self.dispatchEvent(new ProgressEvent('loadend'));
        return;
      }
      const sendBody = (req.body != null && req.method !== 'GET' && req.method !== 'HEAD') ? req.body : (bodyText(body) == null ? body : null);
      xo.call(self, req.method, req.url, true, i.user, i.pw);
      for (const k in req.headers) { try { xh.call(self, k, req.headers[k]); } catch (e) {} }
      xs.call(self, sendBody);
    });
  };

  const sb = navigator.sendBeacon;
  if (sb) {
    navigator.sendBeacon = function (u, d) {
      const url = abs(u);
      const c = cfg();
      const r = (c.rules || []).find((x) => hit(x, 'POST', url) && x.block);
      if (r) { post({ ev: 'applied', rule: r.id, method: 'POST', url, block: true }); return false; }
      return sb.apply(this, arguments);
    };
  }
})();
