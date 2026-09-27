package dev.cookbook.ui

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.consumeWindowInsets
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.MenuBook
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.CloudOff
import androidx.compose.material.icons.outlined.Pause
import androidx.compose.material.icons.outlined.PlayArrow
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.ShoppingCart
import androidx.compose.material.icons.outlined.Style
import androidx.compose.material.icons.outlined.Timer
import androidx.compose.material3.AssistChip
import androidx.compose.material3.AssistChipDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavHostController
import androidx.navigation.NavType
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import androidx.navigation.navArgument
import dev.cookbook.CookBookApp
import dev.cookbook.logic.clock

val LocalApp = staticCompositionLocalOf<CookBookApp> { error("no app") }
val LocalSnackbar = staticCompositionLocalOf { SnackbarHostState() }

private data class Tab(val route: String, val icon: ImageVector, val label: (Strings) -> String)

private val tabs = listOf(
	Tab("list", Icons.AutoMirrored.Outlined.MenuBook) { it.recipes },
	Tab("discover", Icons.Outlined.Style) { it.discover },
	Tab("grocery", Icons.Outlined.ShoppingCart) { it.grocery },
	Tab("settings", Icons.Outlined.Settings) { it.settings },
)

@Composable
fun CookBookRoot(app: CookBookApp) {
	val lang by app.settings.lang.collectAsStateWithLifecycle()
	val snackbar = remember { SnackbarHostState() }
	LaunchedEffect(Unit) {
		app.repo.loadCache()
		app.repo.refresh()
		app.repo.grocery() // sends anything queued while offline
	}
	CookBookTheme {
		CompositionLocalProvider(LocalApp provides app, LocalStrings provides stringsFor(lang), LocalSnackbar provides snackbar) {
			val nav = rememberNavController()
			val s = stringsFor(lang)
			LaunchedEffect(Unit) {
				app.paired.collect { server ->
					app.paired.resetReplayCache()
					val err = app.repo.refresh()
					nav.navigate("list") { popUpTo(0) }
					snackbar.showSnackbar(if (err == null) "✓ ${s.connected}: $server" else "✗ $server: $err")
				}
			}
			val pendingImport by app.pendingImport.collectAsStateWithLifecycle()
			pendingImport?.let { ImportDialog(nav, it) { app.pendingImport.value = null } }
			val entry by nav.currentBackStackEntryAsState()
			val route = entry?.destination?.route
			val showBar = tabs.any { it.route == route }
			Scaffold(
				// Screens own their top bars and insets; this scaffold only adds
				// the bottom bar, so nothing gets padded twice.
				contentWindowInsets = WindowInsets(0, 0, 0, 0),
				snackbarHost = { SnackbarHost(snackbar) },
				bottomBar = {
					Column {
						// Cook mode shows its own timers, large.
						if (route?.startsWith("cook") != true) TimersBar()
						if (showBar) BottomBar(nav, route)
					}
				},
			) { pad ->
				NavHost(nav, startDestination = if (app.settings.configured) "list" else "settings", Modifier.padding(pad).consumeWindowInsets(pad)) {
					composable("list") { ListScreen(nav) }
					composable("discover") { DiscoverScreen(nav) }
					composable("grocery") { GroceryScreen(nav) }
					composable("settings") { SettingsScreen(nav) }
					composable(
						"recipe/{id}",
						arguments = listOf(navArgument("id") { type = NavType.StringType }),
					) { DetailScreen(nav, it.arguments!!.getString("id")!!) }
					composable(
						"cook/{id}?servings={servings}",
						arguments = listOf(
							navArgument("id") { type = NavType.StringType },
							navArgument("servings") { type = NavType.FloatType; defaultValue = 0f },
						),
					) { CookScreen(nav, it.arguments!!.getString("id")!!, it.arguments!!.getFloat("servings").toDouble()) }
					composable(
						"edit/{id}",
						arguments = listOf(navArgument("id") { type = NavType.StringType }),
					) { EditorScreen(nav, it.arguments!!.getString("id")!!) }
					composable(
						"pdf/{id}",
						arguments = listOf(navArgument("id") { type = NavType.StringType }),
					) { PdfScreen(nav, it.arguments!!.getString("id")!!) }
				}
			}
		}
	}
}

@Composable
private fun BottomBar(nav: NavHostController, route: String?) {
	val s = LocalStrings.current
	NavigationBar {
		tabs.forEach { tab ->
			NavigationBarItem(
				selected = route == tab.route,
				onClick = {
					nav.navigate(tab.route) {
						popUpTo("list") { saveState = true }
						launchSingleTop = true
						restoreState = true
					}
				},
				icon = { Icon(tab.icon, null) },
				label = { Text(tab.label(s)) },
			)
		}
	}
}

/** Running timers, visible from every screen: the pasta keeps boiling while you browse. */
@Composable
fun TimersBar() {
	val app = LocalApp.current
	val timers by app.timers.timers.collectAsStateWithLifecycle()
	val now by app.timers.now.collectAsStateWithLifecycle()
	val s = LocalStrings.current
	AnimatedVisibility(timers.isNotEmpty()) {
		Surface(color = MaterialTheme.colorScheme.secondaryContainer) {
			LazyRow(
				Modifier.fillMaxWidth().padding(horizontal = 8.dp, vertical = 4.dp),
				horizontalArrangement = Arrangement.spacedBy(8.dp),
			) {
				items(timers, key = { it.id }) { t ->
					val left = t.left(now)
					AssistChip(
						onClick = { if (t.rang) app.timers.dismiss(t.id) else app.timers.pauseResume(t.id) },
						label = {
							Text(
								if (t.rang) "⏰ ${t.label} – ${s.done}" else "${clock(left)}  ${t.recipe.take(18)}",
								fontWeight = if (t.rang) FontWeight.Bold else FontWeight.Normal,
							)
						},
						leadingIcon = {
							Icon(
								when {
									t.rang -> Icons.Outlined.Timer
									t.pausedLeft != null -> Icons.Outlined.PlayArrow
									else -> Icons.Outlined.Pause
								}, null,
							)
						},
						trailingIcon = {
							Icon(Icons.Outlined.Close, s.dismiss, Modifier.clickable { app.timers.dismiss(t.id) })
						},
						colors = if (t.rang) AssistChipDefaults.assistChipColors(
							containerColor = MaterialTheme.colorScheme.primary,
							labelColor = MaterialTheme.colorScheme.onPrimary,
							leadingIconContentColor = MaterialTheme.colorScheme.onPrimary,
							trailingIconContentColor = MaterialTheme.colorScheme.onPrimary,
						) else AssistChipDefaults.assistChipColors(),
					)
				}
			}
		}
	}
}

/** Small "offline" banner, shown only when the last server contact failed. */
@Composable
fun OfflineBanner() {
	val app = LocalApp.current
	val online by app.repo.online.collectAsStateWithLifecycle()
	if (online == false) {
		Row(
			Modifier.fillMaxWidth().background(MaterialTheme.colorScheme.surfaceVariant).padding(horizontal = 16.dp, vertical = 6.dp),
			verticalAlignment = Alignment.CenterVertically,
			horizontalArrangement = Arrangement.spacedBy(8.dp),
		) {
			Icon(Icons.Outlined.CloudOff, null, Modifier.padding(end = 2.dp))
			Text(LocalStrings.current.offline, style = MaterialTheme.typography.labelMedium)
		}
	}
}

/** Language toggle used on several screens: flips content and UI together. */
@Composable
fun LangToggle() {
	val app = LocalApp.current
	val lang by app.settings.lang.collectAsStateWithLifecycle()
	IconButton(onClick = { app.settings.setLang(if (lang == "hu") "en" else "hu") }) {
		Text(lang.uppercase(), fontWeight = FontWeight.Bold, color = MaterialTheme.colorScheme.primary)
	}
}
