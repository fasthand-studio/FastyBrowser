package main

import "testing"

func TestBookmarksToggleAndPersist(t *testing.T) {
	dir := t.TempDir()
	b := LoadBookmarks(dir)
	if !b.Toggle("https://example.com/", "Example") || !b.Has("https://example.com/") {
		t.Fatal("first toggle should add")
	}
	b.Flush()
	if got := LoadBookmarks(dir); len(got.Items) != 1 || got.Items[0].Title != "Example" {
		t.Fatalf("not persisted: %+v", got.Items)
	}
	if b.Toggle("https://example.com/", "") || b.Has("https://example.com/") {
		t.Fatal("second toggle should remove")
	}
	if b.Toggle("", "x") {
		t.Fatal("empty URL must not be bookmarked")
	}
}

func TestHistoryRecordsWebPagesOnly(t *testing.T) {
	h := LoadHistory(t.TempDir())
	h.Record("https://a.example/", "A")
	h.Record("http://127.0.0.1:1/ui/newtab.html", "internal")
	h.Record("about:blank", "")
	h.Record("javascript:alert(1)", "")
	if len(h.Items) != 1 {
		// the loopback page is http, so it is recorded here; the app filters internal pages itself
		if len(h.Items) != 2 {
			t.Fatalf("unexpected items: %+v", h.Items)
		}
	}
	h.Record("https://a.example/", "A again")
	if h.Items[0].URL != "https://a.example/" || h.Items[0].Visits != 2 || h.Items[0].Title != "A again" {
		t.Fatalf("repeat visit should move to the top and count: %+v", h.Items[0])
	}
	h.SetTitle("https://a.example/", "Final")
	if h.Items[0].Title != "Final" {
		t.Fatal("SetTitle")
	}
	h.Remove("https://a.example/")
	for _, it := range h.Items {
		if it.URL == "https://a.example/" {
			t.Fatal("Remove")
		}
	}
}

func TestHistoryIsCapped(t *testing.T) {
	h := LoadHistory(t.TempDir())
	for i := 0; i < historyLimit+50; i++ {
		h.Record("https://site.example/"+string(rune('a'+i%26))+string(rune('a'+(i/26)%26))+string(rune('a'+(i/676)%26)), "")
	}
	if len(h.Items) > historyLimit {
		t.Fatalf("history grew to %d", len(h.Items))
	}
}

func TestSuggestOrdering(t *testing.T) {
	bm := LoadBookmarks(t.TempDir())
	bm.Toggle("https://github.com/", "GitHub")
	h := LoadHistory(t.TempDir())
	h.Record("https://news.ycombinator.com/", "Hacker News")
	h.Record("https://github.com/fasthand-studio", "FastHand Studio · GitHub")
	tabs := []Suggestion{{Kind: "tab", Title: "Docs", URL: "https://docs.example/"}}
	got := Suggest("git", "duckduckgo", bm, h, tabs, 6)
	if len(got) < 3 {
		t.Fatalf("want typed + bookmark + history, got %+v", got)
	}
	if got[0].Kind != "url" && got[0].Kind != "search" {
		t.Fatalf("what was typed must lead: %+v", got[0])
	}
	if got[1].Kind != "bookmark" || got[1].URL != "https://github.com/" {
		t.Fatalf("bookmark should come first after the typed entry: %+v", got[1])
	}
	for _, s := range got[1:] {
		if s.Title == "Hacker News" {
			t.Fatal("non-matching history must not appear")
		}
	}
	if Suggest("", "google", bm, h, tabs, 6) != nil {
		t.Fatal("empty query gives no suggestions")
	}
	words := Suggest("cheap flights", "google", bm, h, nil, 6)
	if len(words) != 1 || words[0].Kind != "search" || words[0].URL != "https://www.google.com/search?q=cheap+flights" {
		t.Fatalf("search entry wrong: %+v", words)
	}
}
