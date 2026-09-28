// Package feeds imports new recipes from recipe sites on a schedule.
//
// Each source is an RSS feed or a sitemap. A run looks at a source's newest
// links, skips everything seen before (imported, deleted since, or not a
// recipe), and imports at most PerRun new recipes per source. Seen links are
// remembered in <data>/feeds.json, so a recipe you delete is never imported
// again.
package feeds

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"cookbook/internal/scrape"
	"cookbook/internal/store"
)

type Source struct {
	Name    string `json:"name"`
	Lang    string `json:"lang"`
	URL     string `json:"url"`
	Kind    string `json:"kind"` // rss | sitemap
	Enabled bool   `json:"enabled"`
	// SitemapFilter picks child sitemaps of a sitemap index (regexp on URL).
	SitemapFilter string `json:"sitemap_filter,omitempty"`
	// Include keeps only page URLs matching this regexp.
	Include string `json:"include,omitempty"`
}

type Config struct {
	Enabled       bool     `json:"enabled"`
	IntervalHours int      `json:"interval_hours"`
	PerSource     int      `json:"per_source"`
	AutoTranslate bool     `json:"auto_translate"`
	Sources       []Source `json:"sources"`
}

// Defaults are sites checked to publish schema.org Recipe data and to allow
// crawling recipe pages in robots.txt. allrecipes.com and seriouseats.com are
// left out on purpose: they refuse automated requests.
func Defaults() Config {
	return Config{
		Enabled:       true,
		IntervalHours: 24,
		PerSource:     2,
		AutoTranslate: true,
		Sources: []Source{
			{Name: "nosalty", Lang: "hu", Kind: "sitemap", Enabled: true, URL: "https://www.nosalty.hu/fresh-recipes-sitemap.xml", Include: `/recept/`},
			{Name: "sobors", Lang: "hu", Kind: "rss", Enabled: true, URL: "https://sobors.hu/feed/", Include: `/receptek/`},
			{Name: "bbcgoodfood", Lang: "en", Kind: "sitemap", Enabled: true, URL: "https://www.bbcgoodfood.com/sitemap.xml", SitemapFilter: `recipe`, Include: `/recipes/`},
			{Name: "recipetineats", Lang: "en", Kind: "rss", Enabled: true, URL: "https://www.recipetineats.com/feed/"},
			{Name: "budgetbytes", Lang: "en", Kind: "rss", Enabled: true, URL: "https://www.budgetbytes.com/feed/"},
		},
	}
}

// ParseConfig reads the "feeds" object of config.json. Missing fields keep
// their defaults; a "sources" list, when given, replaces the default list.
func ParseConfig(raw json.RawMessage) (Config, error) {
	c := Defaults()
	if len(raw) == 0 {
		return c, nil
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return c, fmt.Errorf("config.json feeds: %w", err)
	}
	if c.IntervalHours < 1 {
		c.IntervalHours = 1
	}
	if c.PerSource < 0 {
		c.PerSource = 0
	}
	for _, s := range c.Sources {
		for _, p := range []string{s.Include, s.SitemapFilter} {
			if _, err := regexp.Compile(p); err != nil {
				return Defaults(), fmt.Errorf("config.json feeds, source %q: bad pattern %q: %w", s.Name, p, err)
			}
		}
		if s.Kind != "rss" && s.Kind != "sitemap" {
			return Defaults(), fmt.Errorf("config.json feeds, source %q: kind must be rss or sitemap", s.Name)
		}
	}
	return c, nil
}

// --- state -----------------------------------------------------------------------

type Seen struct {
	At     time.Time `json:"at"`
	ID     string    `json:"id,omitempty"`
	Result string    `json:"result"` // imported | skipped: … | duplicate
	Source string    `json:"source,omitempty"`
}

type SourceStatus struct {
	LastRun   time.Time `json:"last_run"`
	LastError string    `json:"last_error,omitempty"`
	Imported  int       `json:"imported"` // total, all runs
}

type State struct {
	LastRun time.Time               `json:"last_run"`
	Seen    map[string]Seen         `json:"seen"`
	Sources map[string]SourceStatus `json:"sources"`
}

// SaveFunc stores a scraped recipe and returns its id. It is the server's job
// (media, index, PDF, translation); this package only finds and reads pages.
type SaveFunc func(ctx context.Context, d *scrape.Draft, site string) (string, error)

type Runner struct {
	Fetcher *scrape.Fetcher
	Save    SaveFunc
	// Known returns the id of an existing recipe with this source URL, if any.
	Known func(url string) (string, bool)

	path string
	mu   sync.Mutex
	cfg  Config
	st   State
	busy bool
}

func NewRunner(dataDir string, cfg Config, f *scrape.Fetcher, save SaveFunc, known func(string) (string, bool)) *Runner {
	r := &Runner{Fetcher: f, Save: save, Known: known, path: filepath.Join(dataDir, "feeds.json"), cfg: cfg}
	r.st = State{Seen: map[string]Seen{}, Sources: map[string]SourceStatus{}}
	if b, err := os.ReadFile(r.path); err == nil {
		if err := json.Unmarshal(b, &r.st); err != nil {
			log.Printf("feeds.json: %v (starting fresh)", err)
		}
	}
	if r.st.Seen == nil {
		r.st.Seen = map[string]Seen{}
	}
	if r.st.Sources == nil {
		r.st.Sources = map[string]SourceStatus{}
	}
	return r
}

func (r *Runner) saveState() {
	b, _ := json.MarshalIndent(r.st, "", "  ")
	if err := store.WriteFileAtomic(r.path, b); err != nil {
		log.Printf("feeds.json: %v", err)
	}
}

// --- status ------------------------------------------------------------------------

type Recent struct {
	URL string `json:"url"`
	Seen
}

type Status struct {
	Config  Config                  `json:"config"`
	Running bool                    `json:"running"`
	LastRun time.Time               `json:"last_run"`
	NextRun time.Time               `json:"next_run"`
	Sources map[string]SourceStatus `json:"sources"`
	Recent  []Recent                `json:"recent"`
}

func (r *Runner) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := Status{Config: r.cfg, Running: r.busy, LastRun: r.st.LastRun, Sources: map[string]SourceStatus{}}
	for k, v := range r.st.Sources {
		st.Sources[k] = v
	}
	if r.cfg.Enabled {
		st.NextRun = r.nextRun(time.Now())
	}
	for u, s := range r.st.Seen {
		st.Recent = append(st.Recent, Recent{u, s})
	}
	sort.Slice(st.Recent, func(i, j int) bool { return st.Recent[i].At.After(st.Recent[j].At) })
	if len(st.Recent) > 30 {
		st.Recent = st.Recent[:30]
	}
	return st
}

// nextRun is when the next scheduled run is due: now, if there has never
// been one. The caller passes now, so a "due now" answer can't drift past it.
func (r *Runner) nextRun(now time.Time) time.Time {
	if r.st.LastRun.IsZero() {
		return now
	}
	return r.st.LastRun.Add(time.Duration(r.cfg.IntervalHours) * time.Hour)
}

// due reports whether a scheduled run should start at now. It reads the clock
// once: comparing time.Now() against a nextRun() that itself returned a later
// time.Now() made a fresh install never due, so the schedule never started.
// The caller holds r.mu.
func (r *Runner) due(now time.Time) bool {
	return r.cfg.Enabled && !now.Before(r.nextRun(now))
}

// --- running -----------------------------------------------------------------------

var ErrBusy = errors.New("an import run is already in progress")

// Loop runs the schedule until ctx ends. The first check waits a few minutes
// so a freshly booted laptop is not busy importing while everything starts.
func (r *Runner) Loop(ctx context.Context, firstDelay time.Duration) {
	t := time.NewTimer(firstDelay)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		r.mu.Lock()
		due := r.due(time.Now())
		r.mu.Unlock()
		if due {
			if err := r.Run(ctx); err != nil && !errors.Is(err, ErrBusy) {
				log.Printf("feeds: %v", err)
			}
		}
		t.Reset(10 * time.Minute)
	}
}

// Run does one pass over every enabled source.
func (r *Runner) Run(ctx context.Context) error {
	r.mu.Lock()
	if r.busy {
		r.mu.Unlock()
		return ErrBusy
	}
	r.busy = true
	cfg := r.cfg
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.busy = false
		r.st.LastRun = time.Now().UTC()
		r.saveState()
		r.mu.Unlock()
	}()

	total := 0
	for _, src := range cfg.Sources {
		if !src.Enabled || cfg.PerSource == 0 {
			continue
		}
		n, err := r.runSource(ctx, src, cfg.PerSource)
		total += n
		r.mu.Lock()
		ss := r.st.Sources[src.Name]
		ss.LastRun, ss.Imported, ss.LastError = time.Now().UTC(), ss.Imported+n, ""
		if err != nil {
			ss.LastError = err.Error()
			log.Printf("feeds %s: %v", src.Name, err)
		}
		r.st.Sources[src.Name] = ss
		r.saveState()
		r.mu.Unlock()
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	log.Printf("feeds: run finished, %d new recipes", total)
	return nil
}

// maxTries bounds the pages fetched per source per run, so a feed full of
// articles that are not recipes cannot turn a run into a crawl.
const maxTries = 8

func (r *Runner) runSource(ctx context.Context, src Source, want int) (int, error) {
	urls, err := Discover(ctx, r.Fetcher, src)
	if err != nil {
		return 0, err
	}
	imported, tries := 0, 0
	for _, u := range urls {
		if imported >= want || tries >= maxTries || ctx.Err() != nil {
			break
		}
		r.mu.Lock()
		_, seen := r.st.Seen[u]
		r.mu.Unlock()
		if seen {
			continue
		}
		if id, ok := r.Known(u); ok {
			r.mark(u, Seen{ID: id, Result: "duplicate", Source: src.Name})
			continue
		}
		tries++
		d, err := r.Fetcher.FromURL(ctx, u)
		switch {
		case errors.Is(err, scrape.ErrNoRecipe), errors.Is(err, scrape.ErrUnsupported), errors.Is(err, scrape.ErrDisallowed),
			errors.Is(err, scrape.ErrGone):
			r.mark(u, Seen{Result: "skipped: " + err.Error(), Source: src.Name})
			continue
		case errors.Is(err, scrape.ErrBlocked):
			return imported, err // the site said stop; stop
		case err != nil:
			log.Printf("feeds %s: %s: %v", src.Name, u, err) // transient: retry next run
			continue
		}
		id, err := r.Save(ctx, d, src.Name)
		if err != nil {
			r.mark(u, Seen{Result: "skipped: " + err.Error(), Source: src.Name})
			continue
		}
		r.mark(u, Seen{ID: id, Result: "imported", Source: src.Name})
		log.Printf("feeds %s: imported %s from %s", src.Name, id, u)
		imported++
	}
	return imported, nil
}

func (r *Runner) mark(u string, s Seen) {
	s.At = time.Now().UTC()
	r.mu.Lock()
	r.st.Seen[u] = s
	r.saveState()
	r.mu.Unlock()
}

// MarkImported records a manual import, so the schedule never re-imports it.
func (r *Runner) MarkImported(u, id string) {
	r.mark(u, Seen{ID: id, Result: "imported", Source: "manual"})
}

// --- discovery ---------------------------------------------------------------------

var (
	itemLinkRe = regexp.MustCompile(`(?s)<item\b.*?<link>\s*(?:<!\[CDATA\[)?\s*([^<\s\]]+)`)
	atomLinkRe = regexp.MustCompile(`(?s)<entry\b.*?<link[^>]*href="([^"]+)"`)
	urlRe      = regexp.MustCompile(`(?s)<url>(.*?)</url>`)
	smRe       = regexp.MustCompile(`(?s)<sitemap>(.*?)</sitemap>`)
	locRe      = regexp.MustCompile(`<loc>\s*(?:<!\[CDATA\[)?\s*([^<\s\]]+)`)
	lastmodRe  = regexp.MustCompile(`<lastmod>\s*([^<\s]+)`)
)

type entry struct{ loc, lastmod string }

func entries(body string, re *regexp.Regexp) []entry {
	var out []entry
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		e := entry{}
		if l := locRe.FindStringSubmatch(m[1]); l != nil {
			e.loc = l[1]
		}
		if l := lastmodRe.FindStringSubmatch(m[1]); l != nil {
			e.lastmod = l[1]
		}
		if e.loc != "" {
			out = append(out, e)
		}
	}
	return out
}

// Discover returns a source's candidate recipe URLs, newest first.
func Discover(ctx context.Context, f *scrape.Fetcher, src Source) ([]string, error) {
	body, err := f.Get(ctx, src.URL, 20<<20)
	if err != nil {
		return nil, err
	}
	text := string(body)
	var urls []string
	switch src.Kind {
	case "rss":
		for _, m := range itemLinkRe.FindAllStringSubmatch(text, -1) {
			urls = append(urls, m[1])
		}
		for _, m := range atomLinkRe.FindAllStringSubmatch(text, -1) {
			urls = append(urls, m[1])
		}
	case "sitemap":
		if subs := entries(text, smRe); len(subs) > 0 {
			// A sitemap index: pick the newest matching child sitemap.
			var filter *regexp.Regexp
			if src.SitemapFilter != "" {
				filter = regexp.MustCompile(src.SitemapFilter)
			}
			var pick []entry
			for _, e := range subs {
				if filter == nil || filter.MatchString(e.loc) {
					pick = append(pick, e)
				}
			}
			if len(pick) == 0 {
				return nil, fmt.Errorf("no child sitemap matches %q", src.SitemapFilter)
			}
			sort.SliceStable(pick, func(i, j int) bool {
				if pick[i].lastmod != pick[j].lastmod {
					return pick[i].lastmod > pick[j].lastmod
				}
				return pick[i].loc > pick[j].loc // e.g. 2026-Q3 after 2026-Q2
			})
			if body, err = f.Get(ctx, pick[0].loc, 20<<20); err != nil {
				return nil, err
			}
			text = string(body)
		}
		es := entries(text, urlRe)
		sort.SliceStable(es, func(i, j int) bool { return es[i].lastmod > es[j].lastmod })
		for _, e := range es {
			urls = append(urls, e.loc)
		}
	default:
		return nil, fmt.Errorf("unknown source kind %q", src.Kind)
	}
	var include *regexp.Regexp
	if src.Include != "" {
		include = regexp.MustCompile(src.Include)
	}
	var out []string
	seen := map[string]bool{}
	for _, u := range urls {
		u = strings.TrimSpace(u)
		if seen[u] || (include != nil && !include.MatchString(u)) {
			continue
		}
		seen[u] = true
		out = append(out, u)
		if len(out) >= 60 {
			break
		}
	}
	return out, nil
}
