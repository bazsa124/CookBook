package dev.cookbook.data

import android.content.Context
import dev.cookbook.logic.Grocery
import dev.cookbook.logic.fold
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import java.io.File
import java.io.IOException

/**
 * Offline-first store. The whole book (it is small: ~45 recipes is ~200 KB) is
 * cached from GET /api/export, so browsing, scaling and cook mode work with no
 * connection at all. Writes need the server; `rev` makes conflicts visible.
 */
class Repository(context: Context, private val api: Api, private val settings: Settings) {

	private val cacheFile = File(context.filesDir, "export.json")
	private val groceryFile = File(context.filesDir, "grocery.json")
	private val lock = Mutex()

	private val _export = MutableStateFlow(Export())
	val export: StateFlow<Export> = _export.asStateFlow()

	private val _online = MutableStateFlow<Boolean?>(null)
	/** null = not tried yet. */
	val online: StateFlow<Boolean?> = _online.asStateFlow()

	/** Last list state confirmed by the server. */
	private var serverGrocery = GroceryState()
	private val groceryLock = Mutex()
	private val _grocery = MutableStateFlow(GroceryState())
	/** The list including changes not yet synced; aggregate with [dev.cookbook.logic.Grocery]. */
	val grocery: StateFlow<GroceryState> = _grocery.asStateFlow()

	private val _pendingOps = MutableStateFlow(0)
	val pendingOps: StateFlow<Int> = _pendingOps.asStateFlow()

	val units: Map<String, UnitDef> get() = _export.value.units.associateBy { it.id }

	fun recipe(id: String): Recipe? = _export.value.recipes.firstOrNull { it.id == id }

	suspend fun loadCache() = withContext(Dispatchers.IO) {
		runCatching { _export.value = api.json.decodeFromString(cacheFile.readText()) }
		runCatching { serverGrocery = api.json.decodeFromString(GroceryState.serializer(), groceryFile.readText()) }
		_pendingOps.value = pending().size
		recomputeGrocery()
	}

	/** Pulls the book if it changed. Returns an error message, or null. */
	suspend fun refresh(): String? = lock.withLock {
		if (!settings.configured) return "not configured"
		try {
			val res = api.export(if (cacheFile.exists()) settings.exportEtag else "")
			if (res != null) {
				val (exp, etag) = res
				_export.value = exp
				withContext(Dispatchers.IO) { cacheFile.writeText(api.json.encodeToString(Export.serializer(), exp)) }
				settings.exportEtag = etag
			}
			_online.value = true
			null
		} catch (e: IOException) {
			_online.value = false
			e.message ?: "offline"
		}
	}

	/** Puts a saved recipe into the cache at once, before the next refresh. */
	fun putLocal(r: Recipe) {
		val list = _export.value.recipes.filterNot { it.id == r.id } + r
		_export.value = _export.value.copy(recipes = list.sortedBy { fold(it.title.tr("hu")) })
		settings.exportEtag = "" // force a full refresh next time
	}

	fun removeLocal(id: String) {
		_export.value = _export.value.copy(recipes = _export.value.recipes.filterNot { it.id == id })
		settings.exportEtag = ""
	}

	// --- search ------------------------------------------------------------------

	data class Query(
		val text: String = "",
		val category: String? = null,
		val tags: Set<String> = emptySet(),
		val include: List<String> = emptyList(),
		val exclude: List<String> = emptyList(),
		val pantry: List<String> = emptyList(),
		val favoritesOnly: Boolean = false,
	) {
		val isServerQuery get() = text.isNotBlank() || include.isNotEmpty() || exclude.isNotEmpty() || pantry.isNotEmpty()
	}

	data class Hit(val recipe: Recipe, val match: Match? = null)

	/**
	 * Server search when online (full-text over steps too, and the pantry
	 * ranking), local filtering over the cache otherwise.
	 */
	suspend fun search(q: Query): List<Hit> {
		val all = _export.value.recipes
		val favs = settings.favorites.value
		fun keep(r: Recipe) = (!q.favoritesOnly || r.id in favs) &&
			(q.category == null || r.category == q.category) &&
			q.tags.all { it in r.tags }
		if (q.isServerQuery && _online.value != false) {
			try {
				val res = api.search(q.text, q.category, q.tags.toList(), q.include, q.exclude, q.pantry)
				_online.value = true
				val byId = all.associateBy { it.id }
				return res.mapNotNull { s -> byId[s.id]?.let { Hit(it, s.match) } }.filter { keep(it.recipe) }
			} catch (e: IOException) {
				if (e is ApiException) throw e
				_online.value = false
			}
		}
		return localSearch(all, q).filter { keep(it.recipe) }
	}

	private fun localSearch(all: List<Recipe>, q: Query): List<Hit> {
		val words = fold(q.text).split(' ').filter { it.isNotBlank() }
		fun ingIds(r: Recipe) = r.ingredients.map { it.id }
		var out = all.asSequence().filter { r ->
			val hay = fold(buildString {
				append(r.title.values.joinToString(" ")).append(' ')
				r.ingredients.forEach { append(it.name.values.joinToString(" ")).append(' ') }
				append(r.tags.joinToString(" "))
			})
			words.all { hay.contains(it) } &&
				q.include.all { t -> ingIds(r).any { it.contains(fold(t).replace(' ', '_')) } } &&
				q.exclude.none { t -> ingIds(r).any { it.contains(fold(t).replace(' ', '_')) } }
		}.map { Hit(it) }.toList()
		if (q.pantry.isNotEmpty()) {
			val terms = q.pantry.map { fold(it).replace(' ', '_') }
			out = out.mapNotNull { h ->
				val need = h.recipe.ingredients.filter { it.qty != null && !it.optional && it.id !in STAPLES }
				val have = need.count { i -> terms.any { i.id.contains(it) } }
				if (have == 0) null
				else h.copy(match = Match(have, need.size, have.toDouble() / need.size,
					need.filter { i -> terms.none { i.id.contains(it) } }.map { it.name }))
			}.sortedWith(compareBy({ it.match!!.missing.size }, { -it.match!!.coverage }))
		}
		return out
	}

	// --- writes --------------------------------------------------------------------

	suspend fun catalog(): List<CatalogEntry> = runCatching { api.catalog() }.getOrElse {
		// Offline: derive from the cache.
		_export.value.recipes.flatMap { it.ingredients }.groupBy { it.id }
			.map { (id, l) -> CatalogEntry(id, l.first().name, l.first().category.orEmpty(), l.size) }
			.sortedByDescending { it.uses }
	}

	// --- grocery -------------------------------------------------------------------

	private fun pending(): List<GroceryOp> = runCatching {
		api.json.decodeFromString<List<GroceryOp>>(settings.pendingGroceryOps)
	}.getOrDefault(emptyList())

	private fun setPending(ops: List<GroceryOp>) {
		settings.pendingGroceryOps = if (ops.isEmpty()) "" else api.json.encodeToString(
			kotlinx.serialization.builtins.ListSerializer(GroceryOp.serializer()), ops)
		_pendingOps.value = ops.size
	}

	/** The grocery list as the phone sees it: server state + queued changes. */
	private fun recomputeGrocery() {
		val guess = guesser()
		_grocery.value = pending().fold(serverGrocery) { st, op -> Grocery.apply(st, op, guess) }
	}

	fun guesser(): (L) -> String = Grocery.guesser(_export.value.aisleKeywords)

	/**
	 * Applies grocery changes. Every change works offline: it is applied to the
	 * local list at once, queued, and sent with the next successful sync. The
	 * server merges operations, so changes from two phones combine.
	 * Returns an error only when the server rejected the changes.
	 */
	suspend fun grocery(ops: List<GroceryOp> = emptyList()): String? {
		if (ops.isNotEmpty()) {
			setPending(pending() + ops)
			recomputeGrocery()
		}
		return syncGrocery()
	}

	private suspend fun syncGrocery(): String? = groceryLock.withLock {
		if (!settings.configured) return@withLock null
		val sent = pending()
		try {
			val v = if (sent.isEmpty()) api.grocery() else api.groceryOps(sent)
			// Ops queued while the request was in flight stay queued.
			setPending(pending().drop(sent.size))
			v.state?.let { st ->
				serverGrocery = st
				withContext(Dispatchers.IO) { groceryFile.writeText(api.json.encodeToString(GroceryState.serializer(), st)) }
			}
			_online.value = true
			recomputeGrocery()
			null
		} catch (e: ApiException) {
			// The server refused the batch (e.g. an op for a deleted recipe).
			// Drop it rather than retrying it forever, and resync.
			setPending(pending().drop(sent.size))
			recomputeGrocery()
			e.message
		} catch (e: IOException) {
			_online.value = false
			null
		}
	}

	/** Ids for items added on the phone, so queued ops can refer to them. */
	fun newStaticId(): String = "p-" + java.util.UUID.randomUUID().toString().replace("-", "").take(12)

	companion object {
		/** Mirrors the server's staple list: never "missing" in pantry search. */
		val STAPLES = setOf("so", "salt", "bors", "pepper", "black_pepper", "viz", "water", "olaj", "oil", "cukor", "sugar")
	}
}
