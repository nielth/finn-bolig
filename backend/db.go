package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS listings (
	finnkode        INTEGER PRIMARY KEY,
	status          TEXT    NOT NULL DEFAULT 'unknown', -- active, sold, gone, unknown
	in_feed         INTEGER NOT NULL DEFAULT 0,          -- shown in the main list
	title           TEXT,
	address         TEXT,
	local_area      TEXT,
	district        TEXT,
	price           INTEGER,
	price_total     INTEGER,
	area            INTEGER,
	bedrooms        INTEGER,
	property_type   TEXT,
	lat             REAL,
	lon             REAL,
	url             TEXT,
	images          TEXT    NOT NULL DEFAULT '[]',
	description     TEXT,
	details_fetched INTEGER NOT NULL DEFAULT 0,
	fetch_attempts  INTEGER NOT NULL DEFAULT 0,
	first_seen      TEXT    NOT NULL,
	updated_at      TEXT    NOT NULL
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
CREATE TABLE IF NOT EXISTS scores (
	finnkode   INTEGER NOT NULL,
	version    INTEGER NOT NULL,
	score      INTEGER,
	style      TEXT,
	summary    TEXT,
	error      TEXT,
	created_at TEXT NOT NULL,
	PRIMARY KEY (finnkode, version)
);
`

const defaultPrompt = `I am looking for a home with a modern, funky architectural style.
I care most about the look of the exterior and the interior (materials, light, layout, character),
not about what the ad text says.`

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
	return &DB{db}, nil
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// ListingIn is what the userscript sends. Nil/empty fields do not overwrite stored values.
type ListingIn struct {
	Finnkode     int64    `json:"finnkode"`
	Title        *string  `json:"title"`
	Address      *string  `json:"address"`
	LocalArea    *string  `json:"local_area"`
	Price        *int64   `json:"price"`
	PriceTotal   *int64   `json:"price_total"`
	Area         *int64   `json:"area"`
	Bedrooms     *int64   `json:"bedrooms"`
	PropertyType *string  `json:"property_type"`
	Lat          *float64 `json:"lat"`
	Lon          *float64 `json:"lon"`
	URL          *string  `json:"url"`
	Images       []string `json:"images"`
	Description  *string  `json:"description"`
	Sold         bool     `json:"sold"`
}

type Listing struct {
	Finnkode     int64    `json:"finnkode"`
	Status       string   `json:"status"`
	InFeed       bool     `json:"in_feed"`
	Title        *string  `json:"title"`
	Address      *string  `json:"address"`
	LocalArea    *string  `json:"local_area"`
	District     *string  `json:"district"`
	Price        *int64   `json:"price"`
	PriceTotal   *int64   `json:"price_total"`
	Area         *int64   `json:"area"`
	Bedrooms     *int64   `json:"bedrooms"`
	PropertyType *string  `json:"property_type"`
	Lat          *float64 `json:"lat"`
	Lon          *float64 `json:"lon"`
	URL          *string  `json:"url"`
	Images       []string `json:"images"`
	Description  *string  `json:"description,omitempty"`
	Details      bool     `json:"details_fetched"`
	FirstSeen    string   `json:"first_seen"`
	IsReference  bool     `json:"is_reference"`
	Score        *int64   `json:"score"`
	Style        *string  `json:"style"`
	Summary      *string  `json:"summary"`
	ScoreError   *string  `json:"score_error"`
}

func nullIfEmpty(s *string) *string {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	t := strings.TrimSpace(*s)
	return &t
}

// upsertListing inserts or merges a listing. status may be "" to leave it unchanged.
func (db *DB) upsertListing(in ListingIn, status string, inFeed bool, details bool) error {
	var images *string
	if len(in.Images) > 0 {
		b, _ := json.Marshal(in.Images)
		s := string(b)
		images = &s
	}
	var district *string
	if in.Lat != nil && in.Lon != nil {
		d := districtFor(*in.Lat, *in.Lon)
		if d != "" {
			district = &d
		}
	}
	if in.Sold {
		status = "sold"
	}
	var statusArg any
	if status != "" {
		statusArg = status
	}
	t := now()
	_, err := db.Exec(`
INSERT INTO listings (finnkode, status, in_feed, title, address, local_area, district, price, price_total,
	area, bedrooms, property_type, lat, lon, url, images, description, details_fetched, first_seen, updated_at)
VALUES (?, COALESCE(?, 'unknown'), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, COALESCE(?, '[]'), ?, ?, ?, ?)
ON CONFLICT (finnkode) DO UPDATE SET
	-- a sold listing never goes back to active
	status          = CASE WHEN listings.status = 'sold' THEN 'sold' ELSE COALESCE(?, listings.status) END,
	in_feed         = MAX(listings.in_feed, excluded.in_feed),
	title           = COALESCE(excluded.title, listings.title),
	address         = COALESCE(excluded.address, listings.address),
	local_area      = COALESCE(excluded.local_area, listings.local_area),
	district        = COALESCE(excluded.district, listings.district),
	price           = COALESCE(excluded.price, listings.price),
	price_total     = COALESCE(excluded.price_total, listings.price_total),
	area            = COALESCE(excluded.area, listings.area),
	bedrooms        = COALESCE(excluded.bedrooms, listings.bedrooms),
	property_type   = COALESCE(excluded.property_type, listings.property_type),
	lat             = COALESCE(excluded.lat, listings.lat),
	lon             = COALESCE(excluded.lon, listings.lon),
	url             = COALESCE(excluded.url, listings.url),
	images          = CASE WHEN ? IS NULL THEN listings.images ELSE excluded.images END,
	description     = COALESCE(excluded.description, listings.description),
	details_fetched = MAX(listings.details_fetched, excluded.details_fetched),
	updated_at      = excluded.updated_at`,
		in.Finnkode, statusArg, boolInt(inFeed), nullIfEmpty(in.Title), nullIfEmpty(in.Address),
		nullIfEmpty(in.LocalArea), district, in.Price, in.PriceTotal, in.Area, in.Bedrooms,
		nullIfEmpty(in.PropertyType), in.Lat, in.Lon, nullIfEmpty(in.URL), images,
		nullIfEmpty(in.Description), boolInt(details), t, t,
		statusArg, images)
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

const listingColumns = `l.finnkode, l.status, l.in_feed, l.title, l.address, l.local_area, l.district, l.price,
	l.price_total, l.area, l.bedrooms, l.property_type, l.lat, l.lon, l.url, l.images, l.description,
	l.details_fetched, l.first_seen, r.finnkode IS NOT NULL, s.score, s.style, s.summary, s.error`

const listingJoins = `FROM listings l
	LEFT JOIN refs r ON r.finnkode = l.finnkode
	LEFT JOIN scores s ON s.finnkode = l.finnkode AND s.version = (SELECT version FROM settings WHERE id = 1)`

func scanListing(rows interface{ Scan(...any) error }) (Listing, error) {
	var l Listing
	var images string
	var inFeed, details int
	err := rows.Scan(&l.Finnkode, &l.Status, &inFeed, &l.Title, &l.Address, &l.LocalArea, &l.District,
		&l.Price, &l.PriceTotal, &l.Area, &l.Bedrooms, &l.PropertyType, &l.Lat, &l.Lon, &l.URL, &images,
		&l.Description, &details, &l.FirstSeen, &l.IsReference, &l.Score, &l.Style, &l.Summary, &l.ScoreError)
	if err != nil {
		return l, err
	}
	l.InFeed = inFeed == 1
	l.Details = details == 1
	if err := json.Unmarshal([]byte(images), &l.Images); err != nil || l.Images == nil {
		l.Images = []string{}
	}
	return l, nil
}

type ListingFilter struct {
	Status    string // active, sold, all
	Districts []string
	PriceMin  int64
	PriceMax  int64
	Sort      string // score, price_asc, price_desc, newest
}

func (db *DB) listListings(f ListingFilter) ([]Listing, error) {
	where := []string{"l.in_feed = 1"}
	args := []any{}
	switch f.Status {
	case "sold":
		where = append(where, "l.status = 'sold'")
	case "all":
		where = append(where, "l.status IN ('active', 'sold')")
	default:
		where = append(where, "l.status = 'active'")
	}
	if len(f.Districts) > 0 {
		where = append(where, "l.district IN (?"+strings.Repeat(",?", len(f.Districts)-1)+")")
		for _, d := range f.Districts {
			args = append(args, d)
		}
	}
	price := "COALESCE(l.price_total, l.price)"
	if f.PriceMin > 0 {
		where = append(where, price+" >= ?")
		args = append(args, f.PriceMin)
	}
	if f.PriceMax > 0 {
		where = append(where, price+" <= ?")
		args = append(args, f.PriceMax)
	}
	order := "s.score IS NULL, s.score DESC, l.finnkode DESC"
	switch f.Sort {
	case "price_asc":
		order = price + " IS NULL, " + price + " ASC"
	case "price_desc":
		order = price + " DESC"
	case "newest":
		order = "l.finnkode DESC"
	}
	q := "SELECT " + listingColumns + " " + listingJoins + " WHERE " + strings.Join(where, " AND ") + " ORDER BY " + order
	return db.queryListings(q, args...)
}

func (db *DB) queryListings(q string, args ...any) ([]Listing, error) {
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Listing{}
	for rows.Next() {
		l, err := scanListing(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (db *DB) getListing(finnkode int64) (*Listing, error) {
	row := db.QueryRow("SELECT "+listingColumns+" "+listingJoins+" WHERE l.finnkode = ?", finnkode)
	l, err := scanListing(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

func (db *DB) references() ([]Listing, error) {
	return db.queryListings("SELECT " + listingColumns + " " + listingJoins + " WHERE r.finnkode IS NOT NULL ORDER BY r.added_at DESC")
}

func (db *DB) isReference(finnkode int64) bool {
	var n int
	_ = db.QueryRow("SELECT COUNT(*) FROM refs WHERE finnkode = ?", finnkode).Scan(&n)
	return n > 0
}

func (db *DB) addReference(finnkode int64) error {
	if err := db.upsertListing(ListingIn{Finnkode: finnkode}, "", false, false); err != nil {
		return err
	}
	_, err := db.Exec("INSERT OR IGNORE INTO refs (finnkode, added_at) VALUES (?, ?)", finnkode, now())
	return err
}

func (db *DB) removeReference(finnkode int64) error {
	_, err := db.Exec("DELETE FROM refs WHERE finnkode = ?", finnkode)
	return err
}

type Settings struct {
	Prompt      string `json:"prompt"`
	Version     int64  `json:"version"`
	AutoRescore bool   `json:"auto_rescore"`
}

func (db *DB) settings() (Settings, error) {
	var s Settings
	var auto int
	err := db.QueryRow("SELECT prompt, version, auto_rescore FROM settings WHERE id = 1").Scan(&s.Prompt, &s.Version, &auto)
	s.AutoRescore = auto == 1
	return s, err
}

func (db *DB) saveSettings(prompt string, autoRescore bool) error {
	_, err := db.Exec("UPDATE settings SET prompt = ?, auto_rescore = ?, version = version + 1 WHERE id = 1", prompt, boolInt(autoRescore))
	return err
}

// bumpVersion invalidates all scores, e.g. when references change.
func (db *DB) bumpVersion() error {
	_, err := db.Exec("UPDATE settings SET version = version + 1 WHERE id = 1")
	return err
}

// unscored returns feed listings with images that have no score for the given version.
func (db *DB) unscored(version int64) ([]Listing, error) {
	return db.queryListings(`SELECT `+listingColumns+` `+listingJoins+`
		WHERE l.in_feed = 1 AND l.status IN ('active', 'sold') AND l.images != '[]'
		AND NOT EXISTS (SELECT 1 FROM scores x WHERE x.finnkode = l.finnkode AND x.version = ?)
		ORDER BY l.status = 'sold', l.finnkode DESC`, version)
}

func (db *DB) saveScore(finnkode, version int64, score *int64, style, summary, errMsg *string) error {
	_, err := db.Exec(`INSERT OR REPLACE INTO scores (finnkode, version, score, style, summary, error, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, finnkode, version, score, style, summary, errMsg, now())
	return err
}

func (db *DB) scoreCounts(version int64) (scored, total int, err error) {
	err = db.QueryRow(`SELECT
		(SELECT COUNT(*) FROM scores s JOIN listings l ON l.finnkode = s.finnkode
			WHERE s.version = ? AND l.in_feed = 1 AND l.status IN ('active', 'sold')),
		(SELECT COUNT(*) FROM listings WHERE in_feed = 1 AND status IN ('active', 'sold') AND images != '[]')`,
		version).Scan(&scored, &total)
	return
}

// markGone marks active feed listings not in the given set as gone (after a complete search sync).
func (db *DB) markGone(seen []int64) error {
	if len(seen) == 0 {
		return nil
	}
	q := "UPDATE listings SET status = 'gone', updated_at = ? WHERE status = 'active' AND in_feed = 1 AND finnkode NOT IN (?" +
		strings.Repeat(",?", len(seen)-1) + ")"
	args := []any{now()}
	for _, id := range seen {
		args = append(args, id)
	}
	_, err := db.Exec(q, args...)
	return err
}

// neededDetails returns finnkodes the userscript should fetch ad pages for, most important first.
func (db *DB) neededDetails(limit int) ([]int64, error) {
	rows, err := db.Query(`SELECT l.finnkode FROM listings l LEFT JOIN refs r ON r.finnkode = l.finnkode
		WHERE l.details_fetched = 0 AND l.fetch_attempts < 3 AND (r.finnkode IS NOT NULL OR l.in_feed = 1)
		ORDER BY r.finnkode IS NULL, l.status = 'sold', l.finnkode DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (db *DB) markFetchFailed(finnkode int64) error {
	_, err := db.Exec("UPDATE listings SET fetch_attempts = fetch_attempts + 1 WHERE finnkode = ?", finnkode)
	return err
}

func (db *DB) districtCounts() (map[string]int, error) {
	rows, err := db.Query(`SELECT district, COUNT(*) FROM listings
		WHERE in_feed = 1 AND district IS NOT NULL AND status IN ('active', 'sold') GROUP BY district`)
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
