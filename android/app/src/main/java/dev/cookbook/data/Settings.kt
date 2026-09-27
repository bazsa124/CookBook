package dev.cookbook.data

import android.content.Context
import android.content.SharedPreferences
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * Server address and token live in encrypted prefs (same approach as Remoter);
 * preferences that are not secrets live in plain prefs and are observable.
 */
class Settings(context: Context) {

	private val secure: SharedPreferences = run {
		val key = MasterKey.Builder(context).setKeyScheme(MasterKey.KeyScheme.AES256_GCM).build()
		EncryptedSharedPreferences.create(
			context, "cookbook.secure", key,
			EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
			EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM,
		)
	}
	private val plain: SharedPreferences = context.getSharedPreferences("cookbook", Context.MODE_PRIVATE)

	var server: String
		get() = secure.getString("server", "").orEmpty()
		set(v) = secure.edit().putString("server", v.trim()).apply()

	var token: String
		get() = secure.getString("token", "").orEmpty()
		set(v) = secure.edit().putString("token", v.trim()).apply()

	val configured: Boolean get() = server.isNotEmpty() && token.isNotEmpty()

	/** http://host:8738, whatever the user typed ("100.1.2.3", "nas", "http://nas:8738/"). */
	fun baseUrl(): String {
		var h = server.trim().trimEnd('/')
		if (h.isEmpty()) return ""
		if (!h.startsWith("http://") && !h.startsWith("https://")) h = "http://$h"
		return if (h.substringAfter("://").contains(':')) h else "$h:$DEFAULT_PORT"
	}

	private val _lang = MutableStateFlow(plain.getString("lang", "hu") ?: "hu")
	/** Global content + UI language (the spec's HU/EN toggle). */
	val lang: StateFlow<String> = _lang.asStateFlow()
	fun setLang(v: String) {
		plain.edit().putString("lang", v).apply(); _lang.value = v
	}

	private val _imperial = MutableStateFlow(plain.getBoolean("imperial", false))
	val imperial: StateFlow<Boolean> = _imperial.asStateFlow()
	fun setImperial(v: Boolean) {
		plain.edit().putBoolean("imperial", v).apply(); _imperial.value = v
	}

	private val _favorites = MutableStateFlow(plain.getStringSet("favorites", emptySet())!!.toSet())
	/** Local bookmarks: per phone, never synced (spec: "local bookmarking"). */
	val favorites: StateFlow<Set<String>> = _favorites.asStateFlow()
	fun toggleFavorite(id: String) {
		val next = _favorites.value.toMutableSet().apply { if (!add(id)) remove(id) }
		plain.edit().putStringSet("favorites", next).apply()
		_favorites.value = next
	}

	var exportEtag: String
		get() = plain.getString("export_etag", "").orEmpty()
		set(v) = plain.edit().putString("export_etag", v).apply()

	var pendingGroceryOps: String
		get() = plain.getString("grocery_pending", "").orEmpty()
		set(v) = plain.edit().putString("grocery_pending", v).apply()

	companion object {
		const val DEFAULT_PORT = 8738
	}
}
