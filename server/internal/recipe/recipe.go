// Package recipe is the meta.json schema and the rules that keep it valid.
//
// The schema is the one in the architecture document, extended with optional
// fields only: a meta.json written by hand against the original spec still
// loads unchanged.
package recipe

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Langs are the languages the app is written for, in fallback order.
var Langs = []string{"hu", "en"}

// L is a localised string: {"en": "...", "hu": "..."}. A missing or empty key
// means "not written yet", which is exactly what auto-translation fills in.
type L map[string]string

// Get returns the text in lang, falling back to any other language.
func (l L) Get(lang string) string {
	if s := strings.TrimSpace(l[lang]); s != "" {
		return s
	}
	for _, other := range Langs {
		if s := strings.TrimSpace(l[other]); s != "" {
			return s
		}
	}
	for _, s := range l {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// Has reports whether lang has non-blank text.
func (l L) Has(lang string) bool { return strings.TrimSpace(l[lang]) != "" }

// Empty reports whether no language has text.
func (l L) Empty() bool {
	for _, s := range l {
		if strings.TrimSpace(s) != "" {
			return false
		}
	}
	return true
}

type Recipe struct {
	ID          string       `json:"id"`
	Title       L            `json:"title"`
	Description L            `json:"description,omitempty"`
	Category    string       `json:"category,omitempty"`
	Tags        []string     `json:"tags"`
	Servings    float64      `json:"base_servings"`
	PrepMinutes int          `json:"prep_minutes,omitempty"`
	CookMinutes int          `json:"cook_minutes,omitempty"`
	Hero        string       `json:"hero,omitempty"`
	Ingredients []Ingredient `json:"ingredients"`
	Steps       []Step       `json:"steps"`
	Notes       L            `json:"notes,omitempty"`
	Source      string       `json:"source,omitempty"`
	// VideoURL links a video hosted elsewhere (often YouTube), e.g. from a
	// web import. Uploaded videos are step media instead.
	VideoURL string `json:"video_url,omitempty"`

	// Rev increments on every save; a PUT carrying a stale rev is refused, so
	// two phones editing the same recipe produce a visible conflict instead of
	// a silent overwrite.
	Rev     int       `json:"rev"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
}

type Ingredient struct {
	ID string `json:"id"`
	// Qty nil means "to taste" (só, bors): shown without an amount, never
	// scaled, never summed.
	Qty      *float64 `json:"qty"`
	QtyMax   *float64 `json:"qty_max,omitempty"`
	Unit     L        `json:"unit,omitempty"`
	UnitID   string   `json:"unit_id,omitempty"`
	Name     L        `json:"name"`
	Note     L        `json:"note,omitempty"`
	Category string   `json:"category,omitempty"`
	Group    L        `json:"group,omitempty"`
	Optional bool     `json:"optional,omitempty"`
}

// Step is {"en": "...", "hu": "...", "media": "file.jpg"}: the spec's flat
// localised object, with "media" as the one reserved key.
type Step struct {
	Text  L
	Media string
}

func (s Step) MarshalJSON() ([]byte, error) {
	m := make(map[string]string, len(s.Text)+1)
	for k, v := range s.Text {
		m[k] = v
	}
	if s.Media != "" {
		m["media"] = s.Media
	}
	return json.Marshal(m)
}

func (s *Step) UnmarshalJSON(b []byte) error {
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	s.Media = m["media"]
	delete(m, "media")
	s.Text = L(m)
	return nil
}

var (
	idRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,79}$`)
	mediaRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}\.(jpg|png|mp4|webm)$`)
	// {{beef_shank.qty}}, {{beef_shank}}, {{temp:180}}
	MustacheRe = regexp.MustCompile(`\{\{\s*([a-z0-9_:-]+)(?:\.(qty|unit|name))?\s*\}\}`)
	// [10:00], [120:00], [1:30:00]
	TimerRe = regexp.MustCompile(`\[(\d{1,3}):([0-5]\d)(?::([0-5]\d))?\]`)
)

func ValidID(id string) bool { return idRe.MatchString(id) }

var dupSuffixRe = regexp.MustCompile(`-\d+$`)

// BaseID strips the "-2" suffix that disambiguates an ingredient used twice
// in one recipe (egg in the dough, egg for glazing). Ids are unique within a
// recipe so {{mustache}} references work; the grocery list sums by base id.
func BaseID(id string) string      { return dupSuffixRe.ReplaceAllString(id, "") }
func ValidMediaName(n string) bool { return mediaRe.MatchString(n) }

// IsVideo reports whether a media file name is a video.
func IsVideo(n string) bool { return strings.HasSuffix(n, ".mp4") || strings.HasSuffix(n, ".webm") }

// PosterName is the still frame shown before a video plays:
// "a1b2.mp4" -> "a1b2-poster.jpg". It exists only if ffmpeg made one.
func PosterName(video string) string {
	return strings.TrimSuffix(strings.TrimSuffix(video, ".mp4"), ".webm") + "-poster.jpg"
}

// Slug turns "Nagymama Gulyáslevese" into "nagymama-gulyaslevese". sep is the
// word separator: "-" for recipe ids, "_" for ingredient ids (as in the spec).
func Slug(s, sep string) string {
	s = strings.ToLower(norm.NFD.String(s))
	var b strings.Builder
	pendingSep := false
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Mn, r): // combining accent left over by NFD
			continue
		case r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			if pendingSep && b.Len() > 0 {
				b.WriteString(sep)
			}
			pendingSep = false
			b.WriteRune(r)
		default:
			pendingSep = true
		}
	}
	out := b.String()
	if len(out) > 60 {
		out = strings.TrimRight(out[:60], sep)
	}
	return out
}

// Normalize fills derived fields and tidies input from the editor. It never
// invents content in a language the user did not write.
func (r *Recipe) Normalize(units map[string]Unit) {
	r.Category = strings.TrimSpace(r.Category)
	// Empty lists serialise as [] rather than null: the app and the Typst
	// template both iterate them without a null check.
	if r.Ingredients == nil {
		r.Ingredients = []Ingredient{}
	}
	if r.Steps == nil {
		r.Steps = []Step{}
	}
	if r.Tags == nil {
		r.Tags = []string{}
	}
	tags := r.Tags[:0]
	seen := map[string]bool{}
	for _, t := range r.Tags {
		t = Slug(t, "-")
		if t != "" && !seen[t] {
			seen[t] = true
			tags = append(tags, t)
		}
	}
	sort.Strings(tags)
	r.Tags = tags
	if r.Servings <= 0 {
		r.Servings = 4
	}
	for i := range r.Ingredients {
		ing := &r.Ingredients[i]
		if ing.ID == "" {
			ing.ID = Slug(ing.Name.Get("en"), "_") // Get falls back to hu
		}
		ing.ID = Slug(ing.ID, "_")
		// A known unit code supplies its labels; the free-text labels stay
		// authoritative when the user typed them.
		if u, ok := units[ing.UnitID]; ok {
			if ing.Unit == nil {
				ing.Unit = L{}
			}
			for _, lang := range Langs {
				if !ing.Unit.Has(lang) {
					ing.Unit[lang] = u.Label[lang]
				}
			}
		} else if ing.UnitID != "" {
			ing.UnitID = ""
		}
		if ing.UnitID == "" && ing.Unit != nil {
			ing.UnitID = LookupUnit(units, ing.Unit)
		}
		if ing.Qty != nil && ing.QtyMax != nil && *ing.QtyMax <= *ing.Qty {
			ing.QtyMax = nil
		}
		if ing.Qty == nil {
			ing.QtyMax = nil
		}
	}
}

// Validate returns every problem at once, so the editor can show them together.
func (r *Recipe) Validate() error {
	var errs []error
	if !ValidID(r.ID) {
		errs = append(errs, fmt.Errorf("id %q: lowercase letters, digits, - and _ only", r.ID))
	}
	if r.Title.Empty() {
		errs = append(errs, errors.New("title is required in at least one language"))
	}
	if r.Hero != "" && IsVideo(r.Hero) {
		errs = append(errs, fmt.Errorf("hero %q: the cover must be a photo, videos go on steps", r.Hero))
	}
	if r.VideoURL != "" && !strings.HasPrefix(r.VideoURL, "https://") && !strings.HasPrefix(r.VideoURL, "http://") {
		errs = append(errs, fmt.Errorf("video link %q is not a web address", r.VideoURL))
	}
	if r.Hero != "" && !ValidMediaName(r.Hero) {
		errs = append(errs, fmt.Errorf("hero %q is not a media file name", r.Hero))
	}
	ids := map[string]bool{}
	for i, ing := range r.Ingredients {
		if !idRe.MatchString(ing.ID) {
			errs = append(errs, fmt.Errorf("ingredient %d: invalid id %q", i+1, ing.ID))
		}
		if ids[ing.ID] {
			errs = append(errs, fmt.Errorf("ingredient %d: duplicate id %q", i+1, ing.ID))
		}
		ids[ing.ID] = true
		if ing.Name.Empty() {
			errs = append(errs, fmt.Errorf("ingredient %d: name is required", i+1))
		}
		if ing.Qty != nil && *ing.Qty < 0 {
			errs = append(errs, fmt.Errorf("ingredient %d: negative quantity", i+1))
		}
	}
	for i, st := range r.Steps {
		if st.Media != "" && !ValidMediaName(st.Media) {
			errs = append(errs, fmt.Errorf("step %d: invalid media %q", i+1, st.Media))
		}
		for lang, text := range st.Text {
			for _, m := range MustacheRe.FindAllStringSubmatch(text, -1) {
				ref := m[1]
				if strings.HasPrefix(ref, "temp:") {
					continue
				}
				if !ids[ref] {
					errs = append(errs, fmt.Errorf("step %d (%s): {{%s}} refers to no ingredient", i+1, lang, ref))
				}
			}
		}
	}
	return errors.Join(errs...)
}

// Languages reports which languages the recipe has a title in.
func (r *Recipe) Languages() []string {
	var out []string
	for _, lang := range Langs {
		if r.Title.Has(lang) {
			out = append(out, lang)
		}
	}
	return out
}

// Protected returns the tokens that translation must carry over verbatim, as
// a sorted multiset.
func Protected(s string) []string {
	var out []string
	out = append(out, MustacheRe.FindAllString(s, -1)...)
	out = append(out, TimerRe.FindAllString(s, -1)...)
	sort.Strings(out)
	return out
}
