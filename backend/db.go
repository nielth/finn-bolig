package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS ads (
	finnkode          INTEGER PRIMARY KEY,
	status            TEXT    NOT NULL DEFAULT 'active', -- active, sold, removed
	in_search         INTEGER NOT NULL DEFAULT 0,        -- found by the Stavanger crawl
	first_seen_at     TEXT    NOT NULL,
	last_seen_at      TEXT,                              -- last time it was in the search results
	status_changed_at TEXT,
	-- effective fields used for filtering and sorting (full set in fields)
	title             TEXT,
	address           TEXT,
	local_area        TEXT,
	district          TEXT,
	lat               REAL,
	lon               REAL,
	price             INTEGER,
	price_total       INTEGER,
	area              INTEGER,
	bedrooms          INTEGER,
	property_type     TEXT,
	fields            TEXT    NOT NULL DEFAULT '{}',     -- AdFields, search results merged with ad page
	images            TEXT    NOT NULL DEFAULT '[]',
	facts             TEXT    NOT NULL DEFAULT '[]',
	-- raw data, kept so we can re-parse when finn changes or the parser improves
	search_doc        TEXT,
	search_fields     TEXT    NOT NULL DEFAULT '{}',
	search_hash       TEXT,
	page_fields       TEXT    NOT NULL DEFAULT '{}',
	page_data         TEXT,
	page_html         BLOB,                              -- gzip
	page_url          TEXT,
	page_hash         TEXT,
	page_fetched_at   TEXT,
	page_attempts     INTEGER NOT NULL DEFAULT 0,
	needs_page        INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS ads_status ON ads (status, in_search);

CREATE TABLE IF NOT EXISTS ad_events (
	id       INTEGER PRIMARY KEY,
	finnkode INTEGER NOT NULL,
	at       TEXT    NOT NULL,
	kind     TEXT    NOT NULL, -- new, price_change, updated, sold, removed, relisted
	detail   TEXT
);
CREATE INDEX IF NOT EXISTS ad_events_finnkode ON ad_events (finnkode);

CREATE TABLE IF NOT EXISTS crawls (
	id              INTEGER PRIMARY KEY,
	started_at      TEXT    NOT NULL,
	heartbeat_at    TEXT    NOT NULL,
	search_done_at  TEXT,
	search_complete INTEGER NOT NULL DEFAULT 0,
	finished_at     TEXT,
	pages           INTEGER NOT NULL DEFAULT 0,
	found           INTEGER NOT NULL DEFAULT 0,
	pages_fetched   INTEGER NOT NULL DEFAULT 0,
	new_ads         INTEGER NOT NULL DEFAULT 0,
	updated_ads     INTEGER NOT NULL DEFAULT 0,
	sold_ads        INTEGER NOT NULL DEFAULT 0,
	removed_ads     INTEGER NOT NULL DEFAULT 0,
	error           TEXT
);

CREATE TABLE IF NOT EXISTS crawl_seen (
	crawl_id INTEGER NOT NULL,
	finnkode INTEGER NOT NULL,
	PRIMARY KEY (crawl_id, finnkode)
);

CREATE TABLE IF NOT EXISTS refs (
	finnkode INTEGER PRIMARY KEY,
	added_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS settings (
	id           INTEGER PRIMARY KEY CHECK (id = 1),
	prompt       TEXT    NOT NULL,
	version      INTEGER NOT NULL,
	auto_rescore INTEGER NOT NULL DEFAULT 1
);
`

const defaultPrompt = `I want a mid-century style home (1950s-1970s) with wood panels, exposed brick, teak and warm natural materials.`

// A crawl that has not sent anything for this long is considered dead.
const crawlTimeout = 3 * time.Minute

// Ads are re-fetched after this long even if the search results look unchanged.
const pageMaxAge = 7 * 24 * time.Hour

type DB struct {
	*sql.DB
}

func openDB(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("create schema: %w", err)
	}
	_, err = db.Exec(`INSERT OR IGNORE INTO settings (id, prompt, version) VALUES (1, ?, 1)`, defaultPrompt)
	if err != nil {
		return nil, err
	}
	// Columns added after the first release, so older databases need them.
	for _, col := range []string{
		"paused INTEGER NOT NULL DEFAULT 0",
		"model TEXT NOT NULL DEFAULT ''",
	} {
		_, err = db.Exec("ALTER TABLE settings ADD COLUMN " + col)
		if err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return nil, err
		}
	}
	// Every favourite needs an ad row, so the crawler fetches its page.
	_, err = db.Exec(`INSERT OR IGNORE INTO ads (finnkode, status, first_seen_at, status_changed_at)
		SELECT finnkode, 'active', added_at, added_at FROM refs`)
	if err != nil {
		return nil, err
	}
	return &DB{db}, nil
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func hashOf(parts ...any) string {
	b, _ := json.Marshal(parts)
	sum := sha1.Sum(b)
	return hex.EncodeToString(sum[:])
}

func gzipBytes(s string) []byte {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	_, _ = w.Write([]byte(s))
	_ = w.Close()
	return buf.Bytes()
}

func gunzipString(b []byte) (string, error) {
	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	out, err := io.ReadAll(r)
	return string(out), err
}

func addEvent(tx *sql.Tx, finnkode int64, kind, detail string) error {
	var d any
	if detail != "" {
		d = detail
	}
	_, err := tx.Exec("INSERT INTO ad_events (finnkode, at, kind, detail) VALUES (?, ?, ?, ?)", finnkode, now(), kind, d)
	return err
}

// ---------- crawls ----------

type Crawl struct {
	ID             int64   `json:"id"`
	StartedAt      string  `json:"started_at"`
	HeartbeatAt    string  `json:"heartbeat_at"`
	SearchDoneAt   *string `json:"search_done_at"`
	SearchComplete bool    `json:"search_complete"`
	FinishedAt     *string `json:"finished_at"`
	Pages          int     `json:"pages"`
	Found          int     `json:"found"`
	PagesFetched   int     `json:"pages_fetched"`
	NewAds         int     `json:"new_ads"`
	UpdatedAds     int     `json:"updated_ads"`
	SoldAds        int     `json:"sold_ads"`
	RemovedAds     int     `json:"removed_ads"`
	Error          *string `json:"error"`
}

const crawlColumns = `id, started_at, heartbeat_at, search_done_at, search_complete, finished_at, pages, found,
	pages_fetched, new_ads, updated_ads, sold_ads, removed_ads, error`

func scanCrawl(row interface{ Scan(...any) error }) (Crawl, error) {
	var c Crawl
	var complete int
	err := row.Scan(&c.ID, &c.StartedAt, &c.HeartbeatAt, &c.SearchDoneAt, &complete, &c.FinishedAt, &c.Pages,
		&c.Found, &c.PagesFetched, &c.NewAds, &c.UpdatedAds, &c.SoldAds, &c.RemovedAds, &c.Error)
	c.SearchComplete = complete == 1
	return c, err
}

func (db *DB) crawls(limit int) ([]Crawl, error) {
	rows, err := db.Query("SELECT "+crawlColumns+" FROM crawls ORDER BY id DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Crawl{}
	for rows.Next() {
		c, err := scanCrawl(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (db *DB) oneCrawl(q string, args ...any) (*Crawl, error) {
	c, err := scanCrawl(db.QueryRow("SELECT "+crawlColumns+" FROM crawls "+q, args...))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (db *DB) crawl(id int64) (*Crawl, error) {
	return db.oneCrawl("WHERE id = ?", id)
}

// runningCrawl returns an unfinished crawl that is still sending heartbeats.
func (db *DB) runningCrawl() (*Crawl, error) {
	cutoff := time.Now().Add(-crawlTimeout).UTC().Format(time.RFC3339)
	return db.oneCrawl("WHERE finished_at IS NULL AND heartbeat_at > ? ORDER BY id DESC LIMIT 1", cutoff)
}

func (db *DB) lastCompleteCrawl() (*Crawl, error) {
	return db.oneCrawl("WHERE finished_at IS NOT NULL AND search_complete = 1 ORDER BY id DESC LIMIT 1")
}

func (db *DB) startCrawl() (int64, error) {
	t := now()
	res, err := db.Exec("INSERT INTO crawls (started_at, heartbeat_at) VALUES (?, ?)", t, t)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (db *DB) bumpCrawl(crawlID int64, column string, n int) {
	_, _ = db.Exec(fmt.Sprintf("UPDATE crawls SET %s = %s + ?, heartbeat_at = ? WHERE id = ?", column, column), n, now(), crawlID)
}

func (db *DB) finishSearch(crawlID int64, complete bool) error {
	_, err := db.Exec(`UPDATE crawls SET search_done_at = ?, search_complete = ?, heartbeat_at = ?,
		found = (SELECT COUNT(*) FROM crawl_seen WHERE crawl_id = ?) WHERE id = ?`,
		now(), boolInt(complete), now(), crawlID, crawlID)
	return err
}

func (db *DB) finishCrawl(crawlID int64, errMsg string) error {
	var e any
	if errMsg != "" {
		e = errMsg
	}
	_, err := db.Exec("UPDATE crawls SET finished_at = ?, heartbeat_at = ?, error = ? WHERE id = ?", now(), now(), e, crawlID)
	return err
}

// ---------- ingest ----------

// storeSearchDoc stores one ad from the search results. It returns true if the ad is new.
func (db *DB) storeSearchDoc(crawlID int64, doc map[string]any, firstCrawl bool) (bool, error) {
	finnkode := searchDocFinnkode(doc)
	if finnkode == 0 {
		return false, nil
	}
	fields := fieldsFromSearchDoc(doc)
	docJSON, _ := json.Marshal(doc)
	fieldsJSON, _ := json.Marshal(fields)
	hash := hashOf(fields)

	tx, err := db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("INSERT OR IGNORE INTO crawl_seen (crawl_id, finnkode) VALUES (?, ?)", crawlID, finnkode); err != nil {
		return false, err
	}

	var status string
	var oldHash sql.NullString
	var oldPrice sql.NullInt64
	var inSearch int
	err = tx.QueryRow("SELECT status, search_hash, price, in_search FROM ads WHERE finnkode = ?", finnkode).
		Scan(&status, &oldHash, &oldPrice, &inSearch)
	isNew := err == sql.ErrNoRows
	if err != nil && !isNew {
		return false, err
	}
	t := now()
	if isNew {
		_, err = tx.Exec(`INSERT INTO ads (finnkode, status, in_search, first_seen_at, last_seen_at, status_changed_at,
			search_doc, search_fields, search_hash, needs_page) VALUES (?, 'active', 1, ?, ?, ?, ?, ?, ?, 1)`,
			finnkode, t, t, t, string(docJSON), string(fieldsJSON), hash)
		if err != nil {
			return false, err
		}
		if !firstCrawl {
			if err := addEvent(tx, finnkode, "new", ""); err != nil {
				return false, err
			}
		}
	} else {
		changed := !oldHash.Valid || oldHash.String != hash
		_, err = tx.Exec(`UPDATE ads SET in_search = 1, last_seen_at = ?, search_doc = ?, search_fields = ?, search_hash = ?,
			needs_page = CASE WHEN ? THEN 1 ELSE needs_page END,
			status_changed_at = CASE WHEN status != 'active' THEN ? ELSE status_changed_at END,
			status = 'active'
			WHERE finnkode = ?`, t, string(docJSON), string(fieldsJSON), hash, changed, t, finnkode)
		if err != nil {
			return false, err
		}
		if status != "active" && inSearch == 1 {
			if err := addEvent(tx, finnkode, "relisted", ""); err != nil {
				return false, err
			}
		}
		if fields.Price != nil && oldPrice.Valid && oldPrice.Int64 != *fields.Price {
			detail := fmt.Sprintf("%d → %d", oldPrice.Int64, *fields.Price)
			if err := addEvent(tx, finnkode, "price_change", detail); err != nil {
				return false, err
			}
		}
	}
	if err := refreshFields(tx, finnkode); err != nil {
		return false, err
	}
	return isNew, tx.Commit()
}

// storePage stores a fetched ad page. httpStatus is the status finn returned.
// It returns the ad's status afterwards and whether the page content changed since last time.
func (db *DB) storePage(finnkode int64, httpStatus int, url, page string) (status string, updated bool, err error) {
	tx, err := db.Begin()
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback()

	var oldStatus string
	var oldHash sql.NullString
	var inSearch int
	err = tx.QueryRow("SELECT status, page_hash, in_search FROM ads WHERE finnkode = ?", finnkode).Scan(&oldStatus, &oldHash, &inSearch)
	if err == sql.ErrNoRows {
		// An ad outside the crawl, e.g. a favourite somewhere else.
		t := now()
		_, err = tx.Exec("INSERT INTO ads (finnkode, status, first_seen_at, status_changed_at) VALUES (?, 'active', ?, ?)", finnkode, t, t)
		oldStatus = "active"
	}
	if err != nil {
		return "", false, err
	}

	// An active ad that was missing from the latest complete search is sold or removed.
	disappeared := inSearch == 1 && oldStatus == "active" && missingFromLastSearch(tx, finnkode)
	newStatus := oldStatus

	if httpStatus == 404 || httpStatus == 410 {
		if oldStatus == "active" {
			newStatus = "removed"
		}
		_, err = tx.Exec("UPDATE ads SET needs_page = 0, page_fetched_at = ? WHERE finnkode = ?", now(), finnkode)
		if err != nil {
			return "", false, err
		}
	} else {
		parsed := parseAdPage(page, finnkode)
		pageFields, _ := json.Marshal(parsed.Fields)
		images, _ := json.Marshal(nonNil(parsed.Images))
		facts, _ := json.Marshal(nonNilFacts(parsed.Facts))
		data, _ := json.Marshal(parsed.Data)
		hash := hashOf(parsed.Fields.Description, parsed.Fields.Title, parsed.Images, parsed.Fields.Price)
		updated = oldHash.Valid && oldHash.String != hash
		_, err = tx.Exec(`UPDATE ads SET page_fields = ?, images = ?, facts = ?, page_data = ?, page_html = ?, page_url = ?,
			page_hash = ?, page_fetched_at = ?, needs_page = 0, page_attempts = 0 WHERE finnkode = ?`,
			string(pageFields), string(images), string(facts), string(data), gzipBytes(page), url, hash, now(), finnkode)
		if err != nil {
			return "", false, err
		}
		if updated {
			if err := addEvent(tx, finnkode, "updated", ""); err != nil {
				return "", false, err
			}
		}
		switch {
		case parsed.Sold:
			newStatus = "sold"
		case disappeared:
			newStatus = "removed"
		}
		if err := refreshFields(tx, finnkode); err != nil {
			return "", false, err
		}
	}

	if newStatus != oldStatus {
		_, err = tx.Exec("UPDATE ads SET status = ?, status_changed_at = ? WHERE finnkode = ?", newStatus, now(), finnkode)
		if err != nil {
			return "", false, err
		}
		if err := addEvent(tx, finnkode, newStatus, ""); err != nil {
			return "", false, err
		}
	}
	return newStatus, updated, tx.Commit()
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func nonNilFacts(s []Fact) []Fact {
	if s == nil {
		return []Fact{}
	}
	return s
}

func (db *DB) markPageFailed(finnkode int64) error {
	_, err := db.Exec("UPDATE ads SET page_attempts = page_attempts + 1 WHERE finnkode = ?", finnkode)
	return err
}

// missingFromLastSearch is true if the latest complete search did not include the ad.
func missingFromLastSearch(tx *sql.Tx, finnkode int64) bool {
	var crawlID int64
	err := tx.QueryRow("SELECT id FROM crawls WHERE search_complete = 1 ORDER BY id DESC LIMIT 1").Scan(&crawlID)
	if err != nil {
		return false
	}
	var n int
	_ = tx.QueryRow("SELECT COUNT(*) FROM crawl_seen WHERE crawl_id = ? AND finnkode = ?", crawlID, finnkode).Scan(&n)
	return n == 0
}

// refreshFields recomputes the effective fields: the search results are the most reliable source,
// and the ad page fills in everything else.
func refreshFields(tx *sql.Tx, finnkode int64) error {
	var searchJSON, pageJSON string
	if err := tx.QueryRow("SELECT search_fields, page_fields FROM ads WHERE finnkode = ?", finnkode).Scan(&searchJSON, &pageJSON); err != nil {
		return err
	}
	var search, page AdFields
	_ = json.Unmarshal([]byte(searchJSON), &search)
	_ = json.Unmarshal([]byte(pageJSON), &page)
	f := search
	f.merge(page, false)
	var district *string
	if f.Lat != nil && f.Lon != nil {
		if d := districtFor(*f.Lat, *f.Lon); d != "" {
			district = &d
		}
	}
	fields, _ := json.Marshal(f)
	_, err := tx.Exec(`UPDATE ads SET fields = ?, title = ?, address = ?, local_area = ?, district = ?, lat = ?, lon = ?,
		price = ?, price_total = ?, area = ?, bedrooms = ?, property_type = ? WHERE finnkode = ?`,
		string(fields), f.Title, f.Address, f.LocalArea, district, f.Lat, f.Lon, f.Price, f.PriceTotal, f.Area,
		f.Bedrooms, f.PropertyType, finnkode)
	return err
}

// todo returns the ads the crawler should fetch pages for, most important first.
func (db *DB) todo(crawlID int64, limit int) ([]int64, error) {
	staleBefore := time.Now().Add(-pageMaxAge).UTC().Format(time.RFC3339)
	rows, err := db.Query(`
		SELECT a.finnkode,
			CASE
				WHEN r.finnkode IS NOT NULL AND a.page_fetched_at IS NULL THEN 0  -- favourites without data
				WHEN a.in_search = 1 AND a.status = 'active' AND s.finnkode IS NULL THEN 1  -- gone from search: sold?
				WHEN a.needs_page = 1 THEN 2  -- new or changed
				ELSE 3  -- stale
			END AS priority
		FROM ads a
		LEFT JOIN refs r ON r.finnkode = a.finnkode
		LEFT JOIN crawl_seen s ON s.finnkode = a.finnkode AND s.crawl_id = ?
		WHERE a.page_attempts < 3 AND (
			(r.finnkode IS NOT NULL AND a.page_fetched_at IS NULL)
			OR (a.in_search = 1 AND a.status = 'active' AND s.finnkode IS NULL
				AND (SELECT search_complete FROM crawls WHERE id = ?) = 1)
			OR (a.status = 'active' AND (a.in_search = 1 OR r.finnkode IS NOT NULL)
				AND (a.needs_page = 1 OR a.page_fetched_at IS NULL OR a.page_fetched_at < ?))
		)
		ORDER BY priority, a.finnkode DESC
		LIMIT ?`, crawlID, crawlID, staleBefore, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		var priority int
		if err := rows.Scan(&id, &priority); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// reparseAll re-parses every stored ad page, e.g. after the parser was improved.
func (db *DB) reparseAll() (int, error) {
	rows, err := db.Query("SELECT finnkode, page_html FROM ads WHERE page_html IS NOT NULL")
	if err != nil {
		return 0, err
	}
	type item struct {
		id   int64
		html []byte
	}
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.html); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, it)
	}
	rows.Close()

	for _, it := range items {
		page, err := gunzipString(it.html)
		if err != nil {
			continue
		}
		parsed := parseAdPage(page, it.id)
		pageFields, _ := json.Marshal(parsed.Fields)
		images, _ := json.Marshal(nonNil(parsed.Images))
		facts, _ := json.Marshal(nonNilFacts(parsed.Facts))
		data, _ := json.Marshal(parsed.Data)
		tx, err := db.Begin()
		if err != nil {
			return 0, err
		}
		_, err = tx.Exec("UPDATE ads SET page_fields = ?, images = ?, facts = ?, page_data = ? WHERE finnkode = ?",
			string(pageFields), string(images), string(facts), string(data), it.id)
		if err == nil {
			err = refreshFields(tx, it.id)
		}
		if err != nil {
			tx.Rollback()
			return 0, err
		}
		if err := tx.Commit(); err != nil {
			return 0, err
		}
	}
	return len(items), nil
}

// ---------- reading ads ----------

type Event struct {
	At     string  `json:"at"`
	Kind   string  `json:"kind"`
	Detail *string `json:"detail"`
}

type Ad struct {
	AdFields
	Finnkode        int64    `json:"finnkode"`
	Status          string   `json:"status"`
	InSearch        bool     `json:"in_search"`
	FirstSeenAt     string   `json:"first_seen_at"`
	LastSeenAt      *string  `json:"last_seen_at"`
	StatusChangedAt *string  `json:"status_changed_at"`
	District        *string  `json:"district"`
	Images          []string `json:"images"`
	Facts           []Fact   `json:"facts,omitempty"`
	PageFetchedAt   *string  `json:"page_fetched_at"`
	IsReference     bool     `json:"is_reference"`
	LastEvent       *Event   `json:"last_event"`
	Events          []Event  `json:"events,omitempty"`
}

const adColumns = `a.finnkode, a.status, a.in_search, a.first_seen_at, a.last_seen_at, a.status_changed_at, a.district,
	a.fields, a.images, a.facts, a.page_fetched_at, r.finnkode IS NOT NULL,
	e.at, e.kind, e.detail`

// The latest event other than routine page updates.
const adJoins = `FROM ads a
	LEFT JOIN refs r ON r.finnkode = a.finnkode
	LEFT JOIN ad_events e ON e.id = (SELECT MAX(id) FROM ad_events x WHERE x.finnkode = a.finnkode AND x.kind != 'updated')`

func scanAd(row interface{ Scan(...any) error }) (Ad, error) {
	var a Ad
	var inSearch int
	var fields, images, facts string
	var evAt, evKind, evDetail sql.NullString
	err := row.Scan(&a.Finnkode, &a.Status, &inSearch, &a.FirstSeenAt, &a.LastSeenAt, &a.StatusChangedAt, &a.District,
		&fields, &images, &facts, &a.PageFetchedAt, &a.IsReference, &evAt, &evKind, &evDetail)
	if err != nil {
		return a, err
	}
	a.InSearch = inSearch == 1
	_ = json.Unmarshal([]byte(fields), &a.AdFields)
	_ = json.Unmarshal([]byte(images), &a.Images)
	_ = json.Unmarshal([]byte(facts), &a.Facts)
	if a.Images == nil {
		a.Images = []string{}
	}
	if evKind.Valid {
		a.LastEvent = &Event{At: evAt.String, Kind: evKind.String}
		if evDetail.Valid {
			a.LastEvent.Detail = &evDetail.String
		}
	}
	return a, nil
}

func (db *DB) queryAds(q string, args ...any) ([]Ad, error) {
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Ad{}
	for rows.Next() {
		a, err := scanAd(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

type AdFilter struct {
	Status    string // active, sold, all
	Districts []string
	PriceMin  int64
	PriceMax  int64
	Sort      string // newest, price_asc, price_desc, area_desc
}

func (db *DB) listAds(f AdFilter) ([]Ad, error) {
	where := []string{"a.in_search = 1"}
	args := []any{}
	switch f.Status {
	case "sold":
		where = append(where, "a.status = 'sold'")
	case "all":
		where = append(where, "a.status IN ('active', 'sold')")
	default:
		where = append(where, "a.status = 'active'")
	}
	if len(f.Districts) > 0 {
		where = append(where, "a.district IN (?"+strings.Repeat(",?", len(f.Districts)-1)+")")
		for _, d := range f.Districts {
			args = append(args, d)
		}
	}
	price := "COALESCE(a.price_total, a.price)"
	if f.PriceMin > 0 {
		where = append(where, price+" >= ?")
		args = append(args, f.PriceMin)
	}
	if f.PriceMax > 0 {
		where = append(where, price+" <= ?")
		args = append(args, f.PriceMax)
	}
	order := "a.finnkode DESC"
	switch f.Sort {
	case "price_asc":
		order = price + " IS NULL, " + price + " ASC"
	case "price_desc":
		order = price + " DESC"
	case "area_desc":
		order = "a.area IS NULL, a.area DESC"
	}
	return db.queryAds("SELECT "+adColumns+" "+adJoins+" WHERE "+strings.Join(where, " AND ")+" ORDER BY "+order, args...)
}

func (db *DB) getAd(finnkode int64) (*Ad, error) {
	a, err := scanAd(db.QueryRow("SELECT "+adColumns+" "+adJoins+" WHERE a.finnkode = ?", finnkode))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := db.Query("SELECT at, kind, detail FROM ad_events WHERE finnkode = ? ORDER BY id DESC", finnkode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.At, &e.Kind, &e.Detail); err != nil {
			return nil, err
		}
		a.Events = append(a.Events, e)
	}
	return &a, rows.Err()
}

func (db *DB) adPageData(finnkode int64) (string, error) {
	var data sql.NullString
	err := db.QueryRow("SELECT page_data FROM ads WHERE finnkode = ?", finnkode).Scan(&data)
	return data.String, err
}

func (db *DB) references() ([]Ad, error) {
	return db.queryAds("SELECT " + adColumns + " " + adJoins + " JOIN refs rr ON rr.finnkode = a.finnkode ORDER BY rr.added_at DESC")
}

func (db *DB) addReference(finnkode int64) error {
	t := now()
	_, err := db.Exec("INSERT OR IGNORE INTO ads (finnkode, status, first_seen_at, status_changed_at) VALUES (?, 'active', ?, ?)", finnkode, t, t)
	if err != nil {
		return err
	}
	_, err = db.Exec("INSERT OR IGNORE INTO refs (finnkode, added_at) VALUES (?, ?)", finnkode, t)
	return err
}

func (db *DB) removeReference(finnkode int64) error {
	_, err := db.Exec("DELETE FROM refs WHERE finnkode = ?", finnkode)
	return err
}

func (db *DB) districtCounts() (map[string]int, error) {
	rows, err := db.Query(`SELECT district, COUNT(*) FROM ads
		WHERE in_search = 1 AND district IS NOT NULL AND status IN ('active', 'sold') GROUP BY district`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var d string
		var n int
		if err := rows.Scan(&d, &n); err != nil {
			return nil, err
		}
		out[d] = n
	}
	return out, rows.Err()
}

// ---------- settings ----------

type Settings struct {
	Prompt string `json:"prompt"`
}

func (db *DB) settings() (Settings, error) {
	var s Settings
	err := db.QueryRow("SELECT prompt FROM settings WHERE id = 1").Scan(&s.Prompt)
	return s, err
}

func (db *DB) savePrompt(prompt string) error {
	_, err := db.Exec("UPDATE settings SET prompt = ?, version = version + 1 WHERE id = 1", prompt)
	return err
}
