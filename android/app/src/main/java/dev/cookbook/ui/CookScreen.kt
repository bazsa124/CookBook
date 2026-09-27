package dev.cookbook.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.pager.HorizontalPager
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.automirrored.outlined.List
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavHostController
import dev.cookbook.data.tr
import dev.cookbook.logic.clock
import kotlinx.coroutines.launch

/** Distraction-free cook mode (spec C.3): screen stays on, one big step at a time. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun CookScreen(nav: NavHostController, id: String, servingsArg: Double) {
	val app = LocalApp.current
	val s = LocalStrings.current
	val export by app.repo.export.collectAsStateWithLifecycle()
	val lang by app.settings.lang.collectAsStateWithLifecycle()
	val r = export.recipes.firstOrNull { it.id == id } ?: return
	val servings = if (servingsArg > 0) servingsArg else r.baseServings
	val k = rememberKitchen(r, servings)
	val title = r.title.tr(lang)
	val pager = rememberPagerState { r.steps.size.coerceAtLeast(1) }
	val scope = rememberCoroutineScope()
	var sheet by remember { mutableStateOf(false) }
	val struck = remember { mutableStateListOf<String>() }

	val view = LocalView.current
	DisposableEffect(Unit) {
		view.keepScreenOn = true
		onDispose { view.keepScreenOn = false }
	}

	Scaffold(
		topBar = {
			TopAppBar(
				title = { Text(title, maxLines = 1) },
				navigationIcon = { IconButton(onClick = { nav.popBackStack() }) { Icon(Icons.AutoMirrored.Outlined.ArrowBack, null) } },
				actions = {
					LangToggle()
					IconButton(onClick = { sheet = true }) { Icon(Icons.AutoMirrored.Outlined.List, s.ingredients) }
				},
			)
		},
		bottomBar = {
			Column(Modifier.navigationBarsPadding()) {
				BigTimers()
				Row(Modifier.fillMaxWidth().padding(16.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
					OutlinedButton(
						onClick = { scope.launch { pager.animateScrollToPage(pager.currentPage - 1) } },
						enabled = pager.currentPage > 0,
						modifier = Modifier.weight(1f).height(64.dp),
					) { Text(s.prev, fontSize = 20.sp) }
					val last = pager.currentPage >= r.steps.size - 1
					Button(
						onClick = { if (last) nav.popBackStack() else scope.launch { pager.animateScrollToPage(pager.currentPage + 1) } },
						modifier = Modifier.weight(1f).height(64.dp),
					) { Text(if (last) s.done else s.next, fontSize = 20.sp) }
				}
			}
		},
	) { pad ->
		Column(Modifier.padding(pad).fillMaxSize()) {
			LinearProgressIndicator(
				progress = { (pager.currentPage + 1f) / r.steps.size.coerceAtLeast(1) },
				modifier = Modifier.fillMaxWidth(),
			)
			HorizontalPager(pager, Modifier.fillMaxSize()) { page ->
				val st = r.steps.getOrNull(page)
				Column(
					Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(24.dp),
				) {
					Text("${s.step} ${page + 1} ${s.of} ${r.steps.size}", style = MaterialTheme.typography.titleMedium,
						color = MaterialTheme.colorScheme.primary)
					Spacer(Modifier.height(12.dp))
					if (st != null) {
						StepText(
							k.resolve(st.text.tr(lang), r.ingredients), title,
							MaterialTheme.typography.headlineMedium.copy(lineHeight = 40.sp),
						)
						st.media?.let {
							Spacer(Modifier.height(16.dp))
							StepMedia(r.id, it, Modifier.fillMaxWidth().aspectRatio(4f / 3f).clip(RoundedCornerShape(12.dp)))
						}
					}
				}
			}
		}
	}

	if (sheet) {
		ModalBottomSheet(onDismissRequest = { sheet = false }) {
			Text("${s.ingredients} · ${formatServings(servings, lang)} ${s.servings}",
				Modifier.padding(horizontal = 16.dp), style = MaterialTheme.typography.titleLarge)
			LazyColumn(Modifier.padding(bottom = 24.dp)) { ingredientItems(r.ingredients, k, lang, struck) }
		}
	}
}

/** Timers in cook mode: large enough to read from the stove. */
@Composable
private fun BigTimers() {
	val app = LocalApp.current
	val s = LocalStrings.current
	val timers by app.timers.timers.collectAsStateWithLifecycle()
	val now by app.timers.now.collectAsStateWithLifecycle()
	Column(Modifier.padding(horizontal = 16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
		timers.forEach { t ->
			Row(verticalAlignment = Alignment.CenterVertically) {
				Box(Modifier.weight(1f)) {
					Text(
						if (t.rang) "⏰ ${t.label}" else clock(t.left(now)),
						fontSize = 36.sp, fontWeight = FontWeight.Bold,
						color = if (t.rang) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurface,
					)
				}
				if (!t.rang) FilledTonalButton(onClick = { app.timers.pauseResume(t.id) }) {
					Text(if (t.pausedLeft == null) s.pause else s.resume)
				}
				Spacer(Modifier.padding(4.dp))
				OutlinedButton(onClick = { app.timers.dismiss(t.id) }) { Text(s.dismiss) }
			}
		}
	}
}
