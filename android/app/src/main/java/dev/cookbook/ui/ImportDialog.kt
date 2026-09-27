package dev.cookbook.ui

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.navigation.NavHostController
import kotlinx.coroutines.launch

/**
 * "Import from web": the server fetches the page, reads its recipe data,
 * saves it with its photo and (if configured) translates it. Opened from the
 * recipe list, or with the link prefilled when a browser shares to CookBook.
 */
@Composable
fun ImportDialog(nav: NavHostController, initial: String, onDone: () -> Unit) {
	val app = LocalApp.current
	val s = LocalStrings.current
	val snackbar = LocalSnackbar.current
	val scope = rememberCoroutineScope()
	var url by remember(initial) { mutableStateOf(initial) }
	var busy by remember { mutableStateOf(false) }
	var error by remember { mutableStateOf<String?>(null) }

	fun run() = scope.launch {
		busy = true
		error = null
		runCatching { app.api.importUrl(url.trim()) }
			.onSuccess { res ->
				app.repo.putLocal(res.recipe)
				onDone()
				nav.navigate("recipe/${res.recipe.id}")
				snackbar.showSnackbar(if (res.duplicate) s.alreadyInBook else s.imported)
			}
			.onFailure { error = it.message ?: "error" }
		busy = false
	}

	AlertDialog(
		onDismissRequest = { if (!busy) onDone() },
		title = { Text(s.importWeb) },
		text = {
			Column {
				OutlinedTextField(
					url, { url = it; error = null }, label = { Text(s.recipeLink) },
					placeholder = { Text("https://…") }, singleLine = true, enabled = !busy,
					keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
					modifier = Modifier.fillMaxWidth(),
				)
				if (busy) {
					LinearProgressIndicator(Modifier.fillMaxWidth().padding(top = 12.dp))
					Text(s.importing, style = MaterialTheme.typography.bodySmall)
				}
				error?.let {
					Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall,
						modifier = Modifier.padding(top = 8.dp))
				}
				Text(s.importHint, style = MaterialTheme.typography.bodySmall,
					color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(top = 8.dp))
			}
		},
		confirmButton = {
			TextButton(enabled = !busy && url.startsWith("http"), onClick = { run() }) { Text(s.importWeb) }
		},
		dismissButton = { TextButton(enabled = !busy, onClick = onDone) { Text(s.cancel) } },
	)
}
