// Package typst compiles recipe PDFs with the Typst CLI.
//
// Typst is optional at runtime: without it every other feature works and the
// PDF endpoints answer 503 with the reason, rather than the server refusing to
// start on a box where nobody installed it yet.
package typst

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cookbook/internal/recipe"
	"cookbook/internal/store"
)

//go:embed recipe.typ
var template []byte

// shippedTemplates are SHA-256 sums of earlier built-in templates. A data
// folder holding one of these unmodified gets the current template.
var shippedTemplates = map[string]bool{
	"580140a388463d4b724f52dc3536d71dbd4f92727329367635717a014f26a26e": true, // v1, 2026-09-25
}

var ErrUnavailable = errors.New("typst is not installed on the server (set typst path in config.json or put typst on PATH)")

type Compiler struct {
	bin   string
	store *store.Store
	mu    sync.Mutex // one compile at a time: an old laptop, many recipes
}

// New locates typst. bin may be a path or a bare name looked up on PATH
// (typst.exe on Windows is found by LookPath without the extension).
func New(bin string, st *store.Store) *Compiler {
	if bin == "" {
		bin = "typst"
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		path = ""
	}
	return &Compiler{bin: path, store: st}
}

func (c *Compiler) Available() bool { return c.bin != "" }

func (c *Compiler) Version() string {
	if c.bin == "" {
		return ""
	}
	out, err := exec.Command(c.bin, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (c *Compiler) templatePath() string {
	return filepath.Join(c.store.Root, "templates", "recipe.typ")
}

// Install writes the template (if the user has not got one) and the unit
// table (always: it is data, not style).
func (c *Compiler) Install() error {
	cur, err := os.ReadFile(c.templatePath())
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := store.WriteFileAtomic(c.templatePath(), template); err != nil {
			return err
		}
	case err == nil && !bytes.Equal(cur, template):
		// An unmodified copy of an older shipped template is upgraded; one the
		// user edited is left alone.
		sum := sha256.Sum256(cur)
		if shippedTemplates[hex.EncodeToString(sum[:])] {
			if err := store.WriteFileAtomic(c.templatePath(), template); err != nil {
				return err
			}
			log.Printf("typst: upgraded templates/recipe.typ to the new built-in version")
		} else {
			log.Printf("typst: templates/recipe.typ has local edits; keeping it (the built-in version may have new features)")
		}
	}
	units, _ := json.MarshalIndent(recipe.Units(), "", "  ")
	return store.WriteFileAtomic(filepath.Join(c.store.Root, "templates", "units.json"), units)
}

// Stale reports whether the PDF for lang is missing or older than its inputs.
func (c *Compiler) Stale(id, lang string) bool {
	pdf, err := os.Stat(c.store.PDFPath(id, lang))
	if err != nil {
		return true
	}
	for _, in := range []string{filepath.Join(c.store.Dir(id), "meta.json"), c.templatePath()} {
		if st, err := os.Stat(in); err == nil && st.ModTime().After(pdf.ModTime()) {
			return true
		}
	}
	return false
}

// Compile builds output-<lang>.pdf. Errors carry Typst's own message, which
// is what you need when editing the template.
func (c *Compiler) Compile(ctx context.Context, id, lang string) error {
	if c.bin == "" {
		return ErrUnavailable
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	dir := c.store.Dir(id)
	out := c.store.PDFPath(id, lang)
	// Typst picks the output format from the extension, so the temp name
	// must still end in .pdf.
	tmp := strings.TrimSuffix(out, ".pdf") + ".tmp.pdf"
	cmd := exec.CommandContext(ctx, c.bin, "compile",
		"--root", c.store.Root,
		"--input", "lang="+lang,
		// Only Typst's embedded fonts: the PDF looks the same whether the
		// laptop runs Windows or Linux, and startup skips a system font scan.
		"--ignore-system-fonts",
		filepath.Join(dir, "main.typ"), tmp)
	msg, err := cmd.CombinedOutput()
	if err != nil {
		os.Remove(tmp)
		return fmt.Errorf("typst: %s", strings.TrimSpace(string(msg)))
	}
	return os.Rename(tmp, out)
}

// CompileAll builds a PDF for every language the recipe has a title in, and
// removes PDFs for languages it no longer has.
func (c *Compiler) CompileAll(ctx context.Context, r *recipe.Recipe) map[string]string {
	result := map[string]string{}
	if c.bin == "" {
		result["error"] = ErrUnavailable.Error()
		return result
	}
	for _, lang := range recipe.Langs {
		if !r.Title.Has(lang) {
			os.Remove(c.store.PDFPath(r.ID, lang))
			continue
		}
		if err := c.Compile(ctx, r.ID, lang); err != nil {
			result[lang] = err.Error()
		} else {
			result[lang] = "ok"
		}
	}
	return result
}
