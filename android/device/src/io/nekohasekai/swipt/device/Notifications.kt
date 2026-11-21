package io.nekohasekai.swipt.device

import android.app.ActivityOptions
import android.app.Notification
import android.app.PendingIntent
import android.app.RemoteInput
import android.content.ComponentName
import android.content.Context
import android.content.ContextWrapper
import android.content.Intent
import android.os.Build
import android.os.Bundle
import android.os.HandlerThread
import android.os.ServiceManager
import android.service.notification.NotificationListenerService
import android.service.notification.StatusBarNotification
import com.android.internal.statusbar.IStatusBarService
import com.android.internal.statusbar.NotificationVisibility
import io.nekohasekai.libSwipt.Observer
import io.nekohasekai.libSwipt.Watch
import java.util.concurrent.CopyOnWriteArraySet
import org.json.JSONArray
import org.json.JSONObject

// NotificationManagerService stops trusting the listeners of a uid, and redacts sensitive notifications for them, once one
// of its system listeners unregisters, so the listener stays registered.
internal class Notifications(private val device: Device) {
    private val statusBar = IStatusBarService.Stub.asInterface(ServiceManager.getService("statusbar"))
    private val observers = CopyOnWriteArraySet<Observer>()
    private var listener: Listener? = null

    private inner class Listener : NotificationListenerService() {
        override fun onNotificationPosted(notification: StatusBarNotification) = publish("posted", notification)

        override fun onNotificationRemoved(notification: StatusBarNotification) = publish("removed", notification)

        private fun publish(type: String, notification: StatusBarNotification) {
            if (observers.isEmpty()) return
            val event = JSONObject()
                .put("type", type)
                .put("notification", describe(notification))
                .put("time", System.currentTimeMillis())
                .toString()
            observers.forEach { it.next(event) }
        }
    }

    @Synchronized
    private fun listener(): Listener = listener ?: run {
        val thread = HandlerThread("Notifications").apply { start() }
        val looper = thread.looper
        val context = object : ContextWrapper(device.context) {
            override fun getMainLooper() = looper
        }
        val registering = Listener()
        try {
            REGISTER.invoke(registering, context, COMPONENT, USER_ALL)
        } catch (e: Exception) {
            thread.quitSafely()
            throw e
        }
        registering.also { listener = it }
    }

    fun watch(observer: Observer): Watch {
        listener()
        observers += observer
        return Watch { observers -= observer }
    }

    fun list(): String = JSONArray(active(null).map { describe(it) }).toString()

    private fun active(keys: Array<String>?): Array<StatusBarNotification> =
        listener().getActiveNotifications(keys) ?: throw Failure(Failure.Name.Error, "the notification service did not list the notifications")

    private fun find(key: String): StatusBarNotification =
        active(arrayOf(key)).firstOrNull() ?: throw Failure(Failure.Name.NotFoundError, "the notification $key is gone")

    fun open(key: String) {
        val content = find(key).notification.contentIntent ?: throw Failure(Failure.Name.InvalidStateError, "the notification $key opens nothing")
        send(content, null)
        statusBar.onNotificationClick(key, NotificationVisibility.obtain(key, 0, 1, true))
    }

    fun dismiss(key: String) {
        if (find(key).isOngoing) throw Failure(Failure.Name.NotAllowedError, "the notification $key is ongoing, which only its app can remove")
        listener().cancelNotification(key)
    }

    fun perform(request: String) {
        val given = JSONObject(request)
        val key = given.getString("key")
        val title = given.getString("title")
        val action = find(key).notification.actions?.getOrNull(given.getInt("index"))?.takeIf { titleOf(it) == title }
            ?: throw Failure(Failure.Name.NotFoundError, "the notification $key no longer offers the action ${JSONObject.quote(title)}")
        val intent = action.actionIntent ?: throw Failure(Failure.Name.InvalidStateError, "the action ${JSONObject.quote(title)} does nothing")
        val input = replyInput(action)
        val results = when {
            given.has("reply") -> {
                if (input == null) throw Failure(Failure.Name.TypeError, "the action ${JSONObject.quote(title)} takes no reply")
                Intent().also {
                    RemoteInput.addResultsToIntent(action.remoteInputs, it, Bundle().apply { putCharSequence(input.resultKey, given.getString("reply")) })
                    RemoteInput.setResultsSource(it, RemoteInput.SOURCE_FREE_FORM_INPUT)
                }
            }
            input != null -> throw Failure(Failure.Name.TypeError, "the action ${JSONObject.quote(title)} needs a reply")
            else -> null
        }
        send(intent, results)
    }

    // PendingIntentRecord lets the START_ACTIVITIES_FROM_BACKGROUND of the sender start an activity only when the options
    // opt in, which on Android 17 takes MODE_BACKGROUND_ACTIVITY_START_ALLOW_ALWAYS.
    private fun send(intent: PendingIntent, fillIn: Intent?) {
        val mode = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.BAKLAVA) {
            ActivityOptions.MODE_BACKGROUND_ACTIVITY_START_ALLOW_ALWAYS
        } else {
            ActivityOptions.MODE_BACKGROUND_ACTIVITY_START_ALLOWED
        }
        val options = ActivityOptions.makeBasic().setPendingIntentBackgroundActivityStartMode(mode).toBundle()
        try {
            intent.send(device.context, 0, fillIn, null, null, null, options)
        } catch (_: PendingIntent.CanceledException) {
            throw Failure(Failure.Name.InvalidStateError, "the app canceled the intent of the notification")
        }
    }

    companion object {
        private val COMPONENT = ComponentName(Clipboard.SHELL_PACKAGE, Listener::class.java.name)
        private const val USER_ALL = -1
        private val REGISTER = NotificationListenerService::class.java.getMethod("registerAsSystemService", Context::class.java, ComponentName::class.java, Int::class.javaPrimitiveType)

        private fun replyInput(action: Notification.Action) = action.remoteInputs?.firstOrNull { it.allowFreeFormInput }

        private fun titleOf(action: Notification.Action) = action.title?.toString().orEmpty()

        private fun describe(notification: StatusBarNotification): JSONObject {
            val content = notification.notification
            return JSONObject()
                .put("key", notification.key)
                .put("packageName", notification.packageName)
                .put("time", notification.postTime)
                .putText("title", content.extras.getCharSequence(Notification.EXTRA_TITLE))
                .putText("text", content.extras.getCharSequence(Notification.EXTRA_TEXT))
                .putText("subText", content.extras.getCharSequence(Notification.EXTRA_SUB_TEXT))
                .putText("category", content.category)
                .putFlag("ongoing", notification.isOngoing)
                .putFlag("groupSummary", (content.flags and Notification.FLAG_GROUP_SUMMARY) != 0)
                .put("actions", JSONArray(content.actions.orEmpty().map { JSONObject().put("title", titleOf(it)).putFlag("reply", replyInput(it) != null) }))
        }
    }
}
