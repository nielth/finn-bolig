package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil && v > 0 {
		return v
	}
	return fallback
}

type Server struct {
	db     *DB
	scorer *Scorer
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
	images, err := newImageCache(filepath.Join(dataDir, "images"))
	if err != nil {
		log.Fatal(err)
	}

	apiKey := env("ANTHROPIC_API_KEY", os.Getenv("ANTHROPIC_API"))
	if apiKey == "" {
		log.Print("warning: ANTHROPIC_API_KEY is not set, scoring will fail")
	}
	scorer := &Scorer{
		db:          db,
		images:      images,
		client:      anthropic.NewClient(option.WithAPIKey(apiKey)),
		model:       env("CLAUDE_MODEL", "claude-opus-5-5"),
		effort:      anthropic.BetaOutputConfigEffort(env("CLAUDE_EFFORT", "medium")),
		concurrency: envInt("SCORING_CONCURRENCY", 4),
		maxImages:   envInt("SCORING_MAX_IMAGES", 10),
		maxRefImgs:  envInt("SCORING_MAX_REFERENCE_IMAGES", 5),
		debounce:    time.Duration(envInt("SCORING_DEBOUNCE_SECONDS", 15)) * time.Second,
	}
	s := &Server{db: db, scorer: scorer}

	// Score anything left unscored from a previous run.
	if settings, err := db.settings(); err == nil && settings.AutoRescore {
		scorer.Trigger()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/listings", s.handleListings)
	mux.HandleFunc("GET /api/listings/{finnkode}", s.handleListing)
	mux.HandleFunc("GET /api/districts", s.handleDistricts)
	mux.HandleFunc("GET /api/references", s.handleReferences)
	mux.HandleFunc("POST /api/references", s.handleAddReference)
	mux.HandleFunc("DELETE /api/references/{finnkode}", s.handleRemoveReference)
	mux.HandleFunc("GET /api/settings", s.handleSettings)
	mux.HandleFunc("PUT /api/settings", s.handleSaveSettings)
	mux.HandleFunc("GET /api/models", s.handleModels)
	mux.HandleFunc("GET /api/scoring", s.handleScoring)
	mux.HandleFunc("POST /api/scoring/run", s.handleRunScoring)
	mux.HandleFunc("POST /api/scoring/pause", s.handlePauseScoring)
	mux.HandleFunc("POST /api/scoring/resume", s.handleResumeScoring)
	mux.HandleFunc("POST /api/ingest/search", s.handleIngestSearch)
	mux.HandleFunc("POST /api/ingest/sold", s.handleIngestSold)
	mux.HandleFunc("POST /api/ingest/ad", s.handleIngestAd)
	mux.HandleFunc("POST /api/ingest/failed/{finnkode}", s.handleIngestFailed)
	mux.HandleFunc("GET /api/ingest/needed", s.handleNeeded)

	addr := env("LISTEN_ADDR", ":8080")
	log.Printf("listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, logRequests(mux)))
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if r.URL.Path != "/api/scoring" {
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

func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 20<<20)).Decode(v)
}

func pathFinnkode(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("finnkode"), 10, 64)
}

func (s *Server) autoTrigger() {
	if settings, err := s.db.settings(); err == nil && settings.AutoRescore {
		s.scorer.Trigger()
	}
}

func (s *Server) handleListings(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := ListingFilter{Status: q.Get("status"), Sort: q.Get("sort")}
	if d := strings.TrimSpace(q.Get("districts")); d != "" {
		f.Districts = strings.Split(d, ",")
	}
	f.PriceMin, _ = strconv.ParseInt(q.Get("price_min"), 10, 64)
	f.PriceMax, _ = strconv.ParseInt(q.Get("price_max"), 10, 64)
	listings, err := s.db.listListings(f)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	// The description is only needed on the detail page.
	for i := range listings {
		listings[i].Description = nil
	}
	writeJSON(w, 200, listings)
}

func (s *Server) handleListing(w http.ResponseWriter, r *http.Request) {
	id, err := pathFinnkode(r)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	l, err := s.db.getListing(id)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	if l == nil {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, 200, l)
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
		writeJSON(w, 400, map[string]string{"error": "could not find a finnkode in the input"})
		return
	}
	if err := s.db.addReference(id); err != nil {
		writeErr(w, 500, err)
		return
	}
	s.referencesChanged()
	l, _ := s.db.getListing(id)
	writeJSON(w, 200, l)
}

func (s *Server) handleRemoveReference(w http.ResponseWriter, r *http.Request) {
	id, err := pathFinnkode(r)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := s.db.removeReference(id); err != nil {
		writeErr(w, 500, err)
		return
	}
	s.referencesChanged()
	w.WriteHeader(204)
}

func (s *Server) referencesChanged() {
	if err := s.db.bumpVersion(); err != nil {
		log.Printf("bump version: %v", err)
	}
	s.autoTrigger()
}

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
		writeJSON(w, 400, map[string]string{"error": "prompt can not be empty"})
		return
	}
	if body.Model != "" && !validModel(body.Model) {
		writeJSON(w, 400, map[string]string{"error": "unknown model " + body.Model})
		return
	}
	if err := s.db.saveSettings(body); err != nil {
		writeErr(w, 500, err)
		return
	}
	s.autoTrigger()
	settings, _ := s.db.settings()
	writeJSON(w, 200, settings)
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	out := []ModelOption{}
	for _, m := range models {
		m.Default = m.ID == s.scorer.model
		out = append(out, m)
	}
	writeJSON(w, 200, out)
}

func (s *Server) handleScoring(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.scorer.Status())
}

func (s *Server) handleRunScoring(w http.ResponseWriter, r *http.Request) {
	s.scorer.Trigger()
	writeJSON(w, 200, s.scorer.Status())
}

func (s *Server) handlePauseScoring(w http.ResponseWriter, r *http.Request) {
	if err := s.scorer.Pause(); err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, s.scorer.Status())
}

func (s *Server) handleResumeScoring(w http.ResponseWriter, r *http.Request) {
	if err := s.scorer.Resume(); err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, s.scorer.Status())
}

func (s *Server) handleIngestSearch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Complete bool        `json:"complete"`
		Listings []ListingIn `json:"listings"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, err)
		return
	}
	seen := []int64{}
	for _, l := range body.Listings {
		if l.Finnkode == 0 {
			continue
		}
		if err := s.db.upsertListing(l, "active", true, false); err != nil {
			writeErr(w, 500, err)
			return
		}
		seen = append(seen, l.Finnkode)
	}
	if body.Complete {
		if err := s.db.markGone(seen); err != nil {
			writeErr(w, 500, err)
			return
		}
	}
	s.autoTrigger()
	writeJSON(w, 200, map[string]int{"stored": len(seen)})
}

func (s *Server) handleIngestSold(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Docs []struct {
			AdID int64   `json:"adId"`
			Lat  float64 `json:"lat"`
			Lon  float64 `json:"lon"`
		} `json:"docs"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, err)
		return
	}
	for _, d := range body.Docs {
		lat, lon := d.Lat, d.Lon
		in := ListingIn{Finnkode: d.AdID, Lat: &lat, Lon: &lon}
		if err := s.db.upsertListing(in, "sold", true, false); err != nil {
			writeErr(w, 500, err)
			return
		}
	}
	writeJSON(w, 200, map[string]int{"stored": len(body.Docs)})
}

func (s *Server) handleIngestAd(w http.ResponseWriter, r *http.Request) {
	var in ListingIn
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	if in.Finnkode == 0 {
		writeJSON(w, 400, map[string]string{"error": "finnkode is required"})
		return
	}
	before, _ := s.db.getListing(in.Finnkode)
	if err := s.db.upsertListing(in, "", false, true); err != nil {
		writeErr(w, 500, err)
		return
	}
	gotMoreImages := before == nil || len(in.Images) > len(before.Images)
	if s.db.isReference(in.Finnkode) && gotMoreImages {
		// A reference with new photos changes what Claude compares against.
		s.referencesChanged()
	} else {
		if gotMoreImages {
			// The old score was based on the few preview photos from the search results.
			if err := s.db.deleteScores(in.Finnkode); err != nil {
				log.Printf("delete scores: %v", err)
			}
		}
		s.autoTrigger()
	}
	l, _ := s.db.getListing(in.Finnkode)
	writeJSON(w, 200, l)
}

func (s *Server) handleIngestFailed(w http.ResponseWriter, r *http.Request) {
	id, err := pathFinnkode(r)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := s.db.markFetchFailed(id); err != nil {
		writeErr(w, 500, err)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) handleNeeded(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	ids, err := s.db.neededDetails(limit)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{"finnkodes": ids})
}
