package dev.cookbook.logic

import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.media.AudioAttributes
import android.media.Ringtone
import android.media.RingtoneManager
import android.os.SystemClock
import android.os.VibrationEffect
import android.os.VibratorManager
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch

data class KitchenTimer(
	val id: Int,
	val label: String,
	val recipe: String,
	val totalSeconds: Int,
	/** elapsedRealtime at which it rings; unaffected by clock changes. */
	val endsAt: Long,
	val pausedLeft: Int? = null,
	val rang: Boolean = false,
) {
	fun left(now: Long): Int = pausedLeft ?: ((endsAt - now + 999) / 1000).toInt().coerceAtLeast(0)
}

/**
 * The inline [MM:SS] timers. Several can run at once (pasta and sauce). They
 * live in the app process, tick on one coroutine, and ring with a sound, a
 * vibration and a notification so they are heard from another app too.
 */
class Timers(private val context: Context) {
	private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main)
	private val _timers = MutableStateFlow<List<KitchenTimer>>(emptyList())
	val timers: StateFlow<List<KitchenTimer>> = _timers.asStateFlow()

	private val _now = MutableStateFlow(SystemClock.elapsedRealtime())
	val now: StateFlow<Long> = _now.asStateFlow()

	private var nextId = 1
	private var ringtone: Ringtone? = null

	init {
		val ch = NotificationChannel(CHANNEL, "Kitchen timers", NotificationManager.IMPORTANCE_HIGH)
		context.getSystemService(NotificationManager::class.java).createNotificationChannel(ch)
		scope.launch {
			while (isActive) {
				val now = SystemClock.elapsedRealtime()
				_now.value = now
				val due = _timers.value.filter { !it.rang && it.pausedLeft == null && it.endsAt <= now }
				if (due.isNotEmpty()) {
					_timers.value = _timers.value.map { t -> if (due.any { it.id == t.id }) t.copy(rang = true) else t }
					due.forEach { ring(it) }
				}
				delay(250)
			}
		}
	}

	fun start(label: String, recipe: String, seconds: Int) {
		val t = KitchenTimer(nextId++, label, recipe, seconds, SystemClock.elapsedRealtime() + seconds * 1000L)
		_timers.value = _timers.value + t
	}

	fun pauseResume(id: Int) {
		val now = SystemClock.elapsedRealtime()
		_timers.value = _timers.value.map { t ->
			when {
				t.id != id || t.rang -> t
				t.pausedLeft == null -> t.copy(pausedLeft = t.left(now))
				else -> t.copy(endsAt = now + t.pausedLeft * 1000L, pausedLeft = null)
			}
		}
	}

	fun dismiss(id: Int) {
		_timers.value = _timers.value.filterNot { it.id == id }
		NotificationManagerCompat.from(context).cancel(id)
		if (_timers.value.none { it.rang }) {
			ringtone?.stop(); ringtone = null
		}
	}

	private fun ring(t: KitchenTimer) {
		runCatching {
			val uri = RingtoneManager.getDefaultUri(RingtoneManager.TYPE_ALARM)
				?: RingtoneManager.getDefaultUri(RingtoneManager.TYPE_NOTIFICATION)
			if (ringtone?.isPlaying != true) {
				ringtone = RingtoneManager.getRingtone(context, uri)?.apply {
					audioAttributes = AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_ALARM).build()
					isLooping = true
					play()
				}
			}
		}
		runCatching {
			val vib = context.getSystemService(VibratorManager::class.java).defaultVibrator
			vib.vibrate(VibrationEffect.createWaveform(longArrayOf(0, 400, 200, 400, 200, 400), -1))
		}
		val allowed = androidx.core.content.ContextCompat.checkSelfPermission(
			context, android.Manifest.permission.POST_NOTIFICATIONS,
		) == android.content.pm.PackageManager.PERMISSION_GRANTED
		if (allowed) runCatching {
			val open = PendingIntent.getActivity(
				context, t.id,
				context.packageManager.getLaunchIntentForPackage(context.packageName)!!
					.addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP),
				PendingIntent.FLAG_IMMUTABLE,
			)
			val n = NotificationCompat.Builder(context, CHANNEL)
				.setSmallIcon(android.R.drawable.ic_lock_idle_alarm)
				.setContentTitle("⏰ ${t.label}")
				.setContentText(t.recipe)
				.setPriority(NotificationCompat.PRIORITY_HIGH)
				.setCategory(NotificationCompat.CATEGORY_ALARM)
				.setContentIntent(open)
				.setAutoCancel(true)
				.build()
			NotificationManagerCompat.from(context).notify(t.id, n)
		}
	}

	private companion object {
		const val CHANNEL = "timers"
	}
}
