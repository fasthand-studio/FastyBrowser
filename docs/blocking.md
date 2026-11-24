# How blocking works

The blocker is built in — no extension, no proxy, and no remote filtering
service. It uses Brave's **adblock-rust** engine to turn three public lists —
**EasyList**, **EasyPrivacy** and **StevenBlack's hosts** — into WebKit's own
content-blocking rules. WebKit enforces them before a request leaves the
browser, so blocked ad and tracker requests are never made.

- Rules are split into two WebKit filters because WebKit allows only about
  150,000 rules per filter. WebKit caches the compiled rules on disk, under a
  name made from the lists themselves, so the rules are only rebuilt (a few
  hundred MB of work) when a list changes, not on every launch.
- Every three days the browser downloads fresh lists from `easylist.to` and
  GitHub, recompiles them with adblock-rust and swaps the filters in without a
  restart. It checks again daily. If a download fails, the last good list is
  kept; if no list is available at all, a small built-in list of known ad
  domains is used.
- The **Block ads and trackers** switch in Settings → Privacy turns the
  blocker on and off globally, and **About this site** can pause it for a
  single host.

## YouTube

YouTube's own ads are part of the page itself and have no separate URL to
block, so `assets/js/youtube.js` works the way Brave's and uBlock Origin's
YouTube scriptlets do:

- the inline player response never carries ad keys;
- `JSON.parse` and `Response.json` results are pruned;
- fetch and XHR responses for player, get_watch, next, browse, playlist and
  Shorts requests have `adPlacements`, `adSlots` and `playerAds` renamed to
  `no_ads`, and ad entries are dropped from lists;
- ads that are baked into the stream (YouTube's server-side-inserted ads) are
  seeked past as soon as the player reports one;
- the "ad blockers violate the terms" popup is dismissed;
- ad containers are hidden with Brave's and EasyList's exact selectors.

YouTube changes its ads often, so some will slip through; this is a best
effort, not a guarantee.

## Site compatibility

`assets/js/compat.js` holds small fixes for sites that expect things WebKitGTK
lacks. Today it makes GitHub load its sign-in options (Google, Apple) and
stops GitHub's tab bars from slowing scrolling. Fixes are limited to the one
site they are for.
