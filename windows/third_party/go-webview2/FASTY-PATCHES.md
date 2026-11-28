Vendored from github.com/wailsapp/go-webview2 v1.0.23 (MIT, see LICENSE).
Changes: pkg/edge/chromium.go - errorCallback no longer calls os.Exit(1) and WebResourceRequested no longer calls log.Fatal, so a failed WebView2 call is reported instead of killing the browser.

Also: chromium_<arch>.go SetSize returns early once ShuttingDown() was called, so a late resize does not hit a closed controller.
