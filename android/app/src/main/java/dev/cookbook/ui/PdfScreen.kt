package dev.cookbook.ui

import android.content.Intent
import android.graphics.Bitmap
import android.graphics.Color as AColor
import android.graphics.pdf.PdfRenderer
import android.os.ParcelFileDescriptor
import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.outlined.Share
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.core.content.FileProvider
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavHostController
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.File

/**
 * The server-compiled Typst PDF, rendered with the platform PdfRenderer (no
 * library). The downloaded file stays in the cache, so it opens offline too.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun PdfScreen(nav: NavHostController, id: String) {
	val app = LocalApp.current
	val s = LocalStrings.current
	val context = LocalContext.current
	val lang by app.settings.lang.collectAsStateWithLifecycle()
	var pages by remember { mutableStateOf<List<Bitmap>>(emptyList()) }
	var error by remember { mutableStateOf<String?>(null) }
	val file = remember(id, lang) { File(context.cacheDir, "pdf/$id-$lang.pdf") }

	LaunchedEffect(id, lang) {
		pages = emptyList(); error = null
		val fetched = runCatching { app.api.pdf(id, lang, file) }
		if (fetched.isFailure && !file.exists()) {
			error = "${s.noPdf}: ${fetched.exceptionOrNull()?.message}"
			return@LaunchedEffect
		}
		pages = withContext(Dispatchers.IO) { render(file, context.resources.displayMetrics.widthPixels) }
	}

	Scaffold(
		topBar = {
			TopAppBar(
				title = { Text(s.pdf) },
				navigationIcon = { IconButton(onClick = { nav.popBackStack() }) { Icon(Icons.AutoMirrored.Outlined.ArrowBack, null) } },
				actions = {
					LangToggle()
					IconButton(enabled = pages.isNotEmpty(), onClick = {
						val uri = FileProvider.getUriForFile(context, "${context.packageName}.files", file)
						val send = Intent(Intent.ACTION_SEND).setType("application/pdf")
							.putExtra(Intent.EXTRA_STREAM, uri).addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
						context.startActivity(Intent.createChooser(send, s.share))
					}) { Icon(Icons.Outlined.Share, s.share) }
				},
			)
		},
	) { pad ->
		Box(Modifier.padding(pad).fillMaxSize(), contentAlignment = Alignment.Center) {
			when {
				error != null -> Text(error!!, Modifier.padding(24.dp))
				pages.isEmpty() -> CircularProgressIndicator()
				else -> LazyColumn(verticalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.fillMaxSize()) {
					items(pages) { bmp ->
						Image(
							bmp.asImageBitmap(), null,
							Modifier.fillMaxWidth().aspectRatio(bmp.width.toFloat() / bmp.height),
						)
					}
				}
			}
		}
	}
}

private fun render(file: File, width: Int): List<Bitmap> =
	ParcelFileDescriptor.open(file, ParcelFileDescriptor.MODE_READ_ONLY).use { fd ->
		PdfRenderer(fd).use { pdf ->
			(0 until pdf.pageCount).map { i ->
				pdf.openPage(i).use { page ->
					val w = width.coerceAtMost(1600)
					val bmp = Bitmap.createBitmap(w, w * page.height / page.width, Bitmap.Config.ARGB_8888)
					bmp.eraseColor(AColor.WHITE)
					page.render(bmp, null, null, PdfRenderer.Page.RENDER_MODE_FOR_DISPLAY)
					bmp
				}
			}
		}
	}
