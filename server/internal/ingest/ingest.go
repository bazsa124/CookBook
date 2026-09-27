// Package ingest turns free text from recipe books and web pages into
// structured ingredients and marked-up steps. Shared by the Word importer
// and the web importer so both produce the same shapes.
package ingest

import (
	"fmt"
	"regexp"
	"strings"

	"cookbook/internal/recipe"
)

func LangOf(s string) string {
	if strings.ContainsAny(strings.ToLower(s), "áéíóöőúüű") {
		return "hu"
	}
	low := strings.ToLower(s)
	for _, w := range []string{"ingredient", "instruction", "direction", "method", "the ", " and ", "cup", "teaspoon"} {
		if strings.Contains(low, w) {
			return "en"
		}
	}
	return ""
}

func TitleCase(s string) string {
	if strings.ToUpper(s) != s {
		return s
	}
	r := []rune(strings.ToLower(s))
	if len(r) > 0 {
		r[0] = []rune(strings.ToUpper(string(r[0])))[0]
	}
	return string(r)
}

func LowerFirst(s string) string {
	r := []rune(s)
	if len(r) > 1 && strings.ToUpper(string(r[1])) != string(r[1]) {
		r[0] = []rune(strings.ToLower(string(r[0])))[0]
	}
	return string(r)
}

// --- ingredient lines -----------------------------------------------------------

var fractions = map[rune]float64{'½': .5, '⅓': 1. / 3, '⅔': 2. / 3, '¼': .25, '¾': .75, '⅛': .125, '⅜': .375, '⅝': .625, '⅞': .875, '⅕': .2}

// A quantity: "2", "2,5", "1.5", "1 ½", "½", "1/16", or a range "2-3".
var qtyRe = regexp.MustCompile(`^(\d+(?:[.,]\d+)?(?:\s*[½⅓⅔¼¾⅛⅜⅝⅞⅕])?|\d+/\d+|[½⅓⅔¼¾⅛⅜⅝⅞⅕])(?:\s*[-–]\s*(\d+(?:[.,]\d+)?|\d+/\d+))?\s+`)

var sizeWords = map[string]bool{
	"csapott": true, "púpozott": true, "nagy": true, "kis": true, "kisebb": true, "nagyobb": true,
	"közepes": true, "large": true, "small": true, "medium": true, "heaping": true, "level": true,
}

// Words that describe preparation rather than the item: left out of the id
// so "reszelt sajt" and "sajt" merge on the shopping list.
var idNoise = map[string]bool{
	"friss": true, "reszelt": true, "őrölt": true, "apróra": true, "vágott": true, "szabadtartásos": true,
	"kockázott": true, "fresh": true, "freshly": true, "grated": true, "ground": true, "chopped": true,
	"large": true, "small": true, "medium": true, "nagy": true, "kis": true, "közepes": true,
}

func ParseQty(s string) float64 {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", ".")
	if a, b, ok := strings.Cut(s, "/"); ok {
		var n, d float64
		fmt.Sscan(a, &n)
		fmt.Sscan(b, &d)
		if d == 0 {
			return 0
		}
		return n / d
	}
	var total float64
	var num strings.Builder
	for _, r := range s {
		if f, ok := fractions[r]; ok {
			total += f
		} else if r != ' ' {
			num.WriteRune(r)
		}
	}
	if num.Len() > 0 {
		var v float64
		fmt.Sscan(num.String(), &v)
		total += v
	}
	return total
}

var parenRe = regexp.MustCompile(`\s*\(([^)]*)\)`)

// ParseIngredient reads one ingredient line ("2 ek liszt", "1 (15 oz) can
// pumpkin puree", "só ízlés szerint"). Text fields are stored under lang, which
// may be the placeholder "_" when the language is decided later. ok is false
// when nothing usable is left of the line.
func ParseIngredient(line, group, lang string) (ing recipe.Ingredient, ok bool) {
	ing = recipe.Ingredient{Name: recipe.L{}}
	if group != "" {
		ing.Group = recipe.L{lang: group}
	}
	var notes []string
	// "10g yeast", "250ml milk": split an amount glued to its unit.
	rest := gluedUnit.ReplaceAllString(line, "$1 $2")

	if m := qtyRe.FindStringSubmatch(rest); m != nil {
		q := ParseQty(m[1])
		ing.Qty = &q
		if m[2] != "" {
			mx := ParseQty(m[2])
			ing.QtyMax = &mx
		}
		rest = rest[len(m[0]):]
	}
	// "(15 oz) can pumpkin puree": a leading parenthetical is a note.
	if strings.HasPrefix(rest, "(") {
		if i := strings.Index(rest, ")"); i > 0 {
			notes = append(notes, rest[1:i])
			rest = strings.TrimSpace(rest[i+1:])
		}
	}
	words := strings.Fields(rest)
	if ing.Qty != nil && len(words) > 1 {
		if sizeWords[strings.ToLower(words[0])] && len(words) > 2 {
			notes = append(notes, words[0])
			words = words[1:]
		}
		w := strings.ToLower(strings.TrimSuffix(words[0], "."))
		two := ""
		if len(words) > 2 {
			two = w + " " + strings.ToLower(words[1])
		}
		if id := recipe.MatchUnit(two); two != "" && id != "" {
			ing.UnitID, words = id, words[2:]
		} else if id := recipe.MatchUnit(w); id != "" {
			ing.UnitID, words = id, words[1:]
		}
	}
	name := strings.Join(words, " ")

	for _, m := range parenRe.FindAllStringSubmatch(name, -1) {
		notes = append(notes, m[1])
	}
	name = strings.TrimSpace(parenRe.ReplaceAllString(name, ""))
	low := strings.ToLower(name)
	for _, suf := range []string{" optional", " (optional)"} {
		if strings.HasSuffix(low, suf) {
			ing.Optional = true
			name = strings.TrimSpace(name[:len(name)-len(suf)])
			low = strings.ToLower(name)
		}
	}
	// "(optional)" becomes the flag, not note text.
	kept := notes[:0]
	for _, n := range notes {
		if w := strings.ToLower(strings.TrimSpace(n)); w == "optional" || w == "opcionális" {
			ing.Optional = true
			continue
		}
		kept = append(kept, n)
	}
	notes = kept
	// "ízlés szerint" (to taste) means no amount, whatever number preceded it.
	if strings.Contains(low, "ízlés szerint") || strings.Contains(low, "to taste") {
		ing.Qty, ing.QtyMax = nil, nil
		for _, p := range []string{"ízlés szerint", "to taste"} {
			if i := strings.Index(low, p); i >= 0 {
				name = strings.TrimSpace(name[:i] + name[i+len(p):])
				low = strings.ToLower(name)
			}
		}
		if lang == "en" {
			notes = append(notes, "to taste")
		} else {
			notes = append(notes, "ízlés szerint")
		}
	}
	if lang == "en" {
		if head, tail := splitPrepEN(name); tail != "" {
			name = head
			notes = append(notes, tail)
		}
	}
	if head, tail := SplitPrep(name); tail != "" {
		name = head
		notes = append(notes, tail)
	}
	if head, tail, ok := strings.Cut(name, ", "); ok && !strings.Contains(head, "-") {
		name = head
		notes = append(notes, tail)
	}
	if strings.ToUpper(name) == name {
		name = strings.ToLower(name)
	}
	name = strings.TrimSpace(strings.TrimSuffix(name, ","))
	if name == "" {
		return ing, false
	}
	ing.Name[lang] = name
	if len(notes) > 0 {
		ing.Note = recipe.L{lang: strings.Join(notes, ", ")}
	}
	ing.ID = IngredientID(name, lang)
	return ing, ing.ID != ""
}

// IngredientID derives a stable id from a name, dropping preparation words so
// "reszelt sajt" and "sajt" (or "large eggs" and "egg") merge on the grocery
// list.
func IngredientID(name, lang string) string {
	var idWords []string
	for _, w := range strings.Fields(strings.ToLower(name)) {
		if idNoise[w] {
			continue
		}
		if lang == "en" {
			w = singular(w)
		}
		idWords = append(idWords, w)
	}
	id := recipe.Slug(strings.Join(idWords, " "), "_")
	if id == "" {
		id = recipe.Slug(name, "_")
	}
	return id
}

// singular is a deliberately small English plural rule: enough for
// "eggs", "tomatoes", "berries" to meet their singular form.
func singular(w string) string {
	switch {
	case len(w) <= 3 || strings.HasSuffix(w, "ss") || strings.HasSuffix(w, "us"):
		return w
	case strings.HasSuffix(w, "ies"):
		return w[:len(w)-3] + "y"
	case strings.HasSuffix(w, "oes"):
		return w[:len(w)-2]
	case strings.HasSuffix(w, "s"):
		return w[:len(w)-1]
	}
	return w
}

var participles = map[string]bool{
	"vágva": true, "aprítva": true, "kockázva": true, "szeletelve": true, "reszelve": true,
	"olvasztva": true, "felverve": true, "előfőzve": true, "darálva": true, "zúzva": true,
	"hámozva": true, "megtisztítva": true, "felkockázva": true, "préselve": true, "főzve": true,
	"pirítva": true, "felszeletelve": true, "kimagozva": true, "lecsepegtetve": true, "felaprítva": true,
}

// splitPrep separates Hungarian preparation phrases from the item:
// "fokhagyma apróra vágva" -> "fokhagyma" + "apróra vágva",
// "liszt a habaráshoz" -> "liszt" + "a habaráshoz".
func SplitPrep(name string) (string, string) {
	words := strings.Fields(name)
	for i := 1; i < len(words); i++ {
		w := strings.ToLower(strings.Trim(words[i], ","))
		start := -1
		switch {
		case participles[w]:
			start = i
			// Include an adverb in front: "apróra", "finomra", "kockára".
			if prev := strings.ToLower(words[i-1]); i >= 2 && (strings.HasSuffix(prev, "ra") || strings.HasSuffix(prev, "re")) {
				start = i - 1
			}
		case w == "a" || w == "az":
			for _, rest := range words[i+1:] {
				r := strings.ToLower(rest)
				if strings.HasSuffix(r, "hoz") || strings.HasSuffix(r, "hez") || strings.HasSuffix(r, "höz") {
					start = i
					break
				}
			}
		}
		if start > 0 {
			return strings.Join(words[:start], " "), strings.Join(words[start:], " ")
		}
	}
	return name, ""
}

// --- step text ------------------------------------------------------------------

var (
	huMinRe  = regexp.MustCompile(`(\d+)(?:\s*[-–]\s*(\d+))?\s*(perc\p{L}*)`)
	huHourRe = regexp.MustCompile(`(\d+)(?:\s*[-–]\s*(\d+))?\s*(órá?\p{L}*)`)
	enMinRe  = regexp.MustCompile(`(\d+)(?:\s*(?:-|–|to)\s*(\d+))?\s*(minutes?|mins?)\b`)
	enHourRe = regexp.MustCompile(`(\d+)(?:\s*(?:-|–|to)\s*(\d+))?\s*(hours?)\b`)
	huTempRe = regexp.MustCompile(`(\d{2,3})\s*(?:°C|fok)(\p{L}*)`)
	enTempRe = regexp.MustCompile(`(\d{2,3})\s*°\s*F\b`)
	// "180C/160C fan", "200 degrees C": the British style without a degree sign.
	enCRe = regexp.MustCompile(`\b(\d{2,3})\s*(?:degrees\s*)?C\b`)
)

// timers marks durations as tap-to-start timers, spec style: "20 percig"
// becomes "[20:00] percig". For a range the lower bound becomes the timer
// ("check after 8 minutes") and the text keeps the range.
func Timers(s, lang string) string {
	type rule struct {
		re    *regexp.Regexp
		hours bool
	}
	rules := []rule{{huMinRe, false}, {huHourRe, true}}
	if lang == "en" {
		rules = []rule{{enMinRe, false}, {enHourRe, true}}
	}
	for _, ru := range rules {
		s = ru.re.ReplaceAllStringFunc(s, func(m string) string {
			sub := ru.re.FindStringSubmatch(m)
			var n int
			fmt.Sscan(sub[1], &n)
			if n == 0 || n > 999 {
				return m
			}
			tok := fmt.Sprintf("[%d:00]", n)
			if ru.hours {
				tok = fmt.Sprintf("[%d:00:00]", n)
			}
			if sub[2] != "" {
				return m + " " + tok
			}
			return tok + " " + sub[3]
		})
	}
	return s
}

// temps turns oven temperatures into {{temp:C}} tokens, which the app shows in
// °C or °F. "180 fokos sütőben" -> "{{temp:180}}-os sütőben": Hungarian reads
// "°C" as "fok", so the suffix carries over unchanged.
func Temps(s, lang string) string {
	s = huTempRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := huTempRe.FindStringSubmatch(m)
		suffix := sub[2]
		if suffix == "" {
			return "{{temp:" + sub[1] + "}}"
		}
		return "{{temp:" + sub[1] + "}}-" + suffix
	})
	if lang == "en" {
		s = enCRe.ReplaceAllString(s, "{{temp:$1}}")
	}
	return enTempRe.ReplaceAllStringFunc(s, func(m string) string {
		var f int
		fmt.Sscan(enTempRe.FindStringSubmatch(m)[1], &f)
		c := int(float64(f-32)*5/9/5+0.5) * 5
		return fmt.Sprintf("{{temp:%d}}", c)
	})
}

var gluedUnit = regexp.MustCompile(`^(\d+(?:[.,]\d+)?)(g|kg|dkg|ml|cl|dl|l|oz|lb|lbs)\b`)

var enPrep = map[string]bool{
	"beaten": true, "zested": true, "chopped": true, "diced": true, "minced": true, "sliced": true,
	"grated": true, "softened": true, "melted": true, "peeled": true, "crushed": true, "halved": true,
	"quartered": true, "drained": true, "rinsed": true, "toasted": true, "shredded": true, "cubed": true,
	"trimmed": true, "pitted": true, "deseeded": true, "juiced": true, "sifted": true, "divided": true,
	"finely": true, "roughly": true, "thinly": true, "freshly": true, "coarsely": true, "lightly": true,
	"plus": true,
}

// splitPrepEN moves English preparation words out of the name, leading
// ("finely chopped chives") or trailing ("eggs beaten", "flour plus extra").
func splitPrepEN(name string) (string, string) {
	words := strings.Fields(name)
	lead := 0
	for lead < len(words)-1 && enPrep[strings.ToLower(words[lead])] {
		lead++
	}
	for i := lead + 1; i < len(words); i++ {
		if enPrep[strings.ToLower(strings.Trim(words[i], ","))] {
			note := strings.Join(append(append([]string{}, words[:lead]...), words[i:]...), " ")
			return strings.Join(words[lead:i], " "), note
		}
	}
	if lead > 0 {
		return strings.Join(words[lead:], " "), strings.Join(words[:lead], " ")
	}
	return name, ""
}
