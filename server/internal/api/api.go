// Package api is the HTTP surface the Android app talks to.
package api

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"cookbook/internal/feeds"
	"cookbook/internal/grocery"
	"cookbook/internal/index"
	"cookbook/internal/media"
	"cookbook/internal/recipe"
	"cookbook/internal/scrape"
	"cookbook/internal/store"
	"cookbook/internal/translate"
	"cookbook/internal/typst"
)

type Server struct {
	Version    string
	Token      string
	Port       int // for the pairing link
	Store      *store.Store
	Index      *index.Index
	Typst      *typst.Compiler
	Translator translate.Provider
	Grocery    *grocery.List
	Fetcher    *scrape.Fetcher
	Feeds      *feeds.Runner
	// AutoTranslate: imported recipes also get the other language.
	AutoTranslate bool
	// FFmpeg makes video poster frames; "" when not installed.
	FFmpeg string

	units   map[string]recipe.Unit
	batch   batchJob
	started time.Time
}

func (s *Server) Handler() http.Handler {
	s.units = recipe.Units()
	s.started = time.Now()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/units", s.getUnits)
	mux.HandleFunc("GET /api/categories", s.categories)
	mux.HandleFunc("GET /api/export", s.export)
	mux.HandleFunc("GET /api/recipes", s.search)
	mux.HandleFunc("POST /api/recipes", s.create)
	mux.HandleFunc("GET /api/recipes/{id}", s.get)
	mux.HandleFunc("PUT /api/recipes/{id}", s.update)
	mux.HandleFunc("DELETE /api/recipes/{id}", s.delete)
	mux.HandleFunc("GET /api/recipes/{id}/media", s.listMedia)
	mux.HandleFunc("POST /api/recipes/{id}/media", s.uploadMedia)
	mux.HandleFunc("GET /api/recipes/{id}/media/{file}", s.getMedia)
	mux.HandleFunc("DELETE /api/recipes/{id}/media/{file}", s.deleteMedia)
	mux.HandleFunc("GET /api/recipes/{id}/pdf", s.pdf)
	mux.HandleFunc("GET /api/recipes/{id}/history", s.history)
	mux.HandleFunc("GET /api/recipes/{id}/history/{file}", s.historyFile)
	mux.HandleFunc("GET /api/ingredients", s.ingredients)
	mux.HandleFunc("GET /api/tags", s.tags)
	mux.HandleFunc("POST /api/translate", s.translate)
	mux.HandleFunc("POST /api/import", s.importURL)
	mux.HandleFunc("GET /api/feeds", s.feedStatus)
	mux.HandleFunc("POST /api/feeds/run", s.feedRun)
	mux.HandleFunc("POST /api/admin/retag", s.retag)
	mux.HandleFunc("POST /api/recipes/{id}/translate", s.translateRecipe)
	mux.HandleFunc("GET /api/translate/all", s.batchState)
	mux.HandleFunc("POST /api/translate/all", s.batchStart)
	mux.HandleFunc("GET /api/grocery", s.getGrocery)
	mux.HandleFunc("POST /api/grocery", s.postGrocery)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})

	// Everything under /api/ needs the token. The few pages outside it are
	// public on the tailnet (landing page, APK) or loopback-only (pairing).
	root := http.NewServeMux()
	root.Handle("/api/", s.auth(mux))
	root.HandleFunc("GET /{$}", s.landing)
	root.HandleFunc("GET /app.apk", s.apk)
	root.HandleFunc("GET /pair", s.pairPage)
	root.HandleFunc("GET /pair.png", s.pairQR)
	root.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	return logRequests(root)
}

// --- middleware --------------------------------------------------------------

func (s *Server) auth(next http.Handler) http.Handler {
	want := []byte("Bearer " + s.Token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("Authorization"))
		if subtle.ConstantTimeCompare(got, want) != 1 {
			writeError(w, http.StatusUnauthorized, "missing or wrong token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, sw.status, time.Since(start).Round(time.Millisecond))
	})
}

// --- helpers -----------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string, details ...string) {
	body := map[string]any{"error": msg}
	if len(details) > 0 {
		body["details"] = details
	}
	writeJSON(w, status, body)
}

// writeCached serves JSON with an ETag, so the app's offline refresh costs a
// 304 on mobile data when nothing changed.
func writeCached(w http.ResponseWriter, r *http.Request, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sum := sha256.Sum256(b)
	etag := `"` + hex.EncodeToString(sum[:12]) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(b)
}

func (s *Server) storeError(w http.ResponseWriter, err error) {
	var conflict *store.ConflictError
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.As(err, &conflict):
		writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "current": conflict.Current})
	default:
		log.Printf("error: %v", err)
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func multi(r *http.Request, key string) []string {
	var out []string
	for _, v := range r.URL.Query()[key] {
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

// --- meta endpoints ----------------------------------------------------------

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	tr := map[string]any{"available": s.Translator != nil}
	if s.Translator != nil {
		tr["provider"] = s.Translator.Name()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":   s.Version,
		"os":        runtime.GOOS + "/" + runtime.GOARCH,
		"uptime":    int(time.Since(s.started).Seconds()),
		"recipes":   s.Index.Count(),
		"typst":     map[string]any{"available": s.Typst.Available(), "version": s.Typst.Version()},
		"translate": tr,
		"time":      time.Now().UTC(),
	})
}

func (s *Server) getUnits(w http.ResponseWriter, r *http.Request) {
	writeCached(w, r, recipe.UnitList())
}

func (s *Server) categories(w http.ResponseWriter, r *http.Request) {
	writeCached(w, r, map[string]any{
		"recipe": recipe.RecipeCategories,
		"aisle":  recipe.AisleCategories,
		"method": recipe.Methods,
	})
}

func (s *Server) ingredients(w http.ResponseWriter, r *http.Request) {
	c, err := s.Index.Catalog()
	if err != nil {
		s.storeError(w, err)
		return
	}
	writeCached(w, r, c)
}

func (s *Server) tags(w http.ResponseWriter, r *http.Request) {
	t, err := s.Index.Tags()
	if err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// export is everything the app needs to work offline, in one response.
func (s *Server) export(w http.ResponseWriter, r *http.Request) {
	ids, err := s.Store.IDs()
	if err != nil {
		s.storeError(w, err)
		return
	}
	rs := make([]*recipe.Recipe, 0, len(ids))
	for _, id := range ids {
		rec, err := s.Store.Load(id)
		if err != nil {
			log.Printf("export: %v", err)
			continue
		}
		rs = append(rs, rec)
	}
	writeCached(w, r, map[string]any{
		"recipes":        rs,
		"units":          recipe.UnitList(),
		"categories":     map[string]any{"recipe": recipe.RecipeCategories, "aisle": recipe.AisleCategories, "method": recipe.Methods},
		"aisle_keywords": recipe.AisleKeywords(),
	})
}

// --- recipes -----------------------------------------------------------------

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	res, err := s.Index.Search(index.Query{
		Text:     q.Get("q"),
		Tags:     multi(r, "tag"),
		Category: q.Get("category"),
		Include:  multi(r, "include"),
		Exclude:  multi(r, "exclude"),
		Pantry:   multi(r, "pantry"),
	})
	if err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	rec, err := s.Store.Load(r.PathValue("id"))
	if err != nil {
		s.storeError(w, err)
		return
	}
	writeCached(w, r, rec)
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) { s.save(w, r, true) }
func (s *Server) update(w http.ResponseWriter, r *http.Request) { s.save(w, r, false) }

// save is Pipeline 1: validate, write folder + meta.json, index, compile PDF.
func (s *Server) save(w http.ResponseWriter, r *http.Request, create bool) {
	var rec recipe.Recipe
	dec := json.NewDecoder(io.LimitReader(r.Body, 2<<20))
	if err := dec.Decode(&rec); err != nil {
		writeError(w, http.StatusBadRequest, "invalid recipe JSON: "+err.Error())
		return
	}
	if create {
		if rec.ID == "" {
			rec.ID = recipe.Slug(rec.Title.Get("hu"), "-")
		}
		rec.ID = recipe.Slug(rec.ID, "-")
		if rec.ID == "" {
			rec.ID = "recipe"
		}
	} else {
		rec.ID = r.PathValue("id")
	}
	if rec.Tags == nil {
		rec.Tags = []string{}
	}
	rec.Normalize(s.units)
	for i := range rec.Ingredients {
		if rec.Ingredients[i].Category == "" {
			rec.Ingredients[i].Category = recipe.GuessAisle(rec.Ingredients[i].Name)
		}
	}
	if err := rec.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "recipe is not valid", strings.Split(err.Error(), "\n")...)
		return
	}
	if !create {
		if missing := s.missingMedia(&rec); len(missing) > 0 {
			writeError(w, http.StatusBadRequest, "media files not uploaded", missing...)
			return
		}
	}

	var err error
	if create {
		// New recipes have no media folder yet; references are dropped rather
		// than failing, and the app uploads media after the first save.
		rec.Hero = ""
		for i := range rec.Steps {
			rec.Steps[i].Media = ""
		}
		err = s.Store.Create(&rec)
	} else {
		err = s.Store.Update(&rec)
	}
	if err != nil {
		s.storeError(w, err)
		return
	}
	if err := s.Index.Put(&rec); err != nil {
		log.Printf("index %s: %v", rec.ID, err)
	}
	pdf := s.Typst.CompileAll(r.Context(), &rec)
	status := http.StatusOK
	if create {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{"recipe": rec, "pdf": pdf})
}

func (s *Server) missingMedia(rec *recipe.Recipe) []string {
	var missing []string
	check := func(name string) {
		if name == "" {
			return
		}
		p, err := s.Store.MediaPath(rec.ID, name)
		if err == nil {
			if _, err = os.Stat(p); err == nil {
				return
			}
		}
		missing = append(missing, name)
	}
	check(rec.Hero)
	for _, st := range rec.Steps {
		check(st.Media)
	}
	return missing
}

func (s *Server) delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Store.Delete(id); err != nil {
		s.storeError(w, err)
		return
	}
	s.Index.Remove(id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	h, err := s.Store.History(r.PathValue("id"))
	if err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h)
}

func (s *Server) historyFile(w http.ResponseWriter, r *http.Request) {
	b, err := s.Store.HistoryFile(r.PathValue("id"), r.PathValue("file"))
	if err != nil {
		s.storeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(b)
}

// --- media -------------------------------------------------------------------

const maxUpload = 20 << 20

func (s *Server) listMedia(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.Store.Load(id); err != nil {
		s.storeError(w, err)
		return
	}
	names, err := s.Store.ListMedia(id)
	if err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, names)
}

// uploadMedia accepts one image (multipart field "file", or a raw image body),
// compresses it, and returns the stored name. The recipe references it on the
// next save; the name is random so it can be cached forever.
func (s *Server) uploadMedia(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, media.MaxVideo+1<<20)
	var body io.Reader = r.Body
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		mr, err := r.MultipartReader()
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		for {
			part, err := mr.NextPart()
			if err != nil {
				writeError(w, http.StatusBadRequest, "multipart field \"file\" is required")
				return
			}
			if part.FormName() == "file" {
				body = part
				break
			}
		}
	}
	// Sniff instead of trusting the declared type: phones label files freely.
	br := bufio.NewReaderSize(body, 4096)
	head, _ := br.Peek(512)
	if ext := media.SniffVideo(head); ext != "" {
		s.saveVideo(w, r, id, ext, br)
		return
	}
	data, err := io.ReadAll(io.LimitReader(br, maxUpload+1))
	if err != nil || len(data) > maxUpload {
		writeError(w, http.StatusRequestEntityTooLarge, "image too large (20 MB max)")
		return
	}
	jpg, err := media.Compress(data)
	if err != nil {
		writeError(w, http.StatusUnsupportedMediaType, "not a supported image (jpeg, png, gif, webp) or video (mp4, webm)")
		return
	}
	sum := sha256.Sum256(jpg)
	name := hex.EncodeToString(sum[:8]) + ".jpg"
	if err := s.Store.SaveMedia(id, name, jpg); err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"file": name, "bytes": len(jpg)})
}

// saveVideo streams the upload to disk while hashing it, never holding it in
// memory, then names it by content and makes a poster frame if it can.
func (s *Server) saveVideo(w http.ResponseWriter, r *http.Request, id, ext string, body io.Reader) {
	tmp, err := s.Store.MediaTemp(id)
	if err != nil {
		s.storeError(w, err)
		return
	}
	defer os.Remove(tmp.Name()) // no-op once adopted
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(body, media.MaxVideo+1))
	cerr := tmp.Close()
	if err != nil || cerr != nil {
		writeError(w, http.StatusBadRequest, "upload interrupted")
		return
	}
	if n > media.MaxVideo {
		writeError(w, http.StatusRequestEntityTooLarge, "video too large (300 MB max)")
		return
	}
	name := hex.EncodeToString(h.Sum(nil)[:8]) + "." + ext
	if err := s.Store.AdoptMedia(id, name, tmp.Name()); err != nil {
		s.storeError(w, err)
		return
	}
	poster := ""
	if s.FFmpeg != "" {
		p, _ := s.Store.MediaPath(id, name)
		out, _ := s.Store.MediaPath(id, recipe.PosterName(name))
		if err := media.Poster(r.Context(), s.FFmpeg, p, out); err != nil {
			log.Printf("poster %s/%s: %v", id, name, err)
		} else {
			poster = recipe.PosterName(name)
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{"file": name, "bytes": n, "poster": poster})
}

func (s *Server) getMedia(w http.ResponseWriter, r *http.Request) {
	p, err := s.Store.MediaPath(r.PathValue("id"), r.PathValue("file"))
	if err != nil {
		s.storeError(w, err)
		return
	}
	if _, err := os.Stat(p); err != nil {
		writeError(w, http.StatusNotFound, "media not found")
		return
	}
	// Uploaded names are content hashes, so they never change meaning.
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeFile(w, r, p)
}

func (s *Server) deleteMedia(w http.ResponseWriter, r *http.Request) {
	id, file := r.PathValue("id"), r.PathValue("file")
	rec, err := s.Store.Load(id)
	if err != nil {
		s.storeError(w, err)
		return
	}
	if rec.Hero == file {
		writeError(w, http.StatusConflict, "file is the hero image; save the recipe without it first")
		return
	}
	for _, st := range rec.Steps {
		if st.Media == file {
			writeError(w, http.StatusConflict, "file is used by a step; save the recipe without it first")
			return
		}
	}
	if err := s.Store.DeleteMedia(id, file); err != nil {
		s.storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- pdf ---------------------------------------------------------------------

func (s *Server) pdf(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rec, err := s.Store.Load(id)
	if err != nil {
		s.storeError(w, err)
		return
	}
	lang := r.URL.Query().Get("lang")
	if lang == "" || !rec.Title.Has(lang) {
		langs := rec.Languages()
		if len(langs) == 0 {
			writeError(w, http.StatusNotFound, "recipe has no title in any language")
			return
		}
		if lang == "" || !rec.Title.Has(lang) {
			lang = langs[0]
		}
	}
	if s.Typst.Stale(id, lang) {
		if err := s.Typst.Compile(r.Context(), id, lang); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, typst.ErrUnavailable) {
				status = http.StatusServiceUnavailable
			}
			writeError(w, status, err.Error())
			return
		}
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s-%s.pdf"`, id, lang))
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, s.Store.PDFPath(id, lang))
}

// --- translation -------------------------------------------------------------

func (s *Server) translate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Recipe *recipe.Recipe `json:"recipe"`
		From   string         `json:"from"`
		To     string         `json:"to"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(&req); err != nil || req.Recipe == nil {
		writeError(w, http.StatusBadRequest, "body must be {recipe, from, to}")
		return
	}
	if req.From == "" {
		req.From = "hu"
		if req.To == "hu" {
			req.From = "en"
		}
	}
	if req.To == "" {
		req.To = "en"
		if req.From == "en" {
			req.To = "hu"
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	res, err := translate.Translate(ctx, s.Translator, req.Recipe, req.From, req.To)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, translate.ErrUnavailable) {
			status = http.StatusServiceUnavailable
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// --- grocery -----------------------------------------------------------------

func (s *Server) groceryView(w http.ResponseWriter, st *grocery.State) {
	writeJSON(w, http.StatusOK, grocery.Aggregate(st, s.Store.Load, s.units))
}

func (s *Server) getGrocery(w http.ResponseWriter, r *http.Request) {
	st, err := s.Grocery.Load()
	if err != nil {
		s.storeError(w, err)
		return
	}
	s.groceryView(w, st)
}

func (s *Server) postGrocery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Ops []grocery.Op `json:"ops"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 256<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "body must be {ops: [...]}")
		return
	}
	st, err := s.Grocery.Apply(req.Ops)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.groceryView(w, st)
}
