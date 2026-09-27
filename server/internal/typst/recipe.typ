// CookBook recipe template.
//
// Every recipe's main.typ is a stub that calls `recipe(json("meta.json"))`, so
// this one file styles every PDF. Edit freely: the server writes it only when
// it is missing, and recompiles PDFs whenever this file is newer than them.

#let units = json("/templates/units.json")
#let accent = rgb("#a4462a")
#let muted = rgb("#6b6158")

#let labels = (
  hu: (ingredients: "Hozzávalók", steps: "Elkészítés", servings: "adag", prep: "előkészítés", cook: "főzés", min: "perc", notes: "Megjegyzés", optional: "opcionális", video: "videó az alkalmazásban"),
  en: (ingredients: "Ingredients", steps: "Method", servings: "servings", prep: "prep", cook: "cook", min: "min", notes: "Notes", optional: "optional", video: "video in the app"),
)

// Localised field with fallback to the other language.
#let tr(obj, lang) = {
  if obj == none { return "" }
  if type(obj) == str { return obj }
  let v = obj.at(lang, default: "")
  if v == "" { v = obj.at(if lang == "hu" { "en" } else { "hu" }, default: "") }
  v
}

#let fmt-num(q, lang) = {
  let r = calc.round(float(q), digits: 2)
  let s = if calc.fract(r) == 0 { str(int(r)) } else { str(r) }
  if lang == "hu" { s.replace(".", ",") } else { s }
}

#let fmt-qty(ing, lang) = {
  let q = ing.at("qty", default: none)
  if q == none { return "" }
  let s = fmt-num(q, lang)
  let mx = ing.at("qty_max", default: none)
  if mx != none { s += "–" + fmt-num(mx, lang) }
  s
}

#let unit-label(ing, lang) = {
  let u = tr(ing.at("unit", default: none), lang)
  let id = ing.at("unit_id", default: "")
  if u == "" and id in units { u = tr(units.at(id).label, lang) }
  u
}

// {{id.qty}} {{id.unit}} {{id.name}} {{id}} {{temp:180}}
#let resolve(s, by-id, lang) = {
  let t = s.replace(regex("\{\{\s*temp:(\d+)\s*\}\}"), m => m.captures.at(0) + " °C")
  t.replace(regex("\{\{\s*([a-z0-9_-]+)(?:\.(qty|unit|name))?\s*\}\}"), m => {
    let id = m.captures.at(0)
    let field = m.captures.at(1)
    if id not in by-id { return m.text }
    let i = by-id.at(id)
    if field == "qty" { fmt-qty(i, lang) }
    else if field == "unit" { unit-label(i, lang) }
    else if field == "name" { tr(i.name, lang) }
    else { (fmt-qty(i, lang), unit-label(i, lang), tr(i.name, lang)).filter(x => x != "").join(" ") }
  })
}

// [MM:SS] / [H:MM:SS] become highlighted chips.
#let with-timers(s) = {
  let out = ()
  let pos = 0
  for m in s.matches(regex("\[(\d{1,3}):([0-5]\d)(?::([0-5]\d))?\]")) {
    out.push(s.slice(pos, m.start))
    out.push(box(
      inset: (x: 3pt), outset: (y: 2pt), radius: 3pt, fill: accent.lighten(85%),
      text(weight: "bold", fill: accent, m.text.slice(1, -1)),
    ))
    pos = m.end
  }
  out.push(s.slice(pos))
  out.join()
}

#let recipe(meta, lang: "hu") = {
  let L = labels.at(lang, default: labels.en)
  let ings = meta.at("ingredients", default: none)
  if ings == none { ings = () }
  let by-id = (:)
  for i in ings { by-id.insert(i.id, i) }
  let media(name) = "/recipes/" + meta.id + "/media/" + name

  set document(title: tr(meta.title, lang))
  set page(
    paper: "a4",
    margin: (x: 18mm, top: 16mm, bottom: 18mm),
    footer: context align(center, text(8pt, fill: muted)[#tr(meta.title, lang) · #counter(page).display()]),
  )
  set text(font: ("Libertinus Serif", "New Computer Modern"), size: 10.5pt, lang: lang)
  set par(justify: true, leading: 0.6em)

  // --- header ---
  text(24pt, weight: "bold", fill: accent, tr(meta.title, lang))
  let desc = tr(meta.at("description", default: none), lang)
  if desc != "" { v(-2pt); text(11pt, style: "italic", fill: muted, desc) }

  let facts = ()
  facts.push(fmt-num(meta.at("base_servings", default: 4), lang) + " " + L.servings)
  let prep = meta.at("prep_minutes", default: 0)
  if prep > 0 { facts.push(L.prep + ": " + str(prep) + " " + L.min) }
  let cook = meta.at("cook_minutes", default: 0)
  if cook > 0 { facts.push(L.cook + ": " + str(cook) + " " + L.min) }
  let tags = meta.at("tags", default: ())
  if tags.len() > 0 { facts.push(tags.map(t => "#" + t).join(" ")) }
  block(above: 8pt, below: 10pt, text(9pt, fill: muted, facts.join("   ·   ")))

  let hero = meta.at("hero", default: "")
  if hero != "" {
    block(clip: true, radius: 4pt, below: 12pt, image(media(hero), width: 100%, height: 7.5cm, fit: "cover"))
  }

  // --- ingredients ---
  let ing-block = {
    set par(justify: false)
    block(below: 8pt, text(13pt, weight: "bold", fill: accent, L.ingredients))
    let group = ""
    for i in ings {
      let g = tr(i.at("group", default: none), lang)
      if g != group and g != "" {
        block(above: 9pt, below: 5pt, text(9pt, weight: "bold", fill: muted, upper(g)))
      }
      group = g
      let amount = (fmt-qty(i, lang), unit-label(i, lang)).filter(x => x != "").join(" ")
      let note = tr(i.at("note", default: none), lang)
      block(above: 0pt, below: 5pt, grid(
        columns: (1.6cm, 1fr), gutter: 4pt,
        align(right, text(weight: "bold", amount)),
        {
          tr(i.name, lang)
          if note != "" { text(fill: muted, [, #note]) }
          if i.at("optional", default: false) { text(8pt, fill: muted, [ (#L.optional)]) }
        },
      ))
    }
  }

  // --- steps ---
  let steps = meta.at("steps", default: none)
  if steps == none { steps = () }
  let step-block = {
    block(below: 8pt, text(13pt, weight: "bold", fill: accent, L.steps))
    set enum(numbering: n => text(weight: "bold", fill: accent, str(n) + "."), spacing: 9pt)
    enum(..steps.map(st => {
      with-timers(resolve(tr(st, lang), by-id, lang))
      let m = st.at("media", default: "")
      if m.ends-with(".mp4") or m.ends-with(".webm") {
        // Paper cannot play it; point to the app.
        block(above: 4pt, text(8.5pt, fill: accent, [▶ #L.video]))
      } else if m != "" { block(above: 6pt, clip: true, radius: 3pt, image(media(m), width: 60%)) }
    }))
  }

  grid(columns: (6.2cm, 1fr), column-gutter: 9mm, ing-block, step-block)

  let notes = tr(meta.at("notes", default: none), lang)
  if notes != "" {
    v(10pt)
    block(fill: accent.lighten(92%), inset: 10pt, radius: 4pt, width: 100%)[
      #text(weight: "bold", fill: accent, L.notes) \
      #notes
    ]
  }
}
