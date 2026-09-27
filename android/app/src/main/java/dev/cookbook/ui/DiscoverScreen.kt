package dev.cookbook.ui

import androidx.compose.animation.core.Animatable
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.detectDragGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.Restaurant
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledIconButton
import androidx.compose.material3.FilledTonalIconButton
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavHostController
import dev.cookbook.data.Recipe
import dev.cookbook.data.tr
import kotlinx.coroutines.launch
import kotlin.math.abs

/** Fast View (spec B.2): a card stack, swipe left to pass, right to open. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DiscoverScreen(nav: NavHostController) {
	val app = LocalApp.current
	val s = LocalStrings.current
	val export by app.repo.export.collectAsStateWithLifecycle()
	var seed by rememberSaveable { mutableStateOf(System.currentTimeMillis()) }
	var passed by rememberSaveable { mutableStateOf(listOf<String>()) }
	var category by rememberSaveable { mutableStateOf<String?>(null) }
	val lang by app.settings.lang.collectAsStateWithLifecycle()
	val deck = remember(export.recipes, seed) { export.recipes.shuffled(java.util.Random(seed)) }
	val remaining = deck.filter { category == null || it.category == category }.filterNot { it.id in passed }
	val usedCategories = export.categories.recipe.filter { c -> export.recipes.any { it.category == c.id } }

	Scaffold(topBar = { TopAppBar(title = { Text(s.discover) }, actions = { LangToggle() }) }) { pad ->
		Column(Modifier.padding(pad).fillMaxSize()) {
			CategoryChips(usedCategories, category, lang) { category = it }
			Column(Modifier.weight(1f).fillMaxWidth().padding(16.dp)) {
				Box(Modifier.weight(1f).fillMaxWidth(), contentAlignment = Alignment.Center) {
					if (remaining.isEmpty()) {
						Column(horizontalAlignment = Alignment.CenterHorizontally) {
							Text(s.endOfStack, style = MaterialTheme.typography.titleMedium)
							Spacer(Modifier.height(12.dp))
							Button(onClick = { passed = emptyList(); seed = System.currentTimeMillis() }) { Text(s.startOver) }
						}
					}
					// Two cards: the next one peeks out from under the top one.
					remaining.take(2).reversed().forEachIndexed { i, r ->
						val top = i == remaining.take(2).size - 1
						key(r.id) {
							SwipeCard(
								r, top,
								onPass = { passed = passed + r.id },
								onOpen = { passed = passed + r.id; nav.navigate("recipe/${r.id}") },
							)
						}
					}
				}
				Row(Modifier.fillMaxWidth().padding(top = 16.dp), horizontalArrangement = Arrangement.SpaceEvenly) {
					val top = remaining.firstOrNull()
					FilledTonalIconButton(onClick = { top?.let { passed = passed + it.id } }, Modifier.size(64.dp), enabled = top != null) {
						Icon(Icons.Outlined.Close, s.pass)
					}
					FilledIconButton(
						onClick = { top?.let { passed = passed + it.id; nav.navigate("recipe/${it.id}") } },
						Modifier.size(64.dp), enabled = top != null,
					) { Icon(Icons.Outlined.Restaurant, s.open) }
				}
			}
		}
	}
}

@Composable
private fun SwipeCard(r: Recipe, top: Boolean, onPass: () -> Unit, onOpen: () -> Unit) {
	val app = LocalApp.current
	val s = LocalStrings.current
	val lang by app.settings.lang.collectAsStateWithLifecycle()
	val offsetX = remember { Animatable(0f) }
	val offsetY = remember { Animatable(0f) }
	val scope = rememberCoroutineScope()
	val widthPx = with(LocalDensity.current) { LocalConfiguration.current.screenWidthDp.dp.toPx() }

	Card(
		shape = RoundedCornerShape(24.dp),
		elevation = CardDefaults.cardElevation(if (top) 8.dp else 2.dp),
		modifier = Modifier
			.fillMaxSize()
			.graphicsLayer {
				translationX = offsetX.value
				translationY = offsetY.value
				rotationZ = offsetX.value / widthPx * 15f
				val sc = if (top) 1f else 0.95f
				scaleX = sc; scaleY = sc
			}
			.then(if (!top) Modifier else Modifier.pointerInput(r.id) {
				detectDragGestures(
					onDragEnd = {
						scope.launch {
							when {
								offsetX.value > widthPx * 0.3f -> { offsetX.animateTo(widthPx * 1.5f); onOpen() }
								offsetX.value < -widthPx * 0.3f -> { offsetX.animateTo(-widthPx * 1.5f); onPass() }
								else -> { launch { offsetY.animateTo(0f) }; offsetX.animateTo(0f) }
							}
						}
					},
				) { change, drag ->
					change.consume()
					scope.launch {
						offsetX.snapTo(offsetX.value + drag.x)
						offsetY.snapTo(offsetY.value + drag.y)
					}
				}
			}),
	) {
		Box(Modifier.fillMaxSize()) {
			RecipeImage(r.id, r.hero, Modifier.fillMaxSize())
			Box(
				Modifier.fillMaxSize().background(
					Brush.verticalGradient(0.5f to Color.Transparent, 1f to Color.Black.copy(alpha = 0.75f)),
				),
			)
			Column(Modifier.align(Alignment.BottomStart).padding(20.dp)) {
				Text(r.title.tr(lang), style = MaterialTheme.typography.headlineSmall, color = Color.White, fontWeight = FontWeight.Bold)
				val total = r.prepMinutes + r.cookMinutes
				val meta = buildList {
					if (total > 0) add("⏱ $total ${s.min}")
					add("${formatServings(r.baseServings, lang)} ${s.servings}")
				}
				Text(meta.joinToString("  ·  "), color = Color.White.copy(alpha = 0.9f))
				if (r.tags.isNotEmpty()) {
					Text(tagLine(r.tags, rememberMethods(), lang), color = Color.White.copy(alpha = 0.85f),
						style = MaterialTheme.typography.labelLarge)
				}
			}
			// Swipe hint labels
			val hint = offsetX.value / widthPx
			if (abs(hint) > 0.05f) {
				Text(
					if (hint > 0) s.open.uppercase() else s.pass.uppercase(),
					color = Color.White,
					fontWeight = FontWeight.Black,
					style = MaterialTheme.typography.headlineMedium,
					modifier = Modifier
						.align(if (hint > 0) Alignment.TopStart else Alignment.TopEnd)
						.padding(24.dp)
						.graphicsLayer { alpha = (abs(hint) * 3).coerceAtMost(1f) }
						.clip(RoundedCornerShape(8.dp))
						.background(if (hint > 0) MaterialTheme.colorScheme.tertiary else MaterialTheme.colorScheme.error)
						.padding(horizontal = 12.dp, vertical = 4.dp),
				)
			}
		}
	}
}
