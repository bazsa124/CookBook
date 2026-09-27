package dev.cookbook

import android.app.Application
import coil.ImageLoader
import coil.ImageLoaderFactory
import coil.disk.DiskCache
import dev.cookbook.data.Api
import dev.cookbook.data.Repository
import dev.cookbook.data.Settings
import dev.cookbook.logic.Timers
import kotlinx.coroutines.launch

/** Process-wide singletons; small enough that a DI framework would be ceremony. */
class CookBookApp : Application(), ImageLoaderFactory {
	lateinit var settings: Settings
	lateinit var api: Api
	lateinit var repo: Repository
	lateinit var timers: Timers

	/** Emits the server address after a pairing link filled in the settings. */
	val paired = kotlinx.coroutines.flow.MutableSharedFlow<String>(replay = 1, extraBufferCapacity = 1)

	/** A link to import: "" opens the dialog empty, null closes it. */
	val pendingImport = kotlinx.coroutines.flow.MutableStateFlow<String?>(null)

	override fun onCreate() {
		super.onCreate()
		settings = Settings(this)
		api = Api(settings)
		repo = Repository(this, api, settings)
		timers = Timers(this)
		syncWhenOnline()
	}

	/**
	 * Grocery changes made offline (in the shop, Tailscale down) are sent as soon
	 * as any network is back, without waiting for someone to open the list.
	 */
	private fun syncWhenOnline() {
		val scope = kotlinx.coroutines.CoroutineScope(kotlinx.coroutines.SupervisorJob() + kotlinx.coroutines.Dispatchers.IO)
		val cm = getSystemService(android.net.ConnectivityManager::class.java)
		cm.registerDefaultNetworkCallback(object : android.net.ConnectivityManager.NetworkCallback() {
			override fun onAvailable(network: android.net.Network) {
				scope.launch {
					kotlinx.coroutines.delay(2000) // let Tailscale re-establish its tunnel
					if (repo.pendingOps.value > 0) repo.grocery()
				}
			}
		})
	}

	/**
	 * Coil shares the API's OkHttp client (so photos carry the token) and keeps
	 * a generous disk cache: photos seen once stay available offline.
	 */
	override fun newImageLoader(): ImageLoader = ImageLoader.Builder(this)
		.okHttpClient(api.http)
		.diskCache { DiskCache.Builder().directory(cacheDir.resolve("images")).maxSizeBytes(200L * 1024 * 1024).build() }
		.respectCacheHeaders(false)
		.crossfade(true)
		.build()
}
