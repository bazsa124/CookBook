package grocery

import (
	"testing"

	"cookbook/internal/recipe"
	"cookbook/internal/store"
)

func f(v float64) *float64 { return &v }

func TestAggregateMergesAndConverts(t *testing.T) {
	units := recipe.Units()
	rs := map[string]*recipe.Recipe{
		"a": {ID: "a", Servings: 4, Title: recipe.L{"hu": "A"}, Ingredients: []recipe.Ingredient{
			{ID: "tojas", Qty: f(3), UnitID: "pc", Name: recipe.L{"hu": "tojás"}, Category: "dairy"},
			{ID: "liszt", Qty: f(20), UnitID: "dkg", Name: recipe.L{"hu": "liszt"}},
			{ID: "so", Name: recipe.L{"hu": "só"}},
		}},
		"b": {ID: "b", Servings: 2, Title: recipe.L{"hu": "B"}, Ingredients: []recipe.Ingredient{
			{ID: "tojas", Qty: f(1), UnitID: "pc", Name: recipe.L{"hu": "tojás"}},
			{ID: "liszt", Qty: f(300), UnitID: "g", Name: recipe.L{"hu": "liszt"}},
			{ID: "so", Qty: f(1), UnitID: "tsp", Name: recipe.L{"hu": "só"}},
		}},
	}
	load := func(id string) (*recipe.Recipe, error) {
		if r, ok := rs[id]; ok {
			return r, nil
		}
		return nil, store.ErrNotFound
	}
	st := &State{Recipes: []Selection{{ID: "a", Servings: 4}, {ID: "b", Servings: 4}, {ID: "gone", Servings: 2}}}
	v := Aggregate(st, load, units)

	lines := map[string]Line{}
	for _, s := range v.Sections {
		for _, l := range s.Lines {
			lines[l.Key] = l
		}
	}
	// 3 eggs + 1 egg doubled (b scaled 2 -> 4 servings) = 5 pc
	if l := lines["tojas|kind:"]; l.Qty != nil {
		t.Fatal("count units must not go through base conversion")
	}
	if l, ok := lines["tojas|unit:pc"]; !ok || *l.Qty != 5 {
		t.Errorf("eggs: %+v", lines["tojas|unit:pc"])
	}
	// 200 g + 600 g = 800 g
	if l := lines["liszt|kind:mass"]; l.Qty == nil || *l.Qty != 800 || l.UnitID != "g" {
		t.Errorf("flour: %+v qty=%v", l, deref(l.Qty))
	}
	// Salt had an amount in one recipe: no separate "to taste" line.
	if _, ok := lines["so|taste"]; ok {
		t.Error("to-taste line should be folded into the measured one")
	}
	if !v.Recipes[2].Missing {
		t.Error("deleted recipe should be flagged, not dropped silently")
	}
}

func deref(p *float64) float64 {
	if p == nil {
		return -1
	}
	return *p
}

func TestOps(t *testing.T) {
	l := NewList(t.TempDir())
	st, err := l.Apply([]Op{
		{Op: "add_recipe", ID: "a", Servings: 4},
		{Op: "add_static", Text: "Szemeteszsák", Category: "household"},
		{Op: "check", Key: "tojas|unit:pc", Checked: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Recipes) != 1 || len(st.Static) != 1 || len(st.Checked) != 1 || st.Rev != 1 {
		t.Fatalf("%+v", st)
	}
	st, _ = l.Apply([]Op{{Op: "check", Key: "static:" + st.Static[0].ID, Checked: true}, {Op: "clear_done"}})
	if len(st.Static) != 0 || len(st.Checked) != 0 {
		t.Fatalf("clear_done: %+v", st)
	}
}

// An item added offline carries the phone's id; replaying the queue after a
// lost response must not add it twice, and later ops must find it.
func TestOfflineStaticIDs(t *testing.T) {
	l := NewList(t.TempDir())
	add := Op{Op: "add_static", ID: "p-1a2b3c4d", Text: "Kenyér"}
	l.Apply([]Op{add})
	st, _ := l.Apply([]Op{add, {Op: "check", Key: "static:p-1a2b3c4d", Checked: true}})
	if len(st.Static) != 1 || st.Static[0].ID != "p-1a2b3c4d" || !st.Static[0].Checked {
		t.Fatalf("%+v", st.Static)
	}
	if st.Static[0].Category != "bakery" {
		t.Errorf("category %q", st.Static[0].Category)
	}
}
