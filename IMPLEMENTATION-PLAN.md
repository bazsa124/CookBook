# CookBook — Implementation Plan

Companion to [recipe_app_architecture_document.md](recipe_app_architecture_document.md). The spec says
*what*; this says *how*, and records every place where the implementation deliberately departs from it.

## 0. Constraints that drive the decisions

1. **The host is an old laptop turned NAS, and it may run Linux *or* Windows.** Nothing may assume
   either. The server is one static binary per OS, no runtime to install, no cgo.
2. **Reuse the proven stack from `../Remoter`.** Same tailnet, same Go + Kotlin/Compose toolchain,
   same Android dependency versions (known to build on this machine), same bearer-token pattern.
3. **Files are the truth, SQLite is an index.** Deleting `index.db` must never lose data; it is
   rebuilt from the recipe folders at every start.

## 1. Technology decisions (and deviations from the spec)

| Area | Spec suggestion | Chosen | Why |
|---|---|---|---|
| Server | Node / FastAPI | **Go** (`net/http`, Go 1.22+ routing) | Single static binary; `GOOS=linux` cross-compile from Windows; no Python/Node on the NAS. Same as Remoter. |
| SQLite | SQLite | **`modernc.org/sqlite`** | Pure Go — no cgo, so cross-compiling to Linux/ARM stays trivial. FTS5 included. |
| Documents | Typst CLI | **Typst CLI**, shared template reading `meta.json` via `json()` | The per-recipe `main.typ` is a 3-line stub importing one template, so restyling every PDF is one file edit. Missing Typst → PDFs report `unavailable`, the rest works. |
| Container | Docker Compose | **Native binary first; Dockerfile + compose provided** | Docker on Windows Home means WSL2 + Docker Desktop on an old laptop. Native is lighter on both OSes; Docker is still there for Linux hosts that want it. |
| Client | RN or Kotlin | **Kotlin + Jetpack Compose** | Toolchain and versions already proven in Remoter. PDF via the platform `PdfRenderer` (no library). |
| Translation | Gemini or Groq | **Both**, behind one interface; chosen by which API key is configured | Free tiers change; switching must be a config edit. |

## 2. Data model (`meta.json`)

The spec schema is kept verbatim and **extended**, never changed. Additions, all optional:

- `description`, `category`, `prep_minutes`, `cook_minutes`, `hero` (media filename), `source`,
  `notes`, `rev` (int, optimistic concurrency), `created`, `updated` (UTC RFC 3339).
- Ingredient: `unit_id` (canonical unit code — enables scaling-aware formatting, metric↔imperial and
  grocery summing), `qty_max` (ranges like "10–15 dkg"), `group` (`{en,hu}` — "Tészta", "Töltelék"),
  `optional`. `qty: null` means "to taste".
- Step: `media` (filename in `media/`) — the spec's "media linking".

Localised strings are always `{ "en": "...", "hu": "..." }`; a missing key means "not written yet",
which is exactly what auto-translate fills.

**Units** live in one table on the server (`GET /api/units`), fetched and cached by the app, so the
server (grocery sums) and the app (scaling, conversion) can never disagree. Hungarian kitchen units
(dkg, ek, tk, kk, bögre, gerezd, fej, csipet, db) are first-class.

## 3. On-disk layout

```
<data>/
  config.json            # optional; env vars override
  token                  # bearer token, generated on first start (0600)
  index.db               # disposable SQLite index
  grocery.json           # shared household list: static items, chosen recipes, checked state
  templates/recipe.typ   # the one Typst template (written on first start, user-editable)
  recipes/<id>/
    meta.json
    main.typ             # stub: imports /templates/recipe.typ
    output-hu.pdf        # one per language that has content
    output-en.pdf
    media/hero.jpg ...
    history/<utc>.json   # last 20 revisions of meta.json — the "repository" part
```

Writes are atomic (temp file + rename). Paths are built with `filepath.Join` only.

## 4. API (bearer token on everything under `/api/`)

```
GET    /api/health                      version, os, typst available, translator available
GET    /api/units                       unit table
GET    /api/categories                  recipe categories + aisle categories (localised)
GET    /api/recipes?q=&tag=&category=&include=&exclude=&pantry=&lang=
                                        summaries; pantry= ranks by coverage (reverse search)
GET    /api/recipes/export              every meta.json in one response (app's offline cache)
GET    /api/recipes/{id}                full meta.json
POST   /api/recipes                     create  -> {recipe, pdf}
PUT    /api/recipes/{id}                update (409 if rev mismatch)
DELETE /api/recipes/{id}                moves folder to <data>/trash
POST   /api/recipes/{id}/media          multipart upload -> compressed JPEG, returns filename
GET    /api/recipes/{id}/media/{file}
DELETE /api/recipes/{id}/media/{file}
GET    /api/recipes/{id}/pdf?lang=hu    compiled on demand if stale
GET    /api/recipes/{id}/history
GET    /api/ingredients                 catalog of known ingredient ids (editor autocomplete)
GET    /api/tags
POST   /api/translate                   {recipe, from, to} -> recipe with only-missing fields filled (editor preview, saves nothing)
POST   /api/recipes/{id}/translate?to=en  translate the missing language and save it (app banner)
POST   /api/translate/all               background job: fill every missing language in the book
GET    /api/translate/all               job progress {running, total, done, translated, failed[]}
GET    /api/grocery                     aggregated list (Pipeline 3) + static items
POST   /api/grocery {ops:[...]}         add_recipe | remove_recipe | check | add_static |
                                        remove_static | clear_done | clear_all
```

Grocery changes are **operations, not a whole-state PUT**: two people ticking items in the shop at the
same time must not undo each other.

**The grocery list works fully offline.** The phone keeps the last server state plus a queue of
operations. The list it shows is `apply(serverState, queue)`, aggregated on the phone from the
cached recipes. `logic/Grocery.kt` is a port of `internal/grocery`, and the two are held together by a
shared fixture: `parity_test.go` writes the server's output to
`android/app/src/test/resources/grocery_parity.json`, and `GroceryParityTest.kt` must reproduce it
line for line. Supporting server details:
- the list response includes the raw `state`;
- `add_static` accepts a phone-generated `id`, so queued checks and removes can refer to an item
  added offline, and replaying the same add is a no-op;
- the export carries `aisle_keywords`, so the phone guesses shop sections with the server's table.

The queue is sent when the app starts, when any network becomes available, and on every change or
refresh. If the server rejects a batch, it is dropped rather than retried forever.

## 5. Pipelines — where each one lives

1. **Create/edit:** app → `PUT` → validate → atomic write + history snapshot → index row upsert →
   Typst compile (serialized worker, 60 s timeout; a PDF failure never fails the save).
2. **Scaling & cook mode:** entirely in the app (works offline): multiplier, mustache resolution
   (`{{id.qty}}`, `{{id.unit}}`, `{{id.name}}`), `[MM:SS]` / `[H:MM:SS]` → timer chips, unit
   conversion, keep-screen-on.
3. **Grocery:** server sums by ingredient `id`; quantities in compatible units are converted to a
   common base (mass/volume) before summing; incompatible units stay separate lines. Grouped by aisle
   category in a fixed store order.
4. **Translation:** server builds the prompt from **only the missing fields**, sends it as a flat
   `{path: text}` map (the model never sees ids), requires JSON back, then *verifies* that every
   `{{…}}` and `[…]` token survived unchanged. A string that fails verification is dropped, not
   merged. Manual edits are never overwritten because only empty targets are sent.

## 6. Network & security

- Default bind: **loopback + every local address in `100.64.0.0/10`** (Tailscale CGNAT range),
  discovered at start and re-scanned every minute (Tailscale may come up after the server). Never
  `0.0.0.0` unless explicitly configured (needed inside Docker).
- Port **8738** (Remoter owns 8737).
- Bearer token as in Remoter; Tailscale is the boundary, the token is defence in depth.
- Upload limits: 20 MB per image, images re-encoded (strips EXIF/GPS as a side effect).

## 7. Cross-platform service setup

- `deploy/install-linux.sh` — copies binary, creates `cookbook` user, systemd unit, `typst` hint.
- `deploy/install-windows.ps1` — self-elevates, registers a Scheduled Task "at startup" as SYSTEM
  (runs with nobody logged in — the Remoter lesson), data in `%ProgramData%\CookBook`.
- `deploy/Dockerfile` + `docker-compose.yml` — for Linux hosts that prefer containers.
- `Taskfile`-free: `server/build.ps1` and `server/build.sh` cross-compile windows/linux amd64+arm64.

## 8. Test data

`server/cmd/cookbook-import` parses the early-stage `Receptkönyv.docx` (≈45 Hungarian recipes with
photos): headings → recipes, list items → ingredients (quantity/unit/name parsed, Hungarian units
recognised), "Elkészítése" paragraphs → steps, embedded images → `hero.jpg`, and times in steps
("20 percet") → `[20:00]` timers. It writes straight into a data directory; the server indexes it on
start.

## 9. Build order & exit tests

1. **Server core** — store, index, recipes CRUD, units. *Exit:* `go vet` clean, unit tests for
   parser/aggregation/translation-guard pass, `GOOS=linux` build green.
2. **Typst + media + grocery + translate.** *Exit:* PDF produced for an imported recipe with its
   photo; grocery merges `tojas` across two recipes.
3. **Importer** against the real docx. *Exit:* ≥ 40 recipes imported and searchable.
4. **Android app** — pairing, list/search/pantry, swipe, detail with scaling/units/language, cook
   mode with timers, favorites, grocery, editor with media + auto-translate, PDF view, offline cache.
   *Exit:* `assembleDebug` green.

## 10. Status (2026-09-25)

| Stage | State | Evidence |
|---|---|---|
| 1. Server core | **done** | `go vet` clean; unit tests (recipe, translate guard, grocery) pass; builds for windows/amd64, linux/amd64, linux/arm64, linux/386 |
| 2. Typst, media, grocery, translate | **done** | 28/28 end-to-end API checks against a live server (create, slug collision, validation, upload + EXIF/compress, rev conflict 409, history, search w/o accents, pantry, PDF, trash, path traversal, grocery merge); 42/43 imported PDFs compiled first run, the one failure (recipe with no ingredients) fixed |
| 3. Importer | **done** | 43 recipes from `Receptkönyv.docx` with hero photos; ids merge across recipes (`tojas` ×19, `finomliszt` ×19) |
| 4. Android | **builds, not yet run on a device** | `assembleDebug` green; 10 JVM tests for scaling / conversion / mustache / timers / parsing pass. No emulator image on this machine. |
| Translation | **done** | Live Gemini run: 24/24 fields of an imported recipe translated, `{{temp:…}}` and `[15:00]` preserved, nothing saved until the user saves. Default model is the `gemini-flash-latest` alias, because `gemini-2.5-flash` is closed to new accounts. On 429/5xx it retries, then falls back to `gemini-flash-lite-latest`. |

Importer notes: steps get timers from "20 percig" → `[20:00] percig` (ranges keep their text and get
a timer at the lower bound) and temperatures from "180 fokos" → `{{temp:180}}-os`, which renders as
"180 °C-os" — correct Hungarian, since "°C" is read as "fok". Quantities inside step text are *not*
linked to ingredients automatically; that is safer done by hand in the editor.

## 11. Web import (added 2026-09-27)

- **Only schema.org JSON-LD, no HTML scraping.** Sites publish it for search engines, so it
  survives redesigns. `internal/scrape` reads every shape seen in the wild: `@graph`, `@type` lists,
  HowToSection/HowToStep, ImageObject, yields as arrays, and double-encoded entities.
- **Shared text parsing.** `internal/ingest` holds the ingredient, timer and temperature parsing
  shared with the Word importer (whose output stayed byte-identical after the move). Web input gets
  one extra cleaning step, `webLine`: prices and nested brackets.
- **Politeness.** Requests identify themselves, follow `robots.txt` (RFC 9309 matching with `*`/`$`,
  longest rule wins, `Crawl-delay`), and wait at least 3 s between requests per host. A 403/429
  stops that source for the run. A 403 on `robots.txt` counts as "no bots".
- **Scheduling.** `internal/feeds` runs RSS/sitemap sources. Every link it considers is recorded
  in `feeds.json`: imported, skipped with a reason, or already in the book. A permanent failure
  (not a recipe, 404, disallowed) is never retried; a network error is retried next run. At most 8
  pages are fetched per source per run, so a feed full of articles can't turn into a crawl.
- **Sources left out on purpose:** allrecipes.com and seriouseats.com block automated requests
  intermittently. mindmegette.hu has no usable sitemap, and streetkitchen.hu's sitemap only lists
  category pages. A single-link import still works for them when the site allows it.
- **Live check:** one run imported one recipe from each of the 5 sources in about 3 minutes, all
  with photos, both languages and both PDFs.

Cooking methods (`internal/recipe/methods.go`) are ordinary tags with fixed ids, so no schema,
index or client storage changed. Detection runs on folded text (lower case, accents removed) so
Hungarian and English share one pattern table.

## 12. Video (added 2026-09-27)

- **Where videos go:** step media only; the cover stays a photo (validated). MP4 and WebM are
  recognised by their first bytes, not the declared type.
- **Stored as uploaded:** the upload is streamed to a temp file in the media folder while being
  hashed, then renamed to `<hash>.mp4`, so it is never held in memory. Limit 300 MB. No
  re-encoding.
- **Poster frame:** `<hash>-poster.jpg` via ffmpeg when it is installed (config `ffmpeg`, env
  `FFMPEG_BIN`). It's optional: a missing poster only affects the preview.
- **Streaming:** `http.ServeFile` answers range requests, so seeking works. The app plays videos
  with the platform `VideoView`, which can send the bearer token, so no new dependency. Videos are
  not cached offline.
- **PDF:** a "▶ video in the app" marker at the step.
- **Template upgrades:** the server now replaces `templates/recipe.typ` when it is an unmodified
  copy of an earlier built-in version (known SHA-256 list in `typst.go`). A template the user edited
  is kept, with a log line. **Add the old hash to that list whenever `recipe.typ` changes.**
- **External videos:** `video_url` (e.g. YouTube) is set by hand or filled by web import from a
  schema.org VideoObject. YouTube embed links become watch links so the YouTube app opens them.

## 13. Decided against (2026-09-27)

- **Multi-user accounts.** The tailnet is the access control: whoever is on it is the household.
- **Offline recipe editing.** Recipes are read-only offline (cached export) and edits need the
  server; `rev` makes conflicts visible instead of silent. The grocery list is the exception and
  works fully offline: add and remove recipes and items, tick items, change servings (§4).
