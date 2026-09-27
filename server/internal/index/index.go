// Package index is the SQLite search index over the recipe folders.
//
// It is disposable by design: Rebuild drops everything and re-reads the
// folders, and the server does that on every start. Nothing lives only here.
package index

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"cookbook/internal/recipe"

	_ "modernc.org/sqlite" // pure Go: no cgo, cross-compiles anywhere
)

type Index struct {
	db *sql.DB
	mu sync.Mutex // one writer; SQLite would serialise anyway, this keeps errors out
}

const schema = `
DROP TABLE IF EXISTS recipes;
DROP TABLE IF EXISTS ingredients;
DROP TABLE IF EXISTS tags;
DROP TABLE IF EXISTS fts;
CREATE TABLE recipes (
	id TEXT PRIMARY KEY,
	title TEXT NOT NULL,        -- JSON L
	description TEXT NOT NULL,  -- JSON L
	category TEXT NOT NULL,
	tags TEXT NOT NULL,         -- JSON array
	servings REAL NOT NULL,
	prep INTEGER NOT NULL,
	cook INTEGER NOT NULL,
	hero TEXT NOT NULL,
	updated TEXT NOT NULL,
	langs TEXT NOT NULL,        -- JSON array
	sort_title TEXT NOT NULL
);
CREATE TABLE ingredients (
	recipe_id TEXT NOT NULL,
	ing_id TEXT NOT NULL,
	name_hu TEXT NOT NULL,
	name_en TEXT NOT NULL,
	category TEXT NOT NULL,
	required INTEGER NOT NULL
);
CREATE INDEX ingredients_ing ON ingredients(ing_id);
CREATE INDEX ingredients_recipe ON ingredients(recipe_id);
CREATE TABLE tags (recipe_id TEXT NOT NULL, tag TEXT NOT NULL);
CREATE INDEX tags_tag ON tags(tag);
-- remove_diacritics: "gulyas" finds "gulyás", which is how people type on a phone.
CREATE VIRTUAL TABLE fts USING fts5(id UNINDEXED, title, body, tokenize = 'unicode61 remove_diacritics 2');
`

func Open(path string) (*Index, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("index schema: %w", err)
	}
	return &Index{db: db}, nil
}

func (ix *Index) Close() error { return ix.db.Close() }

// Rebuild replaces the whole index with rs.
func (ix *Index) Rebuild(rs []*recipe.Recipe) error {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	tx, err := ix.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range []string{"recipes", "ingredients", "tags", "fts"} {
		if _, err := tx.Exec("DELETE FROM " + t); err != nil {
			return err
		}
	}
	for _, r := range rs {
		if err := insert(tx, r); err != nil {
			return fmt.Errorf("%s: %w", r.ID, err)
		}
	}
	return tx.Commit()
}

// Put upserts one recipe.
func (ix *Index) Put(r *recipe.Recipe) error {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	tx, err := ix.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := remove(tx, r.ID); err != nil {
		return err
	}
	if err := insert(tx, r); err != nil {
		return err
	}
	return tx.Commit()
}

func (ix *Index) Remove(id string) error {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	tx, err := ix.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := remove(tx, id); err != nil {
		return err
	}
	return tx.Commit()
}

func remove(tx *sql.Tx, id string) error {
	for _, t := range []string{"recipes WHERE id", "ingredients WHERE recipe_id", "tags WHERE recipe_id", "fts WHERE id"} {
		if _, err := tx.Exec("DELETE FROM "+t+" = ?", id); err != nil {
			return err
		}
	}
	return nil
}

func insert(tx *sql.Tx, r *recipe.Recipe) error {
	title, _ := json.Marshal(r.Title)
	desc, _ := json.Marshal(nonNil(r.Description))
	tags, _ := json.Marshal(r.Tags)
	langs, _ := json.Marshal(r.Languages())
	_, err := tx.Exec(`INSERT INTO recipes VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, string(title), string(desc), r.Category, string(tags), r.Servings,
		r.PrepMinutes, r.CookMinutes, r.Hero, r.Updated.UTC().Format("2006-01-02T15:04:05Z"),
		string(langs), recipe.Slug(r.Title.Get("hu"), " "))
	if err != nil {
		return err
	}
	var body []string
	for _, ing := range r.Ingredients {
		required := ing.Qty != nil && !ing.Optional && !recipe.Staples[ing.ID]
		if _, err := tx.Exec(`INSERT INTO ingredients VALUES (?,?,?,?,?,?)`,
			r.ID, ing.ID, ing.Name["hu"], ing.Name["en"], ing.Category, required); err != nil {
			return err
		}
		body = append(body, ing.Name["hu"], ing.Name["en"])
	}
	for _, t := range r.Tags {
		if _, err := tx.Exec(`INSERT INTO tags VALUES (?,?)`, r.ID, t); err != nil {
			return err
		}
		body = append(body, t)
	}
	body = append(body, r.Description["hu"], r.Description["en"], r.Category)
	for _, st := range r.Steps {
		for _, t := range st.Text {
			body = append(body, recipe.MustacheRe.ReplaceAllString(t, " "))
		}
	}
	_, err = tx.Exec(`INSERT INTO fts VALUES (?,?,?)`, r.ID,
		r.Title["hu"]+" "+r.Title["en"], strings.Join(body, " "))
	return err
}

func nonNil(l recipe.L) recipe.L {
	if l == nil {
		return recipe.L{}
	}
	return l
}

// --- queries -----------------------------------------------------------------

type Query struct {
	Text     string
	Tags     []string
	Category string
	Include  []string // every one must appear among the ingredients
	Exclude  []string // none may appear
	Pantry   []string // rank by how much of the recipe these cover
}

type Match struct {
	Have     int        `json:"have"`
	Need     int        `json:"need"`
	Coverage float64    `json:"coverage"`
	Missing  []recipe.L `json:"missing"`
}

type Summary struct {
	ID          string   `json:"id"`
	Title       recipe.L `json:"title"`
	Description recipe.L `json:"description"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
	Servings    float64  `json:"base_servings"`
	PrepMinutes int      `json:"prep_minutes"`
	CookMinutes int      `json:"cook_minutes"`
	Hero        string   `json:"hero"`
	Updated     string   `json:"updated"`
	Langs       []string `json:"langs"`
	Match       *Match   `json:"match,omitempty"`
}

// normTerm makes a user's ingredient word comparable to ingredient ids.
func normTerm(s string) string { return recipe.Slug(s, "_") }

func (ix *Index) Search(q Query) ([]Summary, error) {
	var where []string
	var args []any
	if text := ftsQuery(q.Text); text != "" {
		where = append(where, "id IN (SELECT id FROM fts WHERE fts MATCH ?)")
		args = append(args, text)
	}
	if q.Category != "" {
		where = append(where, "category = ?")
		args = append(args, q.Category)
	}
	for _, t := range q.Tags {
		where = append(where, "id IN (SELECT recipe_id FROM tags WHERE tag = ?)")
		args = append(args, recipe.Slug(t, "-"))
	}
	for _, t := range q.Include {
		if t = normTerm(t); t != "" {
			where = append(where, "id IN (SELECT recipe_id FROM ingredients WHERE ing_id LIKE ?)")
			args = append(args, "%"+t+"%")
		}
	}
	for _, t := range q.Exclude {
		if t = normTerm(t); t != "" {
			where = append(where, "id NOT IN (SELECT recipe_id FROM ingredients WHERE ing_id LIKE ?)")
			args = append(args, "%"+t+"%")
		}
	}
	sqlText := `SELECT id, title, description, category, tags, servings, prep, cook, hero, updated, langs FROM recipes`
	if len(where) > 0 {
		sqlText += " WHERE " + strings.Join(where, " AND ")
	}
	sqlText += " ORDER BY sort_title"

	rows, err := ix.db.Query(sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Summary{}
	for rows.Next() {
		var s Summary
		var title, desc, tags, langs string
		if err := rows.Scan(&s.ID, &title, &desc, &s.Category, &tags, &s.Servings,
			&s.PrepMinutes, &s.CookMinutes, &s.Hero, &s.Updated, &langs); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(title), &s.Title)
		json.Unmarshal([]byte(desc), &s.Description)
		json.Unmarshal([]byte(tags), &s.Tags)
		json.Unmarshal([]byte(langs), &s.Langs)
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(q.Pantry) > 0 {
		return ix.rankByPantry(out, q.Pantry)
	}
	return out, nil
}

// ftsQuery turns "gulyas marha" into `"gulyas"* "marha"*`: every word must
// match, each as a prefix, with FTS syntax characters neutralised.
func ftsQuery(text string) string {
	var parts []string
	for _, w := range strings.Fields(text) {
		w = strings.ReplaceAll(w, `"`, "")
		if w != "" {
			parts = append(parts, `"`+w+`"*`)
		}
	}
	return strings.Join(parts, " ")
}

// rankByPantry is the reverse search: which recipes can I cook from what I
// have? Coverage counts required ingredients only — anything with a quantity,
// not optional, not a staple like salt.
func (ix *Index) rankByPantry(in []Summary, pantry []string) ([]Summary, error) {
	var terms []string
	for _, p := range pantry {
		if t := normTerm(p); t != "" {
			terms = append(terms, t)
		}
	}
	has := func(ingID string) bool {
		for _, t := range terms {
			if strings.Contains(ingID, t) {
				return true
			}
		}
		return false
	}
	rows, err := ix.db.Query(`SELECT recipe_id, ing_id, name_hu, name_en FROM ingredients WHERE required = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	matches := map[string]*Match{}
	for rows.Next() {
		var rid, iid, hu, en string
		if err := rows.Scan(&rid, &iid, &hu, &en); err != nil {
			return nil, err
		}
		m := matches[rid]
		if m == nil {
			m = &Match{Missing: []recipe.L{}}
			matches[rid] = m
		}
		m.Need++
		if has(iid) {
			m.Have++
		} else {
			m.Missing = append(m.Missing, recipe.L{"hu": hu, "en": en})
		}
	}
	out := in[:0]
	for _, s := range in {
		m := matches[s.ID]
		if m == nil || m.Have == 0 {
			continue // nothing in common with the pantry: not a suggestion
		}
		m.Coverage = float64(m.Have) / float64(m.Need)
		s.Match = m
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Match, out[j].Match
		if len(a.Missing) != len(b.Missing) {
			return len(a.Missing) < len(b.Missing)
		}
		return a.Coverage > b.Coverage
	})
	return out, nil
}

// CatalogEntry is one known ingredient id, for editor autocomplete: reusing
// an existing id is what lets the grocery list merge across recipes.
type CatalogEntry struct {
	ID       string   `json:"id"`
	Name     recipe.L `json:"name"`
	Category string   `json:"category"`
	Uses     int      `json:"uses"`
}

func (ix *Index) Catalog() ([]CatalogEntry, error) {
	rows, err := ix.db.Query(`
		SELECT ing_id, MAX(name_hu), MAX(name_en), MAX(category), COUNT(*) AS uses
		FROM ingredients GROUP BY ing_id ORDER BY uses DESC, ing_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CatalogEntry{}
	for rows.Next() {
		var e CatalogEntry
		var hu, en string
		if err := rows.Scan(&e.ID, &hu, &en, &e.Category, &e.Uses); err != nil {
			return nil, err
		}
		e.Name = recipe.L{"hu": hu, "en": en}
		out = append(out, e)
	}
	return out, rows.Err()
}

type TagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

func (ix *Index) Tags() ([]TagCount, error) {
	rows, err := ix.db.Query(`SELECT tag, COUNT(*) FROM tags GROUP BY tag ORDER BY COUNT(*) DESC, tag`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TagCount{}
	for rows.Next() {
		var t TagCount
		if err := rows.Scan(&t.Tag, &t.Count); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (ix *Index) Count() int {
	var n int
	ix.db.QueryRow(`SELECT COUNT(*) FROM recipes`).Scan(&n)
	return n
}
