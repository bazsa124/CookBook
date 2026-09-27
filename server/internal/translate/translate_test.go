package translate

import (
	"context"
	"encoding/json"
	"testing"

	"cookbook/internal/recipe"
)

type fake struct{ reply func(user string) string }

func (f fake) Name() string { return "fake" }
func (f fake) Complete(_ context.Context, _, user string) (string, error) {
	return f.reply(user), nil
}

func sample() *recipe.Recipe {
	q := 500.0
	return &recipe.Recipe{
		ID:    "gulyas",
		Title: recipe.L{"hu": "Gulyás", "en": "My own title"},
		Ingredients: []recipe.Ingredient{
			{ID: "beef", Qty: &q, UnitID: "g", Name: recipe.L{"hu": "marhahús"}},
		},
		Steps: []recipe.Step{
			{Text: recipe.L{"hu": "Főzd a {{beef.name}}t [120:00] percig."}},
			{Text: recipe.L{"hu": "Sózd meg."}},
		},
	}
}

func TestOnlyMissingFieldsAreSent(t *testing.T) {
	m := Missing(sample(), "hu", "en")
	if _, ok := m["title"]; ok {
		t.Error("title already has English and must not be sent (manual edit)")
	}
	for _, k := range []string{"ing.0.name", "step.0", "step.1"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing key %s", k)
		}
	}
	if _, ok := m["ing.0.unit"]; ok {
		t.Error("known unit ids must not be translated")
	}
}

func TestBrokenTokensAreRejected(t *testing.T) {
	r := sample()
	p := fake{reply: func(user string) string {
		out, _ := json.Marshal(map[string]string{
			"ing.0.name": "beef",
			"step.0":     "Cook the {{beef.nev}} for [120:00].", // token altered
			"step.1":     "Season with salt.",
		})
		return "```json\n" + string(out) + "\n```"
	}}
	res, err := Translate(context.Background(), p, r, "hu", "en")
	if err != nil {
		t.Fatal(err)
	}
	if res.Filled != 2 || len(res.Rejected) != 1 || res.Rejected[0] != "step.0" {
		t.Fatalf("filled=%d rejected=%v", res.Filled, res.Rejected)
	}
	if r.Steps[0].Text.Has("en") {
		t.Error("rejected step must stay empty")
	}
	if r.Title["en"] != "My own title" {
		t.Error("manual title overwritten")
	}
	if r.Ingredients[0].Name["en"] != "beef" {
		t.Error("ingredient name not merged")
	}
}
