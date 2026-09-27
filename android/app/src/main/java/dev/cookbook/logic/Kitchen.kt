package dev.cookbook.logic

import dev.cookbook.data.Ingredient
import dev.cookbook.data.L
import dev.cookbook.data.UnitDef
import dev.cookbook.data.tr
import java.text.Normalizer
import kotlin.math.abs
import kotlin.math.floor
import kotlin.math.roundToInt

/** Accent-free lower case: "Gulyás" -> "gulyas". For matching what people type. */
fun fold(s: String): String =
	Normalizer.normalize(s.lowercase(), Normalizer.Form.NFD).replace(Regex("\\p{Mn}+"), "")

/** An ingredient as it should be displayed right now: scaled and converted. */
data class Shown(val qty: Double?, val qtyMax: Double?, val unitId: String?, val unitLabel: String, val name: String, val note: String)

/**
 * Pipeline 2 of the spec, done on the phone: scaling, unit localisation,
 * mustache resolution and timer parsing. Pure functions over the recipe and
 * the unit table, so it works offline and is unit-testable.
 */
class Kitchen(
	private val units: Map<String, UnitDef>,
	val lang: String,
	private val imperial: Boolean,
	/** servings / base_servings */
	private val factor: Double,
) {
	fun show(i: Ingredient): Shown {
		val name = i.name.tr(lang)
		val note = i.note.tr(lang)
		val q = i.qty ?: return Shown(null, null, i.unitId, labelFor(i.unitId, i.unit), name, note)
		var qty = q * factor
		var qtyMax = i.qtyMax?.times(factor)
		var unitId = i.unitId
		val u = units[unitId]
		if (u != null && u.factor > 0) {
			val target = convertTarget(u, qty * u.factor)
			if (target != null && target.id != u.id) {
				val ratio = u.factor / target.factor
				qty *= ratio
				qtyMax = qtyMax?.times(ratio)
				unitId = target.id
			}
		}
		val label = if (unitId == i.unitId) labelFor(unitId, i.unit) else labelFor(unitId, null)
		return Shown(qty, qtyMax, unitId, label, name, note)
	}

	private fun labelFor(unitId: String?, own: L?): String {
		own.tr(lang).takeIf { it.isNotEmpty() }?.let { return it }
		return units[unitId]?.label.tr(lang)
	}

	/**
	 * Picks the unit to show a quantity in, or null to keep it. Spoons and
	 * mugs are used in both systems and never convert.
	 */
	private fun convertTarget(u: UnitDef, base: Double): UnitDef? {
		if (u.system == "both") return null
		val wantImperial = imperial
		if ((u.system == "imperial") == wantImperial) return null
		fun unit(id: String) = units[id]
		return when (u.kind) {
			"mass" -> if (wantImperial) {
				if (base >= 450) unit("lb") else unit("oz")
			} else {
				if (base >= 1000) unit("kg") else unit("g")
			}
			"volume" -> if (wantImperial) {
				when {
					base < 15 -> unit("tsp")
					base < 60 -> unit("tbsp")
					base >= 3785 -> unit("gal")
					else -> unit("cup")
				}
			} else {
				when {
					base >= 1000 -> unit("l")
					base >= 100 -> unit("dl")
					else -> unit("ml")
				}
			}
			else -> null
		}
	}

	fun qtyText(s: Shown): String {
		val q = s.qty ?: return ""
		val fractions = imperial || units[s.unitId]?.kind == "count" || s.unitId == null
		val a = formatQty(q, lang, fractions)
		return if (s.qtyMax != null) "$a–${formatQty(s.qtyMax, lang, fractions)}" else a
	}

	/** "500 g", "2 fej", "" for to-taste. */
	fun amount(i: Ingredient): String {
		val s = show(i)
		return listOf(qtyText(s), s.unitLabel).filter { it.isNotEmpty() }.joinToString(" ")
	}

	fun temperature(celsius: Int): String =
		if (imperial) "${((celsius * 9.0 / 5 + 32) / 5).roundToInt() * 5} °F" else "$celsius °C"

	/** Replaces {{id.qty}}, {{id.unit}}, {{id.name}}, {{id}} and {{temp:C}}. */
	fun resolve(text: String, ingredients: List<Ingredient>): String {
		val byId = ingredients.associateBy { it.id }
		return MUSTACHE.replace(text) { m ->
			val ref = m.groupValues[1]
			val field = m.groupValues[2]
			if (ref.startsWith("temp:")) {
				return@replace ref.removePrefix("temp:").toIntOrNull()?.let { temperature(it) } ?: m.value
			}
			val ing = byId[ref] ?: return@replace m.value
			val s = show(ing)
			when (field) {
				"qty" -> qtyText(s)
				"unit" -> s.unitLabel
				"name" -> s.name
				else -> listOf(qtyText(s), s.unitLabel, s.name).filter { it.isNotEmpty() }.joinToString(" ")
			}
		}
	}

	companion object {
		val MUSTACHE = Regex("""\{\{\s*([a-z0-9_:-]+)(?:\.(qty|unit|name))?\s*\}\}""")
	}
}

/** A piece of step text: plain words or a tap-to-start timer. */
sealed interface Segment {
	data class Text(val text: String) : Segment
	data class Timer(val seconds: Int, val label: String) : Segment
}

val TIMER = Regex("""\[(\d{1,3}):([0-5]\d)(?::([0-5]\d))?\]""")

/** Splits "Főzd [120:00] percig" into text and timer segments. */
fun segments(text: String): List<Segment> {
	val out = mutableListOf<Segment>()
	var pos = 0
	for (m in TIMER.findAll(text)) {
		if (m.range.first > pos) out += Segment.Text(text.substring(pos, m.range.first))
		val a = m.groupValues[1].toInt()
		val b = m.groupValues[2].toInt()
		val c = m.groupValues[3]
		// [MM:SS] per the spec; [H:MM:SS] when a third part is present.
		val secs = if (c.isEmpty()) a * 60 + b else a * 3600 + b * 60 + c.toInt()
		out += Segment.Timer(secs, m.value.trim('[', ']'))
		pos = m.range.last + 1
	}
	if (pos < text.length) out += Segment.Text(text.substring(pos))
	return out
}

private val GLYPHS = listOf(0.25 to "¼", 1.0 / 3 to "⅓", 0.5 to "½", 2.0 / 3 to "⅔", 0.75 to "¾", 0.125 to "⅛")

/**
 * 1.5 -> "1,5" (hu) / "1.5" (en); with fractions 1.5 -> "1½", 0.333 -> "⅓".
 * Large metric amounts are rounded to what a scale shows.
 */
fun formatQty(v: Double, lang: String, fractions: Boolean): String {
	if (fractions) {
		val whole = floor(v)
		val frac = v - whole
		if (frac < 0.06) return fmtNum(whole, lang)
		if (frac > 0.94) return fmtNum(whole + 1, lang)
		GLYPHS.minByOrNull { abs(it.first - frac) }?.let { (value, glyph) ->
			if (abs(value - frac) < 0.05) return (if (whole > 0) fmtNum(whole, lang) else "") + glyph
		}
	}
	val rounded = when {
		v >= 100 -> (v / 5).roundToInt() * 5.0
		v >= 10 -> (v * 2).roundToInt() / 2.0
		else -> (v * 100).roundToInt() / 100.0
	}
	return fmtNum(rounded, lang)
}

private fun fmtNum(v: Double, lang: String): String {
	val s = if (v == floor(v)) v.toLong().toString() else v.toString().trimEnd('0').trimEnd('.')
	return if (lang == "hu") s.replace('.', ',') else s
}

/** "1:05:00" style countdown text. */
fun clock(seconds: Int): String {
	val s = seconds.coerceAtLeast(0)
	val h = s / 3600
	val m = (s % 3600) / 60
	val sec = s % 60
	return if (h > 0) "%d:%02d:%02d".format(h, m, sec) else "%d:%02d".format(m, sec)
}

/** Parses "2,5", "1/2", "1 1/2", "½", "10-15" -> (qty, qtyMax). */
fun parseQty(input: String): Pair<Double?, Double?> {
	val t = input.trim()
	if (t.isEmpty()) return null to null
	val parts = t.split('-', '–').map { it.trim() }
	fun one(s: String): Double? {
		var total = 0.0
		var any = false
		for (tok in s.split(' ').filter { it.isNotEmpty() }) {
			val g = GLYPHS.firstOrNull { tok.endsWith(it.second) }
			val num = if (g != null) tok.removeSuffix(g.second) else tok
			if (g != null) { total += g.first; any = true }
			if (num.isEmpty()) continue
			val v = if ('/' in num) {
				val (a, b) = num.split('/', limit = 2)
				val d = b.toDoubleOrNull() ?: return null
				(a.toDoubleOrNull() ?: return null) / d
			} else num.replace(',', '.').toDoubleOrNull() ?: return null
			total += v; any = true
		}
		return if (any) total else null
	}
	val a = one(parts[0]) ?: return null to null
	val b = parts.getOrNull(1)?.let { one(it) }?.takeIf { it > a }
	return a to b
}
