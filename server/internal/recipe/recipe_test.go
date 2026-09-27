package recipe

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Nagymama Gulyáslevese": "nagymama-gulyaslevese",
		"  Tojás  ":             "tojas",
		"Ő ű":                   "o-u",
	}
	for in, want := range cases {
		if got := Slug(in, "-"); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
	// Upper-case accented letters must fold like lower-case ones.
	if got := Slug("BACONÖS CSIRKÉS", "-"); got != "baconos-csirkes" {
		t.Errorf("upper-case accents: got %q", got)
	}
}

func TestSpecExampleLoads(t *testing.T) {
	spec := `{
	  "id": "nagymama-gulyas",
	  "title": { "en": "Grandma's Goulash", "hu": "Nagymama Gulyáslevese" },
	  "tags": ["soup", "traditional", "beef"],
	  "base_servings": 4,
	  "ingredients": [
	    { "id": "beef_shank", "qty": 500, "unit": { "en": "g", "hu": "g" },
	      "name": { "en": "beef shank", "hu": "marhalábszár" }, "category": "meat" }
	  ],
	  "steps": [
	    { "en": "Cut the {{beef_shank.qty}}{{beef_shank.unit}} of {{beef_shank.name}} into cubes. Boil for [120:00].",
	      "hu": "Vágd kockára a {{beef_shank.qty}}{{beef_shank.unit}} {{beef_shank.name}}t. Főzd [120:00] percig." }
	  ]
	}`
	var r Recipe
	if err := json.Unmarshal([]byte(spec), &r); err != nil {
		t.Fatal(err)
	}
	r.Normalize(Units())
	if err := r.Validate(); err != nil {
		t.Fatalf("spec example should validate: %v", err)
	}
	if r.Ingredients[0].UnitID != "g" {
		t.Errorf("unit id not derived from label: %q", r.Ingredients[0].UnitID)
	}
	// Step round-trips as the spec's flat object.
	b, _ := json.Marshal(r.Steps[0])
	if !strings.HasPrefix(string(b), `{"en":`) || strings.Contains(string(b), "media") {
		t.Errorf("step JSON shape changed: %s", b)
	}
}

func TestValidateCatchesDanglingMustache(t *testing.T) {
	r := Recipe{ID: "x", Title: L{"hu": "X"}, Steps: []Step{{Text: L{"hu": "Add {{nope.qty}}"}}}}
	if err := r.Validate(); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("want dangling reference error, got %v", err)
	}
}

func TestProtected(t *testing.T) {
	got := Protected("Boil {{a.qty}} for [10:00] at {{temp:180}}, then [1:30:00]")
	if len(got) != 4 {
		t.Fatalf("got %v", got)
	}
}

func TestGuessAisle(t *testing.T) {
	cases := map[string]string{
		"tojás": "dairy", "tejföl": "dairy", "kokusztej": "pantry", "csirkecomb": "meat",
		"vöröshagyma": "produce", "őrölt pirospaprika": "spices", "zöldpaprika": "produce",
		"liszt": "pantry", "só": "spices", "fehérbor": "drinks",
	}
	for name, want := range cases {
		if got := GuessAisle(L{"hu": name}); got != want {
			t.Errorf("GuessAisle(%q) = %q, want %q", name, got, want)
		}
	}
}
