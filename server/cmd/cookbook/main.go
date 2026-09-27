// Command cookbook is the recipe server.
//
//	cookbook [-data DIR] [-listen host:port,...] [-print-token]
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"cookbook/internal/api"
	"cookbook/internal/config"
	"cookbook/internal/feeds"
	"cookbook/internal/grocery"
	"cookbook/internal/index"
	"cookbook/internal/media"
	"cookbook/internal/netbind"
	"cookbook/internal/recipe"
	"cookbook/internal/scrape"
	"cookbook/internal/store"
	"cookbook/internal/translate"
	"cookbook/internal/typst"
)

var version = "0.1.0"

func main() {
	dataDir := flag.String("data", "", "data directory (default: $COOKBOOK_DATA or the per-OS default)")
	listen := flag.String("listen", "", "comma-separated host:port list; overrides the loopback+Tailscale default")
	printToken := flag.Bool("print-token", false, "print the API token and exit")
	warm := flag.Bool("warm-pdfs", true, "compile missing or stale PDFs in the background at start")
	retag := flag.Bool("retag", false, "add detected cooking methods to recipes that have none, then exit")
	flag.Parse()

	cfg, err := config.Load(*dataDir)
	if err != nil {
		log.Fatal(err)
	}
	if *printToken {
		fmt.Println(cfg.Token)
		return
	}
	if *listen != "" {
		cfg.Listen = strings.Split(*listen, ",")
	}

	st, err := store.New(cfg.DataDir)
	if err != nil {
		log.Fatal(err)
	}
	if *retag {
		retagAll(st)
		return
	}
	ix, err := index.Open(cfg.DataDir + string(os.PathSeparator) + "index.db")
	if err != nil {
		log.Fatal(err)
	}
	defer ix.Close()

	recipes := loadAll(st)
	if err := ix.Rebuild(recipes); err != nil {
		log.Fatalf("index rebuild: %v", err)
	}
	log.Printf("data %s: %d recipes indexed", cfg.DataDir, len(recipes))

	tc := typst.New(cfg.Typst, st)
	if err := tc.Install(); err != nil {
		log.Fatalf("typst template: %v", err)
	}
	if tc.Available() {
		log.Printf("%s", tc.Version())
	} else {
		log.Printf("typst not found: PDFs disabled until it is installed")
	}
	tr := translate.FromConfig(cfg.GeminiKey, cfg.GeminiModel, cfg.GroqKey, cfg.GroqModel)
	if tr != nil {
		log.Printf("translation via %s", tr.Name())
	} else {
		log.Printf("no GEMINI_API_KEY / GROQ_API_KEY: auto-translate disabled")
	}

	feedCfg, err := feeds.ParseConfig(cfg.Feeds)
	if err != nil {
		log.Printf("%v - using the default import sources", err)
	}
	fetcher := scrape.NewFetcher()
	srv := &api.Server{
		Version: version, Token: cfg.Token, Port: cfg.Port, Store: st, Index: ix,
		Typst: tc, Translator: tr, Grocery: grocery.NewList(cfg.DataDir),
		Fetcher: fetcher, AutoTranslate: feedCfg.AutoTranslate,
		FFmpeg: media.FFmpeg(cfg.FFmpeg),
	}
	if srv.FFmpeg == "" {
		log.Printf("ffmpeg not found: videos work, but without poster frames")
	}
	srv.Feeds = feeds.NewRunner(cfg.DataDir, feedCfg, fetcher, srv.ImportDraft, srv.KnownSource)
	httpSrv := &http.Server{
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	binder := &netbind.Binder{Server: httpSrv, Port: cfg.Port, Explicit: cfg.Listen}
	if bound := binder.Sync(); len(bound) == 0 {
		log.Fatal("could not bind any address")
	}
	if len(cfg.Listen) == 0 && len(netbind.TailscaleIPs()) == 0 {
		log.Printf("no Tailscale address yet: loopback only, re-checking every minute")
	}
	log.Printf("pair a phone: open http://localhost:%d/pair on this machine and scan the QR code", cfg.Port)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go binder.Watch(ctx, time.Minute)
	if feedCfg.Enabled {
		log.Printf("automatic import: %d sources, every %dh, up to %d new recipes per source",
			countEnabled(feedCfg.Sources), feedCfg.IntervalHours, feedCfg.PerSource)
		go srv.Feeds.Loop(ctx, 3*time.Minute)
	}
	if *warm && tc.Available() {
		go warmPDFs(ctx, tc, recipes)
	}

	<-ctx.Done()
	log.Printf("shutting down")
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	httpSrv.Shutdown(shutdown)
}

func loadAll(st *store.Store) []*recipe.Recipe {
	ids, err := st.IDs()
	if err != nil {
		log.Fatal(err)
	}
	out := make([]*recipe.Recipe, 0, len(ids))
	for _, id := range ids {
		r, err := st.Load(id)
		if err != nil {
			// One broken hand-edited file must not take the book offline.
			log.Printf("skipping %s: %v", id, err)
			continue
		}
		out = append(out, r)
	}
	return out
}

// warmPDFs compiles what is missing, one at a time, so the laptop stays
// responsive to API requests while it works through an imported book.
func warmPDFs(ctx context.Context, tc *typst.Compiler, rs []*recipe.Recipe) {
	n := 0
	for _, r := range rs {
		for _, lang := range r.Languages() {
			if ctx.Err() != nil {
				return
			}
			if !tc.Stale(r.ID, lang) {
				continue
			}
			if err := tc.Compile(ctx, r.ID, lang); err != nil {
				log.Printf("pdf %s/%s: %v", r.ID, lang, err)
				continue
			}
			n++
		}
	}
	if n > 0 {
		log.Printf("compiled %d PDFs", n)
	}
}

func countEnabled(ss []feeds.Source) int {
	n := 0
	for _, s := range ss {
		if s.Enabled {
			n++
		}
	}
	return n
}

// retagAll is the offline form of POST /api/admin/retag, for a stopped
// server: the index is rebuilt from the folders at the next start anyway.
func retagAll(st *store.Store) {
	units := recipe.Units()
	n := 0
	for _, r := range loadAll(st) {
		if !recipe.AddDetectedMethods(r) {
			continue
		}
		r.Normalize(units)
		if err := st.Update(r); err != nil {
			log.Printf("%s: %v", r.ID, err)
			continue
		}
		var methods []string
		for _, t := range r.Tags {
			if recipe.IsMethod(t) {
				methods = append(methods, t)
			}
		}
		log.Printf("%-45s %v", r.ID, methods)
		n++
	}
	log.Printf("tagged %d recipes", n)
}
