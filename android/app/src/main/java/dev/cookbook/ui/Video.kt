package dev.cookbook.ui

import android.content.Intent
import android.net.Uri
import android.widget.MediaController
import android.widget.VideoView
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.outlined.OndemandVideo
import androidx.compose.material3.AssistChip
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import dev.cookbook.data.isVideo
import dev.cookbook.data.posterName

/** A step's photo or video. */
@Composable
fun StepMedia(recipeId: String, file: String, modifier: Modifier) {
	if (isVideo(file)) StepVideo(recipeId, file, modifier)
	else RecipeImage(recipeId, file, modifier)
}

/**
 * Poster frame with a play button; tapping streams the video from the server
 * (with seeking) in the platform player. Videos are not cached for offline
 * use: they would crowd the photo cache out.
 */
@Composable
fun StepVideo(recipeId: String, file: String, modifier: Modifier) {
	val app = LocalApp.current
	var playing by remember(recipeId, file) { mutableStateOf(false) }
	if (!playing) {
		Box(modifier.clickable { playing = true }, contentAlignment = Alignment.Center) {
			RecipeImage(recipeId, posterName(file), Modifier.fillMaxSize())
			Icon(
				Icons.Filled.PlayArrow, LocalStrings.current.video, tint = Color.White,
				modifier = Modifier.size(56.dp).background(Color.Black.copy(alpha = 0.5f), CircleShape),
			)
		}
		return
	}
	val url = app.api.mediaUrl(recipeId, file)
	val token = app.settings.token
	var view by remember { mutableStateOf<VideoView?>(null) }
	DisposableEffect(url) { onDispose { view?.stopPlayback() } }
	AndroidView(
		modifier = modifier.background(Color.Black),
		factory = { ctx ->
			VideoView(ctx).apply {
				view = this
				val controls = MediaController(ctx)
				controls.setAnchorView(this)
				setMediaController(controls)
				// The platform player sends these headers on every range request.
				setVideoURI(Uri.parse(url), mapOf("Authorization" to "Bearer $token"))
				setOnPreparedListener { start() }
			}
		},
	)
}

/** "Watch video" for a recipe's external video link (opens YouTube etc.). */
@Composable
fun VideoLinkChip(url: String) {
	val context = LocalContext.current
	AssistChip(
		onClick = { runCatching { context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url))) } },
		label = { Text(LocalStrings.current.watchVideo) },
		leadingIcon = { Icon(Icons.Outlined.OndemandVideo, null) },
	)
}
