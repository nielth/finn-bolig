# finn-bolig (boligjakt)

A personal home-hunting app for Stavanger. It collects homes from finn.no and uses Claude to rank them
by how well their **photos** (exterior and interior) match your taste. Your taste comes from a description you
write and a set of favourite finn ads.

- **Homes**: a finn-like scrollable list with photos, sorted by Claude's match score. Filter on for sale/sold,
  district (Hundvåg, Eiganes og Våland, Madla, …) and price range. Click a home for the full photo gallery.
- **My favourites**: add finn ads you like (link or finnkode), see their photos, remove them.
- **What I want**: the description Claude scores against. Saving it rescores every home automatically.
- **Sync**: how to get data in from finn.no.

## Architecture

```
finn.no in your browser ──userscript──▶ frontend (nginx :8080) ──/api──▶ backend (Go + SQLite) ──▶ Claude API
                                         Svelte app                       scores photos
```

finn.no protects its pages against server-side scraping, so the backend never contacts finn.no. A userscript
(`frontend/public/finn-bolig.user.js`) runs in your own browser on finn.no and posts homes to the backend,
slowly (about one request every 2 seconds). The backend only downloads photos from finn's image CDN to send to
Claude.

Scoring: for each home, Claude (default `claude-opus-5-5`) gets your description, up to 5 photos of each
favourite, and up to 10 photos of the home. The ad text is not sent. Your description and the favourites'
photos are prompt-cached, so a full rescore only pays full price for each home's own photos. Changing the
description or the favourites starts a rescore after 15 seconds of quiet, so several quick edits only cause one
rescore.

## Running

```sh
cp .env.example .env   # set ANTHROPIC_API_KEY
docker compose up -d --build
```

Open http://localhost:8080 and follow the **Sync** page to install the userscript (served from
http://localhost:8080/finn-bolig.user.js).

## Configuration (`.env`)

| Variable | Default | |
|---|---|---|
| `ANTHROPIC_API_KEY` | | Required |
| `CLAUDE_MODEL` | `claude-opus-5-5` | Default model; can be changed on the "What I want" page |
| `CLAUDE_EFFORT` | `medium` | `low`, `medium`, `high` |
| `SCORING_CONCURRENCY` | `4` | Parallel Claude requests |
| `SCORING_MAX_IMAGES` | `10` | Photos per home |
| `SCORING_MAX_REFERENCE_IMAGES` | `5` | Photos per favourite |
| `SCORING_DEBOUNCE_SECONDS` | `15` | Quiet period before a rescore starts |

## Development

```sh
cd backend && DATA_DIR=./data go run .     # API on :8080
cd frontend && bun install && bun run dev  # Vite proxies /api to :8080
```

## Notes

- Districts are assigned from the home's coordinates to the nearest district centre, so homes close to a
  border can land in the neighbouring district.
- "Sync this search" with no more pages marks homes from earlier syncs that are no longer in the results as
  gone. Sync the same (broad) search each time.
- The ad-page parser in the userscript reads finn's embedded page data generically, because the format is
  undocumented. If a field is missing, open an ad and check the `[boligjakt] parsed ad` entry in the console.
