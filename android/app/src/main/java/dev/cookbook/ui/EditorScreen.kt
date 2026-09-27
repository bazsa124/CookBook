package dev.cookbook.ui

import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
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
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.outlined.AddPhotoAlternate
import androidx.compose.material.icons.outlined.ArrowDownward
import androidx.compose.material.icons.outlined.ArrowUpward
import androidx.compose.material.icons.outlined.Check
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material.icons.outlined.HideImage
import androidx.compose.material.icons.outlined.Link
import androidx.compose.material.icons.outlined.Timer
import androidx.compose.material.icons.outlined.Translate
import androidx.compose.material.icons.outlined.Videocam
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Checkbox
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExposedDropdownMenuBox
import androidx.compose.material3.ExposedDropdownMenuDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.MenuAnchorType
import androidx.compose.material3.OutlinedCard
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavHostController
import dev.cookbook.data.ApiException
import dev.cookbook.data.CatalogEntry
import dev.cookbook.data.Conflict
import dev.cookbook.data.Recipe
import dev.cookbook.data.tr
import dev.cookbook.logic.fold
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** The editor (spec A): a structured form that hides meta.json and Typst entirely. */
@OptIn(ExperimentalMaterial3Api::class, androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
@Composable
fun EditorScreen(nav: NavHostController, id: String) {
	val app = LocalApp.current
	val s = LocalStrings.current
	val snackbar = LocalSnackbar.current
	val context = LocalContext.current
	val scope = rememberCoroutineScope()
	val globalLang by app.settings.lang.collectAsStateWithLifecycle()
	val export by app.repo.export.collectAsStateWithLifecycle()
	val isNew = id == "new"
	val st = remember(id) {
		EditorState(if (isNew) null else app.repo.recipe(id), globalLang, export.categories.method.map { it.id }.toSet())
	}
	var catalog by remember { mutableStateOf<List<CatalogEntry>>(emptyList()) }
	var busy by remember { mutableStateOf<String?>(null) }
	var errors by remember { mutableStateOf<List<String>>(emptyList()) }
	var conflict by remember { mutableStateOf<Recipe?>(null) }
	var confirmLeave by remember { mutableStateOf(false) }
	var timerFor by remember { mutableStateOf<EditStep?>(null) }
	var photoTarget by remember { mutableStateOf<(String) -> Unit>({}) }
	val lang = st.lang
	val other = if (lang == "hu") "en" else "hu"

	LaunchedEffect(Unit) { catalog = app.repo.catalog() }

	val picker = rememberLauncherForActivityResult(ActivityResultContracts.PickVisualMedia()) { uri ->
		if (uri != null) scope.launch {
			runCatching { withContext(Dispatchers.IO) { loadPhoto(context.contentResolver, uri) } }
				.onSuccess { photoTarget(st.addPhoto(it)) }
				.onFailure { snackbar.showSnackbar(it.message ?: "photo") }
		}
	}
	var videoTarget by remember { mutableStateOf<(String) -> Unit>({}) }
	val videoPicker = rememberLauncherForActivityResult(ActivityResultContracts.PickVisualMedia()) { uri ->
		if (uri == null) return@rememberLauncherForActivityResult
		val size = runCatching {
			context.contentResolver.query(uri, arrayOf(android.provider.OpenableColumns.SIZE), null, null, null)?.use {
				if (it.moveToFirst()) it.getLong(0) else 0L
			} ?: 0L
		}.getOrDefault(0L)
		if (size > 300L * 1024 * 1024) scope.launch { snackbar.showSnackbar(s.videoTooLarge) }
		else videoTarget(st.addVideo(uri))
	}
	fun pickVideo(onKey: (String) -> Unit) {
		videoTarget = onKey
		videoPicker.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.VideoOnly))
	}

	fun pickPhoto(onKey: (String) -> Unit) {
		photoTarget = onKey
		picker.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly))
	}

	BackHandler(enabled = st.dirty) { confirmLeave = true }

	/** Pipeline 1 from the phone: create if new, upload photos, save. */
	suspend fun save(overwriteRev: Int? = null) {
		busy = s.save
		try {
			overwriteRev?.let { st.rev = it }
			var draft = st.build()
			var recipeId = draft.id
			if (recipeId.isEmpty()) {
				val created = app.api.create(draft.copy(hero = null, steps = draft.steps.map { it.copy(media = null) }))
				recipeId = created.recipe.id
				draft = draft.copy(id = recipeId, rev = created.recipe.rev, created = created.recipe.created)
				st.rev = created.recipe.rev
				st.savedId = recipeId
				app.repo.putLocal(created.recipe)
			}
			// Upload each local photo that is still referenced.
			val names = mutableMapOf<String, String>()
			val used = listOfNotNull(draft.hero) + draft.steps.mapNotNull { it.media }
			for (key in used.filter { it.startsWith("local:") }.distinct()) {
				st.videos[key]?.let { uri ->
					busy = s.uploadingVideo
					names[key] = app.api.uploadVideo(recipeId, context.contentResolver, uri).file
					busy = s.save
				}
				val photo = st.photos[key] ?: continue
				names[key] = app.api.upload(recipeId, photo.jpeg).file
			}
			fun resolve(ref: String?) = ref?.let { names[it] ?: it.takeUnless { r -> r.startsWith("local:") } }
			draft = draft.copy(hero = resolve(draft.hero), steps = draft.steps.map { it.copy(media = resolve(it.media)) })
			val saved = app.api.update(draft)
			app.repo.putLocal(saved.recipe)
			st.dirty = false
			val pdfErr = saved.pdf.entries.firstOrNull { it.value != "ok" }
			snackbar.showSnackbar(if (pdfErr == null) s.saved else "${s.saved} · PDF: ${pdfErr.value.take(80)}")
			nav.popBackStack()
			if (isNew) nav.navigate("recipe/$recipeId")
		} catch (e: ApiException) {
			when (e.status) {
				409 -> conflict = runCatching { app.api.json.decodeFromString<Conflict>(e.body).current }.getOrNull()
				400 -> errors = e.details.ifEmpty { listOf(e.message ?: s.invalid) }
				else -> snackbar.showSnackbar(e.message ?: "error")
			}
		} catch (e: Exception) {
			snackbar.showSnackbar(e.message ?: "error")
		} finally {
			busy = null
		}
	}

	Scaffold(
		topBar = {
			TopAppBar(
				title = { Text(if (isNew) s.newRecipe else s.edit) },
				navigationIcon = {
					IconButton(onClick = { if (st.dirty) confirmLeave = true else nav.popBackStack() }) {
						Icon(Icons.AutoMirrored.Outlined.ArrowBack, null)
					}
				},
				actions = {
					// AI auto-translate (spec A.5 / Pipeline 4)
					IconButton(enabled = busy == null, onClick = {
						scope.launch {
							busy = s.translating
							runCatching { app.api.translate(st.build(), lang, other) }
								.onSuccess {
									st.applyTranslation(it.recipe)
									st.switchLang(other)
									snackbar.showSnackbar(s.translated(it.filled, it.rejected.size))
								}
								.onFailure { snackbar.showSnackbar(it.message ?: "error") }
							busy = null
						}
					}) { Icon(Icons.Outlined.Translate, s.autoTranslate) }
					IconButton(enabled = busy == null, onClick = { scope.launch { save() } }) {
						Icon(Icons.Outlined.Check, s.save)
					}
				},
			)
		},
	) { pad ->
		Column(Modifier.padding(pad).fillMaxSize()) {
			busy?.let {
				LinearProgressIndicator(Modifier.fillMaxWidth())
				Text(it, Modifier.padding(horizontal = 16.dp), style = MaterialTheme.typography.labelSmall)
			}
			LazyColumn(
				Modifier.fillMaxSize(),
				contentPadding = androidx.compose.foundation.layout.PaddingValues(16.dp),
				verticalArrangement = Arrangement.spacedBy(10.dp),
			) {
				item {
					Row(verticalAlignment = Alignment.CenterVertically) {
						Text(s.editing, Modifier.weight(1f), style = MaterialTheme.typography.labelLarge)
						SingleChoiceSegmentedButtonRow {
							SegmentedButton(lang == "hu", { st.switchLang("hu") }, SegmentedButtonDefaults.itemShape(0, 2)) { Text("HU") }
							SegmentedButton(lang == "en", { st.switchLang("en") }, SegmentedButtonDefaults.itemShape(1, 2)) { Text("EN") }
						}
					}
				}
				item {
					PhotoBox(st, st.hero, isNew, id, Modifier.fillMaxWidth().aspectRatio(16f / 9f),
						label = s.heroPhoto,
						onPick = { pickPhoto { key -> st.hero = key } },
						onClear = { st.hero = null; st.dirty = true })
				}
				item {
					Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
						OutlinedTextField(st.title[lang].orEmpty(), { st.title = st.setText(st.title, it) },
							label = { Text("${s.title} ($lang)") }, singleLine = true, modifier = Modifier.fillMaxWidth(),
							supportingText = hint(st.title, other))
						OutlinedTextField(st.description[lang].orEmpty(), { st.description = st.setText(st.description, it) },
							label = { Text("${s.description} ($lang)") }, modifier = Modifier.fillMaxWidth(),
							supportingText = hint(st.description, other))
						CategoryPicker(st, export.categories.recipe, lang)
						Text(s.methods, style = MaterialTheme.typography.labelLarge)
						androidx.compose.foundation.layout.FlowRow(
							horizontalArrangement = Arrangement.spacedBy(8.dp),
						) {
							export.categories.method.forEach { m ->
								androidx.compose.material3.FilterChip(
									m.id in st.methods, { st.toggleMethod(m.id) }, label = { Text(m.label.tr(lang)) },
								)
							}
						}
						OutlinedTextField(st.tagsText, { st.tagsText = it; st.dirty = true }, label = { Text(s.otherTags) },
							placeholder = { Text(s.tagsHint) }, singleLine = true, modifier = Modifier.fillMaxWidth())
						OutlinedTextField(st.videoUrl, { st.videoUrl = it; st.dirty = true }, label = { Text(s.videoLink) },
							placeholder = { Text("https://youtube.com/…") }, singleLine = true, modifier = Modifier.fillMaxWidth(),
							keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri))
						Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
							NumberField(st.servingsText, s.servings, Modifier.weight(1f)) { st.servingsText = it; st.dirty = true }
							NumberField(st.prepText, "${s.prep} (${s.min})", Modifier.weight(1f)) { st.prepText = it; st.dirty = true }
							NumberField(st.cookText, "${s.cook} (${s.min})", Modifier.weight(1f)) { st.cookText = it; st.dirty = true }
						}
					}
				}
				item { SectionTitle(s.ingredients) }
				itemsIndexed(st.ingredients, key = { _, e -> e.uid }) { i, e ->
					IngredientEditor(st, e, i, catalog, export.units.map { it.id to it.label.tr(lang) })
				}
				item { TextButton(onClick = { st.addIngredient() }) { Text("+ ${s.addIngredient}") } }
				item { SectionTitle(s.method) }
				itemsIndexed(st.steps, key = { _, e -> e.uid }) { i, e ->
					StepEditor(st, e, i, isNew, id,
						onTimer = { timerFor = e },
						onPhoto = { pickPhoto { key -> e.step = e.step.copy(media = key); st.dirty = true } },
						onVideo = { pickVideo { key -> e.step = e.step.copy(media = key); st.dirty = true } })
				}
				item { TextButton(onClick = { st.addStep() }) { Text("+ ${s.addStep}") } }
				item {
					OutlinedTextField(st.notes[lang].orEmpty(), { st.notes = st.setText(st.notes, it) },
						label = { Text("${s.notes} ($lang)") }, modifier = Modifier.fillMaxWidth(), minLines = 2)
					Spacer(Modifier.height(48.dp))
				}
			}
		}
	}

	if (errors.isNotEmpty()) {
		AlertDialog(
			onDismissRequest = { errors = emptyList() },
			title = { Text(s.invalid) },
			text = { Text(errors.joinToString("\n") { "• $it" }) },
			confirmButton = { TextButton(onClick = { errors = emptyList() }) { Text(s.ok) } },
		)
	}
	conflict?.let { cur ->
		AlertDialog(
			onDismissRequest = { conflict = null },
			text = { Text(s.conflict) },
			confirmButton = {
				TextButton(onClick = { conflict = null; scope.launch { save(overwriteRev = cur.rev) } }) { Text(s.overwrite) }
			},
			dismissButton = {
				TextButton(onClick = {
					conflict = null
					app.repo.putLocal(cur)
					st.dirty = false
					nav.popBackStack()
				}) { Text(s.reload) }
			},
		)
	}
	if (confirmLeave) {
		AlertDialog(
			onDismissRequest = { confirmLeave = false },
			text = { Text(s.unsaved) },
			confirmButton = { TextButton(onClick = { confirmLeave = false; st.dirty = false; nav.popBackStack() }) { Text(s.discard) } },
			dismissButton = { TextButton(onClick = { confirmLeave = false }) { Text(s.cancel) } },
		)
	}
	timerFor?.let { step ->
		var minutes by remember { mutableStateOf("10") }
		AlertDialog(
			onDismissRequest = { timerFor = null },
			title = { Text(s.insertTimer) },
			text = {
				OutlinedTextField(minutes, { minutes = it.filter(Char::isDigit).take(3) }, label = { Text(s.minutes) },
					keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number), singleLine = true)
			},
			confirmButton = {
				TextButton(onClick = {
					minutes.toIntOrNull()?.takeIf { it > 0 }?.let { st.insert(step, "[$it:00]") }
					timerFor = null
				}) { Text(s.ok) }
			},
			dismissButton = { TextButton(onClick = { timerFor = null }) { Text(s.cancel) } },
		)
	}
}

/** Shows the other language's text under a field, as a translation aid. */
private fun hint(l: Map<String, String>, other: String): (@Composable () -> Unit)? {
	val t = l[other]
	if (t.isNullOrBlank()) return null
	return { Text("${other.uppercase()}: $t", maxLines = 1) }
}

@Composable
private fun SectionTitle(t: String) {
	Text(t, style = MaterialTheme.typography.titleLarge, color = MaterialTheme.colorScheme.primary,
		modifier = Modifier.padding(top = 8.dp))
}

@Composable
private fun NumberField(value: String, label: String, modifier: Modifier, onChange: (String) -> Unit) {
	OutlinedTextField(value, { onChange(it.filter { c -> c.isDigit() || c == '.' || c == ',' }) }, label = { Text(label, maxLines = 1) },
		singleLine = true, keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal), modifier = modifier)
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun CategoryPicker(st: EditorState, categories: List<dev.cookbook.data.Category>, lang: String) {
	val s = LocalStrings.current
	var open by remember { mutableStateOf(false) }
	ExposedDropdownMenuBox(open, { open = it }) {
		OutlinedTextField(
			categories.firstOrNull { it.id == st.category }?.label.tr(lang).ifEmpty { st.category },
			{}, readOnly = true, label = { Text(s.category) },
			trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(open) },
			modifier = Modifier.fillMaxWidth().menuAnchor(MenuAnchorType.PrimaryNotEditable),
		)
		ExposedDropdownMenu(open, { open = false }) {
			categories.forEach { c ->
				DropdownMenuItem({ Text(c.label.tr(lang)) }, onClick = { st.category = c.id; st.dirty = true; open = false })
			}
		}
	}
}

/** A photo slot: server media, a freshly picked local photo, or empty. */
@Composable
private fun PhotoBox(
	st: EditorState, ref: String?, isNew: Boolean, recipeId: String, modifier: Modifier,
	label: String, onPick: () -> Unit, onClear: () -> Unit,
) {
	Box(modifier.clip(RoundedCornerShape(12.dp)).background(MaterialTheme.colorScheme.surfaceVariant).clickable(onClick = onPick)) {
		val local = ref?.let { st.photos[it] }
		when {
			local != null -> Image(local.preview, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
			ref != null && ref in st.videos -> Column(Modifier.align(Alignment.Center), horizontalAlignment = Alignment.CenterHorizontally) {
				Icon(Icons.Outlined.Videocam, null)
				Text(LocalStrings.current.video, style = MaterialTheme.typography.labelMedium)
			}
			ref != null && !isNew && dev.cookbook.data.isVideo(ref) -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
				RecipeImage(recipeId, dev.cookbook.data.posterName(ref), Modifier.fillMaxSize())
				Icon(Icons.Outlined.Videocam, null, tint = androidx.compose.ui.graphics.Color.White)
			}
			ref != null && !isNew -> RecipeImage(recipeId, ref, Modifier.fillMaxSize())
			else -> Column(Modifier.align(Alignment.Center), horizontalAlignment = Alignment.CenterHorizontally) {
				Icon(Icons.Outlined.AddPhotoAlternate, null)
				Text(label, style = MaterialTheme.typography.labelMedium)
			}
		}
		if (ref != null) {
			IconButton(onClick = onClear, Modifier.align(Alignment.TopEnd)) {
				Icon(Icons.Outlined.HideImage, null, tint = MaterialTheme.colorScheme.onPrimary,
					modifier = Modifier.background(MaterialTheme.colorScheme.primary.copy(alpha = 0.7f), RoundedCornerShape(50)).padding(4.dp))
			}
		}
	}
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun IngredientEditor(st: EditorState, e: EditIngredient, index: Int, catalog: List<CatalogEntry>, units: List<Pair<String, String>>) {
	val s = LocalStrings.current
	val lang = st.lang
	OutlinedCard {
		Column(Modifier.padding(8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
			Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
				OutlinedTextField(e.qtyText, { e.qtyText = it; st.dirty = true }, label = { Text(s.qty) }, singleLine = true,
					modifier = Modifier.width(78.dp), keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Text))
				UnitPicker(e, units, Modifier.width(96.dp)) { st.dirty = true }
				NameWithSuggestions(st, e, catalog, Modifier.weight(1f))
			}
			Row(verticalAlignment = Alignment.CenterVertically) {
				Text("{{${e.ing.id.ifEmpty { "…" }}}}", style = MaterialTheme.typography.labelSmall,
					color = MaterialTheme.colorScheme.secondary, modifier = Modifier.weight(1f))
				IconButton(onClick = { st.move(st.ingredients, index, index - 1) }) { Icon(Icons.Outlined.ArrowUpward, s.moveUp) }
				IconButton(onClick = { st.move(st.ingredients, index, index + 1) }) { Icon(Icons.Outlined.ArrowDownward, s.moveDown) }
				IconButton(onClick = { e.expanded = !e.expanded }) {
					Icon(if (e.expanded) Icons.Outlined.ExpandLess else Icons.Outlined.ExpandMore, null)
				}
				IconButton(onClick = { st.ingredients.remove(e); st.dirty = true }) { Icon(Icons.Outlined.Delete, s.delete) }
			}
			if (e.expanded) {
				OutlinedTextField(e.ing.group?.get(lang).orEmpty(), { e.ing = e.ing.copy(group = st.setText(e.ing.group ?: emptyMap(), it)) },
					label = { Text(s.group) }, singleLine = true, modifier = Modifier.fillMaxWidth())
				OutlinedTextField(e.ing.note?.get(lang).orEmpty(), { e.ing = e.ing.copy(note = st.setText(e.ing.note ?: emptyMap(), it)) },
					label = { Text(s.note) }, singleLine = true, modifier = Modifier.fillMaxWidth())
				Row(verticalAlignment = Alignment.CenterVertically) {
					Checkbox(e.ing.optional, { e.ing = e.ing.copy(optional = it); st.dirty = true })
					Text(s.optional)
				}
			}
		}
	}
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun UnitPicker(e: EditIngredient, units: List<Pair<String, String>>, modifier: Modifier, onChange: () -> Unit) {
	val s = LocalStrings.current
	var open by remember { mutableStateOf(false) }
	val current = units.firstOrNull { it.first == e.ing.unitId }?.second ?: e.ing.unit.tr("hu")
	ExposedDropdownMenuBox(open, { open = it }, modifier) {
		OutlinedTextField(current, {}, readOnly = true, label = { Text(s.unit) }, singleLine = true,
			modifier = Modifier.menuAnchor(MenuAnchorType.PrimaryNotEditable))
		ExposedDropdownMenu(open, { open = false }) {
			DropdownMenuItem({ Text("—") }, onClick = { e.ing = e.ing.copy(unitId = null, unit = null); open = false; onChange() })
			units.forEach { (id, label) ->
				DropdownMenuItem({ Text(label) }, onClick = {
					// Labels come from the unit table; drop any free-text label.
					e.ing = e.ing.copy(unitId = id, unit = null); open = false; onChange()
				})
			}
		}
	}
}

/** Name field with autocomplete from ingredients already in the book. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun NameWithSuggestions(st: EditorState, e: EditIngredient, catalog: List<CatalogEntry>, modifier: Modifier) {
	val s = LocalStrings.current
	val lang = st.lang
	val text = e.ing.name[lang].orEmpty()
	var focused by remember { mutableStateOf(false) }
	val q = fold(text)
	val suggestions = if (q.length < 2 || e.idLocked) emptyList() else catalog.filter { c ->
		fold(c.name.tr(lang)).contains(q) || c.id.contains(q.replace(' ', '_'))
	}.take(6)
	ExposedDropdownMenuBox(focused && suggestions.isNotEmpty(), { focused = it }, modifier) {
		OutlinedTextField(text, { st.rename(e, it); focused = true }, label = { Text(s.name) }, singleLine = true,
			modifier = Modifier.menuAnchor(MenuAnchorType.PrimaryEditable))
		ExposedDropdownMenu(focused && suggestions.isNotEmpty(), { focused = false }) {
			suggestions.forEach { c ->
				DropdownMenuItem(
					{ Text("${c.name.tr(lang)}  ·  ${c.id}") },
					onClick = { st.pick(e, c.id, c.name, c.category); focused = false },
				)
			}
		}
	}
}

@Composable
private fun StepEditor(
	st: EditorState, e: EditStep, index: Int, isNew: Boolean, recipeId: String,
	onTimer: () -> Unit, onPhoto: () -> Unit, onVideo: () -> Unit,
) {
	val s = LocalStrings.current
	var refMenu by remember { mutableStateOf(false) }
	OutlinedCard {
		Column(Modifier.padding(8.dp)) {
			Row(verticalAlignment = Alignment.Top) {
				Text("${index + 1}.", fontWeight = FontWeight.Bold, color = MaterialTheme.colorScheme.primary,
					modifier = Modifier.padding(top = 16.dp, end = 6.dp))
				OutlinedTextField(e.value, { st.editStep(e, it) }, modifier = Modifier.weight(1f), minLines = 2,
					supportingText = hint(e.step.text, if (st.lang == "hu") "en" else "hu"))
			}
			Row(verticalAlignment = Alignment.CenterVertically) {
				Box {
					IconButton(onClick = { refMenu = true }) { Icon(Icons.Outlined.Link, s.insertIngredient) }
					DropdownMenu(refMenu, { refMenu = false }) {
						st.ingredients.filter { it.ing.id.isNotEmpty() }.forEach { ing ->
							DropdownMenuItem({ Text(ing.ing.name.tr(st.lang)) }, onClick = {
								st.insert(e, "{{${ing.ing.id}}}"); refMenu = false
							})
						}
					}
				}
				IconButton(onClick = onTimer) { Icon(Icons.Outlined.Timer, s.insertTimer) }
				IconButton(onClick = onPhoto) { Icon(Icons.Outlined.AddPhotoAlternate, s.photo) }
				IconButton(onClick = onVideo) { Icon(Icons.Outlined.Videocam, s.video) }
				Spacer(Modifier.weight(1f))
				IconButton(onClick = { st.move(st.steps, index, index - 1) }) { Icon(Icons.Outlined.ArrowUpward, s.moveUp) }
				IconButton(onClick = { st.move(st.steps, index, index + 1) }) { Icon(Icons.Outlined.ArrowDownward, s.moveDown) }
				IconButton(onClick = { st.steps.remove(e); st.dirty = true }) { Icon(Icons.Outlined.Delete, s.delete) }
			}
			e.step.media?.let { ref ->
				PhotoBox(st, ref, isNew, recipeId, Modifier.size(width = 160.dp, height = 110.dp), s.photo,
					onPick = onPhoto, onClear = { e.step = e.step.copy(media = null); st.dirty = true })
			}
		}
	}
}
