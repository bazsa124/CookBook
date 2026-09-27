package grocery

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"cookbook/internal/recipe"
	"cookbook/internal/store"
)

// The Android app aggregates the list itself when offline. This fixture pins
// the server's output for a scenario covering every merge rule; the app's unit
// test runs its own aggregation on the same input and must produce the same
// lines. Regenerate after an intended change with:
//
//	UPDATE_FIXTURE=1 go test ./internal/grocery/
var fixturePath = filepath.Join("..", "..", "..", "android", "app", "src", "test", "resources", "grocery_parity.json")

type parityFixture struct {
	Recipes  []*recipe.Recipe      `json:"recipes"`
	Units    []recipe.Unit         `json:"units"`
	Aisles   []recipe.Category     `json:"aisles"`
	Keywords []recipe.AisleKeyword `json:"aisle_keywords"`
	State    *State                `json:"state"`
	Expected *View                 `json:"expected"`
}

func parityInput() ([]*recipe.Recipe, *State) {
	rs := []*recipe.Recipe{
		{ID: "a", Servings: 4, Title: recipe.L{"hu": "A"}, Ingredients: []recipe.Ingredient{
			{ID: "tojas", Qty: f(3), UnitID: "pc", Name: recipe.L{"hu": "tojás"}, Category: "dairy"},
			{ID: "tojas-2", Qty: f(1), UnitID: "pc", Name: recipe.L{"hu": "tojás"}, Category: "dairy"},
			{ID: "liszt", Qty: f(20), UnitID: "dkg", Name: recipe.L{"hu": "liszt", "en": "flour"}, Category: "pantry"},
			{ID: "tej", Qty: f(2.5), UnitID: "dl", Name: recipe.L{"hu": "tej"}, Category: "dairy"},
			{ID: "so", Name: recipe.L{"hu": "só"}, Category: "spices"},
			{ID: "bors", Name: recipe.L{"hu": "bors"}, Category: "spices"},
			{ID: "sajt", Qty: f(10), QtyMax: f(15), UnitID: "dkg", Name: recipe.L{"hu": "sajt"}, Category: "dairy"},
			{ID: "petrezselyem", Qty: f(1), UnitID: "bunch", Name: recipe.L{"hu": "petrezselyem"}, Optional: true},
			{ID: "fura", Qty: f(2), Unit: recipe.L{"hu": "marék"}, Name: recipe.L{"hu": "fura dolog"}, Category: "weird"},
		}},
		{ID: "b", Servings: 2, Title: recipe.L{"hu": "B"}, Ingredients: []recipe.Ingredient{
			{ID: "tojas", Qty: f(1), UnitID: "pc", Name: recipe.L{"hu": "tojás"}},
			{ID: "liszt", Qty: f(300), UnitID: "g", Name: recipe.L{"hu": "liszt"}},
			{ID: "tej", Qty: f(1), UnitID: "tbsp", Name: recipe.L{"hu": "tej"}},
			{ID: "so", Qty: f(1), UnitID: "tsp", Name: recipe.L{"hu": "só"}},
			{ID: "sajt", Qty: f(1), UnitID: "pc", Name: recipe.L{"hu": "sajt"}},
			{ID: "hagyma", Qty: f(1.5), UnitID: "head", Name: recipe.L{"hu": "vöröshagyma"}},
		}},
	}
	st := &State{
		Rev:     7,
		Recipes: []Selection{{ID: "a", Servings: 6}, {ID: "b", Servings: 4}, {ID: "gone", Servings: 2}},
		Static: []StaticItem{
			{ID: "s1", Text: "Szemeteszsák", Category: "household"},
			{ID: "s2", Text: "Kenyér", Category: "bakery", Checked: true},
			{ID: "s3", Text: "Valami", Category: ""},
		},
		Checked: []string{"tojas|unit:pc", "stale|key"},
	}
	return rs, st
}

func TestParityFixture(t *testing.T) {
	rs, st := parityInput()
	byID := map[string]*recipe.Recipe{}
	for _, r := range rs {
		byID[r.ID] = r
	}
	load := func(id string) (*recipe.Recipe, error) {
		if r, ok := byID[id]; ok {
			return r, nil
		}
		return nil, store.ErrNotFound
	}
	view := Aggregate(st, load, recipe.Units())
	view.State = nil // the fixture carries the input state separately
	fx := parityFixture{
		Recipes: rs, Units: recipe.UnitList(), Aisles: recipe.AisleCategories,
		Keywords: recipe.AisleKeywords(), State: st, Expected: view,
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	enc.Encode(fx)

	if os.Getenv("UPDATE_FIXTURE") != "" {
		os.MkdirAll(filepath.Dir(fixturePath), 0o755)
		if err := os.WriteFile(fixturePath, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	old, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Skip("no fixture yet; run with UPDATE_FIXTURE=1")
	}
	if !bytes.Equal(bytes.ReplaceAll(old, []byte("\r\n"), []byte("\n")), buf.Bytes()) {
		t.Fatal("server aggregation changed: regenerate the fixture (UPDATE_FIXTURE=1) and make the app's Grocery.kt match")
	}
}
