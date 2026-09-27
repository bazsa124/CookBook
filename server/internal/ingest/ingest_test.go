package ingest

import "testing"

func TestParseIngredient(t *testing.T) {
	cases := []struct {
		line, lang     string
		id             string
		qty, max       float64 // -1 = none
		unit, name, nt string
	}{
		{"15 dkg reszelt sajt", "hu", "sajt", 15, -1, "dkg", "reszelt sajt", ""},
		{"3 gerezd fokhagyma apróra vágva", "hu", "fokhagyma", 3, -1, "clove", "fokhagyma", "apróra vágva"},
		{"2-3 ek olaj", "hu", "olaj", 2, 3, "tbsp", "olaj", ""},
		{"só ízlés szerint", "hu", "so", -1, -1, "", "só", "ízlés szerint"},
		{"10g fast-action dried yeast", "en", "fast_action_dried_yeast", 10, -1, "g", "fast-action dried yeast", ""},
		{"250ml milk", "en", "milk", 250, -1, "ml", "milk", ""},
		{"620g strong white bread flour, plus extra for dusting", "en", "strong_white_bread_flour", 620, -1, "g", "strong white bread flour", "plus extra for dusting"},
		{"5 medium eggs, beaten", "en", "egg", 5, -1, "", "eggs", "medium, beaten"},
		{"2 tbsp finely chopped chives", "en", "chive", 2, -1, "tbsp", "chives", "finely chopped"},
		{"1 ½ ounces vodka", "en", "vodka", 1.5, -1, "oz", "vodka", ""},
		{"1 (15 oz) can pumpkin puree", "en", "pumpkin_puree", 1, -1, "can", "pumpkin puree", "15 oz"},
		{"salt to taste", "en", "salt", -1, -1, "", "salt", "to taste"},
	}
	for _, c := range cases {
		ing, ok := ParseIngredient(c.line, "", c.lang)
		if !ok {
			t.Errorf("%q: not parsed", c.line)
			continue
		}
		q, mx := -1.0, -1.0
		if ing.Qty != nil {
			q = *ing.Qty
		}
		if ing.QtyMax != nil {
			mx = *ing.QtyMax
		}
		if ing.ID != c.id || q != c.qty || mx != c.max || ing.UnitID != c.unit ||
			ing.Name[c.lang] != c.name || ing.Note[c.lang] != c.nt {
			t.Errorf("%q\n got id=%s qty=%v max=%v unit=%s name=%q note=%q\nwant id=%s qty=%v max=%v unit=%s name=%q note=%q",
				c.line, ing.ID, q, mx, ing.UnitID, ing.Name[c.lang], ing.Note[c.lang],
				c.id, c.qty, c.max, c.unit, c.name, c.nt)
		}
	}
}

func TestTimersAndTemps(t *testing.T) {
	cases := []struct{ in, lang, want string }{
		{"Főzd 20 percig.", "hu", "Főzd [20:00] percig."},
		{"180 fokos sütőben 8-10 percig", "hu", "{{temp:180}}-os sütőben 8-10 percig [8:00]"},
		{"Heat the oven to 180C/160C fan. Bake for 25 minutes.", "en", "Heat the oven to {{temp:180}}/{{temp:160}} fan. Bake for [25:00] minutes."},
		{"Bake at 350°F for 1 hour.", "en", "Bake at {{temp:175}} for [1:00:00] hour."},
	}
	for _, c := range cases {
		if got := Timers(Temps(c.in, c.lang), c.lang); got != c.want {
			t.Errorf("%q:\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}
