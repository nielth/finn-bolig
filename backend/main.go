package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// A new daily crawl is due when the last complete crawl is older than this.
const crawlInterval = 20 * time.Hour

type Server struct {
	db *DB
}

func main() {
	dataDir := env("DATA_DIR", "./data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Fatal(err)
	}
	db, err := openDB(filepath.Join(dataDir, "finn-bolig.db"))
	if err != nil {
		log.Fatal(err)
	}
	s := &Server{db: db}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/listings", s.handleListings)
	mux.HandleFunc("GET /api/listings/{finnkode}", s.handleListing)
	mux.HandleFunc("GET /api/listings/{finnkode}/data", s.handleListingData)
	mux.HandleFunc("POST /api/listings/{finnkode}/page", s.handleVisitedPage)
	mux.HandleFunc("GET /api/districts", s.handleDistricts)
	mux.HandleFunc("GET /api/references", s.handleReferences)
	mux.HandleFunc("POST /api/references", s.handleAddReference)
	mux.HandleFunc("DELETE /api/references/{finnkode}", s.handleRemoveReference)
	mux.HandleFunc("GET /api/settings", s.handleSettings)
	mux.HandleFunc("PUT /api/settings", s.handleSaveSettings)

	mux.HandleFunc("GET /api/crawl/status", s.handleCrawlStatus)
	mux.HandleFunc("GET /api/crawls", s.handleCrawls)
	mux.HandleFunc("POST /api/crawl/start", s.handleCrawlStart)
	mux.HandleFunc("POST /api/crawl/{id}/search", s.handleCrawlSearchPage)
	mux.HandleFunc("POST /api/crawl/{id}/search-done", s.handleCrawlSearchDone)
	mux.HandleFunc("GET /api/crawl/{id}/todo", s.handleCrawlTodo)
	mux.HandleFunc("POST /api/crawl/{id}/ad/{finnkode}", s.handleCrawlAd)
	mux.HandleFunc("POST /api/crawl/{id}/failed/{finnkode}", s.handleCrawlFailed)
	mux.HandleFunc("POST /api/crawl/{id}/finish", s.handleCrawlFinish)
	mux.HandleFunc("POST /api/admin/reparse", s.handleReparse)

	addr := env("LISTEN_ADDR", ":8080")
	log.Printf("listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, logRequests(mux)))
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if r.Method != http.MethodGet {
			log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
		}
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, err error) {
	if status >= 500 {
		log.Printf("error: %v", err)
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeMsg(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(v)
}

func readText(r *http.Request) (string, error) {
	b, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, 30<<20))
	return string(b), err
}

func pathInt(r *http.Request, name string) (int64, error) {
	return strconv.ParseInt(r.PathValue(name), 10, 64)
}

// ---------- listings ----------

func (s *Server) handleListings(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := AdFilter{Status: q.Get("status"), Sort: q.Get("sort")}
	if d := strings.TrimSpace(q.Get("districts")); d != "" {
		f.Districts = strings.Split(d, ",")
	}
	f.PriceMin, _ = strconv.ParseInt(q.Get("price_min"), 10, 64)
	f.PriceMax, _ = strconv.ParseInt(q.Get("price_max"), 10, 64)
	ads, err := s.db.listAds(f)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	// Long fields are only needed on the detail page.
	for i := range ads {
		ads[i].Description = nil
		ads[i].Facts = nil
	}
	writeJSON(w, 200, ads)
}

func (s *Server) handleListing(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "finnkode")
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	ad, err := s.db.getAd(id)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	if ad == nil {
		writeMsg(w, 404, "not found")
		return
	}
	writeJSON(w, 200, ad)
}

// handleListingData returns the raw data parsed from the ad page, for debugging the parser.
func (s *Server) handleListingData(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "finnkode")
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	data, err := s.db.adPageData(id)
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(data))
}

// handleVisitedPage stores an ad page the user opened on finn.no, outside of a crawl.
func (s *Server) handleVisitedPage(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "finnkode")
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	page, err := readText(r)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	status, _, err := s.db.storePage(id, 200, r.URL.Query().Get("url"), page)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	ad, err := s.db.getAd(id)
	if err != nil || ad == nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{"status": status, "images": len(ad.Images)})
}

func (s *Server) handleDistricts(w http.ResponseWriter, r *http.Request) {
	counts, err := s.db.districtCounts()
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	type districtCount struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	out := []districtCount{}
	for _, d := range districts {
		out = append(out, districtCount{d.Name, counts[d.Name]})
	}
	writeJSON(w, 200, out)
}

// ---------- favourites ----------

func (s *Server) handleReferences(w http.ResponseWriter, r *http.Request) {
	refs, err := s.db.references()
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, refs)
}

var finnkodeRe = []*regexp.Regexp{
	regexp.MustCompile(`finnkode=(\d+)`),
	regexp.MustCompile(`/(\d{6,})(?:[/?#]|$)`),
	regexp.MustCompile(`^\s*(\d{6,})\s*$`),
}

func parseFinnkode(input string) (int64, bool) {
	for _, re := range finnkodeRe {
		if m := re.FindStringSubmatch(input); m != nil {
			id, err := strconv.ParseInt(m[1], 10, 64)
			return id, err == nil
		}
	}
	return 0, false
}

func (s *Server) handleAddReference(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Input string `json:"input"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, err)
		return
	}
	id, ok := parseFinnkode(body.Input)
	if !ok {
		writeMsg(w, 400, "could not find a finnkode in the input")
		return
	}
	if err := s.db.addReference(id); err != nil {
		writeErr(w, 500, err)
		return
	}
	ad, _ := s.db.getAd(id)
	writeJSON(w, 200, ad)
}

func (s *Server) handleRemoveReference(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "finnkode")
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := s.db.removeReference(id); err != nil {
		writeErr(w, 500, err)
		return
	}
	w.WriteHeader(204)
}

// ---------- settings ----------

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.db.settings()
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, settings)
}

func (s *Server) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	var body Settings
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, err)
		return
	}
	if strings.TrimSpace(body.Prompt) == "" {
		writeMsg(w, 400, "prompt can not be empty")
		return
	}
	if err := s.db.savePrompt(body.Prompt); err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, body)
}

// ---------- crawling ----------
//
// The userscript drives the crawl from the user's browser:
//   start -> search pages -> search-done -> todo -> ad pages -> finish
// It only fetches pages and posts the raw responses; all parsing happens here.

func (s *Server) handleCrawlStatus(w http.ResponseWriter, r *http.Request) {
	running, err := s.db.runningCrawl()
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	last, err := s.db.lastCompleteCrawl()
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	due := running == nil
	if last != nil && last.FinishedAt != nil {
		if t, err := time.Parse(time.RFC3339, *last.FinishedAt); err == nil && time.Since(t) < crawlInterval {
			due = false
		}
	}
	writeJSON(w, 200, map[string]any{"running": running, "last": last, "due": due})
}

func (s *Server) handleCrawls(w http.ResponseWriter, r *http.Request) {
	crawls, err := s.db.crawls(30)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, crawls)
}

func (s *Server) handleCrawlStart(w http.ResponseWriter, r *http.Request) {
	running, err := s.db.runningCrawl()
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	if running != nil {
		writeMsg(w, 409, "a crawl is already running in another tab")
		return
	}
	id, err := s.db.startCrawl()
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	log.Printf("crawl %d started", id)
	writeJSON(w, 200, map[string]int64{"id": id})
}

func (s *Server) crawlFromPath(w http.ResponseWriter, r *http.Request) *Crawl {
	id, err := pathInt(r, "id")
	if err != nil {
		writeErr(w, 400, err)
		return nil
	}
	c, err := s.db.crawl(id)
	if err != nil {
		writeErr(w, 500, err)
		return nil
	}
	if c == nil || c.FinishedAt != nil {
		writeMsg(w, 404, "no such running crawl")
		return nil
	}
	return c
}

func (s *Server) handleCrawlSearchPage(w http.ResponseWriter, r *http.Request) {
	c := s.crawlFromPath(w, r)
	if c == nil {
		return
	}
	text, err := readText(r)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	page := parseSearchPage(text)
	last, err := s.db.lastCompleteCrawl()
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	newAds := 0
	for _, doc := range page.Docs {
		isNew, err := s.db.storeSearchDoc(c.ID, doc, last == nil)
		if err != nil {
			writeErr(w, 500, err)
			return
		}
		if isNew && last != nil {
			newAds++
		}
	}
	s.db.bumpCrawl(c.ID, "pages", 1)
	s.db.bumpCrawl(c.ID, "new_ads", newAds)
	writeJSON(w, 200, map[string]int{"found": len(page.Docs), "new": newAds, "last_page": page.LastPage})
}

func (s *Server) handleCrawlSearchDone(w http.ResponseWriter, r *http.Request) {
	c := s.crawlFromPath(w, r)
	if c == nil {
		return
	}
	var body struct {
		Complete bool `json:"complete"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := s.db.finishSearch(c.ID, body.Complete); err != nil {
		writeErr(w, 500, err)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) handleCrawlTodo(w http.ResponseWriter, r *http.Request) {
	c := s.crawlFromPath(w, r)
	if c == nil {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 5000 {
		limit = 2000
	}
	ids, err := s.db.todo(c.ID, limit)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{"finnkodes": ids})
}

func (s *Server) handleCrawlAd(w http.ResponseWriter, r *http.Request) {
	c := s.crawlFromPath(w, r)
	if c == nil {
		return
	}
	id, err := pathInt(r, "finnkode")
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	httpStatus, _ := strconv.Atoi(r.URL.Query().Get("status"))
	page, err := readText(r)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	before, _ := s.db.getAd(id)
	status, updated, err := s.db.storePage(id, httpStatus, r.URL.Query().Get("url"), page)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	s.db.bumpCrawl(c.ID, "pages_fetched", 1)
	if updated {
		s.db.bumpCrawl(c.ID, "updated_ads", 1)
	}
	if before != nil && before.Status != status {
		switch status {
		case "sold":
			s.db.bumpCrawl(c.ID, "sold_ads", 1)
		case "removed":
			s.db.bumpCrawl(c.ID, "removed_ads", 1)
		}
	}
	images := 0
	if ad, _ := s.db.getAd(id); ad != nil {
		images = len(ad.Images)
	}
	writeJSON(w, 200, map[string]any{"status": status, "updated": updated, "images": images})
}

func (s *Server) handleCrawlFailed(w http.ResponseWriter, r *http.Request) {
	c := s.crawlFromPath(w, r)
	if c == nil {
		return
	}
	id, err := pathInt(r, "finnkode")
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := s.db.markPageFailed(id); err != nil {
		writeErr(w, 500, err)
		return
	}
	s.db.bumpCrawl(c.ID, "pages_fetched", 0) // heartbeat
	w.WriteHeader(204)
}

func (s *Server) handleCrawlFinish(w http.ResponseWriter, r *http.Request) {
	c := s.crawlFromPath(w, r)
	if c == nil {
		return
	}
	var body struct {
		Error string `json:"error"`
	}
	_ = readJSON(r, &body)
	if err := s.db.finishCrawl(c.ID, body.Error); err != nil {
		writeErr(w, 500, err)
		return
	}
	done, _ := s.db.crawl(c.ID)
	if done != nil {
		log.Printf("crawl %d finished: %d found, %d pages fetched, %d new, %d updated, %d sold, %d removed",
			done.ID, done.Found, done.PagesFetched, done.NewAds, done.UpdatedAds, done.SoldAds, done.RemovedAds)
	}
	writeJSON(w, 200, done)
}

func (s *Server) handleReparse(w http.ResponseWriter, r *http.Request) {
	n, err := s.db.reparseAll()
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]int{"reparsed": n})
}
