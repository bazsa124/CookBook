package scrape

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"

	"cookbook/internal/ingest"
	"cookbook/internal/recipe"
)

var (
	ErrNoRecipe    = errors.New("no recipe data on this page")
	ErrUnsupported = errors.New("recipe is not in Hungarian or English")
)

// Draft is a recipe read from a page, before it is saved. Text is already in
// the recipe's language; Images are absolute URLs, best first.
type Draft struct {
	Recipe *recipe.Recipe
	Lang   string
	Images []string
	URL    string
}

var (
	ldRe     = regexp.MustCompile(`(?is)<script[^>]*application/ld\+json[^>]*>(.*?)</script>`)
	htmlLang = regexp.MustCompile(`(?i)<html[^>]*\blang="([a-zA-Z-]+)"`)
)

// FromURL fetches a page and reads its recipe.
func (f *Fetcher) FromURL(ctx context.Context, pageURL string) (*Draft, error) {
	body, err := f.Get(ctx, pageURL, 6<<20)
	if err != nil {
		return nil, err
	}
	return FromHTML(string(body), pageURL)
}

// FromHTML reads the schema.org Recipe from a page's JSON-LD.
func FromHTML(page, pageURL string) (*Draft, error) {
	node := findRecipe(page)
	if node == nil {
		return nil, ErrNoRecipe
	}
	lang := normLang(str(node["inLanguage"]))
	if lang == "" {
		if m := htmlLang.FindStringSubmatch(page); m != nil {
			lang = normLang(m[1])
		}
	}
	title := clean(str(node["name"]))
	if lang == "" {
		lang = ingest.LangOf(title + " " + strings.Join(strList(node["recipeIngredient"]), " "))
	}
	if lang != "hu" && lang != "en" {
		return nil, ErrUnsupported
	}
	if title == "" {
		return nil, ErrNoRecipe
	}

	r := &recipe.Recipe{
		Title:    recipe.L{lang: title},
		Tags:     []string{},
		Servings: yield(node["recipeYield"]),
		Source:   pageURL,
	}
	if d := clean(str(node["description"])); d != "" {
		r.Description = recipe.L{lang: truncate(d, 400)}
	}
	prep, cook, total := minutes(str(node["prepTime"])), minutes(str(node["cookTime"])), minutes(str(node["totalTime"]))
	if cook == 0 && total > prep {
		cook = total - prep
	}
	r.PrepMinutes, r.CookMinutes = prep, cook

	for _, line := range strList(node["recipeIngredient"]) {
		if ing, ok := ingest.ParseIngredient(webLine(clean(line)), "", lang); ok {
			ing.Name[lang] = ingest.LowerFirst(ing.Name[lang])
			ing.Category = recipe.GuessAisle(ing.Name)
			r.Ingredients = append(r.Ingredients, ing)
		}
	}
	dedupeIDs(r)

	for _, text := range instructions(node["recipeInstructions"], "") {
		text = ingest.Timers(ingest.Temps(text, lang), lang)
		r.Steps = append(r.Steps, recipe.Step{Text: recipe.L{lang: text}})
	}
	if len(r.Ingredients) == 0 && len(r.Steps) == 0 {
		return nil, ErrNoRecipe
	}

	r.Category = category(str(node["recipeCategory"])+" "+strings.Join(strList(node["recipeCategory"]), " "),
		title, str(node["keywords"])+" "+strings.Join(strList(node["keywords"]), " "))
	r.VideoURL = videoLink(node["video"])
	return &Draft{Recipe: r, Lang: lang, Images: images(node["image"]), URL: pageURL}, nil
}

// findRecipe walks every JSON-LD block, including @graph arrays, for a node
// whose @type is (or includes) Recipe.
func findRecipe(page string) map[string]any {
	for _, m := range ldRe.FindAllStringSubmatch(page, -1) {
		var v any
		if json.Unmarshal([]byte(strings.TrimSpace(m[1])), &v) != nil {
			continue
		}
		stack := []any{v}
		for len(stack) > 0 {
			x := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			switch t := x.(type) {
			case []any:
				stack = append(stack, t...)
			case map[string]any:
				if isType(t["@type"], "Recipe") {
					return t
				}
				if g, ok := t["@graph"]; ok {
					stack = append(stack, g)
				}
				if me, ok := t["mainEntity"]; ok {
					stack = append(stack, me)
				}
			}
		}
	}
	return nil
}

func isType(v any, want string) bool {
	switch t := v.(type) {
	case string:
		return t == want
	case []any:
		for _, x := range t {
			if s, ok := x.(string); ok && s == want {
				return true
			}
		}
	}
	return false
}

// --- field helpers -------------------------------------------------------------------

func str(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case []any:
		if len(t) > 0 {
			return str(t[0])
		}
	case map[string]any:
		if s, ok := t["@value"].(string); ok {
			return s
		}
		if s, ok := t["name"].(string); ok {
			return s
		}
	}
	return ""
}

func strList(v any) []string {
	switch t := v.(type) {
	case string:
		// keywords are usually "a, b, c"
		if strings.Contains(t, ",") {
			return strings.Split(t, ",")
		}
		return []string{t}
	case []any:
		var out []string
		for _, x := range t {
			if s := str(x); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

var (
	tagRe   = regexp.MustCompile(`(?s)<[^>]*>`)
	blockRe = regexp.MustCompile(`(?i)</?(p|li|br|div|h\d)[^>]*>`)
	spaceRe = regexp.MustCompile(`\s+`)
	numRe   = regexp.MustCompile(`\d+(?:[.,]\d+)?`)
	durRe   = regexp.MustCompile(`(?i)^P(?:(\d+)D)?(?:T(?:(\d+(?:\.\d+)?)H)?(?:(\d+(?:\.\d+)?)M)?(?:(\d+(?:\.\d+)?)S)?)?$`)
)

// clean strips tags and entities (some sites double-encode: "&amp;amp;").
func clean(s string) string {
	s = tagRe.ReplaceAllString(s, " ")
	for i := 0; i < 2 && strings.Contains(s, "&"); i++ {
		s = html.UnescapeString(s)
	}
	return strings.TrimSpace(spaceRe.ReplaceAllString(strings.ReplaceAll(s, " ", " "), " "))
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n])) + "…"
}

func normLang(s string) string {
	s = strings.ToLower(s)
	switch {
	case strings.HasPrefix(s, "hu"):
		return "hu"
	case strings.HasPrefix(s, "en"):
		return "en"
	case s == "":
		return ""
	}
	return s
}

// minutes parses ISO 8601 durations: "PT1H30M" -> 90.
func minutes(s string) int {
	m := durRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0
	}
	f := func(i int) float64 { v, _ := strconv.ParseFloat(m[i], 64); return v }
	return int(f(1)*24*60 + f(2)*60 + f(3) + f(4)/60 + 0.5)
}

// yield reads "4", 4, "Serves 4", ["4", "4 servings"], "4 személyre".
func yield(v any) float64 {
	for _, s := range append([]string{str(v)}, strList(v)...) {
		if m := numRe.FindString(s); m != "" {
			if n, err := strconv.ParseFloat(strings.ReplaceAll(m, ",", "."), 64); err == nil && n > 0 && n < 100 {
				return n
			}
		}
	}
	return 4
}

// instructions flattens every shape sites use: one HTML string, a list of
// strings, HowToStep objects, and HowToSections of steps. A section's name is
// prefixed to its first step, as the Word importer does.
func instructions(v any, prefix string) []string {
	var out []string
	add := func(text string) {
		text = clean(text)
		if text == "" {
			return
		}
		if prefix != "" {
			text = prefix + ": " + text
			prefix = ""
		}
		out = append(out, text)
	}
	switch t := v.(type) {
	case string:
		for _, part := range blockRe.Split(t, -1) {
			for _, line := range strings.Split(part, "\n") {
				add(line)
			}
		}
	case []any:
		for _, x := range t {
			sub := instructions(x, prefix)
			if len(sub) > 0 {
				prefix = ""
			}
			out = append(out, sub...)
		}
	case map[string]any:
		switch {
		case isType(t["@type"], "HowToSection") || t["itemListElement"] != nil:
			out = append(out, instructions(t["itemListElement"], clean(str(t["name"])))...)
		default:
			text := str(t["text"])
			if text == "" {
				text = str(t["name"])
			}
			add(text)
		}
	}
	return out
}

func images(v any) []string {
	var out []string
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case string:
			if strings.HasPrefix(t, "http") {
				out = append(out, t)
			}
		case []any:
			for _, y := range t {
				walk(y)
			}
		case map[string]any:
			walk(t["url"])
			if t["url"] == nil {
				walk(t["contentUrl"])
			}
		}
	}
	walk(v)
	return out
}

// dedupeIDs keeps ingredient ids unique within the recipe ("egg" in the dough
// and "egg" for glazing become egg and egg-2), as the Word importer does.
func dedupeIDs(r *recipe.Recipe) {
	seen := map[string]int{}
	for i := range r.Ingredients {
		id := r.Ingredients[i].ID
		seen[id]++
		if n := seen[id]; n > 1 {
			r.Ingredients[i].ID = fmt.Sprintf("%s-%d", id, n)
		}
	}
}

// --- category -----------------------------------------------------------------------

var categoryWords = []struct {
	id    string
	words []string
}{
	{"cocktail", []string{"cocktail", "koktel", "mojito", "spritz"}},
	{"drink", []string{"drink", "ital", "smoothie", "juice", "lemonade", "limonade", "shake", "latte", "tea", "coffee", "kave"}},
	{"sauce", []string{"sauce", "szosz", "martas", "dressing", "ontet", "pesto", "dip", "gravy", "salsa", "aioli", "mayonnaise"}},
	{"soup", []string{"soup", "leves", "chowder", "broth", "bisque", "ramen"}},
	{"salad", []string{"salad", "salata", "slaw"}},
	{"preserve", []string{"jam", "lekvar", "pickle", "savanyusag", "befott", "chutney", "szorp"}},
	{"bread", []string{"bread", "kenyer", "loaf", "bun", "buns", "zsemle", "kifli", "kalacs", "bagel", "focaccia", "brioche", "baguette"}},
	{"breakfast", []string{"breakfast", "reggeli", "pancake", "pancakes", "palacsinta", "granola", "porridge", "zabkasa", "omelette", "omlett", "brunch"}},
	{"appetizer", []string{"appetizer", "appetiser", "starter", "eloetel", "snack", "falatka", "finger"}},
	{"salty-bake", []string{"pogacsa", "pogi", "cracker", "kreker", "sos sutemeny", "sajtos rud", "grissini"}},
	{"dessert", []string{"dessert", "desszert", "sutemeny", "suti", "cake", "torta", "cookie", "cookies", "brownie", "pudding", "ice cream", "fagylalt", "edesseg", "muffin", "pite", "tart", "cheesecake", "kremes", "fank", "donut"}},
	{"side", []string{"side dish", "side", "koret"}},
	{"main", []string{"main", "foetel", "dinner", "lunch", "ebed", "vacsora", "curry", "stew", "porkolt", "paprikas", "casserole", "rakott"}},
}

// category maps the page's own category first, then the title, then its
// keywords; the first source that matches anything wins. Unknown -> main,
// which is what most recipe sites publish.
func category(sources ...string) string {
	for _, src := range sources {
		text := recipe.FoldText(src)
		if strings.TrimSpace(text) == "" {
			continue
		}
		for _, c := range categoryWords {
			for _, w := range c.words {
				if strings.Contains(text, " "+w+" ") {
					return c.id
				}
			}
		}
	}
	return "main"
}

var priceRe = regexp.MustCompile(`\$\s?\d+(?:\.\d+)?\**`)

// webLine tidies what recipe plugins put in ingredient strings before the
// shared parser sees them: cost annotations ("$0.38", budgetbytes) and nested
// or empty brackets ("buttermilk ((Note 1))", "sour cream (, full fat)").
// Every bracketed part becomes one trailing "(a; b)" note.
func webLine(s string) string {
	s = priceRe.ReplaceAllString(s, "")
	var outer, inner strings.Builder
	var notes []string
	depth := 0
	flush := func() {
		n := strings.Trim(strings.TrimSpace(inner.String()), ",;: ")
		if n != "" {
			notes = append(notes, n)
		}
		inner.Reset()
	}
	for _, r := range s {
		switch {
		case r == '(':
			if depth > 0 {
				inner.WriteByte(' ')
			}
			depth++
		case r == ')' && depth > 0:
			depth--
			if depth == 0 {
				flush()
			}
		case depth > 0:
			inner.WriteRune(r)
		default:
			outer.WriteRune(r)
		}
	}
	if depth > 0 { // unbalanced "(about 1 cup"
		flush()
	}
	out := strings.TrimSpace(spaceRe.ReplaceAllString(outer.String(), " "))
	out = strings.TrimRight(out, ", ")
	for i := range notes {
		notes[i] = spaceRe.ReplaceAllString(notes[i], " ")
	}
	if len(notes) > 0 {
		out += " (" + strings.Join(notes, "; ") + ")"
	}
	return out
}

var ytEmbed = regexp.MustCompile(`^https?://(?:www\.)?youtube(?:-nocookie)?\.com/embed/([A-Za-z0-9_-]{6,})`)

// videoLink picks a link a phone can open from a VideoObject: a YouTube
// embed becomes a normal watch link (opens in the YouTube app), otherwise the
// page or file URL.
func videoLink(v any) string {
	var obj map[string]any
	switch t := v.(type) {
	case map[string]any:
		obj = t
	case []any:
		for _, x := range t {
			if m, ok := x.(map[string]any); ok {
				obj = m
				break
			}
		}
	}
	if obj == nil {
		return ""
	}
	for _, k := range []string{"embedUrl", "contentUrl", "url"} {
		u := str(obj[k])
		if m := ytEmbed.FindStringSubmatch(u); m != nil {
			return "https://www.youtube.com/watch?v=" + m[1]
		}
		if strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://") {
			return u
		}
	}
	return ""
}
