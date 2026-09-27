package dev.cookbook.ui

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Clear
import androidx.compose.material.icons.outlined.CloudDownload
import androidx.compose.material.icons.outlined.FilterList
import androidx.compose.material.icons.outlined.Kitchen
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavHostController
import dev.cookbook.data.Repository
import dev.cookbook.data.tr
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

private fun terms(s: String) = s.split(',').map { it.trim() }.filter { it.isNotEmpty() }

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ListScreen(nav: NavHostController) {
	val app = LocalApp.current
	val s = LocalStrings.current
	val lang by app.settings.lang.collectAsStateWithLifecycle()
	val export by app.repo.export.collectAsStateWithLifecycle()
	val favorites by app.settings.favorites.collectAsStateWithLifecycle()
	val scope = rememberCoroutineScope()

	var text by rememberSaveable { mutableStateOf("") }
	var category by rememberSaveable { mutableStateOf<String?>(null) }
	// Several can be on at once (AND): "Oven" + "#vegetarian".
	var tags by rememberSaveable { mutableStateOf(listOf<String>()) }
	fun toggleTag(t: String) { tags = if (t in tags) tags - t else tags + t }
	var favOnly by rememberSaveable { mutableStateOf(false) }
	var showFilters by rememberSaveable { mutableStateOf(false) }
	var include by rememberSaveable { mutableStateOf("") }
	var exclude by rememberSaveable { mutableStateOf("") }
	var pantry by rememberSaveable { mutableStateOf("") }
	var hits by remember { mutableStateOf<List<Repository.Hit>>(emptyList()) }
	var busy by remember { mutableStateOf(false) }
	var refreshing by remember { mutableStateOf(false) }

	val query = Repository.Query(
		text, category, tags.toSet(), terms(include), terms(exclude), terms(pantry), favOnly,
	)
	LaunchedEffect(query, export, favorites) {
		delay(250) // debounce typing
		busy = true
		hits = runCatching { app.repo.search(query) }.getOrElse { emptyList() }
		busy = false
	}

	val usedCategories = export.categories.recipe.filter { c -> export.recipes.any { it.category == c.id } }
	val methods = rememberMethods()
	val tagCounts = export.recipes.flatMap { it.tags }.groupingBy { it }.eachCount()
	// Methods in the table's order (oven, pot, pan, …), only those in use.
	val usedMethods = export.categories.method.filter { (tagCounts[it.id] ?: 0) > 0 }
	val topTags = tagCounts.entries.filter { it.key !in methods }
		.sortedByDescending { it.value }.take(12).map { it.key }

	Scaffold(
		topBar = {
			TopAppBar(
				title = { Text(s.recipes) },
				actions = {
					LangToggle()
					IconButton(onClick = { app.pendingImport.value = "" }) {
						Icon(Icons.Outlined.CloudDownload, s.importWeb)
					}
					IconButton(onClick = { nav.navigate("edit/new") }) { Icon(Icons.Outlined.Add, s.newRecipe) }
				},
			)
		},
	) { pad ->
		Column(Modifier.padding(pad)) {
			OfflineBanner()
			OutlinedTextField(
				value = text, onValueChange = { text = it },
				placeholder = { Text(s.search) },
				leadingIcon = { Icon(Icons.Outlined.Search, null) },
				trailingIcon = {
					Row {
						if (text.isNotEmpty()) IconButton(onClick = { text = "" }) { Icon(Icons.Outlined.Clear, null) }
						IconButton(onClick = { showFilters = !showFilters }) {
							Icon(Icons.Outlined.FilterList, s.filters,
								tint = if (include + exclude + pantry != "") MaterialTheme.colorScheme.primary
								else MaterialTheme.colorScheme.onSurfaceVariant)
						}
					}
				},
				singleLine = true,
				shape = RoundedCornerShape(28.dp),
				modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp),
			)
			AnimatedVisibility(showFilters) {
				Column(Modifier.padding(horizontal = 16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
					OutlinedTextField(pantry, { pantry = it }, label = { Text(s.pantry) }, placeholder = { Text(s.pantryHint) },
						leadingIcon = { Icon(Icons.Outlined.Kitchen, null) }, singleLine = true, modifier = Modifier.fillMaxWidth())
					Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
						OutlinedTextField(include, { include = it }, label = { Text(s.include) },
							placeholder = { Text(s.ingredientsHint) }, singleLine = true, modifier = Modifier.weight(1f))
						OutlinedTextField(exclude, { exclude = it }, label = { Text(s.exclude) },
							placeholder = { Text(s.ingredientsHint) }, singleLine = true, modifier = Modifier.weight(1f))
					}
				}
			}
			CategoryChips(usedCategories, category, lang) { category = it }
			// Tags (and favorites) on their own row: a different kind of filter.
			LazyRow(
				contentPadding = PaddingValues(horizontal = 16.dp),
				horizontalArrangement = Arrangement.spacedBy(8.dp),
			) {
				item {
					FilterChip(favOnly, { favOnly = !favOnly }, label = { Text(s.favorites) },
						leadingIcon = { Icon(Icons.Filled.Favorite, null, Modifier.size(16.dp)) })
				}
				items(usedMethods, key = { "m" + it.id }) { m ->
					FilterChip(m.id in tags, { toggleTag(m.id) }, label = { Text(m.label.tr(lang)) })
				}
				items(topTags, key = { "t$it" }) { t ->
					FilterChip(t in tags, { toggleTag(t) }, label = { Text("#$t") })
				}
			}
			if (busy) LinearProgressIndicator(Modifier.fillMaxWidth()) else Spacer(Modifier.height(4.dp))

			PullToRefreshBox(
				isRefreshing = refreshing,
				onRefresh = {
					scope.launch {
						refreshing = true
						app.repo.refresh()
						refreshing = false
					}
				},
				modifier = Modifier.fillMaxSize(),
			) {
				if (!app.settings.configured) {
					Text(s.setupFirst, Modifier.padding(24.dp))
				} else if (hits.isEmpty() && !busy) {
					Text(s.noResults, Modifier.padding(24.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
				}
				LazyColumn(
					contentPadding = PaddingValues(16.dp),
					verticalArrangement = Arrangement.spacedBy(12.dp),
					modifier = Modifier.fillMaxSize(),
				) {
					items(hits, key = { it.recipe.id }) { hit ->
						RecipeRow(hit, lang, hit.recipe.id in favorites) { nav.navigate("recipe/${hit.recipe.id}") }
					}
				}
			}
		}
	}
}

@Composable
private fun RecipeRow(hit: Repository.Hit, lang: String, favorite: Boolean, onClick: () -> Unit) {
	val s = LocalStrings.current
	val r = hit.recipe
	Card(Modifier.fillMaxWidth().clickable(onClick = onClick)) {
		Row(verticalAlignment = Alignment.CenterVertically) {
			RecipeImage(r.id, r.hero, Modifier.size(96.dp).clip(RoundedCornerShape(12.dp)))
			Column(Modifier.padding(horizontal = 12.dp, vertical = 8.dp).weight(1f)) {
				Row(verticalAlignment = Alignment.CenterVertically) {
					Text(r.title.tr(lang), style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold,
						maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
					if (favorite) {
						Spacer(Modifier.width(4.dp))
						Icon(Icons.Filled.Favorite, null, Modifier.size(16.dp), tint = MaterialTheme.colorScheme.primary)
					}
				}
				val meta = buildList {
					val total = r.prepMinutes + r.cookMinutes
					if (total > 0) add("$total ${s.min}")
					add("${formatServings(r.baseServings, lang)} ${s.servings}")
					if (!r.title.containsKey(lang)) add("(${r.title.keys.joinToString("/") { it.uppercase() }})")
				}
				Text(meta.joinToString(" · "), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
				if (r.tags.isNotEmpty()) {
					Text(tagLine(r.tags, rememberMethods(), lang), style = MaterialTheme.typography.labelSmall,
						color = MaterialTheme.colorScheme.secondary, maxLines = 1, overflow = TextOverflow.Ellipsis)
				}
				hit.match?.let { m ->
					Spacer(Modifier.height(4.dp))
					LinearProgressIndicator(progress = { m.coverage.toFloat() }, modifier = Modifier.fillMaxWidth())
					Text(
						if (m.missing.isEmpty()) s.canCook
						else "${m.have}/${m.need} · ${s.missing}: " + m.missing.joinToString(", ") { it.tr(lang) },
						style = MaterialTheme.typography.labelSmall, maxLines = 2, overflow = TextOverflow.Ellipsis,
					)
				}
			}
		}
	}
}

/**
 * Single-choice category row ("All" + each category), shared by the recipe
 * list and Discover. Selected chips are filled so the row reads as one control.
 */
@Composable
fun CategoryChips(
	categories: List<dev.cookbook.data.Category>, selected: String?, lang: String, onSelect: (String?) -> Unit,
) {
	val s = LocalStrings.current
	LazyRow(
		contentPadding = PaddingValues(horizontal = 16.dp),
		horizontalArrangement = Arrangement.spacedBy(8.dp),
	) {
		item(key = "all") {
			androidx.compose.material3.FilterChip(
				selected == null, { onSelect(null) }, label = { Text(s.allCategories) },
				colors = categoryChipColors(),
			)
		}
		items(categories, key = { "c" + it.id }) { c ->
			androidx.compose.material3.FilterChip(
				selected == c.id, { onSelect(if (selected == c.id) null else c.id) }, label = { Text(c.label.tr(lang)) },
				colors = categoryChipColors(),
			)
		}
	}
}

@Composable
private fun categoryChipColors() = androidx.compose.material3.FilterChipDefaults.filterChipColors(
	selectedContainerColor = MaterialTheme.colorScheme.primary,
	selectedLabelColor = MaterialTheme.colorScheme.onPrimary,
)

fun formatServings(v: Double, lang: String): String = dev.cookbook.logic.formatQty(v, lang, true)
