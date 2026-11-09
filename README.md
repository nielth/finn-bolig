# finn-bolig (boligjakt)

A personal home-hunting app for Stavanger. A crawler collects every home for sale on finn.no once a day and
stores everything about each ad: all photos, the description, the estate agent, key facts, dates, and a history
of price changes, updates and sales. The frontend is a finn-like list with filters and a photo gallery.

AI ranking is being rebuilt. Claude will write a summary of each home from all its photos and the ad text,
then rank those summaries against what you are looking for.

## Architecture

```
finn.no in your browser ──userscript──▶ frontend (nginx :8080) ──/api──▶ backend (Go + SQLite)
  fetches pages, posts raw responses      Svelte app                      parses, stores, tracks changes
```

finn.no blocks automated crawlers, so the crawl runs in your own browser as a userscript
(`frontend/public/finn-bolig.user.js`). The userscript only fetches pages, 2-4 seconds apart, and posts the raw
responses. The Go backend does all the parsing and stores the raw ad HTML (gzipped). When the parser improves,
`POST /api/admin/reparse` re-parses every stored ad without crawling again.

A crawl:

1. Fetches every search results page for Stavanger (`location=1.20012.20196&is_new_property=false`).
2. Asks the backend which ad pages it needs: favourites without data, ads that disappeared from the search
   results (sold or removed?), new or changed ads, and ads not fetched in 7 days.
3. Fetches those ad pages.

The crawl starts by itself when a finn.no tab is open and the last complete crawl is more than 20 hours old.
Only one tab crawls at a time.

## Running

```sh
cp .env.example .env
docker compose up -d --build
```

Open http://localhost:8080 and follow the **Sync** page to install the userscript (served from
http://localhost:8080/finn-bolig.user.js).

## Development

```sh
cd backend && go test ./... && DATA_DIR=./data go run .   # API on :8080
cd frontend && bun install && bun run dev                 # Vite proxies /api to :8080
```

`GET /api/listings/{finnkode}/data` shows the raw data the parser found for an ad, which helps when adjusting
the parser to finn's format.

## Notes

- Districts are assigned from the home's coordinates to the nearest district centre, so homes close to a
  border can land in the neighbouring district.
