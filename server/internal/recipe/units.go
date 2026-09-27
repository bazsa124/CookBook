package recipe

import (
	"math"
	"strings"
)

// Unit kinds. Only mass and volume convert; everything else is a count of
// something ("2 gerezd", "1 fej") and is summed only against itself.
const (
	KindMass   = "mass"
	KindVolume = "volume"
	KindCount  = "count"
)

// Unit is one entry of the shared unit table. The server serves this table at
// /api/units and the app caches it, so scaling in the app and summing on the
// server use the same numbers.
type Unit struct {
	ID     string  `json:"id"`
	Label  L       `json:"label"`
	Kind   string  `json:"kind"`
	Factor float64 `json:"factor,omitempty"` // grams or millilitres per 1 unit
	System string  `json:"system,omitempty"` // metric | imperial | both
	// Aliases are what people actually type ("evőkanál", "ek.", "tbsp").
	Aliases []string `json:"aliases,omitempty"`
}

var unitList = []Unit{
	{ID: "g", Label: L{"en": "g", "hu": "g"}, Kind: KindMass, Factor: 1, System: "metric", Aliases: []string{"gr", "gramm", "gram", "grams"}},
	{ID: "dkg", Label: L{"en": "dag", "hu": "dkg"}, Kind: KindMass, Factor: 10, System: "metric", Aliases: []string{"dag", "deka", "dekagramm"}},
	{ID: "kg", Label: L{"en": "kg", "hu": "kg"}, Kind: KindMass, Factor: 1000, System: "metric", Aliases: []string{"kilo", "kilogramm"}},
	{ID: "oz", Label: L{"en": "oz", "hu": "uncia"}, Kind: KindMass, Factor: 28.3495, System: "imperial", Aliases: []string{"ounce", "ounces"}},
	{ID: "lb", Label: L{"en": "lb", "hu": "font"}, Kind: KindMass, Factor: 453.592, System: "imperial", Aliases: []string{"lbs", "pound", "pounds"}},

	{ID: "ml", Label: L{"en": "ml", "hu": "ml"}, Kind: KindVolume, Factor: 1, System: "metric", Aliases: []string{"milliliter"}},
	{ID: "cl", Label: L{"en": "cl", "hu": "cl"}, Kind: KindVolume, Factor: 10, System: "metric", Aliases: []string{"centiliter"}},
	{ID: "dl", Label: L{"en": "dl", "hu": "dl"}, Kind: KindVolume, Factor: 100, System: "metric", Aliases: []string{"deciliter"}},
	{ID: "l", Label: L{"en": "l", "hu": "l"}, Kind: KindVolume, Factor: 1000, System: "metric", Aliases: []string{"liter", "litre"}},
	{ID: "tsp", Label: L{"en": "tsp", "hu": "tk"}, Kind: KindVolume, Factor: 5, System: "both", Aliases: []string{"teáskanál", "teaskanal", "teaspoon", "teaspoons", "tk."}},
	{ID: "tbsp", Label: L{"en": "tbsp", "hu": "ek"}, Kind: KindVolume, Factor: 15, System: "both", Aliases: []string{"evőkanál", "evokanal", "tablespoon", "tablespoons", "ek."}},
	{ID: "csp", Label: L{"en": "coffee spoon", "hu": "kk"}, Kind: KindVolume, Factor: 2.5, System: "both", Aliases: []string{"kávéskanál", "kaveskanal", "mokkáskanál", "kk."}},
	{ID: "cup", Label: L{"en": "cup", "hu": "csésze"}, Kind: KindVolume, Factor: 240, System: "imperial", Aliases: []string{"cups", "csesze"}},
	{ID: "mug", Label: L{"en": "mug", "hu": "bögre"}, Kind: KindVolume, Factor: 250, System: "both", Aliases: []string{"bogre", "mugs"}},
	{ID: "gal", Label: L{"en": "gal", "hu": "gallon"}, Kind: KindVolume, Factor: 3785.41, System: "imperial", Aliases: []string{"gallon", "gallons"}},
	{ID: "floz", Label: L{"en": "fl oz", "hu": "folyékony uncia"}, Kind: KindVolume, Factor: 29.5735, System: "imperial", Aliases: []string{"fl. oz", "fluid ounce"}},

	{ID: "pc", Label: L{"en": "pc", "hu": "db"}, Kind: KindCount, Aliases: []string{"darab", "pcs", "piece", "pieces", "db."}},
	{ID: "clove", Label: L{"en": "clove", "hu": "gerezd"}, Kind: KindCount, Aliases: []string{"cloves"}},
	{ID: "head", Label: L{"en": "head", "hu": "fej"}, Kind: KindCount, Aliases: []string{"heads"}},
	{ID: "slice", Label: L{"en": "slice", "hu": "szelet"}, Kind: KindCount, Aliases: []string{"slices"}},
	{ID: "pinch", Label: L{"en": "pinch", "hu": "csipet"}, Kind: KindCount, Aliases: []string{"pinches"}},
	{ID: "bunch", Label: L{"en": "bunch", "hu": "csokor"}, Kind: KindCount, Aliases: []string{"bunches"}},
	{ID: "can", Label: L{"en": "can", "hu": "doboz"}, Kind: KindCount, Aliases: []string{"cans", "konzerv"}},
	{ID: "pack", Label: L{"en": "pack", "hu": "csomag"}, Kind: KindCount, Aliases: []string{"packs", "packet", "cs.", "zacskó"}},
	{ID: "sheet", Label: L{"en": "sheet", "hu": "lap"}, Kind: KindCount, Aliases: []string{"sheets"}},
	{ID: "cube", Label: L{"en": "cube", "hu": "kocka"}, Kind: KindCount, Aliases: []string{"cubes"}},
	{ID: "handful", Label: L{"en": "handful", "hu": "marék"}, Kind: KindCount, Aliases: []string{"marek"}},
	{ID: "drop", Label: L{"en": "drop", "hu": "csepp"}, Kind: KindCount, Aliases: []string{"drops"}},
	{ID: "sprig", Label: L{"en": "sprig", "hu": "szál"}, Kind: KindCount, Aliases: []string{"sprigs", "szal"}},
	{ID: "leaf", Label: L{"en": "leaf", "hu": "levél"}, Kind: KindCount, Aliases: []string{"leaves"}},
	{ID: "shot", Label: L{"en": "shot", "hu": "feles"}, Kind: KindVolume, Factor: 40, System: "both", Aliases: []string{"shots"}},
}

// Units returns the table keyed by id.
func Units() map[string]Unit {
	m := make(map[string]Unit, len(unitList))
	for _, u := range unitList {
		m[u.ID] = u
	}
	return m
}

// UnitList returns the table in display order.
func UnitList() []Unit { return append([]Unit(nil), unitList...) }

// LookupUnit maps free text ("ek", "evőkanál", "tbsp") to a unit id, or "".
func LookupUnit(units map[string]Unit, label L) string {
	for _, lang := range Langs {
		if id := MatchUnit(label[lang]); id != "" {
			return id
		}
	}
	return ""
}

// MatchUnit maps one typed word to a unit id, or "".
func MatchUnit(word string) string {
	w := strings.ToLower(strings.TrimSpace(word))
	if w == "" {
		return ""
	}
	for _, u := range unitList {
		if w == u.ID || w == strings.ToLower(u.Label["hu"]) || w == strings.ToLower(u.Label["en"]) {
			return u.ID
		}
		for _, a := range u.Aliases {
			if w == a {
				return u.ID
			}
		}
	}
	return ""
}

// ToBase converts qty in unit to grams or millilitres. ok is false for counts
// and unknown units.
func ToBase(units map[string]Unit, unitID string, qty float64) (base float64, kind string, ok bool) {
	u, found := units[unitID]
	if !found || u.Factor == 0 {
		return 0, "", false
	}
	return qty * u.Factor, u.Kind, true
}

// FromBase picks a readable metric unit for a base quantity: 1500 g -> 1.5 kg,
// 250 ml -> 2.5 dl.
func FromBase(base float64, kind string) (float64, string) {
	switch kind {
	case KindMass:
		if base >= 1000 {
			return round(base / 1000), "kg"
		}
		return round(base), "g"
	case KindVolume:
		switch {
		case base >= 1000:
			return round(base / 1000), "l"
		case base >= 100:
			return round(base / 100), "dl"
		default:
			return round(base), "ml"
		}
	}
	return base, ""
}

func round(f float64) float64 { return math.Round(f*100) / 100 }

// Categories for recipes (the "Category" field of the editor).
var RecipeCategories = []Category{
	{ID: "breakfast", Label: L{"en": "Breakfast", "hu": "Reggeli"}},
	{ID: "appetizer", Label: L{"en": "Appetizers & snacks", "hu": "Előételek, falatkák"}},
	{ID: "soup", Label: L{"en": "Soups", "hu": "Levesek"}},
	{ID: "main", Label: L{"en": "Main dishes", "hu": "Főételek"}},
	{ID: "side", Label: L{"en": "Side dishes", "hu": "Köretek"}},
	{ID: "salad", Label: L{"en": "Salads", "hu": "Saláták"}},
	{ID: "sauce", Label: L{"en": "Sauces & dressings", "hu": "Szószok, mártások"}},
	{ID: "bread", Label: L{"en": "Breads", "hu": "Kenyerek, pékáruk"}},
	{ID: "salty-bake", Label: L{"en": "Salty bakes", "hu": "Sós sütemények"}},
	{ID: "dessert", Label: L{"en": "Sweets", "hu": "Édességek"}},
	{ID: "preserve", Label: L{"en": "Preserves & pickles", "hu": "Befőttek, savanyúságok"}},
	{ID: "drink", Label: L{"en": "Drinks", "hu": "Italok"}},
	{ID: "cocktail", Label: L{"en": "Cocktails", "hu": "Koktélok"}},
	{ID: "other", Label: L{"en": "Other", "hu": "Egyéb"}},
}

// AisleCategories order the grocery list the way a shop is walked.
var AisleCategories = []Category{
	{ID: "produce", Label: L{"en": "Fruit & vegetables", "hu": "Zöldség, gyümölcs"}},
	{ID: "bakery", Label: L{"en": "Bakery", "hu": "Pékáru"}},
	{ID: "meat", Label: L{"en": "Meat & fish", "hu": "Hús, hal"}},
	{ID: "dairy", Label: L{"en": "Dairy & eggs", "hu": "Tejtermék, tojás"}},
	{ID: "pantry", Label: L{"en": "Pantry", "hu": "Szárazáru"}},
	{ID: "canned", Label: L{"en": "Canned & jarred", "hu": "Konzerv"}},
	{ID: "spices", Label: L{"en": "Spices & condiments", "hu": "Fűszerek, szószok"}},
	{ID: "frozen", Label: L{"en": "Frozen", "hu": "Fagyasztott"}},
	{ID: "drinks", Label: L{"en": "Drinks", "hu": "Italok"}},
	{ID: "household", Label: L{"en": "Household", "hu": "Háztartás"}},
	{ID: "other", Label: L{"en": "Other", "hu": "Egyéb"}},
}

type Category struct {
	ID    string `json:"id"`
	Label L      `json:"label"`
}

// Staples are assumed to be in every kitchen: pantry matching does not count
// them as missing. Both the Hungarian and English slugs are listed because
// ingredient ids come from whichever language the recipe was written in.
var Staples = map[string]bool{
	"so": true, "salt": true, "bors": true, "pepper": true, "black_pepper": true,
	"viz": true, "water": true, "olaj": true, "oil": true, "cukor": true, "sugar": true,
}
