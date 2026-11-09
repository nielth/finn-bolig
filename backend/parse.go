package main

import (
	"encoding/json"
	"html"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// AdFields are the structured fields we extract from finn's search results and ad pages.
// nil means "unknown", so a later source can fill it in.
type AdFields struct {
	Title         *string  `json:"title"`
	Address       *string  `json:"address"`
	LocalArea     *string  `json:"local_area"`
	Lat           *float64 `json:"lat"`
	Lon           *float64 `json:"lon"`
	Price         *int64   `json:"price"`
	PriceTotal    *int64   `json:"price_total"`
	SharedCosts   *int64   `json:"shared_costs"`
	Area          *int64   `json:"area"`
	PlotArea      *int64   `json:"plot_area"`
	Bedrooms      *int64   `json:"bedrooms"`
	Rooms         *int64   `json:"rooms"`
	Floor         *int64   `json:"floor"`
	BuildYear     *int64   `json:"build_year"`
	PropertyType  *string  `json:"property_type"`
	OwnershipType *string  `json:"ownership_type"`
	EnergyLabel   *string  `json:"energy_label"`
	AgentName     *string  `json:"agent_name"`
	AgentCompany  *string  `json:"agent_company"`
	AgentPhone    *string  `json:"agent_phone"`
	PublishedAt   *string  `json:"published_at"`
	FinnUpdatedAt *string  `json:"finn_updated_at"`
	Description   *string  `json:"description"`
	URL           *string  `json:"url"`
}

type Fact struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// ---------- generic JSON helpers ----------

func walk(root any, visit func(m map[string]any) bool) {
	var rec func(v any, depth int)
	rec = func(v any, depth int) {
		if depth > 60 {
			return
		}
		switch x := v.(type) {
		case map[string]any:
			if !visit(x) {
				return
			}
			for _, c := range x {
				rec(c, depth+1)
			}
		case []any:
			for _, c := range x {
				rec(c, depth+1)
			}
		}
	}
	rec(root, 0)
}

// findKey searches breadth-first for the first key in keys whose value passes accept.
func findKey(root any, keys []string, accept func(any) bool) any {
	queue := []any{root}
	for len(queue) > 0 && len(queue) < 200000 {
		node := queue[0]
		queue = queue[1:]
		switch x := node.(type) {
		case map[string]any:
			for _, k := range keys {
				if v, ok := x[k]; ok && v != nil && accept(v) {
					return v
				}
			}
			for _, k := range sortedKeys(x) {
				queue = append(queue, x[k])
			}
		case []any:
			queue = append(queue, x...)
		}
	}
	return nil
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

var digitsRe = regexp.MustCompile(`\d`)

func numOf(v any) (int64, bool) {
	switch x := v.(type) {
	case json.Number:
		f, err := x.Float64()
		return int64(math.Round(f)), err == nil
	case float64:
		return int64(math.Round(x)), !math.IsNaN(x) && !math.IsInf(x, 0)
	case int64:
		return x, true
	case string:
		d := strings.Join(digitsRe.FindAllString(x, -1), "")
		if d == "" || len(d) > 15 {
			return 0, false
		}
		n, err := strconv.ParseInt(d, 10, 64)
		return n, err == nil
	case map[string]any:
		for _, k := range []string{"amount", "value", "size", "size_from", "from"} {
			if n, ok := numOf(x[k]); ok {
				return n, true
			}
		}
	}
	return 0, false
}

func floatOf(v any) (float64, bool) {
	switch x := v.(type) {
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case float64:
		return x, true
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	}
	return 0, false
}

func strOf(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		s := strings.TrimSpace(x)
		return s, s != ""
	case json.Number:
		return x.String(), true
	}
	return "", false
}

func isPositive(v any) bool {
	n, ok := numOf(v)
	return ok && n > 0
}

func isStr(v any) bool {
	_, ok := strOf(v)
	return ok
}

func ptrNum(v any) *int64 {
	if n, ok := numOf(v); ok && n > 0 {
		return &n
	}
	return nil
}

func ptrFloat(v any) *float64 {
	if f, ok := floatOf(v); ok && f != 0 {
		return &f
	}
	return nil
}

func ptrStr(v any) *string {
	if s, ok := strOf(v); ok {
		return &s
	}
	return nil
}

// timeOf turns epoch milliseconds/seconds or a date string into RFC3339.
func timeOf(v any) *string {
	if v == nil {
		return nil
	}
	if f, ok := floatOf(v); ok {
		if f > 1e12 {
			s := time.UnixMilli(int64(f)).UTC().Format(time.RFC3339)
			return &s
		}
		if f > 1e9 {
			s := time.Unix(int64(f), 0).UTC().Format(time.RFC3339)
			return &s
		}
	}
	if s, ok := strOf(v); ok {
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"} {
			if t, err := time.Parse(layout, s); err == nil {
				out := t.UTC().Format(time.RFC3339)
				return &out
			}
		}
	}
	return nil
}

func isTime(v any) bool { return timeOf(v) != nil }

// merge fills nil fields of dst from src. If override is true, non-nil src fields win.
func (dst *AdFields) merge(src AdFields, override bool) {
	var d, s map[string]any
	db, _ := json.Marshal(dst)
	sb, _ := json.Marshal(src)
	_ = json.Unmarshal(db, &d)
	_ = json.Unmarshal(sb, &s)
	for k, v := range s {
		if v != nil && (override || d[k] == nil) {
			d[k] = v
		}
	}
	b, _ := json.Marshal(d)
	*dst = AdFields{}
	_ = json.Unmarshal(b, dst)
}

// ---------- search results ----------

type SearchPage struct {
	Docs     []map[string]any
	LastPage int
}

func parseSearchPage(text string) SearchPage {
	data := decodeTurboStream(text)
	var page SearchPage
	seen := map[int64]bool{}
	walk(data, func(m map[string]any) bool {
		if _, ok := m["ad_id"]; ok {
			if _, ok := m["heading"]; ok {
				// The same ad can appear more than once in the data.
				if id := searchDocFinnkode(m); id != 0 && !seen[id] {
					seen[id] = true
					page.Docs = append(page.Docs, m)
				}
				return false
			}
		}
		if p, ok := m["paging"].(map[string]any); ok {
			if n, ok := numOf(p["last"]); ok {
				page.LastPage = int(n)
			}
		}
		return true
	})
	return page
}

func searchDocFinnkode(doc map[string]any) int64 {
	n, _ := numOf(doc["ad_id"])
	return n
}

func fieldsFromSearchDoc(doc map[string]any) AdFields {
	f := AdFields{
		Title:        ptrStr(doc["heading"]),
		Address:      ptrStr(doc["location"]),
		LocalArea:    ptrStr(doc["local_area_name"]),
		Price:        ptrNum(doc["price_suggestion"]),
		PriceTotal:   ptrNum(doc["price_total"]),
		SharedCosts:  ptrNum(doc["price_shared_cost"]),
		Area:         ptrNum(firstNonNil(doc["area_range"], doc["area"], doc["primary_area"])),
		PlotArea:     ptrNum(doc["area_plot"]),
		Bedrooms:     ptrNum(doc["number_of_bedrooms"]),
		PropertyType: ptrStr(doc["property_type_description"]),
		AgentCompany: ptrStr(doc["organisation_name"]),
		URL:          ptrStr(doc["canonical_url"]),
	}
	if c, ok := doc["coordinates"].(map[string]any); ok {
		f.Lat, f.Lon = ptrFloat(c["lat"]), ptrFloat(c["lon"])
	}
	if t := timeOf(doc["timestamp"]); t != nil {
		f.FinnUpdatedAt = t
	}
	return f
}

func firstNonNil(vs ...any) any {
	for _, v := range vs {
		if v != nil {
			return v
		}
	}
	return nil
}

// ---------- ad pages ----------

type AdPage struct {
	Fields AdFields
	Images []string
	Facts  []Fact
	Sold   bool
	Data   any // the decoded data that belongs to this ad
}

var (
	enqueueRe = regexp.MustCompile(`streamController\.enqueue\(("(?:[^"\\]|\\.)*")\)`)
	jsonTagRe = regexp.MustCompile(`(?s)<script[^>]*type="application/(?:ld\+)?json"[^>]*>(.*?)</script>`)
	metaRe    = regexp.MustCompile(`<meta[^>]+property="(og:[a-z:]+)"[^>]+content="([^"]*)"`)
	imageRe   = regexp.MustCompile(`https://images\.finncdn\.no/dynamic/[^/"'\s\\]+/([^"'\s\\),]+)`)
	soldRe    = regexp.MustCompile(`(?i)^(sold|solgt)$`)
)

// embeddedData returns all JSON data embedded in an ad page.
func embeddedData(page string) []any {
	var roots []any
	var stream strings.Builder
	for _, m := range enqueueRe.FindAllStringSubmatch(page, -1) {
		var chunk string
		if json.Unmarshal([]byte(m[1]), &chunk) == nil {
			stream.WriteString(chunk)
		}
	}
	if stream.Len() > 0 {
		if v := decodeTurboStream(stream.String()); v != nil {
			roots = append(roots, v)
		}
	}
	for _, m := range jsonTagRe.FindAllStringSubmatch(page, -1) {
		var v any
		d := json.NewDecoder(strings.NewReader(html.UnescapeString(m[1])))
		d.UseNumber()
		if d.Decode(&v) == nil {
			roots = append(roots, v)
		}
	}
	return roots
}

// adSubtrees returns the objects that mention this finnkode, so we read the ad's own data rather
// than data about similar ads, followed by the full roots as a fallback.
func adSubtrees(roots []any, finnkode int64) []any {
	var hits []any
	for _, root := range roots {
		walk(root, func(m map[string]any) bool {
			for _, k := range []string{"adId", "ad_id", "finnkode", "finnCode", "id"} {
				if v, ok := m[k]; ok {
					if _, isMap := v.(map[string]any); !isMap {
						if n, ok := numOf(v); ok && n == finnkode {
							hits = append(hits, m)
							return false
						}
					}
				}
			}
			return true
		})
	}
	return append(hits, roots...)
}

func extractImages(page string, finnkode int64) []string {
	clean := strings.NewReplacer(`/`, "/", `/`, "/", `\/`, "/", "&amp;", "&").Replace(page)
	id := strconv.FormatInt(finnkode, 10)
	var grouped strings.Builder
	for i, c := range id {
		if i > 0 && i%3 == 0 {
			grouped.WriteByte('/')
		}
		grouped.WriteRune(c)
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range imageRe.FindAllStringSubmatch(clean, -1) {
		rest := m[1]
		belongs := strings.Contains(rest, "/"+id+"/") || strings.Contains(rest, grouped.String()+"_") || strings.Contains(rest, "/"+id+"_")
		if !belongs || seen[rest] {
			continue
		}
		seen[rest] = true
		out = append(out, "https://images.finncdn.no/dynamic/default/"+rest)
	}
	return out
}

var tagRe = regexp.MustCompile(`<[^>]+>`)

func stripHTML(s string) string {
	s = strings.NewReplacer("<br>", "\n", "<br/>", "\n", "<br />", "\n", "</p>", "\n\n", "</li>", "\n").Replace(s)
	s = html.UnescapeString(tagRe.ReplaceAllString(s, ""))
	return strings.TrimSpace(s)
}

func parseAdPage(page string, finnkode int64) AdPage {
	roots := embeddedData(page)
	trees := adSubtrees(roots, finnkode)
	pick := func(keys []string, accept func(any) bool) any {
		for _, t := range trees {
			if v := findKey(t, keys, accept); v != nil {
				return v
			}
		}
		return nil
	}

	var f AdFields
	f.Title = ptrStr(pick([]string{"heading", "title"}, isStr))
	f.Price = ptrNum(pick([]string{"askingPrice", "price_suggestion", "priceSuggestion"}, isPositive))
	f.PriceTotal = ptrNum(pick([]string{"totalPrice", "price_total", "priceTotal"}, isPositive))
	f.SharedCosts = ptrNum(pick([]string{"sharedCost", "sharedCosts", "jointCost", "monthlyCommonCost", "price_shared_cost"}, isPositive))
	f.Area = ptrNum(pick([]string{"usableArea", "primaryRoomArea", "primaryArea", "internalArea", "area_range", "area", "size"}, isPositive))
	f.PlotArea = ptrNum(pick([]string{"plotArea", "plot_area", "area_plot", "plot"}, isPositive))
	f.Bedrooms = ptrNum(pick([]string{"noOfBedrooms", "numberOfBedrooms", "number_of_bedrooms", "bedrooms"}, isPositive))
	f.Rooms = ptrNum(pick([]string{"noOfRooms", "numberOfRooms", "rooms"}, isPositive))
	f.Floor = ptrNum(pick([]string{"floor", "floorNumber"}, isPositive))
	f.BuildYear = ptrNum(pick([]string{"constructionYear", "buildYear", "yearBuilt", "build_year"}, func(v any) bool {
		n, ok := numOf(v)
		return ok && n > 1700 && n < 2100
	}))
	f.PropertyType = ptrStr(pick([]string{"propertyType", "propertyTypeDescription", "property_type_description"}, isStr))
	f.OwnershipType = ptrStr(pick([]string{"ownershipType", "ownershipTypeDescription", "ownership_type"}, isStr))
	f.EnergyLabel = ptrStr(pick([]string{"energyLabel", "energyRating", "energy_label"}, isStr))
	f.Lat = ptrFloat(pick([]string{"lat", "latitude"}, func(v any) bool { _, ok := floatOf(v); return ok }))
	f.Lon = ptrFloat(pick([]string{"lon", "lng", "longitude"}, func(v any) bool { _, ok := floatOf(v); return ok }))
	f.PublishedAt = timeOf(pick([]string{"published", "publishedDate", "firstPublished", "datePublished", "created"}, isTime))
	f.FinnUpdatedAt = timeOf(pick([]string{"lastModified", "lastUpdated", "updated", "edited", "dateModified", "modified"}, isTime))

	address := pick([]string{"address", "streetAddress", "location"}, func(v any) bool {
		if isStr(v) {
			return true
		}
		m, ok := v.(map[string]any)
		return ok && (m["street"] != nil || m["streetAddress"] != nil)
	})
	if m, ok := address.(map[string]any); ok {
		street, _ := strOf(firstNonNil(m["street"], m["streetAddress"]))
		postal := strings.TrimSpace(toString(firstNonNil(m["postalCode"], m["zipCode"])) + " " + toString(firstNonNil(m["postalName"], m["postalPlace"], m["addressLocality"])))
		s := strings.Trim(strings.Join([]string{street, postal}, ", "), ", ")
		f.Address = ptrStr(s)
	} else {
		f.Address = ptrStr(address)
	}

	// The agent is usually under a broker/contact object.
	agent := pick([]string{"broker", "brokers", "contact", "contacts", "agent"}, func(v any) bool {
		switch x := v.(type) {
		case map[string]any:
			return true
		case []any:
			return len(x) > 0
		}
		return false
	})
	if agent != nil {
		if list, ok := agent.([]any); ok {
			agent = list[0]
		}
		f.AgentName = ptrStr(findKey(agent, []string{"name", "fullName"}, isStr))
		f.AgentPhone = ptrStr(findKey(agent, []string{"mobile", "phone", "phoneNumber", "telephone"}, isStr))
		f.AgentCompany = ptrStr(findKey(agent, []string{"companyName", "organisationName", "company", "organisation"}, isStr))
	}
	if f.AgentCompany == nil {
		f.AgentCompany = ptrStr(pick([]string{"organisationName", "organisation_name", "companyName"}, isStr))
	}

	// The longest description-like text in the ad's data.
	var description string
	for _, t := range trees {
		walk(t, func(m map[string]any) bool {
			for k, v := range m {
				if s, ok := v.(string); ok && len(s) > len(description) {
					lk := strings.ToLower(k)
					if strings.Contains(lk, "description") || strings.Contains(lk, "generaltext") || strings.Contains(lk, "adtext") {
						description = s
					}
				}
			}
			return true
		})
		if description != "" {
			break
		}
	}

	metas := map[string]string{}
	for _, m := range metaRe.FindAllStringSubmatch(page, -1) {
		metas[m[1]] = html.UnescapeString(m[2])
	}
	if description == "" {
		description = metas["og:description"]
	}
	f.Description = ptrStr(stripHTML(description))
	if f.Title == nil {
		f.Title = ptrStr(metas["og:title"])
	}

	// Label/value pairs, e.g. the key facts list on the ad page.
	var facts []Fact
	seen := map[string]bool{}
	for _, t := range trees[:len(trees)-len(roots)] {
		walk(t, func(m map[string]any) bool {
			label, ok1 := strOf(firstNonNil(m["label"], m["title"], m["name"]))
			value, ok2 := strOf(m["value"])
			if !ok2 {
				if n, ok := m["value"].(json.Number); ok {
					value, ok2 = n.String(), true
				}
			}
			if ok1 && ok2 && len(label) < 60 && len(value) < 200 && !seen[label] {
				seen[label] = true
				facts = append(facts, Fact{Label: label, Value: value})
			}
			return true
		})
	}

	status := pick([]string{"adStatus", "status", "state"}, func(v any) bool {
		s, ok := strOf(v)
		return ok && soldRe.MatchString(s)
	})
	sold := status != nil || pick([]string{"isSold", "sold"}, func(v any) bool { b, ok := v.(bool); return ok && b }) != nil

	var data any
	if len(trees) > len(roots) {
		data = trees[:len(trees)-len(roots)]
	} else {
		data = roots
	}
	return AdPage{Fields: f, Images: extractImages(page, finnkode), Facts: facts, Sold: sold, Data: data}
}
