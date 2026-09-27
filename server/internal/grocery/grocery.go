// Package grocery aggregates ingredients from chosen recipes into one shopping
// list, and keeps the household's shared list state in <data>/grocery.json.
package grocery

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"cookbook/internal/recipe"
	"cookbook/internal/store"
)

type Selection struct {
	ID       string  `json:"id"`
	Servings float64 `json:"servings"`
}

type StaticItem struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	Category string `json:"category"`
	Checked  bool   `json:"checked"`
}

// State is what is persisted. Aggregated lines are always recomputed, so
// editing a recipe updates the list without anyone re-adding it.
type State struct {
	Rev     int          `json:"rev"`
	Recipes []Selection  `json:"recipes"`
	Static  []StaticItem `json:"static"`
	Checked []string     `json:"checked"` // line keys
}

type Line struct {
	Key      string   `json:"key"`
	IngID    string   `json:"ingredient_id"`
	Name     recipe.L `json:"name"`
	Category string   `json:"category"`
	Qty      *float64 `json:"qty"`
	UnitID   string   `json:"unit_id,omitempty"`
	Unit     recipe.L `json:"unit,omitempty"`
	Sources  []string `json:"sources"`
	Checked  bool     `json:"checked"`
}

type Section struct {
	Category string       `json:"category"`
	Label    recipe.L     `json:"label"`
	Lines    []Line       `json:"lines"`
	Static   []StaticItem `json:"static"`
}

type ChosenRecipe struct {
	ID       string   `json:"id"`
	Title    recipe.L `json:"title"`
	Servings float64  `json:"servings"`
	Missing  bool     `json:"missing,omitempty"`
}

type View struct {
	Rev      int            `json:"rev"`
	Recipes  []ChosenRecipe `json:"recipes"`
	Sections []Section      `json:"sections"`
	// State is the raw list, so the app can apply queued offline changes on
	// top of it and aggregate on the phone with no connection.
	State *State `json:"state"`
}

// --- aggregation -------------------------------------------------------------

type contribution struct {
	qty      *float64
	unitID   string
	unit     recipe.L
	name     recipe.L
	category string
	source   string
}

// Aggregate implements Pipeline 3: sum by ingredient id, converting between
// compatible units (dkg + g, dl + ml), keeping incompatible ones as separate
// lines, sorted by aisle.
func Aggregate(st *State, load func(id string) (*recipe.Recipe, error), units map[string]recipe.Unit) *View {
	view := &View{Rev: st.Rev, Recipes: []ChosenRecipe{}, State: st}
	byIng := map[string][]contribution{}
	var order []string

	for _, sel := range st.Recipes {
		r, err := load(sel.ID)
		if err != nil {
			view.Recipes = append(view.Recipes, ChosenRecipe{ID: sel.ID, Servings: sel.Servings, Missing: true})
			continue
		}
		view.Recipes = append(view.Recipes, ChosenRecipe{ID: r.ID, Title: r.Title, Servings: sel.Servings})
		factor := 1.0
		if sel.Servings > 0 && r.Servings > 0 {
			factor = sel.Servings / r.Servings
		}
		for _, ing := range r.Ingredients {
			if ing.Optional {
				continue
			}
			c := contribution{unitID: ing.UnitID, unit: ing.Unit, name: ing.Name, category: ing.Category, source: r.ID}
			if ing.Qty != nil {
				// A range buys the upper end: better one egg too many.
				q := *ing.Qty
				if ing.QtyMax != nil {
					q = *ing.QtyMax
				}
				q *= factor
				c.qty = &q
			}
			id := recipe.BaseID(ing.ID)
			if _, seen := byIng[id]; !seen {
				order = append(order, id)
			}
			byIng[id] = append(byIng[id], c)
		}
	}

	checked := map[string]bool{}
	for _, k := range st.Checked {
		checked[k] = true
	}
	sections := map[string]*Section{}
	section := func(cat string) *Section {
		if sections[cat] == nil {
			sections[cat] = &Section{Category: cat, Lines: []Line{}, Static: []StaticItem{}}
		}
		return sections[cat]
	}

	for _, id := range order {
		for _, line := range merge(id, byIng[id], units) {
			line.Checked = checked[line.Key]
			sec := section(line.Category)
			sec.Lines = append(sec.Lines, line)
		}
	}
	for _, it := range st.Static {
		cat := it.Category
		if cat == "" {
			cat = "other"
		}
		sec := section(cat)
		sec.Static = append(sec.Static, it)
	}

	for _, c := range recipe.AisleCategories {
		if s := sections[c.ID]; s != nil {
			s.Label = c.Label
			sort.SliceStable(s.Lines, func(i, j int) bool {
				return strings.ToLower(s.Lines[i].Name.Get("hu")) < strings.ToLower(s.Lines[j].Name.Get("hu"))
			})
			view.Sections = append(view.Sections, *s)
			delete(sections, c.ID)
		}
	}
	// Categories not in the aisle table (hand-edited meta.json) go last.
	var rest []string
	for k := range sections {
		rest = append(rest, k)
	}
	sort.Strings(rest)
	for _, k := range rest {
		s := sections[k]
		s.Label = recipe.L{"en": k, "hu": k}
		view.Sections = append(view.Sections, *s)
	}
	if view.Sections == nil {
		view.Sections = []Section{}
	}
	return view
}

// merge turns every contribution to one ingredient id into one or more lines.
func merge(id string, cs []contribution, units map[string]recipe.Unit) []Line {
	type bucket struct {
		key     string
		kind    string // mass/volume when convertible
		unitIDs map[string]bool
		unit    recipe.L
		unitID  string
		sum     float64 // in base units if kind != "", else in the unit itself
		raw     float64 // sum in the unit itself, valid when one unit id only
		sources []string
	}
	var buckets []*bucket
	find := map[string]*bucket{}
	name, category := recipe.L{}, ""
	var tasteSources []string

	for _, c := range cs {
		for _, lang := range recipe.Langs {
			if !name.Has(lang) && c.name.Has(lang) {
				name[lang] = c.name[lang]
			}
		}
		if category == "" {
			category = c.category
		}
		if c.qty == nil {
			tasteSources = appendUnique(tasteSources, c.source)
			continue
		}
		var key, kind string
		base, k, ok := recipe.ToBase(units, c.unitID, *c.qty)
		switch {
		case ok:
			key, kind = "kind:"+k, k
		case c.unitID != "":
			key = "unit:" + c.unitID
		default:
			key = "label:" + strings.ToLower(c.unit.Get("hu"))
		}
		b := find[key]
		if b == nil {
			b = &bucket{key: key, kind: kind, unitIDs: map[string]bool{}, unit: c.unit, unitID: c.unitID}
			find[key] = b
			buckets = append(buckets, b)
		}
		b.unitIDs[c.unitID] = true
		if ok {
			b.sum += base
		}
		b.raw += *c.qty
		b.sources = appendUnique(b.sources, c.source)
	}
	if category == "" {
		category = recipe.GuessAisle(name)
	}

	var lines []Line
	for _, b := range buckets {
		l := Line{Key: id + "|" + b.key, IngID: id, Name: name, Category: category, Sources: b.sources}
		var q float64
		if b.kind != "" && len(b.unitIDs) > 1 {
			// Mixed units of one kind: sum in grams/ml, show in a readable unit.
			var uid string
			q, uid = recipe.FromBase(b.sum, b.kind)
			l.UnitID, l.Unit = uid, units[uid].Label
		} else {
			q, l.UnitID, l.Unit = b.raw, b.unitID, b.unit
		}
		q = math.Round(q*100) / 100
		l.Qty = &q
		lines = append(lines, l)
	}
	// "Salt to taste" only earns its own line when no recipe gave an amount.
	if len(lines) == 0 && len(tasteSources) > 0 {
		lines = append(lines, Line{Key: id + "|taste", IngID: id, Name: name, Category: category, Sources: tasteSources})
	}
	return lines
}

func appendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

// --- persistence & operations ------------------------------------------------

type List struct {
	path string
	mu   sync.Mutex
}

func NewList(dataDir string) *List { return &List{path: filepath.Join(dataDir, "grocery.json")} }

func (l *List) Load() (*State, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.load()
}

func (l *List) load() (*State, error) {
	st := &State{Recipes: []Selection{}, Static: []StaticItem{}, Checked: []string{}}
	b, err := os.ReadFile(l.path)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, st); err != nil {
		return nil, fmt.Errorf("grocery.json: %w", err)
	}
	return st, nil
}

// Op is one change to the list. Operations rather than whole-state PUTs, so
// two people ticking items off in the shop at the same time do not undo each
// other.
type Op struct {
	Op       string  `json:"op"` // add_recipe remove_recipe check add_static remove_static clear_done clear_all
	ID       string  `json:"id,omitempty"`
	Servings float64 `json:"servings,omitempty"`
	Key      string  `json:"key,omitempty"`
	Checked  bool    `json:"checked,omitempty"`
	Text     string  `json:"text,omitempty"`
	Category string  `json:"category,omitempty"`
}

func (l *List) Apply(ops []Op) (*State, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	st, err := l.load()
	if err != nil {
		return nil, err
	}
	for _, op := range ops {
		if err := apply(st, op); err != nil {
			return nil, err
		}
	}
	st.Rev++
	b, _ := json.MarshalIndent(st, "", "  ")
	if err := store.WriteFileAtomic(l.path, b); err != nil {
		return nil, err
	}
	return st, nil
}

func apply(st *State, op Op) error {
	switch op.Op {
	case "add_recipe":
		if !recipe.ValidID(op.ID) {
			return fmt.Errorf("add_recipe: invalid id")
		}
		for i := range st.Recipes {
			if st.Recipes[i].ID == op.ID {
				st.Recipes[i].Servings = op.Servings
				return nil
			}
		}
		st.Recipes = append(st.Recipes, Selection{ID: op.ID, Servings: op.Servings})
	case "remove_recipe":
		st.Recipes = filter(st.Recipes, func(s Selection) bool { return s.ID != op.ID })
	case "check":
		if strings.HasPrefix(op.Key, "static:") {
			for i := range st.Static {
				if "static:"+st.Static[i].ID == op.Key {
					st.Static[i].Checked = op.Checked
				}
			}
			return nil
		}
		st.Checked = filter(st.Checked, func(k string) bool { return k != op.Key })
		if op.Checked {
			st.Checked = append(st.Checked, op.Key)
		}
	case "add_static":
		text := strings.TrimSpace(op.Text)
		if text == "" {
			return fmt.Errorf("add_static: text is required")
		}
		cat := op.Category
		if cat == "" {
			cat = recipe.GuessAisle(recipe.L{"hu": text, "en": text})
		}
		// The app generates the id when it adds an item offline, so later
		// queued ops (check, remove) can refer to it. Replaying the same add
		// twice must not duplicate the item.
		id := op.ID
		if !staticIDRe.MatchString(id) {
			id = newID()
		}
		for _, it := range st.Static {
			if it.ID == id {
				return nil
			}
		}
		st.Static = append(st.Static, StaticItem{ID: id, Text: text, Category: cat})
	case "remove_static":
		st.Static = filter(st.Static, func(s StaticItem) bool { return s.ID != op.ID })
	case "clear_done":
		st.Static = filter(st.Static, func(s StaticItem) bool { return !s.Checked })
		st.Checked = []string{}
	case "clear_all":
		st.Recipes, st.Static, st.Checked = []Selection{}, []StaticItem{}, []string{}
	default:
		return fmt.Errorf("unknown op %q", op.Op)
	}
	return nil
}

var staticIDRe = regexp.MustCompile(`^[a-z0-9-]{6,40}$`)

func filter[T any](s []T, keep func(T) bool) []T {
	out := s[:0]
	for _, v := range s {
		if keep(v) {
			out = append(out, v)
		}
	}
	return out
}

func newID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}
