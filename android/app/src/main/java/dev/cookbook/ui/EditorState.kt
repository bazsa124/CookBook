package dev.cookbook.ui

import android.content.ContentResolver
import android.graphics.Bitmap
import android.graphics.ImageDecoder
import android.net.Uri
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.input.TextFieldValue
import dev.cookbook.data.Ingredient
import dev.cookbook.data.L
import dev.cookbook.data.Recipe
import dev.cookbook.data.Step
import dev.cookbook.data.tr
import dev.cookbook.data.with
import dev.cookbook.logic.fold
import dev.cookbook.logic.formatQty
import dev.cookbook.logic.parseQty
import java.io.ByteArrayOutputStream

/** One ingredient row while editing. */
class EditIngredient(val uid: Int, ing: Ingredient, qtyText: String, idLocked: Boolean) {
	var ing by mutableStateOf(ing)
	var qtyText by mutableStateOf(qtyText)
	/** Once an id is referenced (or came from the server) it must not drift with the name. */
	var idLocked by mutableStateOf(idLocked)
	var expanded by mutableStateOf(false)
}

/** One step row; [value] is the text field for the language being edited. */
class EditStep(val uid: Int, step: Step, lang: String) {
	var step by mutableStateOf(step)
	var value by mutableStateOf(TextFieldValue(step.text[lang].orEmpty()))
}

/** A photo picked on the phone, not uploaded yet. Referenced as "local:<n>". */
class LocalPhoto(val jpeg: ByteArray, val preview: ImageBitmap)

/**
 * Editor state. Structural fields (quantities, units, photos) are shared by
 * both languages; text fields show the language chosen in [lang].
 */
class EditorState(val original: Recipe?, initialLang: String, private val methodIds: Set<String> = emptySet()) {
	private var nextUid = 1
	var lang by mutableStateOf(initialLang)
		private set

	var title by mutableStateOf(original?.title ?: emptyMap())
	var description by mutableStateOf(original?.description ?: emptyMap())
	var notes by mutableStateOf(original?.notes ?: emptyMap())
	var category by mutableStateOf(original?.category ?: "main")
	/** Cooking methods (fixed ids, chosen with chips) are kept apart from free-text tags. */
	var methods by mutableStateOf(original?.tags?.filter { it in methodIds }?.toSet() ?: emptySet())
	var tagsText by mutableStateOf(original?.tags?.filterNot { it in methodIds }?.joinToString(", ").orEmpty())

	fun toggleMethod(id: String) {
		methods = if (id in methods) methods - id else methods + id
		dirty = true
	}
	var servingsText by mutableStateOf(formatQty(original?.baseServings ?: 4.0, "en", false))
	var prepText by mutableStateOf(original?.prepMinutes?.takeIf { it > 0 }?.toString().orEmpty())
	var cookText by mutableStateOf(original?.cookMinutes?.takeIf { it > 0 }?.toString().orEmpty())
	var hero by mutableStateOf(original?.hero)
	var rev by mutableStateOf(original?.rev ?: 0)
	/** Id assigned by the server on the first save of a new recipe, so a retry updates instead of duplicating. */
	var savedId by mutableStateOf("")
	var dirty by mutableStateOf(false)

	val ingredients = mutableStateListOf<EditIngredient>().apply {
		original?.ingredients?.forEach { add(EditIngredient(nextUid++, it, qtyString(it), idLocked = true)) }
	}
	val steps = mutableStateListOf<EditStep>().apply {
		original?.steps?.forEach { add(EditStep(nextUid++, it, initialLang)) }
	}
	val photos = mutableStateMapOf<String, LocalPhoto>()
	/** Picked videos, referenced as "local:v<n>", uploaded from their Uri on save. */
	val videos = mutableStateMapOf<String, Uri>()
	var videoUrl by mutableStateOf(original?.videoUrl.orEmpty())
	private var photoSeq = 1

	private fun qtyString(i: Ingredient): String {
		val q = i.qty ?: return ""
		val a = formatQty(q, "en", false)
		return i.qtyMax?.let { "$a-${formatQty(it, "en", false)}" } ?: a
	}

	fun switchLang(l: String) {
		lang = l
		steps.forEach { it.value = TextFieldValue(it.step.text[l].orEmpty()) }
	}

	fun setText(field: L, text: String): L { dirty = true; return field.with(lang, text) }

	fun addIngredient() {
		ingredients.add(EditIngredient(nextUid++, Ingredient(), "", idLocked = false).apply { expanded = false })
		dirty = true
	}

	/** Name typed by the user: the id follows it until locked. */
	fun rename(e: EditIngredient, name: String) {
		e.ing = e.ing.copy(name = e.ing.name.with(lang, name))
		if (!e.idLocked) e.ing = e.ing.copy(id = uniqueId(slug(e.ing.name.tr("en")), e))
		dirty = true
	}

	/** Picking a known ingredient reuses its id: that is what merges grocery lines. */
	fun pick(e: EditIngredient, id: String, name: L, category: String) {
		e.ing = e.ing.copy(id = uniqueId(id, e), name = name, category = category.ifEmpty { null })
		e.idLocked = true
		dirty = true
	}

	private fun slug(s: String) = fold(s).replace(Regex("[^a-z0-9]+"), "_").trim('_').take(60)

	private fun uniqueId(base: String, self: EditIngredient): String {
		if (base.isEmpty()) return ""
		var id = base
		var n = 2
		while (ingredients.any { it !== self && it.ing.id == id }) id = "$base-${n++}"
		return id
	}

	fun addStep() {
		steps.add(EditStep(nextUid++, Step(), lang)); dirty = true
	}

	fun editStep(e: EditStep, v: TextFieldValue) {
		e.value = v
		e.step = e.step.copy(text = e.step.text.with(lang, v.text))
		dirty = true
	}

	/** Inserts [token] at the cursor of step [e]. */
	fun insert(e: EditStep, token: String) {
		val v = e.value
		val text = v.text.replaceRange(v.selection.min, v.selection.max, token)
		editStep(e, TextFieldValue(text, TextRange(v.selection.min + token.length)))
		// A referenced ingredient's id is now load-bearing.
		Regex("""\{\{\s*([a-z0-9_-]+)""").findAll(token).forEach { m ->
			ingredients.firstOrNull { it.ing.id == m.groupValues[1] }?.idLocked = true
		}
	}

	fun <T> move(list: MutableList<T>, from: Int, to: Int) {
		if (to !in list.indices) return
		val item = list.removeAt(from)
		list.add(to, item)
		dirty = true
	}

	fun addVideo(uri: Uri): String {
		val key = "local:v${photoSeq++}"
		videos[key] = uri
		dirty = true
		return key
	}

	fun addPhoto(p: LocalPhoto): String {
		val key = "local:${photoSeq++}"
		photos[key] = p
		dirty = true
		return key
	}

	/** Snapshot as a recipe; photo refs may still be "local:n". */
	fun build(): Recipe = Recipe(
		id = original?.id ?: savedId,
		title = title,
		description = description.takeIf { it.isNotEmpty() },
		category = category,
		tags = methods.toList() + tagsText.split(',').map { it.trim() }.filter { it.isNotEmpty() && it !in methods },
		baseServings = servingsText.replace(',', '.').toDoubleOrNull()?.takeIf { it > 0 } ?: 4.0,
		prepMinutes = prepText.toIntOrNull() ?: 0,
		cookMinutes = cookText.toIntOrNull() ?: 0,
		hero = hero,
		ingredients = ingredients.filter { !it.ing.name.values.all { n -> n.isBlank() } }.map {
			val (q, qm) = parseQty(it.qtyText)
			it.ing.copy(qty = q, qtyMax = qm)
		},
		steps = steps.map { it.step }.filter { st -> st.text.values.any { it.isNotBlank() } || st.media != null },
		notes = notes.takeIf { it.isNotEmpty() },
		source = original?.source,
		videoUrl = videoUrl.trim().ifEmpty { null },
		rev = rev,
		created = original?.created,
	)

	/** Takes texts from a translated copy (same shape as [build]). */
	fun applyTranslation(r: Recipe) {
		title = r.title
		description = r.description ?: emptyMap()
		notes = r.notes ?: emptyMap()
		val live = ingredients.filter { !it.ing.name.values.all { n -> n.isBlank() } }
		live.forEachIndexed { i, e ->
			r.ingredients.getOrNull(i)?.let { t -> e.ing = e.ing.copy(name = t.name, note = t.note, group = t.group, unit = t.unit) }
		}
		val liveSteps = steps.filter { st -> st.step.text.values.any { it.isNotBlank() } || st.step.media != null }
		liveSteps.forEachIndexed { i, e -> r.steps.getOrNull(i)?.let { e.step = e.step.copy(text = it.text) } }
		switchLang(lang)
		dirty = true
	}
}

/**
 * Decodes a picked photo scaled to at most [maxEdge] px (ImageDecoder applies
 * EXIF rotation) and compresses it: a 4 MB phone photo becomes ~250 KB before
 * it touches mobile data. The server re-compresses anyway.
 */
fun loadPhoto(resolver: ContentResolver, uri: Uri, maxEdge: Int = 1600): LocalPhoto {
	val src = ImageDecoder.createSource(resolver, uri)
	val bmp = ImageDecoder.decodeBitmap(src) { dec, info, _ ->
		val w = info.size.width
		val h = info.size.height
		val scale = minOf(1f, maxEdge.toFloat() / maxOf(w, h))
		dec.setTargetSize((w * scale).toInt().coerceAtLeast(1), (h * scale).toInt().coerceAtLeast(1))
		dec.allocator = ImageDecoder.ALLOCATOR_SOFTWARE
	}
	val out = ByteArrayOutputStream()
	bmp.compress(Bitmap.CompressFormat.JPEG, 85, out)
	return LocalPhoto(out.toByteArray(), bmp.asImageBitmap())
}
