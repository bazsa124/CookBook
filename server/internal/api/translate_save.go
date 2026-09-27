package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"cookbook/internal/recipe"
	"cookbook/internal/store"
	"cookbook/internal/translate"
)

// Translate-and-save, for one recipe (the app's "Translate" banner) or the
// whole book (a background job). POST /api/translate stays as the editor's
// preview that saves nothing; these write, with the usual rev check, history
// snapshot, index update and PDF compile.

func otherLang(r *recipe.Recipe, to string) string {
	for _, l := range recipe.Langs {
		if l != to && r.Title.Has(l) {
			return l
		}
	}
	return ""
}

func (s *Server) translateAndSave(ctx context.Context, id, to string) (*translate.Result, map[string]string, error) {
	r, err := s.Store.Load(id)
	if err != nil {
		return nil, nil, err
	}
	from := otherLang(r, to)
	if from == "" {
		return nil, nil, fmt.Errorf("%s has no text in another language to translate from", id)
	}
	res, err := translate.Translate(ctx, s.Translator, r, from, to)
	if err != nil {
		return nil, nil, err
	}
	if res.Filled == 0 {
		return res, nil, nil
	}
	r.Normalize(s.units)
	if err := r.Validate(); err != nil {
		return nil, nil, err
	}
	if err := s.Store.Update(r); err != nil {
		return nil, nil, err
	}
	if err := s.Index.Put(r); err != nil {
		log.Printf("index %s: %v", id, err)
	}
	pdf := s.Typst.CompileAll(ctx, r)
	res.Recipe = r
	return res, pdf, nil
}

func translateStatus(err error) int {
	switch {
	case errors.Is(err, translate.ErrUnavailable):
		return http.StatusServiceUnavailable
	default:
		return http.StatusBadGateway
	}
}

// POST /api/recipes/{id}/translate?to=en
func (s *Server) translateRecipe(w http.ResponseWriter, r *http.Request) {
	to := r.URL.Query().Get("to")
	if to != "hu" && to != "en" {
		writeError(w, http.StatusBadRequest, "to must be hu or en")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	res, pdf, err := s.translateAndSave(ctx, r.PathValue("id"), to)
	if err != nil {
		var conflict *store.ConflictError
		if errors.Is(err, store.ErrNotFound) || errors.As(err, &conflict) {
			s.storeError(w, err)
		} else {
			writeError(w, translateStatus(err), err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"recipe": res.Recipe, "filled": res.Filled, "rejected": res.Rejected, "provider": res.Provider, "pdf": pdf,
	})
}

// --- whole-book job ------------------------------------------------------------

type batchFailure struct {
	ID    string `json:"id"`
	Error string `json:"error"`
}

type batchStatus struct {
	Running    bool           `json:"running"`
	Total      int            `json:"total"`
	Done       int            `json:"done"`
	Translated int            `json:"translated"`
	Current    string         `json:"current,omitempty"`
	Failed     []batchFailure `json:"failed"`
	Started    time.Time      `json:"started,omitempty"`
	Finished   time.Time      `json:"finished,omitempty"`
}

type batchJob struct {
	mu sync.Mutex
	st batchStatus
}

func (b *batchJob) snapshot() batchStatus {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := b.st
	st.Failed = append([]batchFailure{}, st.Failed...)
	return st
}

func (b *batchJob) update(f func(*batchStatus)) {
	b.mu.Lock()
	f(&b.st)
	b.mu.Unlock()
}

// GET /api/translate/all
func (s *Server) batchState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.batch.snapshot())
}

// POST /api/translate/all: fill every missing language in the book, one
// recipe at a time, in the background.
func (s *Server) batchStart(w http.ResponseWriter, r *http.Request) {
	if s.Translator == nil {
		writeError(w, http.StatusServiceUnavailable, translate.ErrUnavailable.Error())
		return
	}
	type task struct{ id, to string }
	ids, err := s.Store.IDs()
	if err != nil {
		s.storeError(w, err)
		return
	}
	var tasks []task
	for _, id := range ids {
		rec, err := s.Store.Load(id)
		if err != nil {
			continue
		}
		for _, l := range recipe.Langs {
			if len(translate.Missing(rec, otherLang(rec, l), l)) > 0 && otherLang(rec, l) != "" {
				tasks = append(tasks, task{id, l})
			}
		}
	}

	s.batch.mu.Lock()
	if s.batch.st.Running {
		s.batch.mu.Unlock()
		writeJSON(w, http.StatusConflict, s.batch.snapshot())
		return
	}
	s.batch.st = batchStatus{Running: true, Total: len(tasks), Failed: []batchFailure{}, Started: time.Now().UTC()}
	s.batch.mu.Unlock()

	go func() {
		for i, t := range tasks {
			s.batch.update(func(b *batchStatus) { b.Current = t.id })
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			res, _, err := s.translateAndSave(ctx, t.id, t.to)
			cancel()
			s.batch.update(func(b *batchStatus) {
				b.Done++
				if err != nil {
					b.Failed = append(b.Failed, batchFailure{t.id, err.Error()})
				} else if res.Filled > 0 {
					b.Translated++
				}
			})
			if err != nil {
				log.Printf("translate %s -> %s: %v", t.id, t.to, err)
			} else {
				log.Printf("translate %s -> %s: %d fields, %d rejected (%d/%d)", t.id, t.to, res.Filled, len(res.Rejected), i+1, len(tasks))
			}
			time.Sleep(2 * time.Second) // stay well inside free-tier rate limits
		}
		s.batch.update(func(b *batchStatus) { b.Running = false; b.Current = ""; b.Finished = time.Now().UTC() })
	}()
	writeJSON(w, http.StatusAccepted, s.batch.snapshot())
}
