package dev.cookbook.data

import kotlinx.serialization.KSerializer
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.descriptors.SerialDescriptor
import kotlinx.serialization.encoding.Decoder
import kotlinx.serialization.encoding.Encoder
import kotlinx.serialization.json.JsonDecoder
import kotlinx.serialization.json.JsonEncoder
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonPrimitive

/** A localised string: {"hu": "...", "en": "..."}. */
typealias L = Map<String, String>

val LANGS = listOf("hu", "en")

/** Text in [lang], falling back to the other language. */
fun L?.tr(lang: String): String {
	if (this == null) return ""
	this[lang]?.takeIf { it.isNotBlank() }?.let { return it }
	for (other in LANGS) this[other]?.takeIf { it.isNotBlank() }?.let { return it }
	return values.firstOrNull { it.isNotBlank() }.orEmpty()
}

fun L?.has(lang: String): Boolean = !this?.get(lang).isNullOrBlank()

/** Returns a copy with [lang] set (or removed when blank). */
fun L?.with(lang: String, text: String): L {
	val m = (this ?: emptyMap()).toMutableMap()
	if (text.isBlank()) m.remove(lang) else m[lang] = text
	return m
}

/** meta.json, as in the architecture document plus the optional extensions. */
@Serializable
data class Recipe(
	val id: String = "",
	val title: L = emptyMap(),
	val description: L? = null,
	val category: String? = null,
	val tags: List<String> = emptyList(),
	@SerialName("base_servings") val baseServings: Double = 4.0,
	@SerialName("prep_minutes") val prepMinutes: Int = 0,
	@SerialName("cook_minutes") val cookMinutes: Int = 0,
	val hero: String? = null,
	val ingredients: List<Ingredient> = emptyList(),
	val steps: List<Step> = emptyList(),
	val notes: L? = null,
	val source: String? = null,
	/** A video hosted elsewhere (often YouTube); uploaded videos are step media. */
	@SerialName("video_url") val videoUrl: String? = null,
	val rev: Int = 0,
	val created: String? = null,
	val updated: String? = null,
)

@Serializable
data class Ingredient(
	val id: String = "",
	/** null = "to taste": never scaled, shown without an amount. */
	val qty: Double? = null,
	@SerialName("qty_max") val qtyMax: Double? = null,
	val unit: L? = null,
	@SerialName("unit_id") val unitId: String? = null,
	val name: L = emptyMap(),
	val note: L? = null,
	val category: String? = null,
	val group: L? = null,
	val optional: Boolean = false,
)

/** {"hu": "...", "en": "...", "media": "x.jpg"} — the spec's flat step object. */
@Serializable(with = StepSerializer::class)
data class Step(val text: L = emptyMap(), val media: String? = null)

object StepSerializer : KSerializer<Step> {
	override val descriptor: SerialDescriptor = JsonObject.serializer().descriptor

	override fun deserialize(decoder: Decoder): Step {
		val obj = (decoder as JsonDecoder).decodeJsonElement() as JsonObject
		val text = obj.filterKeys { it != "media" }
			.mapValues { it.value.jsonPrimitive.contentOrNull.orEmpty() }
		return Step(text, obj["media"]?.jsonPrimitive?.contentOrNull)
	}

	override fun serialize(encoder: Encoder, value: Step) {
		val m = value.text.mapValues { JsonPrimitive(it.value) }.toMutableMap()
		value.media?.takeIf { it.isNotEmpty() }?.let { m["media"] = JsonPrimitive(it) }
		(encoder as JsonEncoder).encodeJsonElement(JsonObject(m))
	}
}

@Serializable
data class UnitDef(
	val id: String,
	val label: L = emptyMap(),
	val kind: String = "count",
	val factor: Double = 0.0,
	val system: String? = null,
	val aliases: List<String> = emptyList(),
)

@Serializable
data class Category(val id: String, val label: L = emptyMap())

@Serializable
data class Categories(
	val recipe: List<Category> = emptyList(),
	val aisle: List<Category> = emptyList(),
	/** Cooking methods: fixed tag ids ("oven", "airfryer") with labels. */
	val method: List<Category> = emptyList(),
)

@Serializable
data class ImportResult(val recipe: Recipe, val duplicate: Boolean = false)

@Serializable
data class FeedSource(val name: String, val lang: String = "", val url: String = "", val enabled: Boolean = true)

@Serializable
data class FeedConfig(
	val enabled: Boolean = false,
	@SerialName("interval_hours") val intervalHours: Int = 24,
	@SerialName("per_source") val perSource: Int = 0,
	@SerialName("auto_translate") val autoTranslate: Boolean = false,
	val sources: List<FeedSource> = emptyList(),
)

@Serializable
data class FeedSourceStatus(
	@SerialName("last_run") val lastRun: String = "",
	@SerialName("last_error") val lastError: String? = null,
	val imported: Int = 0,
)

@Serializable
data class FeedRecent(
	val url: String,
	val at: String = "",
	val id: String? = null,
	val result: String = "",
	val source: String? = null,
)

@Serializable
data class FeedStatus(
	val config: FeedConfig = FeedConfig(),
	val running: Boolean = false,
	@SerialName("last_run") val lastRun: String = "",
	@SerialName("next_run") val nextRun: String = "",
	val sources: Map<String, FeedSourceStatus> = emptyMap(),
	val recent: List<FeedRecent> = emptyList(),
)

/** GET /api/export: everything needed offline. */
@Serializable
data class Export(
	val recipes: List<Recipe> = emptyList(),
	val units: List<UnitDef> = emptyList(),
	val categories: Categories = Categories(),
	@SerialName("aisle_keywords") val aisleKeywords: List<AisleKeyword> = emptyList(),
)

@Serializable
data class AisleKeyword(val aisle: String, val words: List<String> = emptyList())

@Serializable
data class Match(val have: Int = 0, val need: Int = 0, val coverage: Double = 0.0, val missing: List<L> = emptyList())

@Serializable
data class Summary(val id: String, val match: Match? = null)

@Serializable
data class SaveResult(val recipe: Recipe, val pdf: Map<String, String> = emptyMap())

@Serializable
data class Conflict(val error: String = "", val current: Recipe? = null)

@Serializable
data class UploadResult(val file: String, val bytes: Long = 0, val poster: String? = null)

fun isVideo(file: String?): Boolean = file != null && (file.endsWith(".mp4") || file.endsWith(".webm"))

/** "a1b2.mp4" -> "a1b2-poster.jpg"; exists only if the server has ffmpeg. */
fun posterName(video: String): String = video.removeSuffix(".mp4").removeSuffix(".webm") + "-poster.jpg"

@Serializable
data class TranslateResult(
	val recipe: Recipe,
	val filled: Int = 0,
	val rejected: List<String> = emptyList(),
	val provider: String = "",
)

@Serializable
data class CatalogEntry(val id: String, val name: L = emptyMap(), val category: String = "", val uses: Int = 0)

@Serializable
data class Health(
	val version: String = "",
	val os: String = "",
	val recipes: Int = 0,
	val typst: Feature = Feature(),
	val translate: Feature = Feature(),
)

@Serializable
data class Feature(val available: Boolean = false, val version: String? = null, val provider: String? = null)

// --- grocery -----------------------------------------------------------------

@Serializable
data class GroceryLine(
	val key: String,
	@SerialName("ingredient_id") val ingredientId: String = "",
	val name: L = emptyMap(),
	val category: String = "",
	val qty: Double? = null,
	@SerialName("unit_id") val unitId: String? = null,
	val unit: L? = null,
	val sources: List<String> = emptyList(),
	val checked: Boolean = false,
)

@Serializable
data class StaticItem(val id: String, val text: String, val category: String = "", val checked: Boolean = false)

@Serializable
data class GrocerySection(
	val category: String,
	val label: L = emptyMap(),
	val lines: List<GroceryLine> = emptyList(),
	val static: List<StaticItem> = emptyList(),
)

@Serializable
data class ChosenRecipe(val id: String, val title: L? = emptyMap(), val servings: Double = 0.0, val missing: Boolean = false)

@Serializable
data class GroceryView(
	val rev: Int = 0,
	val recipes: List<ChosenRecipe> = emptyList(),
	val sections: List<GrocerySection> = emptyList(),
	/** The raw list the view was computed from (server: grocery.json). */
	val state: GroceryState? = null,
)

@Serializable
data class Selection(val id: String, val servings: Double = 0.0)

/** Mirror of the server's grocery.json. */
@Serializable
data class GroceryState(
	val rev: Int = 0,
	val recipes: List<Selection> = emptyList(),
	val static: List<StaticItem> = emptyList(),
	val checked: List<String> = emptyList(),
)

@Serializable
data class GroceryOp(
	val op: String,
	val id: String? = null,
	val servings: Double? = null,
	val key: String? = null,
	val checked: Boolean? = null,
	val text: String? = null,
	val category: String? = null,
)
