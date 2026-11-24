package io.nekohasekai.swipt.app

import android.app.Activity
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.util.Log
import androidx.activity.ComponentActivity
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.ProcessLifecycleOwner
import androidx.lifecycle.lifecycleScope
import io.nekohasekai.swipt.R
import io.nekohasekai.swipt.automation.SwipletActivity
import io.nekohasekai.swipt.automation.SwipletHost
import kotlinx.coroutines.launch

object ErrorAlert {
    private const val TAG = "ErrorAlert"
    private const val CHANNEL = "errors"
    private const val NOTIFICATION = 2

    private val main = Handler(Looper.getMainLooper())
    private val queued = mutableListOf<AlertState>()
    private val notifications: NotificationManager get() = SwiptApplication.application.getSystemService(NotificationManager::class.java)

    var alert by mutableStateOf<AlertState?>(null)
        private set

    fun report(failure: String, error: Throwable) {
        report(AlertState.failure(failure, error))
    }

    fun report(alert: AlertState, swiplet: String? = null) {
        Log.e(TAG, "${alert.title}: ${alert.message}")
        main.post {
            if (enqueue(alert) && !ProcessLifecycleOwner.get().lifecycle.currentState.isAtLeast(Lifecycle.State.STARTED)) post(alert, swiplet)
        }
    }

    fun reopen(title: String, message: String, copyText: String?) {
        enqueue(AlertState(title, message, copyText = copyText))
    }

    fun dismiss() {
        alert?.let { notifications.cancel(tag(it), NOTIFICATION) }
        alert = null
        if (queued.isEmpty()) return
        main.post {
            if (alert == null && queued.isNotEmpty()) alert = queued.removeAt(0)
        }
    }

    private fun enqueue(alert: AlertState): Boolean {
        if ((listOfNotNull(this.alert) + queued).any { it.matches(alert) }) return false
        if (this.alert == null && queued.isEmpty()) {
            this.alert = alert
        } else {
            queued.add(alert)
        }
        return true
    }

    private fun post(alert: AlertState, swiplet: String?) {
        val application = SwiptApplication.application
        notifications.createNotificationChannel(NotificationChannel(CHANNEL, application.getString(R.string.errors_channel), NotificationManager.IMPORTANCE_HIGH))
        val tag = tag(alert)
        val reopen = Intent(application, ErrorAlertActivity::class.java)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            .putExtra(ErrorAlertActivity.EXTRA_TITLE, alert.title)
            .putExtra(ErrorAlertActivity.EXTRA_MESSAGE, alert.message)
            .putExtra(ErrorAlertActivity.EXTRA_COPY, alert.copyText)
            .putExtra(ErrorAlertActivity.EXTRA_SWIPLET, swiplet)
        val notification = Notification.Builder(application, CHANNEL)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle(alert.title)
            .setContentText(alert.message)
            .setStyle(Notification.BigTextStyle().bigText(alert.message))
            .setCategory(Notification.CATEGORY_ERROR)
            .setContentIntent(PendingIntent.getActivity(application, tag.hashCode(), reopen, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT))
            .setAutoCancel(true)
            .build()
        notifications.notify(tag, NOTIFICATION, notification)
    }

    private fun tag(alert: AlertState): String = "${alert.title}\n${alert.message}"

    private fun AlertState.matches(other: AlertState): Boolean = title == other.title && message == other.message
}

class ErrorAlertActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        ErrorAlert.reopen(intent.getStringExtra(EXTRA_TITLE).orEmpty(), intent.getStringExtra(EXTRA_MESSAGE).orEmpty(), intent.getStringExtra(EXTRA_COPY))
        val swiplet = intent.getStringExtra(EXTRA_SWIPLET)
        if (swiplet == null || SwipletHost.isPresented(swiplet)) {
            MainActivity.reveal(this)
            finish()
            return
        }
        lifecycleScope.launch {
            SwipletActivity.openWhenReady(this@ErrorAlertActivity, swiplet)
            finish()
        }
    }

    companion object {
        const val EXTRA_TITLE = "io.nekohasekai.swipt.extra.TITLE"
        const val EXTRA_MESSAGE = "io.nekohasekai.swipt.extra.MESSAGE"
        const val EXTRA_COPY = "io.nekohasekai.swipt.extra.COPY"
        const val EXTRA_SWIPLET = "io.nekohasekai.swipt.extra.SWIPLET"
    }
}

fun Context.openLink(url: String) {
    val intent = Intent(Intent.ACTION_VIEW, Uri.parse(url))
    if (this !is Activity) intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
    try {
        startActivity(intent)
    } catch (error: ActivityNotFoundException) {
        ErrorAlert.report(getString(R.string.couldnt_open_link), error)
    }
}
