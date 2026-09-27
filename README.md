# CookBook

A self-hosted bilingual (HU/EN) recipe system: a small Go server on the NAS laptop, an Android app
on the phone, connected over Tailscale.

- Spec: [recipe_app_architecture_document.md](recipe_app_architecture_document.md)
- How it was built, and every deviation from the spec: [IMPLEMENTATION-PLAN.md](IMPLEMENTATION-PLAN.md)

```
server/    Go server (cmd/cookbook) + Word importer (cmd/cookbook-import)
android/   Kotlin / Jetpack Compose app
deploy/    systemd + Windows scheduled-task installers, Dockerfile
```

## 1. Build

```sh
cd server
./build.sh            # or: powershell -File build.ps1
```

Produces `server/dist/cookbook-{linux-amd64,linux-arm64,linux-386,windows-amd64}` (+ the importer).
Pure Go, no cgo: any of these builds from any OS.

Android (uses Android Studio's bundled JDK):

```sh
cd android
JAVA_HOME="/c/Program Files/Android/Android Studio/jbr" ./gradlew assembleDebug
# -> android/app/build/outputs/apk/debug/app-debug.apk
```

## 2. Install on the NAS laptop

Install [Typst](https://github.com/typst/typst/releases) for PDFs (optional — everything else works without it).

**Linux**

```sh
sudo deploy/install-linux.sh server/dist/cookbook-linux-amd64 server/dist/cookbook-import-linux-amd64
sudo nano /etc/default/cookbook     # optional: GEMINI_API_KEY or GROQ_API_KEY
sudo systemctl restart cookbook
```

**Windows**

```powershell
powershell -ExecutionPolicy Bypass -File .\deploy\install-windows.ps1 `
    -Binary .\server\dist\cookbook-windows-amd64.exe -Importer .\server\dist\cookbook-import-windows-amd64.exe `
    -GeminiKey "<optional>"
```

Runs at boot as SYSTEM (no login needed), data in `%ProgramData%\CookBook`, firewall rule scoped to
`100.64.0.0/10`.

**Docker (Linux)**: `TS_IP=$(tailscale ip -4) docker compose -f deploy/docker-compose.yml up -d --build`

The server listens on **port 8738**, on loopback and the machine's Tailscale address only. It prints the
API token location at start; `cookbook -print-token` shows it.

## 3. Import the old Word recipe book (optional)

```sh
cookbook-import -docx Receptkönyv.docx -data /var/lib/cookbook    # Linux
cookbook-import.exe -docx Receptkönyv.docx                          # Windows (default data dir)
cookbook-import -docx Receptkönyv.docx -dry-run                     # preview as JSON
```

Then restart the server (it re-indexes on start). Re-running skips recipes that already exist.

## 4. Phone

The server does not have a web interface: recipes are only in the Android app.

1. Copy `app-debug.apk` to `<data>/cookbook.apk`. The phone can then download it from
   `http://<tailscale-ip>:8738/`.
2. On the server machine, open `http://localhost:8738/pair` and scan the QR code with the phone's
   camera. The page that opens has an **Open in CookBook app** button, which fills in server and token.
   The pairing page is only served to the server machine itself (loopback). The token is carried in
   the URL fragment, which browsers never send to the server, so it never appears in its log.

Manual alternative: in the app's **Settings**, enter the Tailscale IP (port 8738 is added
automatically) and the token, then tap **Test connection**.

## Auto-translate

Set `GEMINI_API_KEY` (Google AI Studio, free tier) or `GROQ_API_KEY` in the environment or in
`<data>/config.json` (`{"gemini_api_key": "..."}`). Only empty fields are translated; a translation that
alters `{{variables}}` or `[timers]` is rejected, not merged.

The HU/EN toggle can only show text that exists. A recipe written in one language shows a
**Translate** banner in the other language, which translates and saves it. To translate the whole
book at once, `POST /api/translate/all` with the token. Progress is at `GET /api/translate/all`,
and each save keeps the previous version in the recipe's `history/`.

## Importing from recipe sites

**One link:** in the app, tap the download icon on the recipe list and paste a link. Or, in the
phone's browser, use **Share → CookBook**. Any site that publishes schema.org recipe data works,
which is most of them. The server reads the recipe, downloads its photo, labels its cooking methods
and, when a translation key is set, adds the other language. Importing the same link twice opens
the existing recipe instead of creating a copy.

**Automatically:** once a day the server takes up to 2 new recipes from each source:

| Source | Language | Found via |
|---|---|---|
| nosalty.hu | HU | sitemap |
| sobors.hu | HU | RSS |
| bbcgoodfood.com | EN | sitemap |
| recipetineats.com | EN | RSS |
| budgetbytes.com | EN | RSS |

Imports are tagged `#imported` plus the site name, so they're easy to filter and review. Links are
remembered in `<data>/feeds.json`, so a recipe you delete is never imported again. The fetcher
identifies itself, obeys each site's `robots.txt` (including crawl delays), and waits between
requests. Settings in the app shows the status, with a **Run now** button.

Tune it in `<data>/config.json`. Every field is optional, and a `sources` list replaces the
defaults:

```json
{
  "feeds": {
    "enabled": true,
    "interval_hours": 24,
    "per_source": 2,
    "auto_translate": true,
    "sources": [
      {"name": "nosalty", "lang": "hu", "kind": "sitemap", "enabled": true,
       "url": "https://www.nosalty.hu/fresh-recipes-sitemap.xml", "include": "/recept/"}
    ]
  }
}
```

## Videos

A step can have a video instead of a photo: use the camera button next to the photo button in
the editor. MP4 and WebM up to 300 MB are accepted and stored exactly as recorded; an old laptop
would take too long re-encoding them. When [ffmpeg](https://ffmpeg.org) is installed on the
server, a still frame is saved for the preview; without it, videos still play. They stream with
seeking but, unlike photos, aren't kept for offline use. PDFs show a "▶ video in the app" marker
at that step.

A recipe can also link to a video hosted elsewhere, e.g. YouTube; it shows a **Watch video**
button. Web imports fill this in when the page has a video.

## Cooking methods

Recipes carry cooking-method labels: oven, pot, pan, air fryer, grill, deep-fried, microwave, slow
cooker, pressure cooker, fridge, and no-bake (no heat at all). They're detected from the step text
when a recipe is imported, can be set with chips in the editor, and appear as filters on the
recipe list. To label recipes that have none, run `cookbook -retag` with the server stopped, or
call `POST /api/admin/retag` while it runs. Labels you set by hand are never changed.

## Data layout

Recipes are plain folders — back up the data directory and you have backed up everything.
`index.db` is disposable (rebuilt at every start). Deleted recipes go to `<data>/trash/`; the last 20
revisions of each recipe are kept in its `history/` folder. The PDF look is `<data>/templates/recipe.typ`
— edit it and PDFs recompile on next request.

## Recipe text syntax

| In a step | Shows as |
|---|---|
| `{{tojas}}` | `3 db tojás` (scaled, converted, localised) |
| `{{tojas.qty}}` `{{tojas.unit}}` `{{tojas.name}}` | the parts separately |
| `{{temp:180}}` | `180 °C` or `355 °F` |
| `[10:00]`, `[1:30:00]` | a tap-to-start timer |

## Tests

```sh
cd server && go test ./...
cd android && ./gradlew testDebugUnitTest
```
