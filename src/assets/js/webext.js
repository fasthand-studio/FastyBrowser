// WebExtensions API for Fasty Browser (chrome.* and browser.*).
// Runs in three kinds of context, told apart by CFG.ctx:
//   content    a content script, in the extension's own isolated world
//   background the hidden background page
//   page       a popup or options page opened from the extension
// Everything that needs the browser goes to Go through call(); Go answers
// through __fastyReply and pushes events through __fastyDeliver.

(function () {
  'use strict';
  var CFG = window.__fastyCfg;
  if (!CFG || window.__fastyExtAPI) return;
  window.__fastyExtAPI = true;

  var CTX = CFG.ctx;
  var EXT_ID = CFG.id;
  var MANIFEST = CFG.manifest || {};
  var MESSAGES = CFG.messages || {};

  // --- plumbing ---------------------------------------------------------------
  var seq = 0;
  var pending = {};

  function post(msg) {
    if (window.__fastyDead) return;
    msg._ch = 'ext';
    msg.extId = EXT_ID;
    msg.ctx = CTX;
    msg.secret = CFG.secret;
    var mh = window.webkit && window.webkit.messageHandlers;
    var h = mh && (CTX === 'background' ? mh.fastyExtBg : (mh.fastyExt || mh.officeVeil));
    if (h) h.postMessage(msg);
  }

  function call(type, payload, cb) {
    var id = ++seq;
    var promise = new Promise(function (resolve, reject) {
      pending[id] = function (data, err) {
        if (cb) {
          runtime.lastError = err ? { message: err } : null;
          try { cb(data); } finally { runtime.lastError = null; }
          resolve(data);
        } else if (err) {
          reject(new Error(err));
        } else {
          resolve(data);
        }
      };
    });
    post({ type: type, payload: payload === undefined ? null : payload, cbId: id });
    return cb ? undefined : promise;
  }

  window.__fastyReply = function (id, data, err) {
    var fn = pending[id];
    if (!fn) return;
    delete pending[id];
    fn(data === null ? undefined : data, err || '');
  };

  function ev() {
    var ls = [];
    return {
      addListener: function (fn) { if (typeof fn === 'function' && ls.indexOf(fn) < 0) ls.push(fn); },
      removeListener: function (fn) { var i = ls.indexOf(fn); if (i >= 0) ls.splice(i, 1); },
      hasListener: function (fn) { return ls.indexOf(fn) >= 0; },
      hasListeners: function () { return ls.length > 0; },
      _ls: ls,
      _fire: function (args) {
        ls.slice().forEach(function (fn) { try { fn.apply(null, args); } catch (e) { console.error(e); } });
      }
    };
  }

  function last(args) {
    var f = args[args.length - 1];
    return typeof f === 'function' ? f : undefined;
  }

  var EV = {};
  function named(name) { return EV[name] || (EV[name] = ev()); }

  // --- ports ------------------------------------------------------------------
  var ports = {};
  function makePort(id, name, sender) {
    var p = {
      name: name || '',
      sender: sender,
      onMessage: ev(),
      onDisconnect: ev(),
      postMessage: function (m) { post({ type: 'port.msg', payload: { portId: id, msg: m } }); },
      disconnect: function () {
        if (!ports[id]) return;
        delete ports[id];
        post({ type: 'port.close', payload: { portId: id } });
      }
    };
    ports[id] = p;
    return p;
  }
  function openPort(args, tabId) {
    var info = null;
    for (var i = 0; i < args.length; i++) if (args[i] && typeof args[i] === 'object') info = args[i];
    var id = EXT_ID.slice(0, 6) + '-' + Date.now().toString(36) + Math.random().toString(36).slice(2, 7);
    var p = makePort(id, info && info.name);
    post({ type: 'port.open', payload: { portId: id, name: p.name, tabId: tabId } });
    return p;
  }

  // --- messages from Go -------------------------------------------------------
  window.__fastyDeliver = function (kind, d) {
    switch (kind) {
      case 'message': {
        var done = false, isAsync = false;
        var send = function (resp, err) {
          if (done) return;
          done = true;
          post({ type: 'runtime.reply', payload: { reqId: d.reqId, data: resp === undefined ? null : resp, error: err || '' } });
        };
        var fail = function (e) { send(undefined, String((e && e.message) || e || 'Error')); };
        (d.userScript ? runtime.onUserScriptMessage : (d.external ? runtime.onMessageExternal : runtime.onMessage))._ls.slice().forEach(function (fn) {
          try {
            var r = fn(d.message, d.sender, send);
            if (r === true) isAsync = true;
            else if (r && typeof r.then === 'function') { isAsync = true; r.then(function (v) { send(v); }, fail); }
          } catch (e) { fail(e); }
        });
        if (!isAsync && !done) send(undefined);
        break;
      }
      case 'portOpen': {
        var p = makePort(d.portId, d.name, d.sender);
        runtime.onConnect._fire([p]);
        break;
      }
      case 'portMsg': {
        var pm = ports[d.portId];
        if (pm) pm.onMessage._fire([d.msg, pm]);
        break;
      }
      case 'portClose': {
        var pc = ports[d.portId];
        if (pc) { delete ports[d.portId]; pc.onDisconnect._fire([pc]); }
        break;
      }
      case 'storageChanged':
        named('storage.onChanged')._fire([d.changes, d.area]);
        if (storage[d.area] && storage[d.area].onChanged) storage[d.area].onChanged._fire([d.changes]);
        break;
      case 'event':
        named(d.name)._fire(d.args || []);
        break;
    }
  };

  // --- runtime ----------------------------------------------------------------
  var runtime = {
    id: CFG.rid || EXT_ID,
    lastError: null,
    getManifest: function () { return JSON.parse(JSON.stringify(MANIFEST)); },
    getURL: function (path) {
      return CFG.base + String(path || '').replace(/^\/+/, '');
    },
    sendMessage: function () {
      var a = Array.prototype.slice.call(arguments);
      var cb = last(a);
      if (cb) a.pop();
      var target = '';
      if (typeof a[0] === 'string' && a.length > 1 && a[0] !== EXT_ID && a[0] !== runtime.id) target = a[0];
      var message = (typeof a[0] === 'string' && a.length > 1) ? a[1] : a[0];
      return call('runtime.sendMessage', { message: message, target: target }, cb);
    },
    connect: function () { return openPort(arguments); },
    openOptionsPage: function (cb) { return call('runtime.openOptionsPage', null, cb); },
    setUninstallURL: function (u, cb) { if (cb) cb(); return Promise.resolve(); },
    reload: function () {},
    requestUpdateCheck: function (cb) { var r = { status: 'no_update' }; if (cb) cb(r); return Promise.resolve(r); },
    getPlatformInfo: function (cb) {
      var r = { os: 'linux', arch: 'x86-64', nacl_arch: 'x86-64' };
      if (cb) cb(r);
      return Promise.resolve(r);
    },
    getBrowserInfo: function () { return Promise.resolve({ name: 'Fasty', vendor: 'Fasty', version: '1.0', buildID: '1' }); },
    getBackgroundPage: function (cb) { if (cb) cb(CTX === 'background' ? window : null); return Promise.resolve(CTX === 'background' ? window : null); },
    onMessage: named('runtime.onMessage'),
    onMessageExternal: named('runtime.onMessageExternal'),
    onConnect: named('runtime.onConnect'),
    onConnectExternal: named('runtime.onConnectExternal'),
    onUserScriptMessage: named('runtime.onUserScriptMessage'),
    onUserScriptConnect: named('runtime.onUserScriptConnect'),
    OnInstalledReason: { INSTALL: 'install', UPDATE: 'update', CHROME_UPDATE: 'chrome_update', SHARED_MODULE_UPDATE: 'shared_module_update' },
    OnRestartRequiredReason: { APP_UPDATE: 'app_update', OS_UPDATE: 'os_update', PERIODIC: 'periodic' },
    PlatformOs: { MAC: 'mac', WIN: 'win', ANDROID: 'android', CROS: 'cros', LINUX: 'linux', OPENBSD: 'openbsd' },
    PlatformArch: { ARM: 'arm', ARM64: 'arm64', X86_32: 'x86-32', X86_64: 'x86-64' },
    ContextType: { TAB: 'TAB', POPUP: 'POPUP', BACKGROUND: 'BACKGROUND', OFFSCREEN_DOCUMENT: 'OFFSCREEN_DOCUMENT', SIDE_PANEL: 'SIDE_PANEL' },
    onInstalled: named('runtime.onInstalled'),
    onStartup: named('runtime.onStartup'),
    onSuspend: named('runtime.onSuspend'),
    onSuspendCanceled: named('runtime.onSuspendCanceled'),
    onUpdateAvailable: named('runtime.onUpdateAvailable')
  };

  // --- storage ----------------------------------------------------------------
  function mkArea(name) {
    return {
      get: function (keys, cb) {
        if (typeof keys === 'function') { cb = keys; keys = null; }
        return call('storage.get', { area: name, keys: keys === undefined ? null : keys }, cb);
      },
      set: function (items, cb) { return call('storage.set', { area: name, items: items }, cb); },
      remove: function (keys, cb) { return call('storage.remove', { area: name, keys: [].concat(keys) }, cb); },
      clear: function (cb) { return call('storage.clear', { area: name }, cb); },
      getBytesInUse: function (keys, cb) {
        if (typeof keys === 'function') { cb = keys; keys = null; }
        return call('storage.bytes', { area: name, keys: keys }, cb);
      },
      setAccessLevel: function () { return Promise.resolve(); },
      onChanged: ev()
    };
  }
  var storage = {
    local: mkArea('local'),
    sync: mkArea('sync'),
    session: mkArea('session'),
    managed: mkArea('managed'),
    onChanged: named('storage.onChanged')
  };

  // --- tabs / windows ---------------------------------------------------------
  var tabs = {
    TAB_ID_NONE: -1,
    query: function (q, cb) { return call('tabs.query', q || {}, cb); },
    get: function (id, cb) { return call('tabs.get', { tabId: id }, cb); },
    getCurrent: function (cb) { return call('tabs.getCurrent', null, cb); },
    create: function (props, cb) { return call('tabs.create', props || {}, cb); },
    update: function () {
      var a = Array.prototype.slice.call(arguments);
      var cb = last(a);
      var id = typeof a[0] === 'number' ? a[0] : undefined;
      var props = typeof a[0] === 'number' ? a[1] : a[0];
      return call('tabs.update', { tabId: id, props: props || {} }, cb);
    },
    remove: function (ids, cb) { return call('tabs.remove', { tabIds: [].concat(ids) }, cb); },
    reload: function (id, a, b) {
      var cb = last([id, a, b]);
      return call('tabs.reload', { tabId: typeof id === 'number' ? id : undefined }, cb);
    },
    sendMessage: function () {
      var a = Array.prototype.slice.call(arguments);
      var cb = last(a);
      if (cb) a.pop();
      return call('tabs.sendMessage', { tabId: a[0], message: a[1] }, cb);
    },
    connect: function (tabId) { return openPort(Array.prototype.slice.call(arguments, 1), tabId); },
    executeScript: function () {
      var a = Array.prototype.slice.call(arguments);
      var cb = last(a);
      if (cb) a.pop();
      var tabId = typeof a[0] === 'number' ? a[0] : undefined;
      var details = typeof a[0] === 'number' ? a[1] : a[0];
      return call('scripting.execute', { tabId: tabId, code: details && details.code, files: details && details.file ? [details.file] : null, legacy: true }, cb);
    },
    insertCSS: function () {
      var a = Array.prototype.slice.call(arguments);
      var cb = last(a);
      if (cb) a.pop();
      var tabId = typeof a[0] === 'number' ? a[0] : undefined;
      var details = typeof a[0] === 'number' ? a[1] : a[0];
      return call('scripting.insertCSS', { tabId: tabId, css: details && details.code, files: details && details.file ? [details.file] : null }, cb);
    },
    detectLanguage: function (id, cb) { if (typeof id === 'function') cb = id; if (cb) cb('en'); return Promise.resolve('en'); },
    captureVisibleTab: function () { return Promise.reject(new Error('captureVisibleTab is not supported')); },
    getZoom: function (id, cb) { if (typeof id === 'function') cb = id; if (cb) cb(1); return Promise.resolve(1); },
    onCreated: named('tabs.onCreated'),
    onUpdated: named('tabs.onUpdated'),
    onActivated: named('tabs.onActivated'),
    onRemoved: named('tabs.onRemoved'),
    onReplaced: named('tabs.onReplaced'),
    onMoved: named('tabs.onMoved')
  };

  var windows = {
    WINDOW_ID_NONE: -1,
    WINDOW_ID_CURRENT: -2,
    getCurrent: function (a, cb) { cb = last([a, cb]); return call('windows.get', { id: -2, populate: !!(a && typeof a === 'object' && a.populate) }, cb); },
    getLastFocused: function (a, cb) { cb = last([a, cb]); return call('windows.get', { id: -2, populate: !!(a && typeof a === 'object' && a.populate) }, cb); },
    getAll: function (a, cb) { cb = last([a, cb]); return call('windows.getAll', { populate: !!(a && typeof a === 'object' && a.populate) }, cb); },
    get: function (id, a, cb) { cb = last([id, a, cb]); return call('windows.get', { id: id, populate: !!(a && typeof a === 'object' && a.populate) }, cb); },
    create: function (p, cb) { return call('windows.create', p || {}, cb); },
    update: function (id, p, cb) { cb = last([id, p, cb]); return call('windows.update', { id: id, focused: p && p.focused }, cb); },
    remove: function (id, cb) { return call('windows.remove', { id: id }, cb); },
    onCreated: named('windows.onCreated'),
    onRemoved: named('windows.onRemoved'),
    onFocusChanged: named('windows.onFocusChanged'),
    onBoundsChanged: named('windows.onBoundsChanged')
  };

  var scripting = {
    executeScript: function (inj, cb) {
      var payload = { tabId: inj.target && inj.target.tabId, files: inj.files || null, world: inj.world || '' };
      if (inj.func) {
        payload.code = '(' + inj.func.toString() + ').apply(null,' + JSON.stringify(inj.args || []) + ')';
      }
      return call('scripting.execute', payload, cb);
    },
    insertCSS: function (inj, cb) {
      return call('scripting.insertCSS', { tabId: inj.target && inj.target.tabId, css: inj.css, files: inj.files || null }, cb);
    },
    removeCSS: function (inj, cb) { if (cb) cb(); return Promise.resolve(); },
    registerContentScripts: function (s, cb) { return call('scripting.register', { scripts: s }, cb); },
    updateContentScripts: function (s, cb) { return call('scripting.update', { scripts: s }, cb); },
    unregisterContentScripts: function (f, cb) {
      if (typeof f === 'function') { cb = f; f = null; }
      return call('scripting.unregister', { ids: (f && f.ids) || [] }, cb);
    },
    getRegisteredContentScripts: function (f, cb) {
      if (typeof f === 'function') { cb = f; f = null; }
      return call('scripting.getRegistered', { ids: (f && f.ids) || [] }, cb);
    }
  };

  // --- i18n -------------------------------------------------------------------
  var i18n = {
    getMessage: function (key, subs) {
      if (key === '@@extension_id') return EXT_ID;
      if (key === '@@ui_locale') return (CFG.locale || 'en').replace('-', '_');
      if (key === '@@bidi_dir') return 'ltr';
      var m = MESSAGES[String(key).toLowerCase()];
      if (!m) return '';
      var text = m.message || '';
      var list = subs === undefined ? [] : [].concat(subs);
      var ph = m.placeholders || {};
      text = text.replace(/\$([a-zA-Z0-9_@]+)\$/g, function (all, name) {
        var p = ph[name] || ph[name.toLowerCase()];
        if (!p) return all;
        return String(p.content).replace(/\$(\d)/g, function (x, n) { return list[+n - 1] === undefined ? '' : list[+n - 1]; });
      });
      return text.replace(/\$(\d)/g, function (x, n) { return list[+n - 1] === undefined ? '' : list[+n - 1]; });
    },
    getUILanguage: function () { return CFG.locale || 'en'; },
    getAcceptLanguages: function (cb) { var l = [CFG.locale || 'en']; if (l[0] !== 'en' && l[0].indexOf('en') !== 0) l.push('en'); if (cb) cb(l); return Promise.resolve(l); },
    detectLanguage: function (t, cb) {
      var r = { isReliable: false, languages: [{ language: 'en', percentage: 100 }] };
      if (cb) cb(r);
      return Promise.resolve(r);
    }
  };

  // --- alarms (timers live in the background page) ----------------------------
  var alarmTimers = {};
  var alarmInfo = {};
  function fireAlarm(name) {
    var a = alarmInfo[name];
    if (!a) return;
    named('alarms.onAlarm')._fire([{ name: name, scheduledTime: a.scheduledTime, periodInMinutes: a.periodInMinutes }]);
  }
  function clearAlarm(name) {
    if (!alarmInfo[name]) return false;
    clearTimeout(alarmTimers[name]);
    clearInterval(alarmTimers[name]);
    delete alarmTimers[name];
    delete alarmInfo[name];
    return true;
  }
  var alarms = {
    create: function (name, info) {
      if (typeof name === 'object') { info = name; name = ''; }
      info = info || {};
      clearAlarm(name);
      var delay = info.when ? Math.max(0, info.when - Date.now())
        : (info.delayInMinutes !== undefined ? info.delayInMinutes : (info.periodInMinutes || 0)) * 60000;
      var period = info.periodInMinutes ? info.periodInMinutes * 60000 : 0;
      alarmInfo[name] = { name: name, scheduledTime: Date.now() + delay, periodInMinutes: info.periodInMinutes };
      alarmTimers[name] = setTimeout(function () {
        fireAlarm(name);
        if (period) {
          alarmInfo[name].scheduledTime = Date.now() + period;
          alarmTimers[name] = setInterval(function () { alarmInfo[name].scheduledTime = Date.now() + period; fireAlarm(name); }, period);
        } else {
          delete alarmInfo[name];
          delete alarmTimers[name];
        }
      }, delay);
    },
    get: function (name, cb) {
      if (typeof name === 'function') { cb = name; name = ''; }
      var r = alarmInfo[name || ''] || undefined;
      if (cb) cb(r);
      return Promise.resolve(r);
    },
    getAll: function (cb) { var r = Object.keys(alarmInfo).map(function (k) { return alarmInfo[k]; }); if (cb) cb(r); return Promise.resolve(r); },
    clear: function (name, cb) {
      if (typeof name === 'function') { cb = name; name = ''; }
      var r = clearAlarm(name || '');
      if (cb) cb(r);
      return Promise.resolve(r);
    },
    clearAll: function (cb) { Object.keys(alarmInfo).forEach(clearAlarm); if (cb) cb(true); return Promise.resolve(true); },
    onAlarm: named('alarms.onAlarm')
  };

  // --- action / browserAction -------------------------------------------------
  function mkAction() {
    function set(key) {
      return function (d, cb) {
        var v = d && (d[key] !== undefined ? d[key] : d.path);
        return call('action.set', { key: key, value: v === undefined ? null : v }, cb);
      };
    }
    return {
      setIcon: set('icon'),
      setTitle: set('title'),
      setBadgeText: set('text'),
      setBadgeBackgroundColor: set('color'),
      setBadgeTextColor: set('textColor'),
      setPopup: set('popup'),
      getPopup: function (d, cb) { return call('action.get', { key: 'popup' }, last([d, cb])); },
      getTitle: function (d, cb) { return call('action.get', { key: 'title' }, last([d, cb])); },
      getBadgeText: function (d, cb) { return call('action.get', { key: 'text' }, last([d, cb])); },
      enable: function (id, cb) { if (typeof id === 'function') cb = id; if (cb) cb(); return Promise.resolve(); },
      disable: function (id, cb) { if (typeof id === 'function') cb = id; if (cb) cb(); return Promise.resolve(); },
      openPopup: function (o, cb) { return call('action.openPopup', null, last([o, cb])); },
      getUserSettings: function (cb) { var r = { isOnToolbar: true }; if (cb) cb(r); return Promise.resolve(r); },
      onClicked: named('action.onClicked')
    };
  }
  var action = mkAction();
  var browserAction = mkAction();
  browserAction.onClicked = action.onClicked;
  var pageAction = mkAction();
  pageAction.show = function (id, cb) { if (typeof id === 'function') cb = id; if (cb) cb(); return Promise.resolve(); };
  pageAction.hide = pageAction.show;

  // --- context menus ----------------------------------------------------------
  var menuSeq = 0;
  var contextMenus = {
    create: function (p, cb) {
      p = Object.assign({}, p);
      if (p.id === undefined) p.id = 'fasty-menu-' + (++menuSeq);
      call('menus.create', p, cb);
      return p.id;
    },
    update: function (id, p, cb) { return call('menus.update', { id: id, props: p }, cb); },
    remove: function (id, cb) { return call('menus.remove', { id: id }, cb); },
    removeAll: function (cb) { return call('menus.removeAll', null, cb); },
    onClicked: named('contextMenus.onClicked'),
    onShown: named('contextMenus.onShown'),
    ACTION_MENU_TOP_LEVEL_LIMIT: 6
  };
  named('menus.onClicked');

  var noop = ev();

  // webRequest events fire for page navigations; WebKit gives no per-request
  // hook, so listeners can observe main-frame loads but not block requests.
  function patternRe(p) {
    if (p === '<all_urls>') return /^(https?|wss?|ftp|file):/;
    var m = /^(\*|https?|wss?|ftp|file):\/\/([^/]*)(\/.*)$/.exec(p);
    if (!m) return null;
    var esc = function (t) { return t.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'); };
    var host = m[2] === '*' ? '[^/]*' : (m[2].indexOf('*.') === 0 ? '([^/]*\\.)?' + esc(m[2].slice(2)) : esc(m[2]));
    var scheme = m[1] === '*' ? 'https?|wss?' : m[1];
    return new RegExp('^(' + scheme + ')://' + host + '(:\\d+)?' + esc(m[3]).replace(/\\\*/g, '.*') + '$');
  }
  function reqEvent(name) {
    var ls = [];
    return {
      addListener: function (fn, filter) { ls.push({ fn: fn, filter: filter || {} }); },
      removeListener: function (fn) { ls = ls.filter(function (l) { return l.fn !== fn; }); },
      hasListener: function (fn) { return ls.some(function (l) { return l.fn === fn; }); },
      hasListeners: function () { return ls.length > 0; },
      _fire: function (args) {
        var d = args[0] || {};
        ls.slice().forEach(function (l) {
          var urls = l.filter.urls;
          if (urls && urls.length && !urls.some(function (u) { var re = patternRe(u); return re && re.test(d.url || ''); })) return;
          if (l.filter.types && l.filter.types.length && l.filter.types.indexOf(d.type) < 0) return;
          try { l.fn(d); } catch (e) { console.error(e); }
        });
      }
    };
  }
  var webRequest = {
    MAX_HANDLER_BEHAVIOR_CHANGED_CALLS_PER_10_MINUTES: 20,
    handlerBehaviorChanged: function (cb) { if (cb) cb(); return Promise.resolve(); }
  };
  ['onBeforeRequest', 'onBeforeSendHeaders', 'onSendHeaders', 'onHeadersReceived', 'onAuthRequired',
    'onResponseStarted', 'onBeforeRedirect', 'onCompleted', 'onErrorOccurred'].forEach(function (n) {
    EV['webRequest.' + n] = reqEvent(n);
    webRequest[n] = EV['webRequest.' + n];
  });

  var notifications = {
    create: function (id, opts, cb) {
      if (typeof id === 'object') { cb = opts; opts = id; id = ''; }
      id = id || 'n' + Date.now();
      call('notifications.create', { id: id, title: opts && opts.title, message: opts && opts.message });
      if (typeof cb === 'function') cb(id);
      return Promise.resolve(id);
    },
    update: function (id, o, cb) { if (cb) cb(true); return Promise.resolve(true); },
    clear: function (id, cb) { if (cb) cb(true); return Promise.resolve(true); },
    getAll: function (cb) { if (cb) cb({}); return Promise.resolve({}); },
    getPermissionLevel: function (cb) { if (cb) cb('granted'); return Promise.resolve('granted'); },
    onClicked: named('notifications.onClicked'),
    onClosed: named('notifications.onClosed'),
    onButtonClicked: named('notifications.onButtonClicked')
  };

  var grantedPerms = {};
  var mandatoryHosts = [];
  (MANIFEST.permissions || []).concat(MANIFEST.host_permissions || []).forEach(function (p) {
    if (p === '<all_urls>' || String(p).indexOf('://') > 0) { var re = patternRe(p); if (re) mandatoryHosts.push({ p: p, re: re }); }
  });
  function hasPerm(x) { return (MANIFEST.permissions || []).indexOf(x) >= 0 || !!grantedPerms[x]; }
  function hasOrigin(o) {
    if (grantedPerms[o]) return true;
    var probe = String(o).replace(/\*/g, 'x');
    return mandatoryHosts.some(function (h) { return h.p === o || h.re.test(probe); });
  }
  var permissions = {
    contains: function (p, cb) {
      var ok = (p.permissions || []).every(hasPerm) && (p.origins || []).every(hasOrigin);
      if (cb) cb(ok); return Promise.resolve(ok);
    },
    request: function (p, cb) {
      (p.permissions || []).concat(p.origins || []).forEach(function (x) { grantedPerms[x] = true; });
      named('permissions.onAdded')._fire([p]);
      if (cb) cb(true); return Promise.resolve(true);
    },
    remove: function (p, cb) {
      (p.permissions || []).concat(p.origins || []).forEach(function (x) { delete grantedPerms[x]; });
      named('permissions.onRemoved')._fire([p]);
      if (cb) cb(true); return Promise.resolve(true);
    },
    getAll: function (cb) {
      var perms = (MANIFEST.permissions || []).filter(function (x) { return x !== '<all_urls>' && String(x).indexOf('://') < 0; });
      var origins = (MANIFEST.host_permissions || []).concat((MANIFEST.permissions || []).filter(function (x) { return x === '<all_urls>' || String(x).indexOf('://') > 0; }));
      Object.keys(grantedPerms).forEach(function (x) { (String(x).indexOf('://') > 0 || x === '<all_urls>' ? origins : perms).push(x); });
      var r = { permissions: perms, origins: origins };
      if (cb) cb(r); return Promise.resolve(r);
    },
    onAdded: named('permissions.onAdded'),
    onRemoved: named('permissions.onRemoved')
  };


  // --- tab capture: the system's output audio, streamed from Go ---------------
  var captures = {};
  var capturedTracks = {};
  var capSeq = 0;
  if (window.MediaStreamTrack && !MediaStreamTrack.prototype.__fastyPatched) {
    var origStop = MediaStreamTrack.prototype.stop;
    MediaStreamTrack.prototype.stop = function () {
      var f = capturedTracks[this.id];
      origStop.call(this);
      if (f) { delete capturedTracks[this.id]; f(); }
    };
    MediaStreamTrack.prototype.__fastyPatched = true;
  }
  window.__fastyAudio = function (id, b64) {
    var c = captures[id];
    if (!c) return;
    var bin = atob(b64), u8 = new Uint8Array(bin.length);
    for (var i = 0; i < bin.length; i++) u8[i] = bin.charCodeAt(i);
    c.queue.push({ data: new Float32Array(u8.buffer, 0, u8.length >> 2), pos: 0 });
    c.len += u8.length >> 2;
    while (c.len > c.rate * 3 && c.queue.length) c.len -= c.queue.shift().data.length;
  };
  function startCapture(opts, cb) {
    if (!opts || (!opts.audio && !opts.video)) {
      runtime.lastError = { message: 'Extension has not been invoked for the current page (see activeTab permission).' };
      try { if (cb) cb(null); } finally { runtime.lastError = null; }
      return Promise.resolve(null);
    }
    var AC = window.AudioContext || window.webkitAudioContext;
    var rate = 44100;
    var ctx = new AC({ sampleRate: rate });
    var dest = ctx.createMediaStreamDestination();
    var node = ctx.createScriptProcessor(4096, 1, 1);
    var id = 'cap' + (++capSeq);
    var c = captures[id] = { queue: [], len: 0, rate: rate };
    node.onaudioprocess = function (e) {
      var out = e.outputBuffer.getChannelData(0), o = 0;
      while (o < out.length && c.queue.length) {
        var h = c.queue[0], take = Math.min(h.data.length - h.pos, out.length - o);
        out.set(h.data.subarray(h.pos, h.pos + take), o);
        o += take; h.pos += take; c.len -= take;
        if (h.pos >= h.data.length) c.queue.shift();
      }
      while (o < out.length) out[o++] = 0;
    };
    node.connect(dest);
    var stop = function () {
      if (!captures[id]) return;
      delete captures[id];
      post({ type: 'tabcapture.stop', payload: { id: id } });
      try { node.disconnect(); ctx.close(); } catch (e) {}
    };
    dest.stream.getTracks().forEach(function (t) { capturedTracks[t.id] = stop; });
    var onEnded = function () { stop(); };
    dest.stream.addEventListener('inactive', onEnded);
    window.addEventListener('unload', stop);
    post({ type: 'tabcapture.start', payload: { id: id, rate: rate } });
    ctx.resume();
    if (cb) cb(dest.stream);
    return Promise.resolve(dest.stream);
  }
  var tabCapture = {
    capture: startCapture,
    getMediaStreamId: function (o, cb) { if (cb) cb('fasty-capture'); return Promise.resolve('fasty-capture'); },
    getCapturedTabs: function (cb) { if (cb) cb([]); return Promise.resolve([]); },
    onStatusChanged: named('tabCapture.onStatusChanged')
  };
  tabs.captureVisibleTab = function (a, b, c) {
    var cb = last([a, b, c]);
    return call('tabs.capture', null, cb);
  };
  tabs.goBack = function (id, cb) { if (typeof id === 'function') { cb = id; id = undefined; } return call('tabs.goBack', { tabId: id }, cb); };
  tabs.goForward = function (id, cb) { if (typeof id === 'function') { cb = id; id = undefined; } return call('tabs.goForward', { tabId: id }, cb); };
  tabs.duplicate = function (id, cb) { return call('tabs.duplicate', { tabId: id }, cb); };

  var offscreen = {
    createDocument: function (o, cb) { return call('offscreen.create', { url: o && o.url }, cb); },
    closeDocument: function (cb) { return call('offscreen.close', null, cb); },
    hasDocument: function () { return Promise.resolve(false); },
    Reason: { AUDIO_PLAYBACK: 'AUDIO_PLAYBACK', USER_MEDIA: 'USER_MEDIA', DOM_PARSER: 'DOM_PARSER', DOM_SCRAPING: 'DOM_SCRAPING', BLOBS: 'BLOBS', WORKERS: 'WORKERS', IFRAME_SCRIPTING: 'IFRAME_SCRIPTING', LOCAL_STORAGE: 'LOCAL_STORAGE', CLIPBOARD: 'CLIPBOARD', TESTING: 'TESTING', MATCH_MEDIA: 'MATCH_MEDIA', GEOLOCATION: 'GEOLOCATION', DISPLAY_MEDIA: 'DISPLAY_MEDIA', WEB_RTC: 'WEB_RTC' }
  };
  runtime.getContexts = function (f, cb) { if (typeof f === 'function') cb = f; if (cb) cb([]); return Promise.resolve([]); };

  var cookies = {
    get: function (d, cb) { return call('cookies.get', d, cb); },
    getAll: function (d, cb) { return call('cookies.getAll', d || {}, cb); },
    set: function (d, cb) { return call('cookies.set', d, cb); },
    remove: function (d, cb) { return call('cookies.remove', d, cb); },
    getAllCookieStores: function (cb) { var r = [{ id: '0', tabIds: [] }]; if (cb) cb(r); return Promise.resolve(r); },
    onChanged: named('cookies.onChanged')
  };
  var history = {
    search: function (q, cb) { return call('history.search', q || {}, cb); },
    getVisits: function (d, cb) { if (cb) cb([]); return Promise.resolve([]); },
    addUrl: function (d, cb) { return call('history.addUrl', d, cb); },
    deleteUrl: function (d, cb) { return call('history.deleteUrl', d, cb); },
    deleteRange: function (d, cb) { return call('history.deleteAll', null, cb); },
    deleteAll: function (cb) { return call('history.deleteAll', null, cb); },
    onVisited: named('history.onVisited'),
    onVisitRemoved: named('history.onVisitRemoved')
  };
  var bookmarks = {
    getTree: function (cb) { return call('bookmarks.tree', null, cb); },
    search: function (q, cb) { return call('bookmarks.search', typeof q === 'string' ? { query: q } : q, cb); },
    create: function (b, cb) { return call('bookmarks.create', b, cb); },
    remove: function (id, cb) { return call('bookmarks.remove', { id: id }, cb); },
    getRecent: function (n, cb) { return call('bookmarks.search', { query: '' }, cb); },
    get: function (id, cb) { return call('bookmarks.search', { url: id }, cb); },
    onCreated: named('bookmarks.onCreated'),
    onRemoved: named('bookmarks.onRemoved'),
    onChanged: named('bookmarks.onChanged')
  };
  var downloads = {
    download: function (o, cb) { return call('downloads.download', { url: o && o.url }, cb); },
    search: function (q, cb) { if (cb) cb([]); return Promise.resolve([]); },
    onCreated: named('downloads.onCreated'),
    onChanged: named('downloads.onChanged')
  };
  var topSites = { get: function (cb) { return call('topsites.get', null, cb); } };

  var identity = {
    getRedirectURL: function (path) {
      return 'https://' + String(CFG.rid || EXT_ID).toLowerCase().replace(/[^a-z0-9]/g, '') + '.chromiumapp.org/' + (path || '');
    },
    launchWebAuthFlow: function (d, cb) { return call('identity.auth', { url: d && d.url, interactive: !d || d.interactive !== false }, cb); },
    getAuthToken: function (d, cb) {
      cb = last([d, cb]);
      var err = 'OAuth2 request failed: Account sign-in is not available in this browser.';
      if (cb) { runtime.lastError = { message: err }; try { cb(); } finally { runtime.lastError = null; } return; }
      return Promise.reject(new Error(err));
    },
    getProfileUserInfo: function (d, cb) { cb = last([d, cb]); var r = { email: '', id: '' }; if (cb) cb(r); return Promise.resolve(r); },
    removeCachedAuthToken: function (d, cb) { if (cb) cb(); return Promise.resolve(); },
    clearAllCachedAuthTokens: function (cb) { if (cb) cb(); return Promise.resolve(); },
    onSignInChanged: named('identity.onSignInChanged')
  };

  var natives = {};
  runtime.sendNativeMessage = function (app, msg, cb) { return call('native.send', { app: app, message: msg }, cb); };
  runtime.connectNative = function (app) {
    var id = 'n' + Date.now().toString(36) + Math.random().toString(36).slice(2, 6);
    var p = {
      name: app, onMessage: ev(), onDisconnect: ev(),
      postMessage: function (m) { post({ type: 'native.msg', payload: { id: id, message: m } }); },
      disconnect: function () { delete natives[id]; post({ type: 'native.close', payload: { id: id } }); }
    };
    natives[id] = p;
    post({ type: 'native.open', payload: { app: app, id: id } });
    return p;
  };
  var origDeliver = window.__fastyDeliver;
  window.__fastyDeliver = function (kind, d) {
    if (kind === 'nativeMsg') { var np = natives[d.id]; if (np) np.onMessage._fire([d.msg, np]); return; }
    if (kind === 'nativeClose') {
      var nc = natives[d.id];
      if (nc) { delete natives[d.id]; runtime.lastError = d.error ? { message: d.error } : null; try { nc.onDisconnect._fire([nc]); } finally { runtime.lastError = null; } }
      return;
    }
    return origDeliver(kind, d);
  };

  // --- tabs extras, search, sessions, user scripts and friends ----------------------
  tabs.move = function (ids, props, cb) { return call('tabs.move', { tabIds: [].concat(ids), index: props && props.index }, cb); };
  tabs.getZoom = function (id, cb) { if (typeof id === 'function') { cb = id; id = undefined; } return call('tabs.zoom', { tabId: id }, cb); };
  tabs.setZoom = function () {
    var a = Array.prototype.slice.call(arguments), cb = last(a);
    if (cb) a.pop();
    var id = (typeof a[0] === 'number' && a.length > 1) ? a[0] : undefined;
    var z = a.length > 1 ? a[1] : a[0];
    return call('tabs.zoom', { tabId: id, zoom: z }, cb ? function () { cb(); } : undefined);
  };
  tabs.discard = function (id, cb) { if (typeof id === 'function') { cb = id; id = undefined; } return call('tabs.discard', { tabId: id }, cb); };
  tabs.group = function (o, cb) { if (cb) cb(1); return Promise.resolve(1); };
  tabs.ungroup = function (ids, cb) { if (cb) cb(); return Promise.resolve(); };
  tabs.highlight = function (o, cb) { if (cb) cb({}); return Promise.resolve({}); };
  tabs.TAB_GROUP_ID_NONE = -1;
  ['onAttached', 'onDetached', 'onHighlighted', 'onZoomChange'].forEach(function (n) { tabs[n] = named('tabs.' + n); });

  var search = { query: function (o, cb) { return call('search.query', o || {}, cb); } };
  var sessions = {
    MAX_SESSION_RESULTS: 25,
    getRecentlyClosed: function (f, cb) { cb = last([f, cb]); if (cb) cb([]); return Promise.resolve([]); },
    getDevices: function (f, cb) { cb = last([f, cb]); if (cb) cb([]); return Promise.resolve([]); },
    restore: function (id, cb) { cb = last([id, cb]); if (cb) cb({}); return Promise.resolve({}); },
    onChanged: named('sessions.onChanged')
  };
  var tabGroups = {
    TAB_GROUP_ID_NONE: -1,
    query: function (q, cb) { if (cb) cb([]); return Promise.resolve([]); },
    get: function (id, cb) { var e = new Error('No group with id: ' + id); if (cb) { runtime.lastError = { message: e.message }; try { cb(); } finally { runtime.lastError = null; } return; } return Promise.reject(e); },
    update: function (id, p, cb) { if (cb) cb({}); return Promise.resolve({}); },
    move: function (id, p, cb) { if (cb) cb({}); return Promise.resolve({}); },
    onCreated: named('tabGroups.onCreated'), onUpdated: named('tabGroups.onUpdated'),
    onMoved: named('tabGroups.onMoved'), onRemoved: named('tabGroups.onRemoved')
  };
  var readingList = {
    query: function (q, cb) { if (cb) cb([]); return Promise.resolve([]); },
    addEntry: function (e, cb) { if (cb) cb(); return Promise.resolve(); },
    removeEntry: function (e, cb) { if (cb) cb(); return Promise.resolve(); },
    updateEntry: function (e, cb) { if (cb) cb(); return Promise.resolve(); }
  };
  var fontSettings = {
    getFontList: function (cb) { if (cb) cb([]); return Promise.resolve([]); },
    getFont: function (d, cb) { var r = { fontId: '', levelOfControl: 'not_controllable' }; if (cb) cb(r); return Promise.resolve(r); },
    setFont: function (d, cb) { if (cb) cb(); return Promise.resolve(); }
  };
  var theme = { update: function (a, b) { var cb = last([a, b]); if (cb) cb(); return Promise.resolve(); }, reset: function (a, b) { var cb = last([a, b]); if (cb) cb(); return Promise.resolve(); }, getCurrent: function () { return Promise.resolve({}); } };
  function setting() {
    var v = { value: false, levelOfControl: 'controllable_by_this_extension' };
    return {
      get: function (d, cb) { cb = last([d, cb]); if (cb) cb(v); return Promise.resolve(v); },
      set: function (d, cb) { cb = last([d, cb]); if (cb) cb(); return Promise.resolve(); },
      clear: function (d, cb) { cb = last([d, cb]); if (cb) cb(); return Promise.resolve(); },
      onChange: ev()
    };
  }
  function settingBag() { return new Proxy({}, { get: function (t, p) { return t[p] || (t[p] = setting()); } }); }
  var privacy = { network: settingBag(), services: settingBag(), websites: settingBag() };
  var dom = { openOrClosedShadowRoot: function (el) { return (el && el.shadowRoot) || null; } };
  if (CTX === 'content' || CTX === 'userscript') {
    try { Object.defineProperty(Element.prototype, 'openOrClosedShadowRoot', { get: function () { return this.shadowRoot; }, configurable: true }); } catch (e) {}
  }

  var dynSeq = 0;
  function normScripts(o) {
    return {
      id: o.id || ('fx-' + Date.now().toString(36) + (++dynSeq)),
      matches: o.matches || [], excludeMatches: o.excludeMatches || o.exclude_matches || [],
      js: (o.js || []).map(function (x) { return typeof x === 'string' ? x : (x.code !== undefined ? { code: x.code } : x.file); }),
      css: (o.css || []).map(function (x) { return typeof x === 'string' ? x : (x.code !== undefined ? { code: x.code } : x.file); }),
      runAt: o.runAt || o.run_at || 'document_idle', allFrames: !!(o.allFrames || o.all_frames),
      world: o.world || '', persistAcrossSessions: false
    };
  }
  // Firefox: contentScripts.register(options) returns a handle; Chrome: userScripts.register([...]).
  function registerHandle(type, o) {
    var sc = normScripts(o);
    return call(type, { scripts: [sc] }).then(function () {
      return { unregister: function () { return call(type.replace('register', 'unregister'), { ids: [sc.id] }); } };
    });
  }
  var contentScripts = { register: function (o) { return registerHandle('scripting.register', o); } };
  var userScripts = {
    ExecutionWorld: { USER_SCRIPT: 'USER_SCRIPT', MAIN: 'MAIN' },
    register: function (a, cb) {
      if (Array.isArray(a)) return call('userscripts.register', { scripts: a.map(normScripts) }, cb);
      return registerHandle('userscripts.register', a);
    },
    update: function (a, cb) { return call('userscripts.update', { scripts: a.map(normScripts) }, cb); },
    unregister: function (f, cb) { if (typeof f === 'function') { cb = f; f = null; } return call('userscripts.unregister', { ids: (f && f.ids) || [] }, cb); },
    getScripts: function (f, cb) { if (typeof f === 'function') { cb = f; f = null; } return call('userscripts.get', { ids: (f && f.ids) || [] }, cb); },
    configureWorld: function (p, cb) { return call('userscripts.configure', p || {}, cb); },
    resetWorldConfiguration: function (cb) { return call('userscripts.configure', {}, cb); }
  };
  scripting.ExecutionWorld = { ISOLATED: 'ISOLATED', MAIN: 'MAIN' };
  webRequest.ResourceType = { MAIN_FRAME: 'main_frame', SUB_FRAME: 'sub_frame', STYLESHEET: 'stylesheet', SCRIPT: 'script', IMAGE: 'image', FONT: 'font', OBJECT: 'object', XMLHTTPREQUEST: 'xmlhttprequest', PING: 'ping', CSP_REPORT: 'csp_report', MEDIA: 'media', WEBSOCKET: 'websocket', OTHER: 'other' };
  ['OnBeforeRequestOptions', 'OnBeforeSendHeadersOptions', 'OnSendHeadersOptions', 'OnHeadersReceivedOptions', 'OnAuthRequiredOptions',
    'OnResponseStartedOptions', 'OnBeforeRedirectOptions', 'OnCompletedOptions', 'OnErrorOccurredOptions'].forEach(function (n) {
    webRequest[n] = { BLOCKING: 'blocking', REQUEST_BODY: 'requestBody', REQUEST_HEADERS: 'requestHeaders', RESPONSE_HEADERS: 'responseHeaders', EXTRA_HEADERS: 'extraHeaders', ASYNC_BLOCKING: 'asyncBlocking' };
  });

  var api = {
    runtime: runtime,
    storage: storage,
    tabs: tabs,
    windows: windows,
    scripting: scripting,
    i18n: i18n,
    alarms: alarms,
    action: action,
    browserAction: browserAction,
    pageAction: pageAction,
    contextMenus: contextMenus,
    menus: contextMenus,
    webRequest: webRequest,
    tabCapture: tabCapture,
    offscreen: offscreen,
    history: history,
    bookmarks: bookmarks,
    downloads: downloads,
    topSites: topSites,
    search: search,
    sessions: sessions,
    tabGroups: tabGroups,
    readingList: readingList,
    fontSettings: fontSettings,
    theme: theme,
    privacy: privacy,
    dom: dom,
    contentScripts: contentScripts,
    userScripts: userScripts,
    identity: identity,
    notifications: notifications,
    permissions: permissions,
    extension: {
      getURL: runtime.getURL,
      inIncognitoContext: false,
      getBackgroundPage: function () { return CTX === 'background' ? window : null; },
      getViews: function () { return []; },
      isAllowedIncognitoAccess: function (cb) { if (cb) cb(false); return Promise.resolve(false); },
      isAllowedFileSchemeAccess: function (cb) { if (cb) cb(false); return Promise.resolve(false); },
      lastError: null
    },
    webNavigation: {
      onBeforeNavigate: named('webNavigation.onBeforeNavigate'),
      onCommitted: named('webNavigation.onCommitted'),
      onDOMContentLoaded: named('webNavigation.onDOMContentLoaded'),
      onCompleted: named('webNavigation.onCompleted'),
      onErrorOccurred: named('webNavigation.onErrorOccurred'),
      onHistoryStateUpdated: named('webNavigation.onHistoryStateUpdated'),
      onReferenceFragmentUpdated: named('webNavigation.onReferenceFragmentUpdated'),
      getFrame: function (d, cb) { if (cb) cb(null); return Promise.resolve(null); },
      getAllFrames: function (d, cb) { if (cb) cb([]); return Promise.resolve([]); }
    },
    commands: {
      getAll: function (cb) { return call('commands.getAll', null, cb); },
      onCommand: named('commands.onCommand')
    },
    management: {
      getSelf: function (cb) { return call('management.get', { id: CFG.rid || EXT_ID }, cb); },
      getAll: function (cb) { return call('management.getAll', null, cb); },
      get: function (id, cb) { return call('management.get', { id: id }, cb); },
      setEnabled: function (id, on, cb) { return call('management.setEnabled', { id: id, enabled: on }, cb); },
      uninstall: function (id, o, cb) { cb = last([id, o, cb]); return call('management.uninstall', { id: typeof id === 'string' ? id : '' }, cb); },
      uninstallSelf: function (o, cb) { cb = last([o, cb]); return call('management.uninstall', { id: '' }, cb); },
      getPermissionWarningsById: function (id, cb) { if (cb) cb([]); return Promise.resolve([]); },
      launchApp: function (id, cb) { if (cb) cb(); return Promise.resolve(); },
      onInstalled: named('management.onInstalled'), onEnabled: named('management.onEnabled'),
      onDisabled: named('management.onDisabled'), onUninstalled: named('management.onUninstalled')
    },
    declarativeNetRequest: {
      RuleActionType: { BLOCK: 'block', REDIRECT: 'redirect', ALLOW: 'allow', UPGRADE_SCHEME: 'upgradeScheme', MODIFY_HEADERS: 'modifyHeaders', ALLOW_ALL_REQUESTS: 'allowAllRequests' },
      ResourceType: { MAIN_FRAME: 'main_frame', SUB_FRAME: 'sub_frame', STYLESHEET: 'stylesheet', SCRIPT: 'script', IMAGE: 'image', FONT: 'font', OBJECT: 'object', XMLHTTPREQUEST: 'xmlhttprequest', PING: 'ping', CSP_REPORT: 'csp_report', MEDIA: 'media', WEBSOCKET: 'websocket', OTHER: 'other' },
      MAX_NUMBER_OF_DYNAMIC_RULES: 30000, MAX_NUMBER_OF_UNSAFE_DYNAMIC_RULES: 5000, MAX_NUMBER_OF_SESSION_RULES: 5000,
      GUARANTEED_MINIMUM_STATIC_RULES: 30000, MAX_NUMBER_OF_ENABLED_STATIC_RULESETS: 50, MAX_NUMBER_OF_STATIC_RULESETS: 100,
      updateDynamicRules: function (d, cb) { return call('dnr.updateDynamic', d || {}, cb); },
      getDynamicRules: function (f, cb) { if (typeof f === 'function') cb = f; return call('dnr.getDynamic', null, cb); },
      updateSessionRules: function (d, cb) { return call('dnr.updateSession', d || {}, cb); },
      getSessionRules: function (f, cb) { if (typeof f === 'function') cb = f; return call('dnr.getSession', null, cb); },
      updateEnabledRulesets: function (d, cb) { return call('dnr.updateRulesets', d || {}, cb); },
      getEnabledRulesets: function (cb) { return call('dnr.getEnabledRulesets', null, cb); },
      getDisabledRuleIds: function (o, cb) { if (cb) cb([]); return Promise.resolve([]); },
      updateStaticRules: function (o, cb) { if (cb) cb(); return Promise.resolve(); },
      getAvailableStaticRuleCount: function (cb) { if (cb) cb(300000); return Promise.resolve(300000); },
      isRegexSupported: function (o, cb) { return call('dnr.isRegexSupported', { regex: o && o.regex }, cb); },
      getMatchedRules: function (f, cb) { if (typeof f === 'function') cb = f; var r = { rulesMatchedInfo: [] }; if (cb) cb(r); return Promise.resolve(r); },
      setExtensionActionOptions: function (o, cb) { if (cb) cb(); return Promise.resolve(); },
      testMatchOutcome: function (o, cb) { var r = { matchedRules: [] }; if (cb) cb(r); return Promise.resolve(r); },
      onRuleMatchedDebug: noop
    },
    declarativeContent: { onPageChanged: { addRules: function () {}, removeRules: function () {}, getRules: function () {} } },
    idle: {
      queryState: function (s, cb) { if (cb) cb('active'); return Promise.resolve('active'); },
      setDetectionInterval: function () {},
      onStateChanged: named('idle.onStateChanged')
    },
    omnibox: { setDefaultSuggestion: function () {}, onInputStarted: noop, onInputChanged: noop, onInputEntered: noop, onInputCancelled: noop },
    sidePanel: { setOptions: function (o, cb) { if (cb) cb(); return Promise.resolve(); }, setPanelBehavior: function (o, cb) { if (cb) cb(); return Promise.resolve(); } },
    cookies: cookies
  };

  // --- cross-origin requests -----------------------------------------------------
  // Extension pages may reach hosts in their permissions without CORS; WebKit
  // doesn't allow that, so such requests go through Go. Content scripts try
  // the page's own network first and fall back only when it is refused.
  var hostRes = [];
  (MANIFEST.permissions || []).concat(MANIFEST.host_permissions || [], MANIFEST.optional_host_permissions || [], MANIFEST.optional_permissions || [])
    .forEach(function (p) {
      if (p === '<all_urls>' || String(p).indexOf('://') > 0) { var re = patternRe(p); if (re) hostRes.push(re); }
    });
  function viaProxy(url) {
    try {
      var u = new URL(url, location.href);
      if (!/^https?:$/.test(u.protocol) || u.origin === location.origin) return false;
      return hostRes.some(function (re) { return re.test(u.href); });
    } catch (e) { return false; }
  }
  function ab2b64(buf) {
    var u8 = new Uint8Array(buf), s = '';
    for (var i = 0; i < u8.length; i += 8192) s += String.fromCharCode.apply(null, u8.subarray(i, i + 8192));
    return btoa(s);
  }
  function b642ab(b64) {
    var bin = atob(b64), u8 = new Uint8Array(bin.length);
    for (var i = 0; i < bin.length; i++) u8[i] = bin.charCodeAt(i);
    return u8.buffer;
  }
  function proxyFetch(req) {
    return req.arrayBuffer().then(function (buf) {
      return call('net.fetch', {
        url: req.url, method: req.method, headers: Array.from(req.headers.entries()),
        body: buf.byteLength ? ab2b64(buf) : '', credentials: req.credentials, redirect: req.redirect
      });
    }).then(function (r) {
      var nullBody = r.status === 204 || r.status === 205 || r.status === 304;
      var resp = new Response(nullBody ? null : b642ab(r.body), { status: r.status, statusText: r.statusText, headers: r.headers });
      try { Object.defineProperty(resp, 'url', { value: r.url }); } catch (e) {}
      return resp;
    }, function (e) { throw new TypeError(e && e.message || 'Failed to fetch'); });
  }
  var nativeFetch = window.fetch ? window.fetch.bind(window) : null;
  if (nativeFetch && hostRes.length) {
    window.fetch = function (input, init) {
      var req;
      try { req = new Request(input, init); } catch (e) { return nativeFetch(input, init); }
      if (!viaProxy(req.url)) return nativeFetch(input, init);
      if (CTX === 'content') {
        return nativeFetch(req.clone()).catch(function () { return proxyFetch(req); });
      }
      return proxyFetch(req);
    };
  }

  var NativeXHR = window.XMLHttpRequest;
  if (NativeXHR && hostRes.length && CTX !== 'content') {
    var xState = new WeakMap();
    var fire = function (x, type, extra) {
      var ev;
      try { ev = new ProgressEvent(type, extra || {}); } catch (e) { ev = new Event(type); }
      x.dispatchEvent(ev);
    };
    window.XMLHttpRequest = class FastyXHR extends NativeXHR {
      open(method, url) {
        var abs = '';
        try { abs = new URL(url, location.href).href; } catch (e) {}
        if (abs && viaProxy(abs)) {
          xState.set(this, { method: method, url: abs, headers: [], state: 1, status: 0, statusText: '', body: null, text: '', respHeaders: [], url2: abs });
          super.open(method, 'about:blank');
          fire(this, 'readystatechange');
          return;
        }
        xState.delete(this);
        return super.open.apply(this, arguments);
      }
      setRequestHeader(k, v) {
        var st = xState.get(this);
        if (st) { st.headers.push([k, v]); return; }
        return super.setRequestHeader(k, v);
      }
      send(body) {
        var st = xState.get(this);
        if (!st) return super.send(body);
        var self = this;
        var opts = { method: st.method };
        if (body != null && st.method.toUpperCase() !== 'GET' && st.method.toUpperCase() !== 'HEAD') opts.body = body;
        var req = new Request(st.url, opts);
        st.headers.forEach(function (h) { req.headers.set(h[0], h[1]); });
        var credentials = this.withCredentials ? 'include' : 'same-origin';
        req.arrayBuffer().then(function (buf) {
          return call('net.fetch', {
            url: st.url, method: st.method, headers: Array.from(req.headers.entries()),
            body: buf.byteLength ? ab2b64(buf) : '', credentials: credentials, redirect: 'follow'
          });
        }).then(function (r) {
          if (st.aborted) return;
          st.status = r.status; st.statusText = r.statusText; st.respHeaders = r.headers; st.url2 = r.url;
          st.body = b642ab(r.body);
          st.state = 2; fire(self, 'readystatechange');
          st.state = 3; fire(self, 'readystatechange');
          st.state = 4; fire(self, 'readystatechange');
          fire(self, 'load', { loaded: st.body.byteLength, total: st.body.byteLength });
          fire(self, 'loadend');
        }, function () {
          if (st.aborted) return;
          st.state = 4; st.status = 0;
          fire(self, 'readystatechange'); fire(self, 'error'); fire(self, 'loadend');
        });
      }
      abort() {
        var st = xState.get(this);
        if (!st) return super.abort();
        st.aborted = true; st.state = 0;
        fire(this, 'abort'); fire(this, 'loadend');
      }
      getResponseHeader(n) {
        var st = xState.get(this);
        if (!st) return super.getResponseHeader(n);
        var vals = st.respHeaders.filter(function (h) { return h[0].toLowerCase() === String(n).toLowerCase(); }).map(function (h) { return h[1]; });
        return vals.length ? vals.join(', ') : null;
      }
      getAllResponseHeaders() {
        var st = xState.get(this);
        if (!st) return super.getAllResponseHeaders();
        return st.respHeaders.map(function (h) { return h[0].toLowerCase() + ': ' + h[1]; }).join('\r\n') + '\r\n';
      }
      overrideMimeType() {}
      get readyState() { var st = xState.get(this); return st ? st.state : super.readyState; }
      get status() { var st = xState.get(this); return st ? st.status : super.status; }
      get statusText() { var st = xState.get(this); return st ? st.statusText : super.statusText; }
      get responseURL() { var st = xState.get(this); return st ? st.url2 : super.responseURL; }
      get responseText() {
        var st = xState.get(this);
        if (!st) return super.responseText;
        return st.body ? new TextDecoder().decode(st.body) : '';
      }
      get response() {
        var st = xState.get(this);
        if (!st) return super.response;
        if (st.state < 4 || !st.body) return this.responseType === 'json' ? null : '';
        switch (this.responseType) {
          case 'arraybuffer': return st.body;
          case 'blob': return new Blob([st.body]);
          case 'json': try { return JSON.parse(new TextDecoder().decode(st.body)); } catch (e) { return null; }
          default: return new TextDecoder().decode(st.body);
        }
      }
    };
  }

  // Members the shim doesn't implement degrade to harmless stand-ins instead of
  // throwing "undefined is not a function", and are logged once.
  var warned = {};
  function unsupported(path) {
    if (!warned[path]) { warned[path] = 1; try { console.warn('[fasty] ' + path + ' is not implemented'); } catch (e) {} }
  }
  function wrapNs(name, obj) {
    return new Proxy(obj, {
      get: function (t, p) {
        if (p in t) return t[p];
        if (typeof p !== 'string' || p === 'then' || p === 'toJSON' || p === 'constructor' || p.charAt(0) === '_') return undefined;
        if (/^on[A-Z]/.test(p)) return (t[p] = ev());
        if (/^[A-Z][A-Z0-9_]*$/.test(p)) return undefined;
        if (/^[A-Z]/.test(p)) return new Proxy({}, { get: function (e, k) { if (k in Object.prototype) return Object.prototype[k]; return typeof k === 'string' ? k.toLowerCase() : undefined; }, has: function () { return true; } });
        return (t[p] = function () {
          unsupported(name + '.' + p);
          var cb = last(arguments);
          if (cb) { try { cb(); } catch (e) {} return undefined; }
          return Promise.resolve();
        });
      }
    });
  }
  Object.keys(api).forEach(function (n) {
    if (api[n] && typeof api[n] === 'object') api[n] = wrapNs(n, api[n]);
  });

  window.chrome = api;
  window.browser = api;

  // --- console and error forwarding (background and extension pages) ------------
  if (CTX !== 'content') {
    ['log', 'info', 'warn', 'error', 'debug'].forEach(function (lvl) {
      var orig = console[lvl];
      console[lvl] = function () {
        try {
          var text = Array.prototype.map.call(arguments, function (a) {
            if (a instanceof Error) return a.stack || String(a);
            if (typeof a === 'object') { try { return JSON.stringify(a); } catch (e) { return String(a); } }
            return String(a);
          }).join(' ').slice(0, 800);
          post({ type: 'log', payload: { level: lvl, text: text } });
        } catch (e) {}
        return orig.apply(console, arguments);
      };
    });
    window.addEventListener('error', function (e) {
      post({ type: 'log', payload: { level: 'uncaught', text: (e.message || '') + ' @ ' + (e.filename || '') + ':' + (e.lineno || 0) } });
    });
    window.addEventListener('unhandledrejection', function (e) {
      post({ type: 'log', payload: { level: 'unhandled', text: String(e.reason && e.reason.message || e.reason) + ' | ' + String(e.reason && e.reason.stack || '').split('\n')[0] } });
    });
  }

  if (CTX === 'content' || CTX === 'userscript') {
    var contentErrs = 0;
    var report = function (text) {
      if (++contentErrs > 20) return;
      try { post({ type: 'log', payload: { level: 'content-error', text: String(text).slice(0, 400) + ' [' + location.hostname + ']' } }); } catch (e) {}
    };
    window.addEventListener('error', function (e) { report((e.message || 'error') + ' @ ' + (e.filename || '') + ':' + (e.lineno || 0)); });
    window.addEventListener('unhandledrejection', function (e) { report('unhandled: ' + (e.reason && e.reason.message || e.reason)); });
  }

  // --- background page niceties -----------------------------------------------
  if (CTX === 'background') {
    // A service worker's importScripts(): fetch synchronously and evaluate.
    window.importScripts = function () {
      for (var i = 0; i < arguments.length; i++) {
        var url = new URL(arguments[i], CFG.base).href;
        var x = new XMLHttpRequest();
        x.open('GET', url, false);
        x.send();
        if (x.status && x.status !== 200) throw new Error('importScripts failed: ' + url);
        (0, eval)(x.responseText + '\n//# sourceURL=' + url);
      }
    };
    window.skipWaiting = function () { return Promise.resolve(); };
    window.clients = { claim: function () { return Promise.resolve(); }, matchAll: function () { return Promise.resolve([]); }, get: function () { return Promise.resolve(undefined); }, openWindow: function (u) { return call('tabs.create', { url: u }); } };
    var reg = new EventTarget();
    reg.scope = CFG.base;
    reg.active = { state: 'activated', scriptURL: CFG.base };
    reg.installing = null; reg.waiting = null;
    reg.update = function () { return Promise.resolve(reg); };
    reg.unregister = function () { return Promise.resolve(true); };
    reg.showNotification = function (t, o) { call('notifications.create', { title: t, message: o && o.body }); return Promise.resolve(); };
    reg.getNotifications = function () { return Promise.resolve([]); };
    reg.navigationPreload = { enable: function () { return Promise.resolve(); }, disable: function () { return Promise.resolve(); }, setHeaderValue: function () { return Promise.resolve(); }, getState: function () { return Promise.resolve({ enabled: false, headerValue: 'true' }); } };
    reg.pushManager = { getSubscription: function () { return Promise.resolve(null); }, subscribe: function () { return Promise.reject(new Error('Push is not supported')); }, permissionState: function () { return Promise.resolve('denied'); } };
    window.registration = reg;
    window.serviceWorker = { state: 'activated', scriptURL: CFG.base, addEventListener: function () {}, postMessage: function () {} };

    window.addEventListener('load', function () {
      ['install', 'activate'].forEach(function (t) {
        var e = new Event(t); e.waitUntil = function () {};
        try { window.dispatchEvent(e); } catch (x) {}
      });
      setTimeout(function () {
        if (CFG.reason) runtime.onInstalled._fire([{ reason: CFG.reason }]);
        runtime.onStartup._fire([]);
      }, 0);
    });
  }
})();
