package dev.cookbook.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.outlined.AddShoppingCart
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.FavoriteBorder
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.PictureAsPdf
import androidx.compose.material.icons.outlined.PlayArrow
import androidx.compose.material.icons.outlined.Remove
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExtendedFloatingActionButton
import androidx.compose.material3.FilledTonalIconButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.LinkAnnotation
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.TextLinkStyles
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.withLink
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavHostController
import dev.cookbook.data.GroceryOp
import dev.cookbook.data.Ingredient
import dev.cookbook.data.Recipe
import dev.cookbook.data.has
import dev.cookbook.data.tr
import dev.cookbook.logic.Kitchen
import dev.cookbook.logic.Segment
import dev.cookbook.logic.segments
import kotlinx.coroutines.launch

/** Kitchen for [r] at [servings] with the current language and unit system. */
@Composable
fun rememberKitchen(r: Recipe, servings: Double): Kitchen {
	val app = LocalApp.current
	val lang by app.settings.lang.collectAsStateWithLifecycle()
	val imperial by app.settings.imperial.collectAsStateWithLifecycle()
	val export by app.repo.export.collectAsStateWithLifecycle()
	return remember(r, servings, lang, imperial, export.units) {
		Kitchen(export.units.associateBy { it.id }, lang, imperial, servings / r.baseServings.coerceAtLeast(0.01))
	}
}

/** Step text with its [MM:SS] timers as tappable chips. */
@Composable
fun StepText(text: String, recipeTitle: String, style: TextStyle, modifier: Modifier = Modifier) {
	val app = LocalApp.current
	val chip = SpanStyle(
		color = MaterialTheme.colorScheme.onPrimaryContainer,
		background = MaterialTheme.colorScheme.primaryContainer,
		fontWeight = FontWeight.Bold,
	)
	val annotated: AnnotatedString = buildAnnotatedString {
		for (seg in segments(text)) when (seg) {
			is Segment.Text -> append(seg.text)
			is Segment.Timer -> withLink(
				LinkAnnotation.Clickable("timer", TextLinkStyles(chip)) {
					app.timers.start(seg.label, recipeTitle, seg.seconds)
				},
			) { append(" ⏱ ${seg.label} ") }
		}
	}
	Text(annotated, style = style, modifier = modifier)
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DetailScreen(nav: NavHostController, id: String) {
	val app = LocalApp.current
	val s = LocalStrings.current
	val snackbar = LocalSnackbar.current
	val scope = rememberCoroutineScope()
	val export by app.repo.export.collectAsStateWithLifecycle()
	val lang by app.settings.lang.collectAsStateWithLifecycle()
	val imperial by app.settings.imperial.collectAsStateWithLifecycle()
	val favorites by app.settings.favorites.collectAsStateWithLifecycle()
	val r = export.recipes.firstOrNull { it.id == id }
	if (r == null) {
		Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { Text(s.loading) }
		return
	}
	var servings by rememberSaveable(id) { mutableStateOf(r.baseServings) }
	val k = rememberKitchen(r, servings)
	val struck = remember(id) { mutableStateListOf<String>() }
	var menu by remember { mutableStateOf(false) }
	var confirmDelete by remember { mutableStateOf(false) }
	val title = r.title.tr(lang)

	Scaffold(
		topBar = {
			TopAppBar(
				title = { Text(title, maxLines = 1) },
				navigationIcon = { IconButton(onClick = { nav.popBackStack() }) { Icon(Icons.AutoMirrored.Outlined.ArrowBack, null) } },
				actions = {
					LangToggle()
					IconButton(onClick = { app.settings.toggleFavorite(id) }) {
						if (id in favorites) Icon(Icons.Filled.Favorite, s.favorites, tint = MaterialTheme.colorScheme.primary)
						else Icon(Icons.Outlined.FavoriteBorder, s.favorites)
					}
					IconButton(onClick = { menu = true }) { Icon(Icons.Outlined.MoreVert, null) }
					DropdownMenu(menu, { menu = false }) {
						DropdownMenuItem({ Text(s.edit) }, leadingIcon = { Icon(Icons.Outlined.Edit, null) },
							onClick = { menu = false; nav.navigate("edit/$id") })
						DropdownMenuItem({ Text(s.pdf) }, leadingIcon = { Icon(Icons.Outlined.PictureAsPdf, null) },
							onClick = { menu = false; nav.navigate("pdf/$id") })
						DropdownMenuItem({ Text(s.delete) }, leadingIcon = { Icon(Icons.Outlined.Delete, null) },
							onClick = { menu = false; confirmDelete = true })
					}
				},
			)
		},
		floatingActionButton = {
			ExtendedFloatingActionButton(
				onClick = { nav.navigate("cook/$id?servings=${servings.toFloat()}") },
				icon = { Icon(Icons.Outlined.PlayArrow, null) },
				text = { Text(s.cookMode) },
			)
		},
	) { pad ->
		LazyColumn(Modifier.padding(pad).fillMaxSize()) {
			item {
				RecipeImage(r.id, r.hero, Modifier.fillMaxWidth().aspectRatio(16f / 10f))
				if (!r.title.has(lang)) TranslateBanner(id, lang)
				Column(Modifier.padding(16.dp)) {
					Text(title, style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.Bold)
					r.description.tr(lang).takeIf { it.isNotEmpty() }?.let {
						Text(it, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
					}
					val times = buildList {
						if (r.prepMinutes > 0) add("${s.prep}: ${r.prepMinutes} ${s.min}")
						if (r.cookMinutes > 0) add("${s.cook}: ${r.cookMinutes} ${s.min}")
					}
					if (times.isNotEmpty()) Text(times.joinToString(" · "), style = MaterialTheme.typography.labelLarge)
					r.videoUrl?.let { VideoLinkChip(it) }
					if (r.tags.isNotEmpty()) Text(tagLine(r.tags, rememberMethods(), lang),
						style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.secondary)
					Spacer(Modifier.height(12.dp))
					Row(verticalAlignment = Alignment.CenterVertically) {
						// Dynamic yield scaling (spec C.1)
						FilledTonalIconButton(onClick = { if (servings > 1) servings -= 1 else if (servings > 0.5) servings = 0.5 }) {
							Icon(Icons.Outlined.Remove, null)
						}
						Text("${formatServings(servings, lang)} ${s.servings}", Modifier.widthIn(min = 88.dp).padding(horizontal = 8.dp),
							style = MaterialTheme.typography.titleMedium)
						FilledTonalIconButton(onClick = { servings = if (servings < 1) 1.0 else servings + 1 }) {
							Icon(Icons.Outlined.Add, null)
						}
						Spacer(Modifier.weight(1f))
						// Unit localisation (spec C.2)
						SingleChoiceSegmentedButtonRow {
							SegmentedButton(!imperial, { app.settings.setImperial(false) }, SegmentedButtonDefaults.itemShape(0, 2)) { Text("g/ml") }
							SegmentedButton(imperial, { app.settings.setImperial(true) }, SegmentedButtonDefaults.itemShape(1, 2)) { Text("oz/cup") }
						}
					}
					Spacer(Modifier.height(8.dp))
					OutlinedButton(onClick = {
						scope.launch {
							val err = app.repo.grocery(listOf(GroceryOp("add_recipe", id = id, servings = servings)))
							snackbar.showSnackbar(err ?: s.added)
						}
					}) {
						Icon(Icons.Outlined.AddShoppingCart, null)
						Spacer(Modifier.width(8.dp))
						Text(s.addToList)
					}
				}
				Text(s.ingredients, Modifier.padding(horizontal = 16.dp), style = MaterialTheme.typography.titleLarge,
					color = MaterialTheme.colorScheme.primary)
			}
			ingredientItems(r.ingredients, k, lang, struck)
			item {
				Spacer(Modifier.height(16.dp))
				Text(s.method, Modifier.padding(horizontal = 16.dp), style = MaterialTheme.typography.titleLarge,
					color = MaterialTheme.colorScheme.primary)
			}
			itemsIndexed(r.steps) { i, st ->
				Row(Modifier.padding(horizontal = 16.dp, vertical = 6.dp)) {
					Text("${i + 1}.", Modifier.width(28.dp), fontWeight = FontWeight.Bold, color = MaterialTheme.colorScheme.primary)
					Column(Modifier.weight(1f)) {
						StepText(k.resolve(st.text.tr(lang), r.ingredients), title, MaterialTheme.typography.bodyLarge)
						st.media?.let {
							Spacer(Modifier.height(6.dp))
							StepMedia(r.id, it, Modifier.fillMaxWidth(0.8f).aspectRatio(4f / 3f).clip(RoundedCornerShape(8.dp)))
						}
					}
				}
			}
			item {
				r.notes.tr(lang).takeIf { it.isNotEmpty() }?.let {
					Column(Modifier.padding(16.dp)) {
						Text(s.notes, style = MaterialTheme.typography.titleMedium, color = MaterialTheme.colorScheme.primary)
						Text(it, style = MaterialTheme.typography.bodyMedium)
					}
				}
				Spacer(Modifier.height(96.dp)) // clear the FAB
			}
		}
	}

	if (confirmDelete) {
		AlertDialog(
			onDismissRequest = { confirmDelete = false },
			text = { Text(s.deleteConfirm) },
			confirmButton = {
				TextButton(onClick = {
					confirmDelete = false
					scope.launch {
						runCatching { app.api.delete(id) }
							.onSuccess { app.repo.removeLocal(id); nav.popBackStack() }
							.onFailure { snackbar.showSnackbar(it.message ?: "error") }
					}
				}) { Text(s.delete) }
			},
			dismissButton = { TextButton(onClick = { confirmDelete = false }) { Text(s.cancel) } },
		)
	}
}

/**
 * Shown when the recipe has no text in the selected language: the toggle can
 * only show what exists, so offer to create it (translated and saved on the server).
 */
@Composable
private fun TranslateBanner(id: String, lang: String) {
	val app = LocalApp.current
	val s = LocalStrings.current
	val snackbar = LocalSnackbar.current
	val scope = rememberCoroutineScope()
	var busy by remember(id, lang) { mutableStateOf(false) }
	androidx.compose.material3.Surface(color = MaterialTheme.colorScheme.secondaryContainer, modifier = Modifier.fillMaxWidth()) {
		Row(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
			Text(s.notTranslated, Modifier.weight(1f), style = MaterialTheme.typography.bodyMedium)
			if (busy) androidx.compose.material3.CircularProgressIndicator(Modifier.size(24.dp), strokeWidth = 2.dp)
			else androidx.compose.material3.FilledTonalButton(onClick = {
				scope.launch {
					busy = true
					runCatching { app.api.translateAndSave(id, lang) }
						.onSuccess {
							app.repo.putLocal(it.recipe)
							snackbar.showSnackbar(s.translated(it.filled, it.rejected.size))
						}
						.onFailure { snackbar.showSnackbar(it.message ?: "error") }
					busy = false
				}
			}) { Text(s.translateNow) }
		}
	}
}

/** Ingredient rows, grouped; tapping one strikes it through (mise en place). */
fun androidx.compose.foundation.lazy.LazyListScope.ingredientItems(
	ingredients: List<Ingredient>, k: Kitchen, lang: String, struck: MutableList<String>,
) {
	var lastGroup = ""
	ingredients.forEach { ing ->
		val g = ing.group.tr(lang)
		if (g.isNotEmpty() && g != lastGroup) {
			item(key = "g-${ing.id}") {
				Text(g.uppercase(), Modifier.padding(start = 16.dp, top = 12.dp, bottom = 2.dp),
					style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.secondary)
			}
		}
		lastGroup = g
		item(key = ing.id) {
			val done = ing.id in struck
			val shown = k.show(ing)
			val deco = if (done) TextDecoration.LineThrough else null
			val alpha = if (done) 0.45f else 1f
			Row(
				Modifier.fillMaxWidth().clickable { if (!struck.remove(ing.id)) struck.add(ing.id) }
					.padding(horizontal = 16.dp, vertical = 6.dp),
			) {
				Text(k.amount(ing), Modifier.width(96.dp), fontWeight = FontWeight.Bold, textDecoration = deco,
					color = MaterialTheme.colorScheme.onSurface.copy(alpha = alpha))
				Column(Modifier.weight(1f)) {
					Text(shown.name, textDecoration = deco, color = MaterialTheme.colorScheme.onSurface.copy(alpha = alpha))
					val extra = listOfNotNull(shown.note.takeIf { it.isNotEmpty() },
						if (ing.optional) LocalStrings.current.optional else null)
					if (extra.isNotEmpty()) Text(extra.joinToString(", "), style = MaterialTheme.typography.bodySmall,
						color = MaterialTheme.colorScheme.onSurfaceVariant)
				}
			}
			HorizontalDivider(Modifier.padding(horizontal = 16.dp), color = MaterialTheme.colorScheme.surfaceVariant)
		}
	}
}
