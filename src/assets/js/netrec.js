// Network recording, page side. Only injected while the user has recording
// switched on. The browser itself sees every request and response header
// (record.go); this adds what only the page knows: the bodies of fetch(),
// XMLHttpRequest, sendBeacon and form submissions, which WebKit does not hand
// to the browser. Everything is reported to the browser, never sent anywhere.
(() => {
  if (window.__fastyNet) return;
  window.__fastyNet = true;
  const on = () => window.__fastyRecOn !== false;
  const post = (m) => {
    try {
      window.webkit.messageHandlers.officeVeil.postMessage(
        Object.assign({ _ch: 'netrec', t: Date.now(), frame: location.href }, m));
    } catch (e) {}
  };
  const MAX = 16384;
  const cap = (s) => (typeof s === 'string' && s.length > MAX) ? s.slice(0, MAX) + '…[truncated, ' + s.length + ' chars]' : s;
  const abs = (u) => { try { return new URL(u, location.href).href; } catch (e) { return String(u); } };
  const textual = (ct) => /json|text|xml|x-www-form-urlencoded|graphql|javascript/i.test(ct || '') && !/css/i.test(ct || '');
  function bodyText(b) {
    if (b == null) return '';
    if (typeof b === 'string') return b;
    if (b instanceof URLSearchParams) return b.toString();
    if (typeof FormData !== 'undefined' && b instanceof FormData) {
      const o = [];
      for (const [k, v] of b) o.push(k + '=' + (typeof v === 'string' ? v : '[file ' + v.name + ' ' + v.size + 'B]'));
      return o.join('&');
    }
    if (typeof Blob !== 'undefined' && b instanceof Blob) return '[blob ' + b.size + 'B ' + b.type + ']';
    if (b instanceof ArrayBuffer || ArrayBuffer.isView(b)) return '[binary ' + b.byteLength + 'B]';
    return String(b);
  }
  function headerObj(h) {
    const o = {};
    try { new Headers(h).forEach((v, k) => { o[k] = v; }); } catch (e) {}
    return o;
  }

  const of = window.fetch;
  if (of) {
    window.fetch = function (input, init) {
      if (!on()) return of.apply(this, arguments);
      let rec = { kind: 'fetch', method: 'GET', url: '' };
      try {
        const rq = (typeof Request !== 'undefined' && input instanceof Request) ? input : null;
        rec.url = abs(rq ? rq.url : input);
        rec.method = String((init && init.method) || (rq && rq.method) || 'GET').toUpperCase();
        rec.reqHeaders = headerObj((init && init.headers) || (rq && rq.headers) || {});
        rec.credentials = (init && init.credentials) || (rq && rq.credentials) || 'same-origin';
        rec.reqBody = cap(bodyText(init && init.body));
      } catch (e) {}
      return of.apply(this, arguments).then((r) => {
        try {
          const ct = r.headers.get('content-type') || '';
          if (textual(ct)) {
            r.clone().text().then((t) => post(Object.assign({}, rec, { status: r.status, type: r.type, respBody: cap(t) })), () => {});
          } else {
            post(Object.assign({}, rec, { status: r.status, type: r.type, respBody: '[' + (ct || 'no content type') + ']' }));
          }
        } catch (e) {}
        return r;
      }, (e) => {
        post(Object.assign({}, rec, { error: String(e) }));
        throw e;
      });
    };
  }

  const xo = XMLHttpRequest.prototype.open, xs = XMLHttpRequest.prototype.send, xh = XMLHttpRequest.prototype.setRequestHeader;
  XMLHttpRequest.prototype.open = function (m, u) {
    this.__f = { kind: 'xhr', method: String(m).toUpperCase(), url: abs(u), reqHeaders: {} };
    return xo.apply(this, arguments);
  };
  XMLHttpRequest.prototype.setRequestHeader = function (k, v) {
    if (this.__f) this.__f.reqHeaders[k] = v;
    return xh.apply(this, arguments);
  };
  XMLHttpRequest.prototype.send = function (body) {
    const f = this.__f;
    if (f && on()) {
      f.reqBody = cap(bodyText(body));
      this.addEventListener('loadend', () => {
        let rb = '';
        try {
          const ct = this.getResponseHeader('content-type') || '';
          rb = (this.responseType === '' || this.responseType === 'text')
            ? (textual(ct) ? cap(this.responseText) : '[' + (ct || 'no content type') + ']')
            : '[' + this.responseType + ']';
        } catch (e) {}
        post(Object.assign({}, f, { status: this.status, respBody: rb, error: this.status === 0 ? 'failed or blocked' : undefined }));
      });
    }
    return xs.apply(this, arguments);
  };

  const sb = navigator.sendBeacon;
  if (sb) {
    navigator.sendBeacon = function (u, d) {
      if (on()) post({ kind: 'beacon', method: 'POST', url: abs(u), reqBody: cap(bodyText(d)) });
      return sb.apply(this, arguments);
    };
  }

  document.addEventListener('submit', (e) => {
    if (!on()) return;
    try {
      const f = e.target;
      post({ kind: 'form', method: String(f.method || 'GET').toUpperCase(), url: abs(f.action || location.href),
             reqBody: cap(bodyText(new FormData(f))) });
    } catch (err) {}
  }, true);
})();
