package main

import (
	"encoding/json"
	"os"
	"testing"
)

// testdata/turbo.json is produced by the real turbo-stream encoder (the format finn uses).
func loadFixtures(t *testing.T) (search, ad string) {
	t.Helper()
	b, err := os.ReadFile("testdata/turbo.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct{ Search, Ad string }
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return f.Search, f.Ad
}

func TestParseSearchPage(t *testing.T) {
	search, _ := loadFixtures(t)
	page := parseSearchPage(search)
	if len(page.Docs) != 1 || page.LastPage != 3 {
		t.Fatalf("got %d docs, last page %d", len(page.Docs), page.LastPage)
	}
	f := fieldsFromSearchDoc(page.Docs[0])
	if searchDocFinnkode(page.Docs[0]) != 479861168 || *f.Price != 5000000 || *f.PriceTotal != 5100000 || *f.Lat != 58.99 {
		t.Fatalf("unexpected fields: %+v", f)
	}
	if f.FinnUpdatedAt == nil || *f.FinnUpdatedAt != "2025-11-01T12:26:40Z" {
		t.Fatalf("timestamp: %v", f.FinnUpdatedAt)
	}
}

func TestParseAdPage(t *testing.T) {
	_, stream := loadFixtures(t)
	chunk, _ := json.Marshal(stream)
	html := `<html><head><meta property="og:title" content="OG title"></head><body>
		<img src="https://images.finncdn.no/dynamic/960w/2026/10/vertical-2/31/8/479/861/168_aaa.jpg">
		<img src="https://images.finncdn.no/dynamic/960w/2026/10/vertical-2/31/8/479/861/168_bbb.jpg">
		<img src="https://images.finncdn.no/dynamic/960w/2026/10/vertical-2/31/8/479/861/168_aaa.jpg">
		<img src="https://images.finncdn.no/dynamic/960w/2026/10/vertical-2/01/1/000/000/001_other.jpg">
		<script>window.__reactRouterContext.streamController.enqueue(` + string(chunk) + `);</script>
		</body></html>`

	p := parseAdPage(html, 479861168)
	if len(p.Images) != 2 || p.Images[0] != "https://images.finncdn.no/dynamic/default/2026/10/vertical-2/31/8/479/861/168_aaa.jpg" {
		t.Fatalf("images: %v", p.Images)
	}
	f := p.Fields
	check := func(name string, ok bool) {
		if !ok {
			t.Errorf("%s wrong: %+v", name, f)
		}
	}
	check("title", f.Title != nil && *f.Title == "Funkis enebolig med utsikt")
	check("price", f.Price != nil && *f.Price == 4900000) // not the recommendation's price
	check("build year", f.BuildYear != nil && *f.BuildYear == 1962)
	check("agent", f.AgentName != nil && *f.AgentName == "Kari Megler" && f.AgentCompany != nil && *f.AgentCompany == "Megler AS")
	check("description", f.Description != nil && *f.Description == "Flott hus\n\nmed panel")
	check("published", f.PublishedAt != nil && *f.PublishedAt == "2026-10-31T00:00:00Z")
	if len(p.Facts) != 2 || p.Facts[0].Label != "Byggeår" {
		t.Errorf("facts: %+v", p.Facts)
	}
}

func TestStoreAndCrawlFlow(t *testing.T) {
	search, stream := loadFixtures(t)
	db, err := openDB(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	crawl, _ := db.startCrawl()
	for _, doc := range parseSearchPage(search).Docs {
		if _, err := db.storeSearchDoc(crawl, doc, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.finishSearch(crawl, true); err != nil {
		t.Fatal(err)
	}
	todo, err := db.todo(crawl, 10)
	if err != nil || len(todo) != 1 {
		t.Fatalf("todo %v %v", todo, err)
	}
	chunk, _ := json.Marshal(stream)
	html := `<script>streamController.enqueue(` + string(chunk) + `)</script>`
	if _, _, err := db.storePage(479861168, 200, "https://www.finn.no/x", html); err != nil {
		t.Fatal(err)
	}
	_ = db.finishCrawl(crawl, "")
	ad, err := db.getAd(479861168)
	if err != nil || ad == nil {
		t.Fatal(err)
	}
	// Search results win over the ad page for the price; the page fills in the rest.
	if *ad.Price != 5000000 || ad.BuildYear == nil || ad.District == nil || *ad.District != "Hundvåg" {
		t.Fatalf("ad: %+v", ad)
	}

	// Next day the ad is gone from the search results and its page says sold.
	crawl2, _ := db.startCrawl()
	_ = db.finishSearch(crawl2, true)
	todo, _ = db.todo(crawl2, 10)
	if len(todo) != 1 {
		t.Fatalf("expected the missing ad to be re-fetched, got %v", todo)
	}
	status, _, err := db.storePage(479861168, 200, "", `<script type="application/json">{"adId":479861168,"adStatus":"SOLD"}</script>`)
	if err != nil || status != "sold" {
		t.Fatalf("status %q err %v", status, err)
	}
}
