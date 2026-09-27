package dev.cookbook.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.CloudUpload
import androidx.compose.material.icons.outlined.DeleteSweep
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material3.Checkbox
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.InputChip
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
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavHostController
import dev.cookbook.data.GroceryLine
import dev.cookbook.data.GroceryOp
import dev.cookbook.data.Ingredient
import dev.cookbook.data.tr
import dev.cookbook.logic.Kitchen
import kotlinx.coroutines.launch

/** The grocery system (spec D): aggregated recipe lines + the static household list. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun GroceryScreen(nav: NavHostController) {
	val app = LocalApp.current
	val s = LocalStrings.current
	val snackbar = LocalSnackbar.current
	val scope = rememberCoroutineScope()
	val state by app.repo.grocery.collectAsStateWithLifecycle()
	val pending by app.repo.pendingOps.collectAsStateWithLifecycle()
	val lang by app.settings.lang.collectAsStateWithLifecycle()
	val imperial by app.settings.imperial.collectAsStateWithLifecycle()
	val export by app.repo.export.collectAsStateWithLifecycle()
	// Computed on the phone from the cached recipes, so the list works offline.
	val view = remember(state, export) {
		dev.cookbook.logic.Grocery.aggregate(
			state, export.recipes.associateBy { it.id }, export.units.associateBy { it.id },
			export.categories.aisle, app.repo.guesser(),
		)
	}
	var newItem by remember { mutableStateOf("") }
	var menu by remember { mutableStateOf(false) }
	var refreshing by remember { mutableStateOf(false) }
	// Display-only kitchen: localised unit labels and unit-system conversion.
	val k = remember(lang, imperial, export.units) { Kitchen(export.units.associateBy { it.id }, lang, imperial, 1.0) }

	fun send(vararg ops: GroceryOp) = scope.launch {
		app.repo.grocery(ops.toList())?.let { snackbar.showSnackbar(it) }
	}

	LaunchedEffect(Unit) { app.repo.grocery() }

	Scaffold(
		topBar = {
			TopAppBar(
				title = { Text(s.grocery) },
				actions = {
					LangToggle()
					IconButton(onClick = { menu = true }) { Icon(Icons.Outlined.MoreVert, null) }
					DropdownMenu(menu, { menu = false }) {
						DropdownMenuItem({ Text(s.clearDone) }, leadingIcon = { Icon(Icons.Outlined.DeleteSweep, null) },
							onClick = { menu = false; send(GroceryOp("clear_done")) })
						DropdownMenuItem({ Text(s.clearAll) }, leadingIcon = { Icon(Icons.Outlined.Close, null) },
							onClick = { menu = false; send(GroceryOp("clear_all")) })
					}
				},
			)
		},
	) { pad ->
		Column(Modifier.padding(pad).fillMaxSize()) {
			OfflineBanner()
			if (pending > 0) {
				Row(Modifier.padding(horizontal = 16.dp, vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
					Icon(Icons.Outlined.CloudUpload, null)
					Spacer(Modifier.width(8.dp))
					Text("$pending ${s.queued}", style = MaterialTheme.typography.labelMedium)
				}
			}
			// Static list integration (spec D.3): ad-hoc household items.
			OutlinedTextField(
				value = newItem, onValueChange = { newItem = it },
				placeholder = { Text(s.addItem) },
				singleLine = true,
				keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
				keyboardActions = KeyboardActions(onDone = {
					if (newItem.isNotBlank()) { send(GroceryOp("add_static", id = app.repo.newStaticId(), text = newItem.trim())); newItem = "" }
				}),
				trailingIcon = {
					IconButton(onClick = {
						if (newItem.isNotBlank()) { send(GroceryOp("add_static", id = app.repo.newStaticId(), text = newItem.trim())); newItem = "" }
					}) { Icon(Icons.Outlined.Add, null) }
				},
				modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp),
			)
			PullToRefreshBox(
				isRefreshing = refreshing,
				onRefresh = { scope.launch { refreshing = true; app.repo.refresh(); app.repo.grocery(); refreshing = false } },
				modifier = Modifier.fillMaxSize(),
			) {
				LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
					if (view.recipes.isNotEmpty()) {
						item {
							Text(s.fromRecipes, Modifier.padding(start = 16.dp, top = 8.dp), style = MaterialTheme.typography.labelLarge)
							Row(
								Modifier.fillMaxWidth().padding(horizontal = 16.dp),
								horizontalArrangement = Arrangement.spacedBy(8.dp),
							) {
								Column(verticalArrangement = Arrangement.spacedBy(0.dp)) {
									view.recipes.forEach { c ->
										InputChip(
											selected = false,
											onClick = { if (!c.missing) nav.navigate("recipe/${c.id}") },
											label = { Text("${c.title.tr(lang).ifEmpty { c.id }} · ${formatServings(c.servings, lang)} ${s.servings}") },
											trailingIcon = {
												Icon(Icons.Outlined.Close, s.remove,
													Modifier.clickable { send(GroceryOp("remove_recipe", id = c.id)) })
											},
										)
									}
								}
							}
						}
					}
					if (view.sections.isEmpty()) {
						item { Text(s.listEmpty, Modifier.padding(24.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
					}
					view.sections.forEach { sec ->
						item(key = "h-${sec.category}") {
							Text(sec.label.tr(lang), Modifier.padding(start = 16.dp, top = 16.dp, bottom = 4.dp),
								style = MaterialTheme.typography.titleMedium, color = MaterialTheme.colorScheme.primary)
						}
						items(sec.lines.sortedBy { it.checked }, key = { it.key }) { line ->
							CheckRow(line.checked, lineText(line, k), line.sources.size.takeIf { it > 1 }?.let { "×$it" }) {
								send(GroceryOp("check", key = line.key, checked = !line.checked))
							}
						}
						items(sec.static.sortedBy { it.checked }, key = { "static:" + it.id }) { item ->
							CheckRow(item.checked, item.text, null,
								onRemove = { send(GroceryOp("remove_static", id = item.id)) }) {
								send(GroceryOp("check", key = "static:${item.id}", checked = !item.checked))
							}
						}
					}
				}
			}
		}
	}
}

private fun lineText(l: GroceryLine, k: Kitchen): String {
	val amount = k.amount(Ingredient(id = l.ingredientId, qty = l.qty, unit = l.unit, unitId = l.unitId, name = l.name))
	return listOf(amount, l.name.tr(k.lang)).filter { it.isNotEmpty() }.joinToString(" ")
}

@Composable
private fun CheckRow(checked: Boolean, text: String, badge: String?, onRemove: (() -> Unit)? = null, onToggle: () -> Unit) {
	Row(
		Modifier.fillMaxWidth().clickable(onClick = onToggle).padding(horizontal = 8.dp),
		verticalAlignment = Alignment.CenterVertically,
	) {
		Checkbox(checked, { onToggle() })
		Text(
			text, Modifier.weight(1f),
			textDecoration = if (checked) TextDecoration.LineThrough else null,
			color = if (checked) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.onSurface,
			fontWeight = if (checked) FontWeight.Normal else FontWeight.Medium,
		)
		badge?.let { Text(it, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.secondary) }
		onRemove?.let { IconButton(onClick = it) { Icon(Icons.Outlined.Close, null) } }
	}
}
