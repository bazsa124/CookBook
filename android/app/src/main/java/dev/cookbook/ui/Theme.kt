package dev.cookbook.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.Box
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Restaurant
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import coil.compose.SubcomposeAsyncImage
import dev.cookbook.CookBookApp

private val Terracotta = Color(0xFFA4462A)

private val Light = lightColorScheme(
	primary = Terracotta,
	onPrimary = Color.White,
	primaryContainer = Color(0xFFFFDBCF),
	onPrimaryContainer = Color(0xFF3A0B00),
	secondary = Color(0xFF6B5B3E),
	secondaryContainer = Color(0xFFF4E1C1),
	tertiary = Color(0xFF4C6545),
	tertiaryContainer = Color(0xFFCEEBC3),
	background = Color(0xFFFFFBF7),
	surface = Color(0xFFFFFBF7),
	surfaceVariant = Color(0xFFF3E6DF),
)

private val Dark = darkColorScheme(
	primary = Color(0xFFFFB59C),
	onPrimary = Color(0xFF5E1700),
	primaryContainer = Color(0xFF842F14),
	secondary = Color(0xFFD8C5A6),
	tertiary = Color(0xFFB3CFA9),
	tertiaryContainer = Color(0xFF354D2F),
)

@Composable
fun CookBookTheme(content: @Composable () -> Unit) {
	MaterialTheme(colorScheme = if (isSystemInDarkTheme()) Dark else Light, content = content)
}

/** A recipe photo from the server (cached on disk by Coil), or a placeholder. */
@Composable
fun RecipeImage(recipeId: String, file: String?, modifier: Modifier = Modifier) {
	val placeholder = @Composable {
		Box(modifier.background(MaterialTheme.colorScheme.surfaceVariant), contentAlignment = Alignment.Center) {
			Icon(Icons.Outlined.Restaurant, null, tint = MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.4f))
		}
	}
	if (file.isNullOrEmpty()) {
		placeholder()
		return
	}
	val app = LocalContext.current.applicationContext as CookBookApp
	SubcomposeAsyncImage(
		model = app.api.mediaUrl(recipeId, file),
		contentDescription = null,
		contentScale = ContentScale.Crop,
		modifier = modifier,
		loading = { placeholder() },
		error = { placeholder() },
	)
}
