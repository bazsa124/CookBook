// Command cookbook-import turns a Word recipe book into recipe folders.
//
//	cookbook-import -docx Receptkönyv.docx [-data DIR] [-dry-run]
//
// Expected shape (what the family book uses, and what most copy-pasted web
// recipes end up as): Heading 1 = category, Heading 2 = recipe title, then an
// "Ingredients"/"Hozzávalók" section of list items and a "Method"/"Elkészítés"
// section. Sub-headings inside a section become ingredient groups or step
// prefixes. The first image in a recipe becomes the hero photo.
//
// Run it while the server is stopped, or restart the server afterwards: the
// server re-indexes the folders on start.
package main

import (
	"archive/zip"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"io"
	"log"
	"os"
	"path"
	"regexp"
	"strings"

	"cookbook/internal/config"
	"cookbook/internal/ingest"
	"cookbook/internal/media"
	"cookbook/internal/recipe"
	"cookbook/internal/store"
)

func main() {
	docx := flag.String("docx", "", "path to the .docx recipe book")
	dataDir := flag.String("data", "", "data directory (default: same as the server)")
	dry := flag.Bool("dry-run", false, "print the parsed recipes as JSON instead of writing")
	flag.Parse()
	if *docx == "" {
		flag.Usage()
		os.Exit(2)
	}

	paras, files, err := readDocx(*docx)
	if err != nil {
		log.Fatal(err)
	}
	drafts := parse(paras)

	if *dry {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		for _, d := range drafts {
			enc.Encode(d.r)
		}
		log.Printf("%d recipes parsed", len(drafts))
		return
	}

	cfg, err := config.Load(*dataDir)
	if err != nil {
		log.Fatal(err)
	}
	st, err := store.New(cfg.DataDir)
	if err != nil {
		log.Fatal(err)
	}
	units := recipe.Units()
	created, skipped := 0, 0
	for _, d := range drafts {
		r := d.r
		if _, err := st.Load(r.ID); err == nil {
			skipped++ // re-running the import must not duplicate the book
			continue
		}
		r.Normalize(units)
		// Media are written after the folder exists; keep the references aside.
		images := d.images
		r.Hero = ""
		stepMedia := map[int]string{}
		if err := r.Validate(); err != nil {
			log.Printf("skip %q: %v", r.Title.Get("hu"), err)
			continue
		}
		if err := st.Create(r); err != nil {
			log.Printf("skip %q: %v", r.Title.Get("hu"), err)
			continue
		}
		for i, img := range images {
			data, ok := files[img.target]
			if !ok {
				continue
			}
			jpg, err := media.Compress(data)
			if err != nil {
				log.Printf("%s: image %s: %v", r.ID, img.target, err)
				continue
			}
			name := fmt.Sprintf("img%d.jpg", i+1)
			if i == 0 {
				name = "hero.jpg"
			}
			if err := st.SaveMedia(r.ID, name, jpg); err != nil {
				log.Printf("%s: %v", r.ID, err)
				continue
			}
			if i == 0 {
				r.Hero = name
			} else if img.step >= 0 && img.step < len(r.Steps) && stepMedia[img.step] == "" {
				stepMedia[img.step] = name
			}
		}
		for i, m := range stepMedia {
			r.Steps[i].Media = m
		}
		if r.Hero != "" || len(stepMedia) > 0 {
			if err := st.Update(r); err != nil {
				log.Printf("%s: %v", r.ID, err)
			}
		}
		created++
		log.Printf("imported %-45s %2d ingredients, %2d steps, %d images", r.ID, len(r.Ingredients), len(r.Steps), len(images))
	}
	log.Printf("done: %d imported, %d already present, into %s", created, skipped, cfg.DataDir)
}

// --- docx reading -------------------------------------------------------------

type para struct {
	style  string
	list   bool
	text   string
	images []string // relationship ids
}

var (
	paraRe  = regexp.MustCompile(`(?s)<w:p[ >].*?</w:p>`)
	styleRe = regexp.MustCompile(`<w:pStyle w:val="([^"]+)"`)
	runRe   = regexp.MustCompile(`(?s)<w:t(?: [^>]*)?>([^<]*)</w:t>|<w:tab/>|<w:br/>`)
	embedRe = regexp.MustCompile(`r:embed="([^"]+)"`)
	relRe   = regexp.MustCompile(`<Relationship [^>]*?Id="([^"]+)"[^>]*?Target="([^"]+)"`)
	relRe2  = regexp.MustCompile(`<Relationship [^>]*?Target="([^"]+)"[^>]*?Id="([^"]+)"`)
)

// readDocx returns the paragraphs and the media files keyed by relationship
// id. Regexes rather than a full OOXML model: only four things are needed.
func readDocx(p string) ([]para, map[string][]byte, error) {
	z, err := zip.OpenReader(p)
	if err != nil {
		return nil, nil, err
	}
	defer z.Close()
	read := func(name string) ([]byte, error) {
		f, err := z.Open(name)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return io.ReadAll(f)
	}
	doc, err := read("word/document.xml")
	if err != nil {
		return nil, nil, err
	}
	rels, _ := read("word/_rels/document.xml.rels")
	targets := map[string]string{}
	for _, m := range relRe.FindAllStringSubmatch(string(rels), -1) {
		targets[m[1]] = m[2]
	}
	for _, m := range relRe2.FindAllStringSubmatch(string(rels), -1) {
		targets[m[2]] = m[1]
	}
	files := map[string][]byte{}
	for id, t := range targets {
		if strings.HasPrefix(t, "media/") {
			if b, err := read(path.Join("word", t)); err == nil {
				files[id] = b
			}
		}
	}

	var out []para
	for _, px := range paraRe.FindAllString(string(doc), -1) {
		var p para
		if m := styleRe.FindStringSubmatch(px); m != nil {
			p.style = m[1]
		}
		p.list = strings.Contains(px, "<w:numPr>")
		var b strings.Builder
		for _, m := range runRe.FindAllStringSubmatch(px, -1) {
			if m[1] != "" {
				b.WriteString(html.UnescapeString(m[1]))
			} else if !strings.HasPrefix(m[0], "<w:t") {
				b.WriteByte(' ')
			}
		}
		p.text = clean(b.String())
		for _, m := range embedRe.FindAllStringSubmatch(px, -1) {
			p.images = append(p.images, m[1])
		}
		out = append(out, p)
	}
	return out, files, nil
}

func clean(s string) string {
	s = strings.NewReplacer("\u00a0", " ", "\ufeff", "", "\u25a2", "", "\u200b", "").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

// --- parsing ------------------------------------------------------------------

type image struct {
	target string // relationship id
	step   int    // index of the step it follows, -1 if before any
}

type draft struct {
	r      *recipe.Recipe
	images []image
}

var (
	headingRe     = regexp.MustCompile(`^(?:Heading|Cmsor)(\d)$`)
	ingredientsRe = regexp.MustCompile(`(?i)^(hozzáva|ingredient)`)
	methodRe      = regexp.MustCompile(`(?i)^(elkészít|elkészit|directions|instructions|method|how to make|step-by-step)`)
)

var categoryOf = map[string]string{
	"soups": "soup", "main dishes": "main", "salty desserts": "salty-bake", "sweets": "dessert",
	"drinks": "drink", "alcohols": "cocktail", "others": "other",
	"levesek": "soup", "főételek": "main", "sós sütemények": "salty-bake", "édességek": "dessert", "italok": "drink",
}

func headingLevel(style string) int {
	if m := headingRe.FindStringSubmatch(style); m != nil {
		return int(m[1][0] - '0')
	}
	return 0
}

// The parser is a small state machine per recipe.
const (
	modeIntro = iota
	modeIngredients
	modeMethod
)

func parse(paras []para) []draft {
	var out []draft
	var cur *draft
	var lang string
	mode := modeIntro
	category := "other"
	var group, stepPrefix string
	var intro []string

	finish := func() {
		if cur == nil {
			return
		}
		r := cur.r
		// Intro paragraphs with no method section were the method (a
		// cocktail described in prose); otherwise they are the description.
		if len(r.Steps) == 0 {
			for _, t := range intro {
				r.Steps = append(r.Steps, recipe.Step{Text: recipe.L{"_": t}})
			}
		} else if len(intro) > 0 {
			r.Description = recipe.L{"_": strings.Join(intro, " ")}
		}
		finalize(r, lang)
		if len(r.Ingredients) > 0 || len(r.Steps) > 0 {
			out = append(out, *cur)
		}
		cur = nil
	}

	for _, p := range paras {
		level := headingLevel(p.style)
		if strings.HasPrefix(p.style, "TOC") || p.style == "Title" {
			continue
		}
		if level == 1 && p.text == "" {
			continue
		}
		if level == 1 {
			finish()
			category = categoryOf[strings.ToLower(p.text)]
			if category == "" {
				category = "other"
			}
			continue
		}
		// An empty Heading 2 is a blank line that inherited the style, not a
		// new recipe (it would split the one above in two).
		if level == 2 && p.text == "" {
			level = 0
		}
		if level == 2 {
			finish()
			cur = &draft{r: &recipe.Recipe{Category: category, Tags: []string{}, Servings: 4, Source: "Receptkönyv.docx"}}
			cur.r.Title = recipe.L{"_": ingest.TitleCase(strings.TrimSuffix(strings.TrimSpace(p.text), " recept"))}
			lang, mode, group, stepPrefix, intro = "", modeIntro, "", "", nil
			for _, img := range p.images {
				cur.images = append(cur.images, image{img, -1})
			}
			continue
		}
		if cur == nil {
			continue
		}
		for _, img := range p.images {
			cur.images = append(cur.images, image{img, len(cur.r.Steps) - 1})
		}
		text := p.text
		if text == "" {
			continue
		}
		bare := strings.TrimSpace(strings.TrimSuffix(text, ":"))

		switch {
		case ingredientsRe.MatchString(bare) && !p.list:
			mode, group = modeIngredients, ""
			if lang == "" {
				lang = ingest.LangOf(bare)
			}
			continue
		case methodRe.MatchString(bare) && !p.list:
			mode, stepPrefix = modeMethod, ""
			if lang == "" {
				lang = ingest.LangOf(bare)
			}
			continue
		}
		if lang == "" {
			lang = ingest.LangOf(text)
		}

		// A sub-heading: a Heading 3/4, or a short non-list line ending in ':'.
		isSub := level >= 3 || (strings.HasSuffix(text, ":") && len([]rune(text)) < 45 && (!p.list || mode == modeMethod))
		if mode == modeIngredients && strings.EqualFold(bare, "garnish") {
			isSub = true
		}

		switch mode {
		case modeIntro:
			if p.list {
				// Ingredients without a heading (Real Polyjuice).
				mode = modeIngredients
				addIngredient(cur.r, text, group)
			} else if !isSub {
				intro = append(intro, text)
			}
		case modeIngredients:
			switch {
			case isSub:
				group = bare
			case strings.Contains(text, ": ") && len([]rune(text)) > 30:
				// "Mocktail Version: Use cola instead..." is a note, not an item.
				appendNote(cur.r, lang, text)
			default:
				addIngredient(cur.r, text, group)
			}
		case modeMethod:
			if isSub {
				stepPrefix = bare + ": "
				continue
			}
			text = strings.TrimPrefix(text, "Write instructions ")
			cur.r.Steps = append(cur.r.Steps, recipe.Step{Text: recipe.L{"_": stepPrefix + text}})
			stepPrefix = ""
		}
	}
	finish()
	return out
}

func appendNote(r *recipe.Recipe, lang, text string) {
	if r.Notes == nil {
		r.Notes = recipe.L{}
	}
	key := lang
	if key == "" {
		key = "_"
	}
	r.Notes[key] = strings.TrimSpace(r.Notes[key] + " " + text)
}

// finalize moves the placeholder "_" language to the detected one, and fixes
// what can only be decided once the whole recipe is read.
func finalize(r *recipe.Recipe, lang string) {
	if lang == "" {
		lang = "hu"
	}
	move := func(l recipe.L) recipe.L {
		if l == nil {
			return nil
		}
		if v, ok := l["_"]; ok {
			delete(l, "_")
			l[lang] = v
		}
		return l
	}
	r.Title = move(r.Title)
	r.Notes = move(r.Notes)
	r.Description = move(r.Description)
	r.ID = recipe.Slug(r.Title.Get(lang), "-")
	for i := range r.Steps {
		r.Steps[i].Text = move(r.Steps[i].Text)
		r.Steps[i].Text[lang] = ingest.Timers(ingest.Temps(r.Steps[i].Text[lang], lang), lang)
	}
	seen := map[string]int{}
	vegetarian := true
	for i := range r.Ingredients {
		ing := &r.Ingredients[i]
		ing.Name, ing.Note, ing.Group, ing.Unit = move(ing.Name), move(ing.Note), move(ing.Group), move(ing.Unit)
		ing.Name[lang] = ingest.LowerFirst(ing.Name[lang])
		ing.Category = recipe.GuessAisle(ing.Name)
		if ing.Category == "meat" {
			vegetarian = false
		}
		seen[ing.ID]++
		if n := seen[ing.ID]; n > 1 {
			ing.ID = fmt.Sprintf("%s-%d", ing.ID, n)
		}
	}
	if vegetarian && len(r.Ingredients) > 0 && r.Category != "drink" && r.Category != "cocktail" {
		r.Tags = append(r.Tags, "vegetarian")
	}
	t := strings.ToLower(r.Title.Get(lang))
	if strings.Contains(t, "gyors") || strings.Contains(t, "quick") || strings.Contains(t, "egyedényes") {
		r.Tags = append(r.Tags, "quick")
	}
	if r.Category == "drink" || r.Category == "cocktail" {
		if lang == "en" {
			r.Tags = append(r.Tags, "harry-potter")
		}
		r.Servings = 1
	}
}

func addIngredient(r *recipe.Recipe, line, group string) {
	if ing, ok := ingest.ParseIngredient(line, group, "_"); ok {
		r.Ingredients = append(r.Ingredients, ing)
	}
}
