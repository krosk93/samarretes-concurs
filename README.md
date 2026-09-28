# Samarretes Concurs — T-shirt pickup search

Catalan UI to search people and mark T-shirt pickup, backed by Google Sheets.

## Setup

1. **Google Cloud service account:**
   - Create a project + service account, add a JSON key.
   - Share the Google Sheet with the service account email as **Editor**.
2. **Configure:**
   ```sh
   cp .env.example .env
   # fill SPREADSHEET_ID (from sheet URL) and SHEET_NAME (or leave empty = first sheet)
   # put the key at credentials.json (or set GOOGLE_CREDENTIALS_JSON as base64)
   ```
3. **Run:**
   ```sh
   go run .
   # open http://localhost:8080
   ```

## Docker

```sh
docker build -t samarretes-concurs .
docker run --env-file .env -p 8080:8080 -v ./credentials.json:/app/credentials.json:ro samarretes-concurs
# or: docker compose up --build
```

Images are also published to GHCR on push to `main` / tags `v*`
(`ghcr.io/<owner>/<repo>`).

## Sheet format

- Row 1 = headers. Data from row 2 on, one row per person.
- Columns are found by header name (case/accent-insensitive substring):
  - name: contains `nom` (e.g. `Nom i Cognoms`)
  - alias: contains `ali` (e.g. `Alies`)
  - size: contains `talla` (e.g. `Talla samarreta`)
  - collected: contains `recollida` (e.g. `Samarreta recollida`)
  - colla (optional context): contains `colla`
- Collected parses `TRUE/true/1/SI/X/...` (also ✓) as true.
- Toggle writes `TRUE`/`FALSE` with `USER_ENTERED` to the recollida column
  of that row, so Sheets treats it as a real boolean checkbox value.

## SHEET_NAME vs gid note

`SHEET_NAME` is the visible tab name (e.g. `Respostes`), not the numeric
`gid` from the URL. Leave it empty to use the first tab automatically.

## Env vars

| Var | Default | Description |
| --- | --- | --- |
| SPREADSHEET_ID | (required) | Sheet ID from URL |
| SHEET_NAME | "" (first sheet) | Tab name |
| GOOGLE_CREDENTIALS_FILE | credentials.json | Service-account key path |
| GOOGLE_CREDENTIALS_JSON | "" | Base64-encoded key JSON (overrides file; `base64 -i credentials.json \| tr -d '\n'`) |
| PORT | 8080 | HTTP port |
| REFRESH_INTERVAL_SECONDS | 60 | Cache refresh period |

## API

- `GET /` — frontend
- `GET /api/health` → `{"ok":true}`
- `GET /api/search?q=` → top 50 matches
- `GET /api/people` → all rows
- `POST /api/toggle` `{"row":int,"recollida":bool}` → updated object
