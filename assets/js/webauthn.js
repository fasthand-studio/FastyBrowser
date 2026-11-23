// The standard WebAuthn API (navigator.credentials + PublicKeyCredential),
// backed by the browser's built-in passkeys (webauthn.go). WebKitGTK has none
// of its own. Only the top-level page gets it, and the browser, not this
// script, decides the page's origin.
(() => {
  'use strict';
  if (window.PublicKeyCredential || !window.isSecureContext) return;
  if (window.top !== window) return;

  const post = (m) => window.webkit.messageHandlers.officeVeil.postMessage(m);
  const pending = new Map();
  let seq = 0;

  const toBytes = (x) => {
    if (x instanceof ArrayBuffer) return new Uint8Array(x);
    if (ArrayBuffer.isView(x)) return new Uint8Array(x.buffer, x.byteOffset, x.byteLength);
    throw new TypeError('Expected a BufferSource');
  };
  const enc = (x) => {
    const b = toBytes(x);
    let s = '';
    for (let i = 0; i < b.length; i++) s += String.fromCharCode(b[i]);
    return btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
  };
  const dec = (s) => {
    s = String(s).replace(/-/g, '+').replace(/_/g, '/');
    while (s.length % 4) s += '=';
    const bin = atob(s);
    const u = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) u[i] = bin.charCodeAt(i);
    return u.buffer;
  };

  function call(op, payload, signal) {
    return new Promise((resolve, reject) => {
      if (signal && signal.aborted) return reject(new DOMException('The operation was aborted.', 'AbortError'));
      const id = ++seq;
      pending.set(id, { resolve, reject });
      if (signal) {
        signal.addEventListener('abort', () => {
          if (pending.delete(id)) {
            post({ _ch: 'webauthn', op: 'cancel', id });
            reject(new DOMException('The operation was aborted.', 'AbortError'));
          }
        }, { once: true });
      }
      post(Object.assign({ _ch: 'webauthn', op, id }, payload));
    });
  }

  // The browser calls this with each answer.
  Object.defineProperty(window, '__fastyWA', {
    value: Object.freeze({
      done(id, out) {
        const p = pending.get(id);
        if (!p) return;
        pending.delete(id);
        if (out.ok) p.resolve(out.result);
        else p.reject(new DOMException(out.error.message, out.error.name));
      }
    })
  });

  class AuthenticatorResponse {
    constructor(clientDataJSON) { Object.defineProperty(this, 'clientDataJSON', { value: dec(clientDataJSON), enumerable: true }); }
  }
  class AuthenticatorAttestationResponse extends AuthenticatorResponse {
    #r;
    constructor(r) {
      super(r.clientDataJSON);
      this.#r = r;
      Object.defineProperty(this, 'attestationObject', { value: dec(r.attestationObject), enumerable: true });
    }
    getTransports() { return ['internal']; }
    getAuthenticatorData() { return dec(this.#r.authenticatorData); }
    getPublicKey() { return dec(this.#r.publicKey); }
    getPublicKeyAlgorithm() { return this.#r.publicKeyAlg; }
  }
  class AuthenticatorAssertionResponse extends AuthenticatorResponse {
    constructor(r) {
      super(r.clientDataJSON);
      Object.defineProperty(this, 'authenticatorData', { value: dec(r.authenticatorData), enumerable: true });
      Object.defineProperty(this, 'signature', { value: dec(r.signature), enumerable: true });
      Object.defineProperty(this, 'userHandle', { value: r.userHandle ? dec(r.userHandle) : null, enumerable: true });
    }
  }

  class Credential {
    get id() { return this._id; }
    get type() { return this._type; }
    static isConditionalMediationAvailable() { return Promise.resolve(false); }
  }
  class PublicKeyCredential extends Credential {
    constructor(id, response, attachment) {
      super();
      Object.defineProperty(this, '_id', { value: id });
      Object.defineProperty(this, '_type', { value: 'public-key' });
      Object.defineProperty(this, 'rawId', { value: dec(id), enumerable: true });
      Object.defineProperty(this, 'response', { value: response, enumerable: true });
      Object.defineProperty(this, 'authenticatorAttachment', { value: attachment, enumerable: true });
    }
    getClientExtensionResults() { return {}; }
    toJSON() {
      const r = this.response, out = { id: this.id, rawId: this.id, type: 'public-key',
        authenticatorAttachment: this.authenticatorAttachment, clientExtensionResults: {} };
      if (r instanceof AuthenticatorAttestationResponse) {
        out.response = { clientDataJSON: enc(r.clientDataJSON), attestationObject: enc(r.attestationObject),
          transports: r.getTransports(), authenticatorData: enc(r.getAuthenticatorData()),
          publicKey: enc(r.getPublicKey()), publicKeyAlgorithm: r.getPublicKeyAlgorithm() };
      } else {
        out.response = { clientDataJSON: enc(r.clientDataJSON), authenticatorData: enc(r.authenticatorData),
          signature: enc(r.signature), userHandle: r.userHandle ? enc(r.userHandle) : undefined };
      }
      return out;
    }
    static isUserVerifyingPlatformAuthenticatorAvailable() { return Promise.resolve(true); }
    static isConditionalMediationAvailable() { return Promise.resolve(false); }
    static getClientCapabilities() {
      return Promise.resolve({ userVerifyingPlatformAuthenticator: true, conditionalCreate: false,
        conditionalGet: false, hybridTransport: false, passkeyPlatformAuthenticator: true, relatedOrigins: false });
    }
    static parseCreationOptionsFromJSON(j) {
      return Object.assign({}, j, { challenge: dec(j.challenge), user: Object.assign({}, j.user, { id: dec(j.user.id) }),
        excludeCredentials: (j.excludeCredentials || []).map((c) => Object.assign({}, c, { id: dec(c.id) })) });
    }
    static parseRequestOptionsFromJSON(j) {
      return Object.assign({}, j, { challenge: dec(j.challenge),
        allowCredentials: (j.allowCredentials || []).map((c) => Object.assign({}, c, { id: dec(c.id) })) });
    }
  }

  const credentials = {
    async create(options) {
      const pk = options && options.publicKey;
      if (!pk) return null;
      const r = await call('create', {
        rpId: (pk.rp && pk.rp.id) || location.hostname,
        rpName: pk.rp && pk.rp.name,
        challenge: enc(pk.challenge),
        user: { id: enc(pk.user.id), name: pk.user.name, displayName: pk.user.displayName },
        algs: (pk.pubKeyCredParams || []).map((p) => p.alg),
        exclude: (pk.excludeCredentials || []).map((c) => enc(c.id)),
        userVerification: pk.authenticatorSelection && pk.authenticatorSelection.userVerification
      }, options.signal);
      return new PublicKeyCredential(r.id, new AuthenticatorAttestationResponse(r), 'platform');
    },
    async get(options) {
      const pk = options && options.publicKey;
      if (!pk) return null; // passwords and federated credentials aren't offered
      if (options.mediation === 'conditional') {
        return new Promise(() => {}); // no autofill UI: never answers, as when nothing is chosen
      }
      const r = await call('get', {
        rpId: pk.rpId || location.hostname,
        challenge: enc(pk.challenge),
        allow: (pk.allowCredentials || []).map((c) => enc(c.id)),
        userVerification: pk.userVerification
      }, options.signal);
      return new PublicKeyCredential(r.id, new AuthenticatorAssertionResponse(r), 'platform');
    },
    store(c) { return Promise.resolve(c); },
    preventSilentAccess() { return Promise.resolve(); }
  };

  Object.defineProperty(window, 'PublicKeyCredential', { value: PublicKeyCredential, configurable: true, writable: true });
  Object.defineProperty(window, 'Credential', { value: Credential, configurable: true, writable: true });
  Object.defineProperty(window, 'AuthenticatorResponse', { value: AuthenticatorResponse, configurable: true, writable: true });
  Object.defineProperty(window, 'AuthenticatorAttestationResponse', { value: AuthenticatorAttestationResponse, configurable: true, writable: true });
  Object.defineProperty(window, 'AuthenticatorAssertionResponse', { value: AuthenticatorAssertionResponse, configurable: true, writable: true });
  if (!navigator.credentials) {
    Object.defineProperty(Navigator.prototype, 'credentials', { get() { return credentials; }, configurable: true });
  }
})();
