// The ad blocker's rules: filter lists and the rules built from them.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// --- Shield ------------------------------------------------------------------

var unwanted = []string{
	"doubleclick.net", "googlesyndication.com", "googleadservices.com",
	"googletagservices.com", "google-analytics.com", "googletagmanager.com",
	"adservice.google.com", "amazon-adsystem.com", "adnxs.com", "adsrvr.org",
	"criteo.com", "criteo.net", "taboola.com", "outbrain.com",
	"rubiconproject.com", "pubmatic.com", "openx.net", "casalemedia.com",
	"smartadserver.com", "sharethrough.com", "indexww.com", "bidswitch.net",
	"33across.com", "teads.tv", "moatads.com", "adroll.com",
	"scorecardresearch.com", "quantserve.com", "chartbeat.com",
	"hotjar.com", "mouseflow.com", "fullstory.com", "clarity.ms",
	"mixpanel.com", "amplitude.com", "segment.com", "segment.io",
	"branch.io", "appsflyer.com", "adjust.com", "analytics.tiktok.com",
	"connect.facebook.net", "ads-twitter.com", "analytics.twitter.com",
}

var slots = []string{
	".adsbygoogle", "ins.adsbygoogle", `[id^="google_ads_"]`,
	`[id^="div-gpt-ad"]`, `[id^="taboola-"]`, "#taboola-below-article",
	`iframe[src*="doubleclick.net"]`, `iframe[src*="googlesyndication"]`,
	`iframe[src*="amazon-adsystem"]`,
	".ad-slot", ".ad-slot-container", ".top-banner-ad-container",
	".ad-leaderboard", ".ad-billboard", ".ad-giga", ".ad-mpu", ".ad-mrec",
	".ad-unit", ".adunit", ".adslot", ".dfp-ad", ".gpt-ad", ".w_ad",
}

// YouTube serves in-feed ads inside its own page, so there is no ad URL to
// block. Hide the renderer elements that hold them instead.
var youtubeSlots = []string{
	"ytd-ad-slot-renderer", "ytd-display-ad-renderer",
	"ytd-in-feed-ad-layout-renderer", "ytd-promoted-sparkles-web-renderer",
	"ytd-promoted-video-renderer", "ytd-banner-promo-renderer",
	"ytd-inline-survey-renderer",
}

var slotsBySite = []struct {
	sites    []string
	selector string
}{
	{[]string{"*as.com", "*elpais.com"}, ".ad"},
	{[]string{"*theguardian.com"}, ".top-fronts-banner-ad-container"},
	{[]string{"*independent.co.uk", "*the-independent.com"}, "#billboard-wrapper"},
	{[]string{"*cnn.com"}, ".ad-slot-header__wrapper"},
}

var youtubePaths = []string{
	"/api/stats/ads", "/pagead/", "/ptracking", "/get_midroll_info",
	"/youtubei/v1/player/ad_break", "/pcs/activeview",
}

func networkFilter(domain string) string {
	return `^https?://([^/]+\.)?` + strings.ReplaceAll(domain, ".", `\.`) + `[:/]`
}

type rule map[string]any

// The lists the blocker compiles. They are bundled into the binary at build
// time and refreshed in the background at runtime, like Brave's component
// updates — YouTube changes its ads, so a static list stops working.
type filterSource struct {
	name string
	url  string
}

var filterSources = []filterSource{
	{"easylist", "https://easylist.to/easylist/easylist.txt"},
	{"easyprivacy", "https://easylist.to/easylist/easyprivacy.txt"},
	{"stevenblack", "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts"},
}

const filterAge = 3 * 24 * time.Hour

func filtersDir() string { return filepath.Join(cacheDir(), "filters", "lists") }

func listPath(name string) string { return filepath.Join(filtersDir(), name+".txt") }

// bundledList is the copy shipped inside the binary by build.sh.
func bundledList(name string) string {
	b, err := assets.ReadFile("assets/filters/" + name + ".txt")
	if err != nil {
		return ""
	}
	return string(b)
}

// filterList prefers the locally refreshed copy, falling back to the bundled
// one. That way a failed update never leaves the browser unprotected.
func filterList(name string) string {
	if b, err := os.ReadFile(listPath(name)); err == nil && len(b) > 0 {
		return string(b)
	}
	return bundledList(name)
}

// generatedRules compiles EasyList and EasyPrivacy through adblock-rust into
// WebKit's own content-blocking format.
func generatedRules() ([]rule, error) {
	easy := filterList("easylist")
	privacy := filterList("easyprivacy")
	if easy == "" || privacy == "" {
		return nil, fmt.Errorf("adblock: filter lists are missing")
	}
	js, err := adblockRules([]string{easy, privacy}, 0)
	if err != nil {
		return nil, err
	}
	var rules []rule
	if err := json.Unmarshal([]byte(js), &rules); err != nil {
		return nil, err
	}
	if len(rules) == 0 {
		return nil, fmt.Errorf("adblock: no rules generated")
	}
	return rules, nil
}

func shieldRules() []rule {
	var rules, exceptions []rule
	generated, err := generatedRules()
	if err == nil {
		// adblock-rust places ignore-previous-rules exceptions last; our own
		// rules must go before them or first-party requests ignore them.
		for _, r := range generated {
			if a, ok := r["action"].(map[string]any); ok && a["type"] == "ignore-previous-rules" {
				exceptions = append(exceptions, r)
			} else {
				rules = append(rules, r)
			}
		}
		log.Printf("fasty: shield: %d rules from EasyList + EasyPrivacy", len(generated))
	} else {
		log.Printf("fasty: shield: %v; using the small built-in list", err)
		for _, d := range unwanted {
			rules = append(rules, rule{"trigger": rule{"url-filter": networkFilter(d), "load-type": []string{"third-party"}},
				"action": rule{"type": "block"}})
		}
	}
	rules = append(rules, rule{"trigger": rule{"url-filter": ".*"},
		"action": rule{"type": "css-display-none", "selector": strings.Join(slots, ", ")}})
	rules = append(rules, rule{"trigger": rule{"url-filter": ".*", "if-domain": []string{"*youtube.com"}},
		"action": rule{"type": "css-display-none", "selector": strings.Join(youtubeSlots, ", ")}})
	for _, p := range youtubePaths {
		rules = append(rules, rule{"trigger": rule{"url-filter": `^https?://([^/]+\.)?youtube\.com` + strings.ReplaceAll(p, ".", `\.`)},
			"action": rule{"type": "block"}})
	}
	rules = append(rules, rule{"trigger": rule{"url-filter": `^https?://([^/]+\.)?youtube\.com/api/stats/qoe\?.*adformat=`},
		"action": rule{"type": "block"}})
	for _, f := range []string{`^https?://([^/]+\.)?google\.com/pagead/`, `^https?://imasdk\.googleapis\.com/`} {
		rules = append(rules, rule{"trigger": rule{"url-filter": f, "if-domain": []string{"*youtube.com", "*youtube-nocookie.com"}},
			"action": rule{"type": "block"}})
	}
	for _, s := range slotsBySite {
		rules = append(rules, rule{"trigger": rule{"url-filter": ".*", "if-domain": s.sites},
			"action": rule{"type": "css-display-none", "selector": s.selector}})
	}
	return append(rules, exceptions...)
}

// hostRules compiles StevenBlack's hosts list. WebKit caps each filter at
// about 150k rules, so it lives in its own filter alongside shieldRules().
func hostRules() []rule {
	text := filterList("stevenblack")
	if text == "" {
		log.Printf("fasty: shield: hosts: list is missing")
		return nil
	}
	js, err := adblockRules([]string{text}, 1)
	if err != nil {
		log.Printf("fasty: shield: hosts: %v", err)
		return nil
	}
	var rules []rule
	if err := json.Unmarshal([]byte(js), &rules); err != nil {
		log.Printf("fasty: shield: hosts: %v", err)
		return nil
	}
	log.Printf("fasty: shield: %d rules from StevenBlack hosts", len(rules))
	return rules
}
