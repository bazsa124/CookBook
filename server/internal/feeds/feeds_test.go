package feeds

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cookbook/internal/scrape"
)

func recipePage(name string) string {
	return fmt.Sprintf(`<html lang="en"><script type="application/ld+json">
{"@type":"Recipe","name":%q,"recipeIngredient":["2 eggs"],"recipeInstructions":["Boil the eggs for 8 minutes."]}
</script></html>`, name)
}

// A fake site: a feed listing a recipe, an article (no recipe data), a page
// robots.txt forbids, another recipe, and one already in the book.
func fakeSite(t *testing.T) *httptest.Server {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			fmt.Fprint(w, "User-agent: *\nDisallow: /private/\n")
		case "/feed":
			fmt.Fprint(w, "<rss><channel><link>"+srv.URL+"</link>")
			for _, p := range []string{"/r/soup", "/news", "/private/r", "/r/cake", "/r/known"} {
				fmt.Fprintf(w, "<item><title>x</title><link>%s%s</link></item>", srv.URL, p)
			}
			fmt.Fprint(w, "</channel></rss>")
		case "/news":
			fmt.Fprint(w, "<html><p>ten tips</p></html>")
		case "/private/r":
			t.Error("robots.txt was ignored")
		default:
			if strings.HasPrefix(r.URL.Path, "/r/") {
				fmt.Fprint(w, recipePage(strings.TrimPrefix(r.URL.Path, "/r/")))
				return
			}
			http.NotFound(w, r)
		}
	}))
	return srv
}

func TestRunImportsOnlyNewRecipes(t *testing.T) {
	site := fakeSite(t)
	defer site.Close()
	f := scrape.NewFetcher()
	f.MinDelay = 0

	var saved []string
	save := func(_ context.Context, d *scrape.Draft, _ string) (string, error) {
		saved = append(saved, d.Recipe.Title["en"])
		return "id-" + d.Recipe.Title["en"], nil
	}
	known := func(u string) (string, bool) { return "existing", strings.HasSuffix(u, "/r/known") }

	cfg := Config{Enabled: true, IntervalHours: 24, PerSource: 5,
		Sources: []Source{{Name: "fake", Lang: "en", Kind: "rss", Enabled: true, URL: site.URL + "/feed"}}}
	dir := t.TempDir()
	r := NewRunner(dir, cfg, f, save, known)
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Join(saved, ",") != "soup,cake" {
		t.Fatalf("saved %v", saved)
	}
	st := r.Status()
	if st.Sources["fake"].Imported != 2 {
		t.Errorf("status %+v", st.Sources)
	}
	if res := st.Recent; len(res) != 5 {
		t.Errorf("recent %+v", res)
	}

	// A second run, from a fresh process reading feeds.json, imports nothing:
	// imported, skipped, disallowed and known links are all remembered.
	saved = nil
	r2 := NewRunner(dir, cfg, f, save, known)
	if err := r2.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(saved) != 0 {
		t.Errorf("re-imported %v", saved)
	}
}

func TestPerSourceLimit(t *testing.T) {
	site := fakeSite(t)
	defer site.Close()
	f := scrape.NewFetcher()
	f.MinDelay = 0
	n := 0
	save := func(context.Context, *scrape.Draft, string) (string, error) { n++; return fmt.Sprint(n), nil }
	cfg := Config{Enabled: true, IntervalHours: 24, PerSource: 1,
		Sources: []Source{{Name: "fake", Kind: "rss", Enabled: true, URL: site.URL + "/feed"}}}
	r := NewRunner(t.TempDir(), cfg, f, save, func(string) (string, bool) { return "", false })
	r.Run(context.Background())
	if n != 1 {
		t.Errorf("imported %d, want 1 (per_source)", n)
	}
}

func TestParseConfig(t *testing.T) {
	c, err := ParseConfig([]byte(`{"per_source": 5}`))
	if err != nil || c.PerSource != 5 || len(c.Sources) != len(Defaults().Sources) || !c.Enabled {
		t.Errorf("partial config should keep defaults: %+v %v", c, err)
	}
	if _, err := ParseConfig([]byte(`{"sources":[{"name":"x","kind":"rss","include":"("}]}`)); err == nil {
		t.Error("bad regexp accepted")
	}
}
