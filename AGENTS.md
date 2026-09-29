# AGENTS.md

Single-module Go app (`package main`, flat layout). Catalan UI searches people and marks T-shirt pickup, backed by Google Sheets (in-memory cache, no DB).

## Layout

- `main.go` — server, `Store` cache, config, Sheets read/write, API routes
- `search.go` — `Normalize()` + tiered `SearchPeople()` (cap 50)
- `colla.go` — colla sigles resolution, embeds `colles.json`
- `alias.go` — `ShortenAlias()` (max 8 **runes**, not bytes)
- `static/` (`index.html`, `app.js`, `style.css`) — no bundler, served via `//go:embed static`
- `colles.json` — colla reference data

## Commands

```sh
go run .                          # needs .env + credentials, serves :8080
go test ./...                     # all tests
go test -run TestShortenAlias .   # focused alias test
go vet ./...                      # no lint/test CI exists — run vet+test before push
docker compose up --build         # prod-like; keeps container on 8080, don't change PORT with compose
```

## Env / credentials (all gitignored, never commit)

`cp .env.example .env`. `SPREADSHEET_ID` required. `SHEET_NAME=""` = first tab (visible tab name, **not** numeric `gid`). `GOOGLE_CREDENTIALS_JSON` (base64, `base64 -i credentials.json | tr -d '\n'`) overrides `GOOGLE_CREDENTIALS_FILE` (`credentials.json`). Sheet must be shared with service-account email as **Editor**. `REFRESH_INTERVAL_SECONDS` (default 60) drives background cache refresh.

## Sheet contract

- Row 1 = headers. Column match = accent/case-insensitive substring: `nom`, `ali`, `talla`, `recollida`, `colla`, modalitat tries `pinya` → `pla` → `vols fer`. `nom` falls back to column A; blank nom+alias rows skipped from cache.
- Writes are `TRUE`/`FALSE` with `USER_ENTERED` to `SheetTitle!COL<row>`; `Row` is 1-based, data starts at 2. Out-of-range row → 400; missing size column → 409.
- `POST /api/size` allowlist only: `S M L XL XXL` + canonical `Ja tinc una samarreta dels Xiquets del Serrallo` (match case/whitespace/accent-insensitive, stored canonically). UI renders that literal as "No vull samarreta".
- `parseCollected` truthy: `true/1/si/x/yes/y/ok/fet/recollida/recollit` + `✓ ✅ ✔ ☑`.

## Conventions / gotchas

- Text matching always goes through `Normalize()` (`search.go`: lowercase, strip accents incl. `ç→c ñ→n`, collapse spaces). Reuse it; don't add ad-hoc `ToLower`.
- Search tiers in order: exact substring → every-query-token-is-prefix → all-tokens-substring → fuzzy (`lithammer/fuzzysearch`) **only if tiers 0–2 score zero**. Haystack = nom + alies + colla + resolved sigles + official names. Queries normalizing to sense-colla (`cap`, `sense colla`, …) return only `CAP` people.
- Colla resolution: `CollaSiglesTotes()` splits multi-colla cells on `, ; + / i amb antigament anteriorment`, but only trusts split if **every** part resolves to a known sigle (protects names like "Sant Pere i Sant Pau"). Unknown short single tokens (≤5 runes) uppercased, else trimmed original kept for free-text search.
- Alias edits: keep `ShortenAlias` rune-aware; `TestShortenAlias_AllLinesFit8Runes` fails if any `alias.txt` line exceeds 8 runes.
- API: `GET /api/health`, `GET /api/search?q=`, `GET /api/people`, `POST /api/toggle {"row","recollida"}`, `POST /api/size {"row","talla"}`. Frontend has no framework; keep API shapes stable.
- CI (`.github/workflows/docker-publish.yml`) only builds/pushes to GHCR on `main` / `v*` — no test gate.
