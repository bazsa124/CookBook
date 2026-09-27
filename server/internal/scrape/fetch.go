// Package scrape reads recipes from web pages.
//
// It only uses the schema.org Recipe data (JSON-LD) that recipe sites publish
// for search engines: stable across redesigns, and no HTML scraping. Fetching
// is deliberately polite: it identifies itself, obeys robots.txt, and waits
// between requests to the same host.
package scrape

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const UserAgent = "CookBook/0.1 (personal recipe collection; +https://github.com/)"

var (
	ErrDisallowed = errors.New("robots.txt disallows this page")
	ErrBlocked    = errors.New("the site refuses automated access")
	ErrGone       = errors.New("page not found")
)

type Fetcher struct {
	Client   *http.Client
	MinDelay time.Duration // between two requests to the same host

	mu     sync.Mutex
	last   map[string]time.Time
	robots map[string]*robots
}

func NewFetcher() *Fetcher {
	return &Fetcher{
		Client:   &http.Client{Timeout: 30 * time.Second},
		MinDelay: 3 * time.Second,
		last:     map[string]time.Time{},
		robots:   map[string]*robots{},
	}
}

// Get fetches u, honouring robots.txt and the per-host delay. limit caps the
// body size.
func (f *Fetcher) Get(ctx context.Context, u string, limit int64) ([]byte, error) {
	pu, err := url.Parse(u)
	if err != nil || (pu.Scheme != "http" && pu.Scheme != "https") {
		return nil, fmt.Errorf("not a web address: %q", u)
	}
	rb := f.robotsFor(ctx, pu)
	if !rb.allowed(pu.EscapedPath() + queryPart(pu)) {
		return nil, ErrDisallowed
	}
	if err := f.wait(ctx, pu.Host, rb.delay); err != nil {
		return nil, err
	}
	return f.raw(ctx, u, limit)
}

func queryPart(u *url.URL) string {
	if u.RawQuery == "" {
		return ""
	}
	return "?" + u.RawQuery
}

func (f *Fetcher) raw(ctx context.Context, u string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "hu,en;q=0.8")
	resp, err := f.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
		return nil, fmt.Errorf("%w (HTTP %d)", ErrBlocked, resp.StatusCode)
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return nil, fmt.Errorf("%w (HTTP %d)", ErrGone, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// wait enforces the gap between requests to one host. The slot is reserved
// before sleeping, so concurrent callers queue up instead of all waking at once.
func (f *Fetcher) wait(ctx context.Context, host string, crawlDelay time.Duration) error {
	gap := f.MinDelay
	if crawlDelay > gap {
		gap = crawlDelay
	}
	f.mu.Lock()
	next := f.last[host].Add(gap)
	now := time.Now()
	if next.Before(now) {
		next = now
	}
	f.last[host] = next
	f.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Until(next)):
		return nil
	}
}

// --- robots.txt -------------------------------------------------------------------

type rule struct {
	allow bool
	re    *regexp.Regexp
	len   int
}

type robots struct {
	rules []rule
	delay time.Duration
}

func (f *Fetcher) robotsFor(ctx context.Context, u *url.URL) *robots {
	key := u.Scheme + "://" + u.Host
	f.mu.Lock()
	rb, ok := f.robots[key]
	f.mu.Unlock()
	if ok {
		return rb
	}
	rb = &robots{}
	body, err := f.raw(ctx, key+"/robots.txt", 512<<10)
	switch {
	case err == nil:
		rb = parseRobots(string(body))
	case errors.Is(err, ErrBlocked):
		rb = parseRobots("User-agent: *\nDisallow: /\n")
	}
	// A missing or unreadable robots.txt means "no restrictions" (the
	// convention), but a 403 on it is taken as a refusal of bots.
	f.mu.Lock()
	f.robots[key] = rb
	f.mu.Unlock()
	return rb
}

// parseRobots keeps the group for our agent if there is one, else "*".
// Wildcards (* and $) are supported; the longest matching rule wins, and allow
// wins ties, as in RFC 9309.
func parseRobots(body string) *robots {
	type group struct {
		agents []string
		lines  [][2]string
	}
	var groups []*group
	var cur *group
	lastWasAgent := false
	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		line := sc.Text()
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k, v = strings.ToLower(strings.TrimSpace(k)), strings.TrimSpace(v)
		if k == "user-agent" {
			if cur == nil || !lastWasAgent {
				cur = &group{}
				groups = append(groups, cur)
			}
			cur.agents = append(cur.agents, strings.ToLower(v))
			lastWasAgent = true
			continue
		}
		lastWasAgent = false
		if cur != nil {
			cur.lines = append(cur.lines, [2]string{k, v})
		}
	}
	pick := func(match func(string) bool) *group {
		for _, g := range groups {
			for _, a := range g.agents {
				if match(a) {
					return g
				}
			}
		}
		return nil
	}
	g := pick(func(a string) bool { return strings.Contains(a, "cookbook") })
	if g == nil {
		g = pick(func(a string) bool { return a == "*" })
	}
	rb := &robots{}
	if g == nil {
		return rb
	}
	for _, kv := range g.lines {
		switch kv[0] {
		case "allow", "disallow":
			if kv[1] == "" {
				continue // "Disallow:" with nothing allows everything
			}
			rb.rules = append(rb.rules, rule{allow: kv[0] == "allow", re: robotsPattern(kv[1]), len: len(kv[1])})
		case "crawl-delay":
			if s, err := strconv.ParseFloat(kv[1], 64); err == nil && s > 0 && s < 120 {
				rb.delay = time.Duration(s * float64(time.Second))
			}
		}
	}
	return rb
}

func robotsPattern(p string) *regexp.Regexp {
	anchored := strings.HasSuffix(p, "$")
	p = strings.TrimSuffix(p, "$")
	parts := strings.Split(p, "*")
	for i := range parts {
		parts[i] = regexp.QuoteMeta(parts[i])
	}
	expr := "^" + strings.Join(parts, ".*")
	if anchored {
		expr += "$"
	}
	return regexp.MustCompile(expr)
}

func (rb *robots) allowed(path string) bool {
	if path == "" {
		path = "/"
	}
	best, allow := -1, true
	for _, r := range rb.rules {
		if r.re.MatchString(path) && (r.len > best || (r.len == best && r.allow)) {
			best, allow = r.len, r.allow
		}
	}
	return allow
}
