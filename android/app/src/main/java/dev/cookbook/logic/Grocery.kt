package dev.cookbook.logic

import dev.cookbook.data.Category
import dev.cookbook.data.ChosenRecipe
import dev.cookbook.data.GroceryLine
import dev.cookbook.data.GroceryOp
import dev.cookbook.data.GrocerySection
import dev.cookbook.data.GroceryState
import dev.cookbook.data.GroceryView
import dev.cookbook.data.L
import dev.cookbook.data.LANGS
import dev.cookbook.data.Recipe
import dev.cookbook.data.Selection
import dev.cookbook.data.StaticItem
import dev.cookbook.data.UnitDef
import dev.cookbook.data.AisleKeyword
import dev.cookbook.data.tr
import kotlin.math.roundToLong

/**
 * The grocery list, computed on the phone so it works with no server.
 *
 * This is a port of server/internal/grocery (apply + Aggregate + merge). The
 * two must agree line for line: GroceryParityTest runs this on the fixture the
 * Go tests generate from the server's own output.
 */
object Grocery {

	/** Applies one operation, exactly like the server's apply(). */
	fun apply(st: GroceryState, op: GroceryOp, guess: (L) -> String): GroceryState = when (op.op) {
		"add_recipe" -> {
			val id = op.id ?: return st
			val servings = op.servings ?: 0.0
			if (st.recipes.any { it.id == id }) st.copy(recipes = st.recipes.map { if (it.id == id) it.copy(servings = servings) else it })
			else st.copy(recipes = st.recipes + Selection(id, servings))
		}
		"remove_recipe" -> st.copy(recipes = st.recipes.filterNot { it.id == op.id })
		"check" -> {
			val key = op.key.orEmpty()
			if (key.startsWith("static:")) {
				st.copy(static = st.static.map { if ("static:${it.id}" == key) it.copy(checked = op.checked == true) else it })
			} else {
				val rest = st.checked.filterNot { it == key }
				st.copy(checked = if (op.checked == true) rest + key else rest)
			}
		}
		"add_static" -> {
			val text = op.text?.trim().orEmpty()
			val id = op.id.orEmpty()
			if (text.isEmpty() || st.static.any { it.id == id }) st
			else {
				val cat = op.category?.takeIf { it.isNotEmpty() } ?: guess(mapOf("hu" to text, "en" to text))
				st.copy(static = st.static + StaticItem(id, text, cat))
			}
		}
		"remove_static" -> st.copy(static = st.static.filterNot { it.id == op.id })
		"clear_done" -> st.copy(static = st.static.filterNot { it.checked }, checked = emptyList())
		"clear_all" -> GroceryState(rev = st.rev)
		else -> st
	}

	private class Contribution(val qty: Double?, val unitId: String?, val unit: L?, val name: L, val category: String?, val source: String)

	private class Bucket(val key: String, val kind: String, val unit: L?, val unitId: String?) {
		val unitIds = mutableSetOf<String>()
		var sum = 0.0
		var raw = 0.0
		val sources = mutableListOf<String>()
	}

	private val DUP = Regex("-\\d+$")

	fun aggregate(
		st: GroceryState,
		recipes: Map<String, Recipe>,
		units: Map<String, UnitDef>,
		aisles: List<Category>,
		guess: (L) -> String,
	): GroceryView {
		val chosen = mutableListOf<ChosenRecipe>()
		val byIng = LinkedHashMap<String, MutableList<Contribution>>()
		for (sel in st.recipes) {
			val r = recipes[sel.id]
			if (r == null) {
				chosen += ChosenRecipe(sel.id, emptyMap(), sel.servings, missing = true)
				continue
			}
			chosen += ChosenRecipe(r.id, r.title, sel.servings)
			val factor = if (sel.servings > 0 && r.baseServings > 0) sel.servings / r.baseServings else 1.0
			for (ing in r.ingredients) {
				if (ing.optional) continue
				// A range buys the upper end: better one egg too many.
				val q = ing.qty?.let { (ing.qtyMax ?: it) * factor }
				val id = ing.id.replace(DUP, "")
				byIng.getOrPut(id) { mutableListOf() } += Contribution(q, ing.unitId, ing.unit, ing.name, ing.category, r.id)
			}
		}

		val checked = st.checked.toSet()
		val sections = LinkedHashMap<String, Pair<MutableList<GroceryLine>, MutableList<StaticItem>>>()
		fun section(cat: String) = sections.getOrPut(cat) { mutableListOf<GroceryLine>() to mutableListOf() }

		for ((id, cs) in byIng) {
			for (line in merge(id, cs, units, guess)) {
				section(line.category).first += line.copy(checked = line.key in checked)
			}
		}
		for (item in st.static) section(item.category.ifEmpty { "other" }).second += item

		val out = mutableListOf<GrocerySection>()
		for (c in aisles) {
			val s = sections.remove(c.id) ?: continue
			out += GrocerySection(c.id, c.label, s.first.sortedBy { it.name.tr("hu").lowercase() }, s.second)
		}
		// Categories outside the aisle table go last, as on the server.
		for (k in sections.keys.sorted()) {
			val s = sections.getValue(k)
			out += GrocerySection(k, mapOf("en" to k, "hu" to k), s.first, s.second)
		}
		return GroceryView(st.rev, chosen, out, st)
	}

	private fun merge(id: String, cs: List<Contribution>, units: Map<String, UnitDef>, guess: (L) -> String): List<GroceryLine> {
		val buckets = LinkedHashMap<String, Bucket>()
		val name = mutableMapOf<String, String>()
		var category = ""
		val tasteSources = mutableListOf<String>()
		for (c in cs) {
			for (lang in LANGS) {
				if (name[lang].isNullOrBlank() && !c.name[lang].isNullOrBlank()) name[lang] = c.name.getValue(lang)
			}
			if (category.isEmpty()) category = c.category.orEmpty()
			val q = c.qty
			if (q == null) {
				if (c.source !in tasteSources) tasteSources += c.source
				continue
			}
			val u = units[c.unitId]
			val convertible = u != null && u.factor != 0.0
			val key = when {
				convertible -> "kind:${u!!.kind}"
				!c.unitId.isNullOrEmpty() -> "unit:${c.unitId}"
				else -> "label:" + c.unit.tr("hu").lowercase()
			}
			val b = buckets.getOrPut(key) { Bucket(key, if (convertible) u!!.kind else "", c.unit, c.unitId) }
			b.unitIds += c.unitId.orEmpty()
			if (convertible) b.sum += q * u!!.factor
			b.raw += q
			if (c.source !in b.sources) b.sources += c.source
		}
		if (category.isEmpty()) category = guess(name)

		val lines = buckets.values.map { b ->
			var unitId = b.unitId
			var unit = b.unit
			val q = if (b.kind.isNotEmpty() && b.unitIds.size > 1) {
				// Mixed units of one kind: sum in grams/ml, show a readable unit.
				val (v, uid) = fromBase(b.sum, b.kind)
				unitId = uid
				unit = units[uid]?.label
				v
			} else b.raw
			GroceryLine("$id|${b.key}", id, name, category, round2(q), unitId?.ifEmpty { null }, unit, b.sources)
		}
		if (lines.isEmpty() && tasteSources.isNotEmpty()) {
			return listOf(GroceryLine("$id|taste", id, name, category, null, null, null, tasteSources))
		}
		return lines
	}

	private fun fromBase(base: Double, kind: String): Pair<Double, String> = when (kind) {
		"mass" -> if (base >= 1000) round2(base / 1000) to "kg" else round2(base) to "g"
		"volume" -> when {
			base >= 1000 -> round2(base / 1000) to "l"
			base >= 100 -> round2(base / 100) to "dl"
			else -> round2(base) to "ml"
		}
		else -> base to ""
	}

	private fun round2(v: Double) = (v * 100).roundToLong() / 100.0

	/** Port of recipe.GuessAisle, over the keyword table the server exports. */
	fun guesser(keywords: List<AisleKeyword>): (L) -> String = { name ->
		var hit = "other"
		run {
			for (lang in LANGS) {
				val slug = fold(name[lang].orEmpty()).replace(Regex("[^a-z0-9]+"), "_").trim('_')
				if (slug.isEmpty()) continue
				val s = "_${slug}_"
				for (group in keywords) for (w in group.words) {
					val match = if (w.length <= 3) s.contains("_${w}_")
					else s.contains("_$w") || (s.contains(w) && w.length >= 5)
					if (match) { hit = group.aisle; return@run }
				}
			}
		}
		hit
	}
}
