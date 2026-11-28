package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestCurtainHidesPerSiteAndPersists(t *testing.T) {
	dir := t.TempDir()
	c := LoadCurtain(dir)
	if !c.Hide("news.example", "div.promo", "Promo banner") || c.Hide("news.example", "div.promo", "again") {
		t.Fatal("a selector is added once")
	}
	if c.Hide("", "x", "") || c.Hide("a.example", "", "") {
		t.Fatal("host and selector are required")
	}
	c.Hide("news.example", "#cookie", "Cookies")
	if css := c.CSS("news.example"); !strings.Contains(css, "div.promo{display:none!important}") || !strings.Contains(css, "#cookie{display:none!important}") {
		t.Fatalf("css: %q", css)
	}
	if c.CSS("other.example") != "" {
		t.Fatal("other sites are untouched")
	}
	c.Flush()
	c2 := LoadCurtain(dir)
	if len(c2.Veils("news.example")) != 2 {
		t.Fatalf("not persisted: %+v", c2.Sites)
	}
	c2.Show("news.example", "div.promo")
	if len(c2.Veils("news.example")) != 1 {
		t.Fatal("Show")
	}
	c2.ShowAll("news.example")
	if len(c2.Hosts()) != 0 {
		t.Fatal("ShowAll")
	}
}

func TestImportBookmarksHTML(t *testing.T) {
	doc := `<!DOCTYPE NETSCAPE-Bookmark-file-1><DL><p>
<DT><H3>Folder</H3><DL><p>
<DT><A HREF="https://example.com/" ADD_DATE="1">Example &amp; Co</A>
<DT><A HREF='https://go.dev/doc' ICON="data:xx">The <b>Go</b> docs</A>
<DT><A HREF="javascript:alert(1)">bad</A>
<DT><A HREF="https://example.com/">duplicate</A>
<DT><A HREF="ftp://x.example/">ftp</A>
</DL><p></DL>`
	got := ImportBookmarksHTML(doc)
	if len(got) != 2 || got[0].Title != "Example & Co" || got[1].Title != "The Go docs" || got[1].URL != "https://go.dev/doc" {
		t.Fatalf("import: %+v", got)
	}
	b := LoadBookmarks(t.TempDir())
	b.Toggle("https://example.com/", "mine")
	if n := b.AddMany(got); n != 1 || len(b.Items) != 2 {
		t.Fatalf("AddMany added %d: %+v", n, b.Items)
	}
}

func TestEnginesAndCustomSearch(t *testing.T) {
	for _, e := range EngineNames {
		if _, ok := searchEngines[e.Key]; !ok {
			t.Errorf("engine %q listed but missing", e.Key)
		}
	}
	if len(EngineNames) != 8 {
		t.Errorf("want the 8 Linux engines, have %d", len(EngineNames))
	}
	CustomSearch = "https://search.example/?q=%s"
	defer func() { CustomSearch = "" }()
	if got := ResolveInput("hello world", "custom"); got != "https://search.example/?q=hello+world" {
		t.Errorf("custom: %q", got)
	}
	CustomSearch = "not a url %s"
	if got := ResolveInput("hi", "custom"); !strings.HasPrefix(got, "https://duckduckgo.com/") {
		t.Errorf("a bad custom address falls back: %q", got)
	}
}

func TestSettingsNewFieldsNormalize(t *testing.T) {
	s := Settings{SearchEngine: "custom", CustomEngine: "no placeholder", PageZoom: 9000}.Normalize()
	if s.SearchEngine != "duckduckgo" || s.PageZoom != 300 || s.ShieldPaused == nil {
		t.Errorf("%+v", s)
	}
	s = Settings{SearchEngine: "custom", CustomEngine: "https://x.example/?q=%s", PageZoom: 10}.Normalize()
	if s.SearchEngine != "custom" || s.PageZoom != 50 {
		t.Errorf("%+v", s)
	}
	d := DefaultSettings()
	if !d.SmoothScroll || !d.HardwareAccel || d.PageZoom != 100 {
		t.Errorf("defaults: %+v", d)
	}
	if !reflect.DeepEqual(d.ShieldPaused, []string{}) {
		t.Errorf("ShieldPaused default %#v", d.ShieldPaused)
	}
}
