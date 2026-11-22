// Extension store helper for Fasty Browser.
// Injected on Chrome Web Store and Firefox Add-ons to enable installing
// extensions directly into Fasty.

(function () {
  'use strict';
  var STORE = '';
  var host = location.hostname;

  if (host === 'chromewebstore.google.com' || host === 'chrome.google.com') {
    STORE = 'chrome';
  } else if (host.endsWith('addons.mozilla.org')) {
    STORE = 'firefox';
  } else {
    return;
  }

  function send(op, extId, url) {
    try {
      window.webkit.messageHandlers.officeVeil.postMessage(
        { _ch: 'extStore', op: op, store: STORE, extId: extId, url: url || '' }
      );
    } catch (e) {
      console.error('Fasty: could not send request', e);
    }
  }
  function sendInstall(extId, url) { send('install', extId, url); }

  // Every Fasty button follows the install state Go reports.
  var BTN = { installed: false, extId: '', url: '' };
  function paint(btn) {
    var base = btn.getAttribute('data-base') || '#4285f4';
    var hover = btn.getAttribute('data-hover') || '#3367d6';
    btn.disabled = false;
    if (BTN.installed) {
      btn.textContent = 'Remove from Fasty';
      btn.style.background = '#d93025';
      btn.onmouseenter = function () { btn.style.background = '#b7211a'; };
      btn.onmouseleave = function () { btn.style.background = '#d93025'; };
    } else {
      btn.textContent = 'Add to Fasty';
      btn.style.background = base;
      btn.onmouseenter = function () { btn.style.background = hover; };
      btn.onmouseleave = function () { btn.style.background = base; };
    }
  }
  window.__fastyStoreState = function (installed) {
    BTN.installed = !!installed;
    document.querySelectorAll('.fasty-install-btn').forEach(paint);
  };
  function press(btn) {
    btn.textContent = BTN.installed ? 'Removing…' : 'Installing…';
    btn.disabled = true;
    if (BTN.installed) send('remove', BTN.extId, '');
    else sendInstall(BTN.extId, BTN.url);
  }
  function wire(btn, extId, url, base, hover) {
    BTN.extId = extId;
    BTN.url = url || BTN.url;
    btn.setAttribute('data-base', base);
    btn.setAttribute('data-hover', hover);
    btn.onclick = function (e) { e.preventDefault(); e.stopPropagation(); press(btn); };
    paint(btn);
    send('status', extId, '');
  }

  // --- Chrome Web Store ---
  if (STORE === 'chrome') {
    // Hide the "Switch to Chrome" banner and the disabled "Add to Chrome" button.
    var style = document.createElement('style');
    style.textContent =
      '.xX710b, .Ojy58b, .IMCthb { display: none !important; }' +
      'button[aria-label*="Switch to Chrome"] { display: none !important; }' +
      'button[disabled][aria-label*="Add to Chrome"] { display: none !important; }' +
      'button[disabled][aria-label*="install"] { display: none !important; }' +
      'div[class*="UWpb5"] { display: none !important; }' +
      '.mUIrbf-LgbsSe[aria-label*="Install Chrome"] { display: none !important; }';
    (document.head || document.documentElement).appendChild(style);

    // The URL format: https://chromewebstore.google.com/detail/<name>/<id>
    // or /detail/<id>
    function getChromeExtId() {
      var parts = location.pathname.split('/');
      // /detail/name/id or /detail/id
      var idx = parts.indexOf('detail');
      if (idx >= 0) {
        var candidate = parts[parts.length - 1];
        if (/^[a-z]{32}$/.test(candidate)) return candidate;
        if (parts.length > idx + 2) {
          candidate = parts[idx + 2];
          if (/^[a-z]{32}$/.test(candidate)) return candidate;
        }
      }
      return null;
    }

    function addFastyButton() {
      if (document.querySelector('.fasty-install-btn')) return;
      var extId = getChromeExtId();
      if (!extId) return;

      // Find the "Add to Chrome" button area and add ours next to it
      var targets = document.querySelectorAll('button');
      var added = false;
      targets.forEach(function (btn) {
        var text = (btn.textContent || '').trim().toLowerCase();
        if (text.includes('add to') || text.includes('install') || text.includes('get')) {
          if (btn.closest && !btn.parentElement.querySelector('.fasty-install-btn')) {
            var fastyBtn = document.createElement('button');
            fastyBtn.className = 'fasty-install-btn';
            fastyBtn.textContent = 'Add to Fasty';
            fastyBtn.style.cssText = 'margin-left:8px;padding:8px 20px;border-radius:20px;' +
              'background:#4285f4;color:#fff;border:none;cursor:pointer;font-size:14px;font-weight:500;' +
              'font-family:inherit;vertical-align:middle;';
            btn.parentElement.insertBefore(fastyBtn, btn.nextSibling);
            wire(fastyBtn, extId, '', '#4285f4', '#3367d6');
            added = true;
          }
        }
      });

      if (!added) {
        // Fallback: add a floating button
        var container = document.querySelector('[class*="header"]') ||
                       document.querySelector('header') ||
                       document.querySelector('h1');
        if (container && !document.querySelector('.fasty-install-btn')) {
          var fastyBtn = document.createElement('button');
          fastyBtn.className = 'fasty-install-btn';
          fastyBtn.textContent = 'Add to Fasty';
          fastyBtn.style.cssText = 'margin:8px;padding:8px 20px;border-radius:20px;' +
            'background:#4285f4;color:#fff;border:none;cursor:pointer;font-size:14px;font-weight:500;' +
            'display:inline-block;';
          container.appendChild(fastyBtn);
          wire(fastyBtn, extId, '', '#4285f4', '#3367d6');
        }
      }
    }

    // Run on load and on SPA navigation
    addFastyButton();
    var _lastUrl = location.href;
    new MutationObserver(function () {
      if (location.href !== _lastUrl) {
        _lastUrl = location.href;
        setTimeout(addFastyButton, 1000);
      }
      // Also retry if button not yet placed
      if (!document.querySelector('.fasty-install-btn') && getChromeExtId()) {
        setTimeout(addFastyButton, 500);
      }
    }).observe(document.body || document.documentElement, { childList: true, subtree: true });
  }

  // --- Firefox Add-ons ---
  if (STORE === 'firefox') {
    // URL format: https://addons.mozilla.org/en-US/firefox/addon/<slug>/
    // The XPI download link is on the page or at /firefox/downloads/file/<id>/<name>.xpi

    function addFirefoxButton() {
      if (document.querySelector('.fasty-install-btn')) return;

      // Find the install button
      var installBtns = document.querySelectorAll('.AMInstallButton-button, .InstallButtonWrapper-download-link, [class*="InstallButton"] a, [class*="InstallButton"] button');
      if (installBtns.length === 0) {
        // Try generic approach
        installBtns = document.querySelectorAll('a[href*="/downloads/file/"]');
      }

      var xpiUrl = '';
      installBtns.forEach(function (btn) {
        var href = btn.getAttribute('href') || '';
        if (href.includes('/downloads/file/') && href.includes('.xpi')) {
          xpiUrl = href;
        }
      });

      // Also check for the download link in meta or data attributes
      if (!xpiUrl) {
        var dlLink = document.querySelector('a[href$=".xpi"]');
        if (dlLink) xpiUrl = dlLink.getAttribute('href');
      }

      // Look for data attributes that might contain the file URL
      if (!xpiUrl) {
        var installData = document.querySelector('[data-addon-id]');
        if (installData) {
          var addonId = installData.getAttribute('data-addon-id');
          // We'll need the slug from the URL
          var slugMatch = location.pathname.match(/\/addon\/([^/]+)/);
          if (slugMatch) {
            // Try the API endpoint
            xpiUrl = 'https://addons.mozilla.org/firefox/downloads/latest/' + slugMatch[1] + '/addon-latest.xpi';
          }
        }
      }

      var slug = '';
      var m = location.pathname.match(/\/addon\/([^/]+)/);
      if (m) slug = m[1];
      if (!xpiUrl && slug) {
        xpiUrl = 'https://addons.mozilla.org/firefox/downloads/latest/' + slug + '/addon-latest.xpi';
      }

      var added = false;
      installBtns.forEach(function (btn) {
        if (btn.parentElement && !btn.parentElement.querySelector('.fasty-install-btn')) {
          var fastyBtn = document.createElement('button');
          fastyBtn.className = 'fasty-install-btn';
          fastyBtn.textContent = 'Add to Fasty';
          fastyBtn.style.cssText = 'margin-left:8px;padding:10px 24px;border-radius:4px;' +
            'background:#0060df;color:#fff;border:none;cursor:pointer;font-size:14px;font-weight:600;' +
            'font-family:inherit;';
          btn.parentElement.insertBefore(fastyBtn, btn.nextSibling);
          var url = xpiUrl;
          if (url && !url.startsWith('http')) url = location.origin + url;
          wire(fastyBtn, slug, url, '#0060df', '#003eaa');
          added = true;
        }
      });

      if (!added && slug) {
        var container = document.querySelector('.Addon-header') ||
                       document.querySelector('[class*="AddonTitle"]') ||
                       document.querySelector('h1');
        if (container && !document.querySelector('.fasty-install-btn')) {
          var fastyBtn = document.createElement('button');
          fastyBtn.className = 'fasty-install-btn';
          fastyBtn.textContent = 'Add to Fasty';
          fastyBtn.style.cssText = 'margin:12px 0;padding:10px 24px;border-radius:4px;' +
            'background:#0060df;color:#fff;border:none;cursor:pointer;font-size:14px;font-weight:600;' +
            'display:block;';
          container.appendChild(fastyBtn);
          var url2 = xpiUrl;
          if (url2 && !url2.startsWith('http')) url2 = location.origin + url2;
          wire(fastyBtn, slug, url2, '#0060df', '#003eaa');
        }
      }
    }

    addFirefoxButton();
    setTimeout(addFirefoxButton, 2000);
    new MutationObserver(function () {
      if (!document.querySelector('.fasty-install-btn')) {
        setTimeout(addFirefoxButton, 500);
      }
    }).observe(document.body || document.documentElement, { childList: true, subtree: true });
  }
})();
