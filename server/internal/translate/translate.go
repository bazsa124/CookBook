// Package translate fills missing language fields of a recipe with an LLM.
//
// The model never sees the recipe structure. It gets a flat {key: text} map of
// exactly the fields that are empty in the target language, so it cannot touch
// ids, quantities or anything the user already wrote. What comes back is
// verified token by token before a single field is merged.
package translate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"cookbook/internal/recipe"
)

type Provider interface {
	Name() string
	Complete(ctx context.Context, system, user string) (string, error)
}

var ErrUnavailable = errors.New("no translation provider configured (set GEMINI_API_KEY or GROQ_API_KEY)")

var langNames = map[string]string{"hu": "Hungarian", "en": "English"}

// Missing collects every field that has text in from but none in to.
func Missing(r *recipe.Recipe, from, to string) map[string]string {
	out := map[string]string{}
	add := func(key string, l recipe.L) {
		if l.Has(from) && !l.Has(to) {
			out[key] = l[from]
		}
	}
	add("title", r.Title)
	add("description", r.Description)
	add("notes", r.Notes)
	for i, ing := range r.Ingredients {
		p := "ing." + strconv.Itoa(i) + "."
		add(p+"name", ing.Name)
		add(p+"note", ing.Note)
		add(p+"group", ing.Group)
		if ing.UnitID == "" { // known units have fixed labels in both languages
			add(p+"unit", ing.Unit)
		}
	}
	for i, st := range r.Steps {
		add("step."+strconv.Itoa(i), st.Text)
	}
	return out
}

// Result reports what happened, so the editor can say "3 fields could not be
// translated safely" instead of silently leaving them blank.
type Result struct {
	Recipe   *recipe.Recipe `json:"recipe"`
	Filled   int            `json:"filled"`
	Rejected []string       `json:"rejected"`
	Provider string         `json:"provider"`
}

func Translate(ctx context.Context, p Provider, r *recipe.Recipe, from, to string) (*Result, error) {
	if p == nil {
		return nil, ErrUnavailable
	}
	if langNames[from] == "" || langNames[to] == "" || from == to {
		return nil, fmt.Errorf("unsupported language pair %s -> %s", from, to)
	}
	src := Missing(r, from, to)
	res := &Result{Recipe: r, Rejected: []string{}, Provider: p.Name()}
	if len(src) == 0 {
		return res, nil
	}
	payload, _ := json.Marshal(src)
	raw, err := p.Complete(ctx, systemPrompt(from, to), string(payload))
	if err != nil {
		return nil, err
	}
	got, err := parseObject(raw)
	if err != nil {
		return nil, fmt.Errorf("%s returned something that is not a JSON object: %w", p.Name(), err)
	}
	res.Filled, res.Rejected = Apply(r, to, src, got)
	return res, nil
}

func systemPrompt(from, to string) string {
	return fmt.Sprintf(`You translate recipe text from %[1]s to %[2]s.

Input: a JSON object mapping keys to %[1]s text.
Output: a JSON object with exactly the same keys, each value translated to %[2]s. Output JSON only.

Rules:
- Tokens in double curly braces, like {{beef_shank.qty}} or {{temp:180}}, are placeholders. Copy them exactly, character for character. Never translate, reorder inside, or remove them. Adjust the surrounding grammar instead.
- Timer tokens in square brackets, like [10:00] or [1:30:00], must also be copied exactly.
- Keys starting with "step." are cooking instructions: use natural cookbook style in the imperative.
- Keys ending in ".name" are ingredient names: translate to the common grocery name, lowercase, no quantity.
- Keys ending in ".unit" are measurement units: use the usual %[2]s abbreviation.
- Keep it concise. Do not add explanations.`, langNames[from], langNames[to])
}

// parseObject tolerates a model wrapping its JSON in a ``` fence.
func parseObject(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if i := strings.Index(raw, "{"); i > 0 {
		raw = raw[i:]
	}
	if i := strings.LastIndex(raw, "}"); i >= 0 && i < len(raw)-1 {
		raw = raw[:i+1]
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out, nil
}

// Apply merges translations into r. A value is accepted only when its key was
// asked for, it is non-empty, and it carries exactly the same protected tokens
// as the source. Everything else is reported as rejected.
func Apply(r *recipe.Recipe, to string, src, got map[string]string) (filled int, rejected []string) {
	rejected = []string{}
	keys := make([]string, 0, len(src))
	for k := range src {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, key := range keys {
		text := strings.TrimSpace(got[key])
		if text == "" || !slices.Equal(recipe.Protected(src[key]), recipe.Protected(text)) {
			rejected = append(rejected, key)
			continue
		}
		if target := field(r, key); target != nil {
			if *target == nil {
				*target = recipe.L{}
			}
			(*target)[to] = text
			filled++
		}
	}
	return filled, rejected
}

// field resolves a key produced by Missing back to the L it came from.
func field(r *recipe.Recipe, key string) *recipe.L {
	switch key {
	case "title":
		return &r.Title
	case "description":
		return &r.Description
	case "notes":
		return &r.Notes
	}
	parts := strings.Split(key, ".")
	if len(parts) < 2 {
		return nil
	}
	i, err := strconv.Atoi(parts[1])
	if err != nil || i < 0 {
		return nil
	}
	switch {
	case parts[0] == "step" && len(parts) == 2 && i < len(r.Steps):
		return &r.Steps[i].Text
	case parts[0] == "ing" && len(parts) == 3 && i < len(r.Ingredients):
		ing := &r.Ingredients[i]
		switch parts[2] {
		case "name":
			return &ing.Name
		case "note":
			return &ing.Note
		case "group":
			return &ing.Group
		case "unit":
			return &ing.Unit
		}
	}
	return nil
}
