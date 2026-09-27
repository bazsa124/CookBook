package dev.cookbook.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.MultipartBody
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import java.io.File
import java.io.IOException
import java.util.concurrent.TimeUnit

/** An error the server explained, as opposed to "could not reach it". */
class ApiException(val status: Int, message: String, val details: List<String> = emptyList(), val body: String = "") :
	IOException(message)

@Serializable
private data class ErrorBody(val error: String = "", val details: List<String> = emptyList())

@Serializable
private data class TranslateRequest(val recipe: Recipe, val from: String, val to: String)

@Serializable
private data class ImportRequest(val url: String)

@Serializable
private data class OpsRequest(val ops: List<GroceryOp>)

class Api(private val settings: Settings) {

	val json = Json {
		ignoreUnknownKeys = true
		explicitNulls = false
		encodeDefaults = true
		coerceInputValues = true
	}

	/** Shared with Coil, so images carry the token too. */
	val http: OkHttpClient = OkHttpClient.Builder()
		.connectTimeout(8, TimeUnit.SECONDS)
		// Saving compiles PDFs on an old laptop; translation waits on an LLM.
		.readTimeout(120, TimeUnit.SECONDS)
		.addInterceptor { chain ->
			val req = chain.request()
			val base = settings.baseUrl()
			// Only our own server gets the token.
			if (base.isNotEmpty() && req.url.toString().startsWith(base)) {
				chain.proceed(req.newBuilder().header("Authorization", "Bearer ${settings.token}").build())
			} else chain.proceed(req)
		}
		.build()

	private val jsonType = "application/json; charset=utf-8".toMediaType()

	fun url(path: String): String = settings.baseUrl() + path

	fun mediaUrl(recipeId: String, file: String): String = url("/api/recipes/$recipeId/media/$file")

	private suspend fun call(req: Request): Pair<Int, String> = withContext(Dispatchers.IO) {
		if (settings.baseUrl().isEmpty()) throw IOException("No server configured")
		http.newCall(req).execute().use { resp ->
			val body = resp.body?.string().orEmpty()
			if (resp.code >= 400) {
				val err = runCatching { json.decodeFromString<ErrorBody>(body) }.getOrNull()
				throw ApiException(resp.code, err?.error?.ifEmpty { null } ?: "HTTP ${resp.code}", err?.details.orEmpty(), body)
			}
			resp.code to body
		}
	}

	private suspend inline fun <reified T> get(path: String): T =
		json.decodeFromString(call(Request.Builder().url(url(path)).build()).second)

	private suspend inline fun <reified B, reified T> send(method: String, path: String, body: B): T {
		val req = Request.Builder().url(url(path))
			.method(method, json.encodeToString(kotlinx.serialization.serializer<B>(), body).toRequestBody(jsonType))
			.build()
		return json.decodeFromString(call(req).second)
	}

	suspend fun health(): Health = get("/api/health")

	/** Returns null when unchanged since [etag] (304). */
	suspend fun export(etag: String): Pair<Export, String>? = withContext(Dispatchers.IO) {
		val req = Request.Builder().url(url("/api/export")).apply {
			if (etag.isNotEmpty()) header("If-None-Match", etag)
		}.build()
		http.newCall(req).execute().use { resp ->
			if (resp.code == 304) return@withContext null
			val body = resp.body?.string().orEmpty()
			if (!resp.isSuccessful) throw ApiException(resp.code, "HTTP ${resp.code}")
			json.decodeFromString<Export>(body) to resp.header("ETag").orEmpty()
		}
	}

	suspend fun search(
		q: String = "", category: String? = null, tags: List<String> = emptyList(),
		include: List<String> = emptyList(), exclude: List<String> = emptyList(), pantry: List<String> = emptyList(),
	): List<Summary> {
		val u = url("/api/recipes").toHttpUrl().newBuilder().apply {
			if (q.isNotBlank()) addQueryParameter("q", q)
			category?.let { addQueryParameter("category", it) }
			tags.forEach { addQueryParameter("tag", it) }
			if (include.isNotEmpty()) addQueryParameter("include", include.joinToString(","))
			if (exclude.isNotEmpty()) addQueryParameter("exclude", exclude.joinToString(","))
			if (pantry.isNotEmpty()) addQueryParameter("pantry", pantry.joinToString(","))
		}.build()
		return json.decodeFromString(call(Request.Builder().url(u).build()).second)
	}

	suspend fun recipe(id: String): Recipe = get("/api/recipes/$id")

	suspend fun create(r: Recipe): SaveResult = send("POST", "/api/recipes", r)

	suspend fun update(r: Recipe): SaveResult = send("PUT", "/api/recipes/${r.id}", r)

	suspend fun delete(id: String) {
		call(Request.Builder().url(url("/api/recipes/$id")).delete().build())
	}

	suspend fun upload(recipeId: String, jpeg: ByteArray): UploadResult {
		val body = MultipartBody.Builder().setType(MultipartBody.FORM)
			.addFormDataPart("file", "photo.jpg", jpeg.toRequestBody("image/jpeg".toMediaType()))
			.build()
		val req = Request.Builder().url(url("/api/recipes/$recipeId/media")).post(body).build()
		return json.decodeFromString(call(req).second)
	}

	/**
	 * Uploads a video straight from its content Uri, streamed: a 200 MB clip
	 * is never held in memory. The server stores it as-is (no re-encoding).
	 */
	suspend fun uploadVideo(recipeId: String, resolver: android.content.ContentResolver, uri: android.net.Uri): UploadResult {
		val stream = object : okhttp3.RequestBody() {
			override fun contentType() = "video/mp4".toMediaType()
			override fun writeTo(sink: okio.BufferedSink) {
				resolver.openInputStream(uri)!!.use { input ->
					val buf = ByteArray(64 * 1024)
					while (true) {
						val n = input.read(buf)
						if (n < 0) break
						sink.write(buf, 0, n)
					}
				}
			}
		}
		val body = MultipartBody.Builder().setType(MultipartBody.FORM)
			.addFormDataPart("file", "video.mp4", stream)
			.build()
		val req = Request.Builder().url(url("/api/recipes/$recipeId/media")).post(body).build()
		// Uploading minutes of video over mobile data outlasts the normal timeout.
		val slow = http.newBuilder().writeTimeout(15, TimeUnit.MINUTES).readTimeout(5, TimeUnit.MINUTES).build()
		return withContext(Dispatchers.IO) {
			slow.newCall(req).execute().use { resp ->
				val text = resp.body?.string().orEmpty()
				if (resp.code >= 400) {
					val err = runCatching { json.decodeFromString<ErrorBody>(text) }.getOrNull()
					throw ApiException(resp.code, err?.error ?: "HTTP ${resp.code}")
				}
				json.decodeFromString(text)
			}
		}
	}

	suspend fun translate(r: Recipe, from: String, to: String): TranslateResult =
		send("POST", "/api/translate", TranslateRequest(r, from, to))

	/** Translates the missing language on the server and saves it. */
	suspend fun translateAndSave(id: String, to: String): TranslateResult {
		val req = Request.Builder().url(url("/api/recipes/$id/translate?to=$to"))
			.post(ByteArray(0).toRequestBody(null)).build()
		return json.decodeFromString(call(req).second)
	}

	/** Imports a recipe from a web page (server fetches and parses it). */
	suspend fun importUrl(url: String): ImportResult = send("POST", "/api/import", ImportRequest(url))

	suspend fun feeds(): FeedStatus = get("/api/feeds")

	suspend fun runFeeds(): FeedStatus = send("POST", "/api/feeds/run", emptyMap<String, String>())

	suspend fun catalog(): List<CatalogEntry> = get("/api/ingredients")

	suspend fun grocery(): GroceryView = get("/api/grocery")

	suspend fun groceryOps(ops: List<GroceryOp>): GroceryView = send("POST", "/api/grocery", OpsRequest(ops))

	/** Downloads the recipe PDF into [dest]. */
	suspend fun pdf(id: String, lang: String, dest: File) = withContext(Dispatchers.IO) {
		val req = Request.Builder().url(url("/api/recipes/$id/pdf?lang=$lang")).build()
		http.newCall(req).execute().use { resp ->
			if (!resp.isSuccessful) {
				val err = runCatching { json.decodeFromString<ErrorBody>(resp.body?.string().orEmpty()) }.getOrNull()
				throw ApiException(resp.code, err?.error ?: "HTTP ${resp.code}")
			}
			dest.parentFile?.mkdirs()
			val tmp = File(dest.path + ".part")
			tmp.outputStream().use { out -> resp.body!!.byteStream().copyTo(out) }
			tmp.renameTo(dest)
		}
	}
}
