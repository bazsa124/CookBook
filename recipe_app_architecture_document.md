# Recipe System Architecture & Specification

## 1. System Overview

A self-hosted, offline-first-capable recipe management system. It treats every recipe as an isolated "repository" containing structured bilingual data, media, and a compiled Typst PDF. The client is a native Android application securely connected to the home server via Tailscale.

## 2. Infrastructure & Network Connections

* **Host Machine:** Old laptop running a Linux distribution (e.g., Ubuntu Server or Debian).
* **Containerization:** Docker & Docker Compose. This ensures the recipe API doesn't conflict with other services running on the laptop.
* **Network (Tailscale):**
  * The server and the Android client are both nodes on the same Tailscale network (Tailnet).
  * The Android app will communicate with the server using the server's static Tailscale IP (e.g., `100.x.y.z`) or MagicDNS name.
  * *Benefit:* Zero port forwarding required on your home router; total privacy; works seamlessly whether you are on home Wi-Fi or cellular data.

## 3. Recommended Tech Stack

* **Backend Server:** **Node.js (Express or Fastify)** or **Python (FastAPI)**.
  * *Why:* Both handle JSON manipulation flawlessly and can easily spawn child processes to run the Typst CLI.
* **Document Engine:** **Typst**.
  * *Why:* Much faster than LaTeX, easier syntax, installed natively on the server.
* **Database (Indexing):** **SQLite**.
  * *Why:* While recipes are stored as folders, parsing hundreds of JSON files for a search query is slow. An SQLite database will act as a fast *index* for metadata (tags, categories, ingredients).
* **Client (Android):** **React Native (Expo)** or **Kotlin (Jetpack Compose)**.
  * *Why:* React Native has excellent libraries for Swipe-card UIs and PDF viewing, allowing rapid development.
* **Translation Service:** **Google Gemini API (Free Tier)** or **Groq (Llama 3 API - Free Tier)**.
  * *Why:* These offer generous free tiers for developers and are exceptionally good at structured JSON-to-JSON outputs, which is necessary to avoid breaking the mustache variables during translation.

## 4. Core Features (Detailed)

### A. The Editor Experience
The editor abstracts away Typst complexity, acting as a structured form:
1. **General Data (Metadata):** Inputs for Name, Description, Category, and Tags.
2. **Ingredients List:** Structured input: `Quantity` | `Unit` | `Item` (e.g., 2 | cups | Flour).
3. **Instructions:** A step-by-step block editor with media linking.
4. **Media Manager:** Drag-and-drop auto-compressing media zone.
5. **AI Auto-Translation:** A one-tap button in the editor that automatically translates missing language fields (e.g., translating HU inputs to EN) without overwriting manual edits, while preserving all variable tags and standard IDs.

### B. The Viewer & Discovery Engine
1. **Advanced Search & Reverse Search (Pantry Matching):** Powered by the JSON index. Filter by tags, exclude ingredients, or match available pantry items.
2. **Fast View (Swipe UI):** Stack of cards showing Hero Image, Title, Prep Time, and Tags. Swipe left to pass, right to open.
3. **Favorites:** Local bookmarking system.

### C. Active Kitchen Features (Cook Mode)
1. **Dynamic Yield Scaling:** Instantly calculates new amounts based on serving size changes.
2. **Unit Localization:** Instantly flips the entire recipe between Imperial and Metric.
3. **Distraction-Free Cook Mode:** Locks screen awake, massive text, one instruction at a time.
4. **Inline Timers:** Time values (e.g., "[10:00]") become tap-to-start timers.
5. **Seamless Bilingual Toggle:** UI reads global state (HU/EN) and pulls matching language keys.

### D. The Grocery System
1. **Automated List Generation:** Aggregates ingredients from selected JSONs, combining identical items based on standardized IDs.
2. **Aisle Grouping:** Auto-sorts lists based on backend category tags (Produce, Meat, Dairy).
3. **Static List Integration:** Manual text-input for ad-hoc household items (e.g., "Szemeteszsák").

## 5. Directory & Data Structure

The backend stores recipes in the file system.

```
/data/recipes/
  ├── /nagymama-gulyas/
  │    ├── meta.json
  │    ├── main.typ
  │    ├── output.pdf
  │    └── /media/
  │         └── hero.jpg
```

### Core Schema (`meta.json`)

```json
{
  "id": "nagymama-gulyas",
  "title": { "en": "Grandma's Goulash", "hu": "Nagymama Gulyáslevese" },
  "tags": ["soup", "traditional", "beef"],
  "base_servings": 4,
  "ingredients": [
    {
      "id": "beef_shank",
      "qty": 500,
      "unit": { "en": "g", "hu": "g" },
      "name": { "en": "beef shank", "hu": "marhalábszár" },
      "category": "meat"
    }
  ],
  "steps": [
    { 
      "en": "Cut the {{beef_shank.qty}}{{beef_shank.unit}} of {{beef_shank.name}} into cubes. Boil for [120:00].", 
      "hu": "Vágd kockára a {{beef_shank.qty}}{{beef_shank.unit}} {{beef_shank.name}}t. Főzd [120:00] percig." 
    }
  ]
}
```

## 6. Working Logic & Pipelines

### Pipeline 1: Creating/Editing a Recipe
1. User fills out the form in the Android app.
2. App sends a request with JSON data and media to the server.
3. Server creates/updates the folder and `meta.json`.
4. Server updates the SQLite Search Index.
5. Server executes Typst CLI to generate `output.pdf`.
6. Server returns success.

### Pipeline 2: Dynamic Scaling & Cook Mode
1. User selects "Scale to 6 servings" (from base 4).
2. App calculates multiplier: `6 / 4 = 1.5`.
3. App multiplies every `qty` in the ingredients array by 1.5.
4. App uses regex to find mustache tags (`{{beef_shank.qty}}`) in the steps, mapping them to the scaled values.
5. App parses timer brackets `[MM:SS]` into interactive buttons.

### Pipeline 3: Grocery Aggregation
1. User selects recipes for the cart.
2. Server reads `meta.json` for selected recipes.
3. Server groups and sums ingredients by their exact `id`.
4. Server returns the aggregated list, sorted by `category`, merged with static list items.

### Pipeline 4: AI Auto-Translation
1. User writes a recipe entirely in one language (e.g., Hungarian) in the Editor.
2. User taps "Auto-Translate to English".
3. The App sends the partial JSON to the server.
4. The server constructs a strict system prompt for the Free AI API (e.g., Gemini or Groq) instructing it to:
   * Read the existing `hu` fields.
   * Generate the corresponding `en` translations.
   * **CRITICAL:** Do not alter or translate the IDs (`id`), mustache variables (`{{...}}`), or timer brackets (`[...]`).
   * Return a valid, fully populated JSON structure.
5. The API returns the completed bilingual JSON.
6. The Editor UI is updated with the translated fields, allowing the user to review or tweak the AI's translation before saving.