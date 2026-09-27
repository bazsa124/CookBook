package dev.cookbook.ui

import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.cookbook.data.Category
import dev.cookbook.data.tr

/**
 * Cooking methods are tags with fixed ids ("oven", "no-bake"); the server
 * supplies their labels. They are shown by name, other tags as #tag.
 */
@Composable
fun rememberMethods(): Map<String, Category> {
	val export by LocalApp.current.repo.export.collectAsStateWithLifecycle()
	return export.categories.method.associateBy { it.id }
}

fun tagLabel(tag: String, methods: Map<String, Category>, lang: String): String =
	methods[tag]?.label?.tr(lang) ?: "#$tag"

/** "Sütő · Hűtős   #vegetarian #quick": methods first, by name. */
fun tagLine(tags: List<String>, methods: Map<String, Category>, lang: String): String {
	val (m, other) = tags.partition { it in methods }
	return listOf(
		m.joinToString(" · ") { tagLabel(it, methods, lang) },
		other.joinToString(" ") { "#$it" },
	).filter { it.isNotEmpty() }.joinToString("   ")
}
