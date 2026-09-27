package dev.cookbook.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.outlined.Visibility
import androidx.compose.material.icons.outlined.VisibilityOff
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavHostController
import kotlinx.coroutines.launch

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsScreen(nav: NavHostController) {
	val app = LocalApp.current
	val s = LocalStrings.current
	val scope = rememberCoroutineScope()
	val lang by app.settings.lang.collectAsStateWithLifecycle()
	val imperial by app.settings.imperial.collectAsStateWithLifecycle()
	var server by remember { mutableStateOf(app.settings.server) }
	var token by remember { mutableStateOf(app.settings.token) }
	var status by remember { mutableStateOf("") }
	var testing by remember { mutableStateOf(false) }
	var showToken by remember { mutableStateOf(false) }
	// A pairing link may fill these while this screen is open.
	androidx.compose.runtime.LaunchedEffect(Unit) {
		app.paired.collect { server = app.settings.server; token = app.settings.token }
	}

	Scaffold(topBar = { TopAppBar(title = { Text(s.settings) }) }) { pad ->
		Column(
			Modifier.padding(pad).fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
			verticalArrangement = Arrangement.spacedBy(12.dp),
		) {
			Text(s.server, style = MaterialTheme.typography.titleMedium)
			OutlinedTextField(
				server, { server = it }, label = { Text(s.server) }, placeholder = { Text(s.serverHint) },
				singleLine = true, keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
				modifier = Modifier.fillMaxWidth(),
			)
			OutlinedTextField(
				token, { token = it }, label = { Text(s.token) }, placeholder = { Text(s.tokenHint) },
				singleLine = true,
				visualTransformation = if (showToken) androidx.compose.ui.text.input.VisualTransformation.None else PasswordVisualTransformation(),
				trailingIcon = {
					androidx.compose.material3.IconButton(onClick = { showToken = !showToken }) {
						androidx.compose.material3.Icon(
							if (showToken) androidx.compose.material.icons.Icons.Outlined.VisibilityOff
							else androidx.compose.material.icons.Icons.Outlined.Visibility, null,
						)
					}
				},
				modifier = Modifier.fillMaxWidth(),
			)
			Button(
				enabled = !testing && server.isNotBlank() && token.isNotBlank(),
				onClick = {
					app.settings.server = server
					app.settings.token = token
					scope.launch {
						testing = true
						status = runCatching { app.api.health() }.fold(
							onSuccess = { h ->
								app.settings.exportEtag = ""
								app.repo.refresh()
								buildString {
									append("✓ ${s.connected} – CookBook ${h.version} (${h.os}), ${h.recipes} ${s.recipes.lowercase()}\n")
									append("Typst: ${if (h.typst.available) h.typst.version else "–"}\n")
									append("${s.autoTranslate}: ${if (h.translate.available) h.translate.provider else "–"}")
								}
							},
							onFailure = { "✗ ${it.message}" },
						)
						testing = false
					}
				},
			) { Text(s.test) }
			if (status.isNotEmpty()) Card { Text(status, Modifier.padding(12.dp)) }
			Text(s.pairHint, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)

			Text(s.language, style = MaterialTheme.typography.titleMedium)
			SingleChoiceSegmentedButtonRow {
				SegmentedButton(lang == "hu", { app.settings.setLang("hu") }, SegmentedButtonDefaults.itemShape(0, 2)) { Text("Magyar") }
				SegmentedButton(lang == "en", { app.settings.setLang("en") }, SegmentedButtonDefaults.itemShape(1, 2)) { Text("English") }
			}
			Text(s.units, style = MaterialTheme.typography.titleMedium)
			SingleChoiceSegmentedButtonRow {
				SegmentedButton(!imperial, { app.settings.setImperial(false) }, SegmentedButtonDefaults.itemShape(0, 2)) { Text(s.metric) }
				SegmentedButton(imperial, { app.settings.setImperial(true) }, SegmentedButtonDefaults.itemShape(1, 2)) { Text(s.imperial) }
			}
			if (app.settings.configured) FeedsCard(nav)
		}
	}
}

/** Status of the server's automatic web import, with a "Run now" button. */
@Composable
private fun FeedsCard(nav: NavHostController) {
	val app = LocalApp.current
	val s = LocalStrings.current
	val snackbar = LocalSnackbar.current
	val scope = rememberCoroutineScope()
	var status by remember { mutableStateOf<dev.cookbook.data.FeedStatus?>(null) }
	var error by remember { mutableStateOf<String?>(null) }

	suspend fun load() {
		runCatching { app.api.feeds() }.onSuccess { status = it; error = null }.onFailure { error = it.message }
	}
	androidx.compose.runtime.LaunchedEffect(Unit) { load() }
	// While a run is going, refresh every few seconds so imports appear as they land.
	androidx.compose.runtime.LaunchedEffect(status?.running) {
		while (status?.running == true) {
			kotlinx.coroutines.delay(5000)
			load()
			if (status?.running == false) app.repo.refresh()
		}
	}

	Text(s.autoImport, style = MaterialTheme.typography.titleMedium)
	Card {
		Column(Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
			error?.let { Text("✗ $it", color = MaterialTheme.colorScheme.error) }
			val st = status ?: return@Column
			val cfg = st.config
			Text(
				if (!cfg.enabled) s.off
				else "${cfg.sources.count { it.enabled }} · ${cfg.perSource}/${cfg.intervalHours}h" +
					if (cfg.autoTranslate) " · HU⇄EN" else "",
				style = MaterialTheme.typography.bodyMedium,
			)
			Text("${s.lastRun}: ${st.lastRun.takeIf { !it.startsWith("0001") }?.let(::shortTime) ?: s.never}" +
				if (cfg.enabled && !st.running) "  ·  ${s.nextRun}: ${shortTime(st.nextRun)}" else "",
				style = MaterialTheme.typography.bodySmall)
			cfg.sources.forEach { src ->
				val ss = st.sources[src.name]
				Text(
					"${src.name} (${src.lang}): ${ss?.imported ?: 0}" + (ss?.lastError?.let { "  ✗ ${it.take(60)}" } ?: ""),
					style = MaterialTheme.typography.bodySmall,
					color = if (src.enabled) MaterialTheme.colorScheme.onSurface else MaterialTheme.colorScheme.onSurfaceVariant,
				)
			}
			val recent = st.recent.filter { it.result == "imported" && it.id != null }.take(5)
			recent.forEach { r ->
				androidx.compose.material3.TextButton(onClick = { nav.navigate("recipe/${r.id}") }) {
					Text("• ${r.id}", style = MaterialTheme.typography.bodySmall)
				}
			}
			Button(
				enabled = !st.running,
				onClick = {
					scope.launch {
						runCatching { app.api.runFeeds() }
							.onSuccess { status = it }
							.onFailure { snackbar.showSnackbar(it.message ?: "error"); load() }
					}
				},
			) { Text(if (st.running) s.running else s.runNow) }
		}
	}
}

/** "2026-09-27T10:28:17Z" -> local "09-27 12:28". */
private fun shortTime(iso: String): String = runCatching {
	val t = java.time.Instant.parse(iso).atZone(java.time.ZoneId.systemDefault())
	"%02d-%02d %02d:%02d".format(t.monthValue, t.dayOfMonth, t.hour, t.minute)
}.getOrDefault(iso)
