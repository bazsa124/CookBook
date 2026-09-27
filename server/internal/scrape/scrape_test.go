package scrape

import (
	"errors"
	"testing"

	"cookbook/internal/recipe"
)

// A page shaped like the hard cases seen in the wild: the recipe nested in an
// @graph with other nodes, @type as a list, HowToSections, an ImageObject,
// yield as an array, entities in text, British temperatures.
const page = `<!doctype html><html lang="en-GB"><head>
<script type="application/ld+json">{"@context":"https://schema.org","@graph":[
 {"@type":"WebSite","name":"Example"},
 {"@type":["Recipe","NewsArticle"],"name":"Tomato &amp;amp; basil soup",
  "description":"<p>A <b>quick</b> soup.</p>",
  "recipeYield":["4","4 servings"],"prepTime":"PT15M","totalTime":"PT1H",
  "recipeCategory":"Soup","keywords":"soup, vegetarian",
  "image":{"@type":"ImageObject","url":"https://img.example.com/soup.jpg"},
  "video":{"@type":"VideoObject","name":"How to","embedUrl":"https://www.youtube.com/embed/dQw4w9WgXcQ?rel=0"},
  "recipeIngredient":["2 tbsp olive oil","1 large onion, finely chopped ($0.40)",
    "800g chopped tomatoes","1 tsp sugar ((optional))","salt (, to taste)"],
  "recipeInstructions":[
   {"@type":"HowToSection","name":"Soup","itemListElement":[
     {"@type":"HowToStep","text":"Heat the oil in a large pot and cook the onion for 10 mins."},
     {"@type":"HowToStep","text":"Add the tomatoes and simmer for 20 minutes."}]},
   {"@type":"HowToSection","name":"Croutons","itemListElement":[
     {"@type":"HowToStep","text":"Bake the bread at 200C for 8 mins."}]}]}
]}</script></head><body></body></html>`

func TestFromHTML(t *testing.T) {
	d, err := FromHTML(page, "https://example.com/soup")
	if err != nil {
		t.Fatal(err)
	}
	r := d.Recipe
	if d.Lang != "en" || r.Title["en"] != "Tomato & basil soup" {
		t.Errorf("lang=%s title=%q", d.Lang, r.Title["en"])
	}
	if r.Description["en"] != "A quick soup." {
		t.Errorf("description %q", r.Description["en"])
	}
	if r.Servings != 4 || r.PrepMinutes != 15 || r.CookMinutes != 45 {
		t.Errorf("servings=%v prep=%d cook=%d", r.Servings, r.PrepMinutes, r.CookMinutes)
	}
	if r.Category != "soup" || r.Source != "https://example.com/soup" {
		t.Errorf("category=%s source=%s", r.Category, r.Source)
	}
	if r.VideoURL != "https://www.youtube.com/watch?v=dQw4w9WgXcQ" {
		t.Errorf("video %q", r.VideoURL)
	}
	if len(d.Images) != 1 || d.Images[0] != "https://img.example.com/soup.jpg" {
		t.Errorf("images %v", d.Images)
	}
	want := []struct{ id, name, note string }{
		{"olive_oil", "olive oil", ""},
		{"onion", "onion", "large, finely chopped"},
		{"tomato", "tomatoes", "chopped"}, // merges with plain tomatoes
		{"sugar", "sugar", ""},
		{"salt", "salt", "to taste"},
	}
	if len(r.Ingredients) != len(want) {
		t.Fatalf("ingredients: %+v", r.Ingredients)
	}
	for i, w := range want {
		g := r.Ingredients[i]
		if g.ID != w.id || g.Name["en"] != w.name || g.Note["en"] != w.note {
			t.Errorf("ingredient %d: got %s %q %q, want %s %q %q", i, g.ID, g.Name["en"], g.Note["en"], w.id, w.name, w.note)
		}
	}
	if !r.Ingredients[3].Optional {
		t.Error("((optional)) not recognised")
	}
	steps := []string{
		"Soup: Heat the oil in a large pot and cook the onion for [10:00] mins.",
		"Add the tomatoes and simmer for [20:00] minutes.",
		"Croutons: Bake the bread at {{temp:200}} for [8:00] mins.",
	}
	for i, s := range steps {
		if i >= len(r.Steps) || r.Steps[i].Text["en"] != s {
			t.Errorf("step %d: got %+v, want %q", i, r.Steps, s)
			break
		}
	}
	r.ID = "tomato-basil-soup" // assigned by the server on save
	r.Normalize(recipe.Units())
	if err := r.Validate(); err != nil {
		t.Errorf("scraped recipe does not validate: %v", err)
	}
	if m := recipe.DetectMethods(r); len(m) < 2 || m[0] != "oven" {
		t.Errorf("methods %v", m)
	}
}

func TestNoRecipe(t *testing.T) {
	_, err := FromHTML(`<html><script type="application/ld+json">{"@type":"Article"}</script></html>`, "x")
	if !errors.Is(err, ErrNoRecipe) {
		t.Errorf("got %v", err)
	}
	_, err = FromHTML(`<html lang="de"><script type="application/ld+json">{"@type":"Recipe","name":"Suppe","recipeIngredient":["1 Zwiebel"]}</script></html>`, "x")
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("German page: got %v", err)
	}
}

func TestWebLine(t *testing.T) {
	cases := map[string]string{
		"1/2 cup buttermilk ((Note 1 for easy substitution))":  "1/2 cup buttermilk (Note 1 for easy substitution)",
		"1/2 cup sour cream (, full fat)":                      "1/2 cup sour cream (full fat)",
		"1 medium yellow onion (diced, (about 1½ cups) $1.13)": "1 medium yellow onion (diced, about 1½ cups)",
		"4 Tbsp salted butter ($0.38)":                         "4 Tbsp salted butter",
		"2 ribs celery (diced, (about 1 cup":                   "2 ribs celery (diced, about 1 cup)",
	}
	for in, want := range cases {
		if got := webLine(in); got != want {
			t.Errorf("%q:\n got %q\nwant %q", in, got, want)
		}
	}
}

func TestDurationsAndYield(t *testing.T) {
	for in, want := range map[string]int{"PT1H30M": 90, "PT45M": 45, "P0DT2H": 120, "PT0.5H": 30, "": 0, "garbage": 0} {
		if got := minutes(in); got != want {
			t.Errorf("minutes(%q) = %d, want %d", in, got, want)
		}
	}
	for in, want := range map[any]float64{"Serves 6": 6, "4 személyre": 4, float64(8): 8, "a lot": 4} {
		if got := yield(in); got != want {
			t.Errorf("yield(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestRobots(t *testing.T) {
	rb := parseRobots(`
User-agent: Googlebot
Disallow: /

User-agent: *
Disallow: /search
Disallow: /*?adag=*
Allow: /search/help
Crawl-delay: 10
`)
	for path, want := range map[string]bool{
		"/recept/gulyas":        true,
		"/search?q=x":           false,
		"/search/help":          true,
		"/recept/gulyas?adag=6": false,
	} {
		if got := rb.allowed(path); got != want {
			t.Errorf("allowed(%q) = %v, want %v", path, got, want)
		}
	}
	if rb.delay.Seconds() != 10 {
		t.Errorf("crawl-delay %v", rb.delay)
	}
	if !parseRobots("User-agent: *\nDisallow:\n").allowed("/anything") {
		t.Error("empty Disallow must allow everything")
	}
}
