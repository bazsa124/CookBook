package dev.cookbook

import android.Manifest
import android.content.Intent
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import dev.cookbook.ui.CookBookRoot

class MainActivity : ComponentActivity() {
	override fun onCreate(savedInstanceState: Bundle?) {
		super.onCreate(savedInstanceState)
		enableEdgeToEdge()
		// Timers must be able to ring from the background.
		registerForActivityResult(ActivityResultContracts.RequestPermission()) {}
			.launch(Manifest.permission.POST_NOTIFICATIONS)
		if (savedInstanceState == null) {
			handlePairing(intent)
			handleShare(intent)
		}
		setContent { CookBookRoot(application as CookBookApp) }
	}

	override fun onNewIntent(intent: Intent) {
		super.onNewIntent(intent)
		handlePairing(intent)
		handleShare(intent)
	}

	/** "Share → CookBook" from a browser: import the shared recipe page. */
	private fun handleShare(intent: Intent?) {
		if (intent?.action != Intent.ACTION_SEND) return
		val text = intent.getStringExtra(Intent.EXTRA_TEXT) ?: return
		val url = Regex("""https?://\S+""").find(text)?.value ?: return
		(application as CookBookApp).pendingImport.value = url
	}

	/** cookbook://pair?server=100.x.y.z:8738&token=... from the server's pairing QR. */
	private fun handlePairing(intent: Intent?) {
		val uri = intent?.data ?: return
		if (uri.scheme != "cookbook" || uri.host != "pair") return
		val server = uri.getQueryParameter("server").orEmpty()
		val token = uri.getQueryParameter("token").orEmpty()
		if (server.isEmpty() || token.isEmpty()) return
		val app = application as CookBookApp
		app.settings.server = server
		app.settings.token = token
		app.settings.exportEtag = ""
		app.paired.tryEmit(server)
	}
}
