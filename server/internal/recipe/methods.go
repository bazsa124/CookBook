package recipe

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Cooking methods are ordinary tags with fixed ids ("oven", "airfryer"), so
// filtering, the index and meta.json need nothing new; this table gives them
// localised labels and lets them be detected from the step text.
var Methods = []Category{
	{ID: "oven", Label: L{"en": "Oven", "hu": "Sütő"}},
	{ID: "pot", Label: L{"en": "Pot", "hu": "Lábas, fazék"}},
	{ID: "pan", Label: L{"en": "Pan", "hu": "Serpenyő"}},
	{ID: "airfryer", Label: L{"en": "Air fryer", "hu": "Airfryer"}},
	{ID: "grill", Label: L{"en": "Grill", "hu": "Grill"}},
	{ID: "deep-fry", Label: L{"en": "Deep-fried", "hu": "Bő olajban sült"}},
	{ID: "microwave", Label: L{"en": "Microwave", "hu": "Mikró"}},
	{ID: "slow-cooker", Label: L{"en": "Slow cooker", "hu": "Lassú főző"}},
	{ID: "pressure-cooker", Label: L{"en": "Pressure cooker", "hu": "Kukta"}},
	{ID: "fridge", Label: L{"en": "Fridge / chilled", "hu": "Hűtős"}},
	{ID: "no-bake", Label: L{"en": "No-bake", "hu": "Sütés nélkül"}},
}

// Patterns run over folded text (lower case, accents removed, see Slug) of
// the title and steps, Hungarian and English together.
var methodPatterns = []struct {
	id string
	re *regexp.Regexp
}{
	// "sütőpor" (baking powder) must not count, hence the explicit endings.
	{"oven", regexp.MustCompile(`\bsuto(ben|t|bol|be|re|n|nkben)?\b|sutopapir|tepsi|elomelegitett|\boven\b|preheat|baking (sheet|tray|dish|tin|pan)|\broast|\bbake[ds]?\b|tempmark\d`)},
	{"airfryer", regexp.MustCompile(`air ?fryer|airfryer|forro ?levegos`)},
	{"deep-fry", regexp.MustCompile(`\bbo (olaj|zsirad)|forro olajba|deep[ -]?fr(y|ied)|\bfryer\b`)},
	{"pan", regexp.MustCompile(`serpenyo|\bwok\b|frying pan|\bskillet|\bsaute|\bpan[- ]?fr(y|ied)`)},
	{"pot", regexp.MustCompile(`\blabas|fazek|\bbogracs|\bfoz(zuk|d|ni|zed)|forral|\bpot\b|saucepan|\bboil|simmer|stockpot|dutchoven`)},
	{"grill", regexp.MustCompile(`\bgrill|roston|barbecue|\bbbq\b`)},
	{"microwave", regexp.MustCompile(`mikro(hullamu)?\b|mikroba|microwave`)},
	{"slow-cooker", regexp.MustCompile(`slow ?cooker|crock ?pot|lassu ?fozo`)},
	{"pressure-cooker", regexp.MustCompile(`\bkukta|pressure cooker|instant pot`)},
	{"fridge", regexp.MustCompile(`\bhuto(be|ben|szekreny)|\bhutsuk|\bhutjuk|\bbehut|fridge|refrigerat|\bchill`)},
}

var heatMethods = map[string]bool{
	"oven": true, "airfryer": true, "deep-fry": true, "pan": true, "pot": true,
	"grill": true, "microwave": true, "slow-cooker": true, "pressure-cooker": true,
}

// FoldText lower-cases, strips accents and turns everything else into single
// spaces, padded so \b-style matching works at the ends. {{temp:180}} becomes
// "tempmark180", which the oven pattern treats as an oven temperature.
func FoldText(s string) string {
	s = strings.ReplaceAll(s, "{{temp:", " tempmark")
	s = strings.ToLower(norm.NFD.String(s))
	var b strings.Builder
	b.WriteByte(' ')
	space := true
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Mn, r):
		case r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			b.WriteRune(r)
			space = false
		default:
			if !space {
				b.WriteByte(' ')
				space = true
			}
		}
	}
	b.WriteByte(' ')
	return b.String()
}

// DetectMethods reads a recipe's title and steps and returns method ids.
// "no-bake" means the recipe uses no heat at all (drinks excepted: a cocktail
// being "no-bake" is true but useless as a filter).
func DetectMethods(r *Recipe) []string {
	var b strings.Builder
	for _, l := range r.Title {
		b.WriteString(l + " ")
	}
	for _, st := range r.Steps {
		for _, t := range st.Text {
			b.WriteString(t + " ")
		}
	}
	// A Dutch oven is a pot, not an oven.
	text := strings.ReplaceAll(FoldText(b.String()), " dutch oven", " dutchoven")
	var out []string
	heat := false
	for _, p := range methodPatterns {
		if p.re.MatchString(text) {
			out = append(out, p.id)
			heat = heat || heatMethods[p.id]
		}
	}
	if !heat && len(r.Steps) > 0 && r.Category != "drink" && r.Category != "cocktail" {
		out = append(out, "no-bake")
	}
	return out
}

// IsMethod reports whether a tag is one of the fixed method ids.
func IsMethod(tag string) bool {
	for _, m := range Methods {
		if m.ID == tag {
			return true
		}
	}
	return false
}

// AddDetectedMethods tags r with detected methods if it has no method tag
// yet. A method set by hand is never second-guessed.
func AddDetectedMethods(r *Recipe) bool {
	for _, t := range r.Tags {
		if IsMethod(t) {
			return false
		}
	}
	found := DetectMethods(r)
	if len(found) == 0 {
		return false
	}
	r.Tags = append(r.Tags, found...)
	return true
}
