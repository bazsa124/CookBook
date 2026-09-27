package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"cookbook/internal/feeds"
	"cookbook/internal/media"
	"cookbook/internal/recipe"
	"cookbook/internal/scrape"
)

// Web import: one link on request (the app's "Import from web" and share
// target), or many on a schedule (feeds.Runner, which calls importDraft).

// siteTag turns "https://www.nosalty.hu/recept/x" into "nosalty".
func siteTag(pageURL string) string {
	u, err := url.Parse(pageURL)
	if err != nil {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if i := strings.LastIndex(host, "."); i > 0 {
		host = host[:i]
	}
	if i := strings.LastIndex(host, "."); i >= 0 {
		host = host[i+1:]
	}
	return recipe.Slug(host, "-")
}

// sourceIndex maps source URLs to recipe ids, for duplicate checks.
func (s *Server) sourceIndex() map[string]string {
	out := map[string]string{}
	ids, _ := s.Store.IDs()
	for _, id := range ids {
		if r, err := s.Store.Load(id); err == nil && r.Source != "" {
			out[r.Source] = id
		}
	}
	return out
}

// KnownSource reports an existing recipe imported from u.
func (s *Server) KnownSource(u string) (string, bool) {
	id, ok := s.sourceIndex()[u]
	return id, ok
}

// ImportDraft saves a scraped recipe: tags, photo, index, PDF, and (if
// configured) the other language.
func (s *Server) ImportDraft(ctx context.Context, d *scrape.Draft, site string) (string, error) {
	r := d.Recipe
	r.ID = recipe.Slug(r.Title[d.Lang], "-")
	if site == "" {
		site = siteTag(d.URL)
	}
	r.Tags = append(r.Tags, "imported")
	if site != "" {
		r.Tags = append(r.Tags, site)
	}
	recipe.AddDetectedMethods(r)
	r.Normalize(s.units)
	if err := r.Validate(); err != nil {
		return "", err
	}
	if err := s.Store.Create(r); err != nil {
		return "", err
	}
	// The photo is best-effort: a recipe without one is still a recipe.
	for _, img := range d.Images {
		data, err := s.Fetcher.Get(ctx, img, 15<<20)
		if err != nil {
			continue
		}
		jpg, err := media.Compress(data)
		if err != nil {
			continue
		}
		if err := s.Store.SaveMedia(r.ID, "hero.jpg", jpg); err == nil {
			r.Hero = "hero.jpg"
			if err := s.Store.Update(r); err != nil {
				log.Printf("import %s: %v", r.ID, err)
			}
		}
		break
	}
	if err := s.Index.Put(r); err != nil {
		log.Printf("index %s: %v", r.ID, err)
	}
	s.Typst.CompileAll(ctx, r)
	if s.AutoTranslate && s.Translator != nil {
		to := "en"
		if d.Lang == "en" {
			to = "hu"
		}
		tctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
		if _, _, err := s.translateAndSave(tctx, r.ID, to); err != nil {
			log.Printf("import %s: translation to %s failed: %v", r.ID, to, err)
		}
		cancel()
	}
	return r.ID, nil
}

// POST /api/import {url, preview}
func (s *Server) importURL(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL     string `json:"url"`
		Preview bool   `json:"preview"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil || req.URL == "" {
		writeError(w, http.StatusBadRequest, "body must be {url}")
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	if id, ok := s.KnownSource(req.URL); ok {
		rec, _ := s.Store.Load(id)
		writeJSON(w, http.StatusOK, map[string]any{"recipe": rec, "duplicate": true})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	d, err := s.Fetcher.FromURL(ctx, req.URL)
	if err != nil {
		status := http.StatusBadGateway
		switch {
		case errors.Is(err, scrape.ErrNoRecipe), errors.Is(err, scrape.ErrUnsupported):
			status = http.StatusUnprocessableEntity
		case errors.Is(err, scrape.ErrDisallowed), errors.Is(err, scrape.ErrBlocked):
			status = http.StatusForbidden
		}
		writeError(w, status, err.Error())
		return
	}
	if req.Preview {
		writeJSON(w, http.StatusOK, map[string]any{"recipe": d.Recipe, "images": d.Images})
		return
	}
	id, err := s.ImportDraft(ctx, d, "")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.Feeds != nil {
		s.Feeds.MarkImported(req.URL, id)
	}
	rec, _ := s.Store.Load(id)
	writeJSON(w, http.StatusCreated, map[string]any{"recipe": rec, "duplicate": false})
}

// GET /api/feeds
func (s *Server) feedStatus(w http.ResponseWriter, r *http.Request) {
	if s.Feeds == nil {
		writeError(w, http.StatusServiceUnavailable, "automatic import is not configured")
		return
	}
	writeJSON(w, http.StatusOK, s.Feeds.Status())
}

// POST /api/feeds/run: start a run now, in the background.
func (s *Server) feedRun(w http.ResponseWriter, r *http.Request) {
	if s.Feeds == nil {
		writeError(w, http.StatusServiceUnavailable, "automatic import is not configured")
		return
	}
	if s.Feeds.Status().Running {
		writeJSON(w, http.StatusConflict, s.Feeds.Status())
		return
	}
	go func() {
		if err := s.Feeds.Run(context.Background()); err != nil && !errors.Is(err, feeds.ErrBusy) {
			log.Printf("feeds: %v", err)
		}
	}()
	time.Sleep(100 * time.Millisecond) // let the status flip to running
	writeJSON(w, http.StatusAccepted, s.Feeds.Status())
}

// POST /api/admin/retag: add detected cooking methods to recipes that have
// none. Hand-set methods are left alone; every change keeps a history copy.
func (s *Server) retag(w http.ResponseWriter, r *http.Request) {
	ids, err := s.Store.IDs()
	if err != nil {
		s.storeError(w, err)
		return
	}
	changed := map[string][]string{}
	for _, id := range ids {
		rec, err := s.Store.Load(id)
		if err != nil || !recipe.AddDetectedMethods(rec) {
			continue
		}
		rec.Normalize(s.units)
		if err := s.Store.Update(rec); err != nil {
			log.Printf("retag %s: %v", id, err)
			continue
		}
		s.Index.Put(rec)
		var methods []string
		for _, t := range rec.Tags {
			if recipe.IsMethod(t) {
				methods = append(methods, t)
			}
		}
		changed[id] = methods
	}
	writeJSON(w, http.StatusOK, map[string]any{"changed": len(changed), "recipes": changed})
}
