package io.nekohasekai.swipt.device

import android.app.ActivityThread
import android.app.Application
import android.app.IActivityManager
import android.app.IActivityTaskManager
import android.content.Context
import android.content.pm.IPackageManager
import android.hardware.display.DisplayManager
import android.hardware.display.IDisplayManager
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.os.PowerManager
import android.os.Process
import android.os.ServiceManager
import android.util.Log
import android.view.IWindowManager
import io.nekohasekai.libSwipt.DeviceLayer
import io.nekohasekai.libSwipt.Observer
import io.nekohasekai.libSwipt.ScreenCapture
import io.nekohasekai.libSwipt.ScreenConsumer
import io.nekohasekai.libSwipt.ScreenshotConsumer
import io.nekohasekai.libSwipt.Watch
import java.util.concurrent.Executor
import org.json.JSONArray
import org.json.JSONObject

internal const val TAG = "Swipt"

class Device : DeviceLayer {
    internal val context = systemContext()
    internal val root = Process.myUid() == Process.ROOT_UID
    internal val callingPackage = if (root) "root" else Clipboard.SHELL_PACKAGE
    internal val activities: IActivityManager = IActivityManager.Stub.asInterface(ServiceManager.getService(Context.ACTIVITY_SERVICE))
    internal val tasks: IActivityTaskManager = IActivityTaskManager.Stub.asInterface(ServiceManager.getService("activity_task"))
    internal val packages: IPackageManager = IPackageManager.Stub.asInterface(ServiceManager.getService("package"))
    internal val windows: IWindowManager = IWindowManager.Stub.asInterface(ServiceManager.getService(Context.WINDOW_SERVICE))
    internal val displayService: IDisplayManager = IDisplayManager.Stub.asInterface(ServiceManager.getService(Context.DISPLAY_SERVICE))
    internal val displayManager: DisplayManager = context.getSystemService(DisplayManager::class.java)
    internal val displays = Displays(this)
    private val accessibility = Accessibility()
    private val input = Input()
    private val applications = Applications(this)
    private val notifications = Notifications(this)
    private val screen = Screen(this)
    private val keyguard = Keyguard(this, accessibility, input)
    private val clipboard = Clipboard(context, activities)
    private val states = mapOf(
        "foreground" to applications.foreground,
        "locked" to displays.locked,
        "displays" to displays.all,
        "clipboard" to clipboard.state,
    )
    private val wakeLock by lazy {
        context.getSystemService(PowerManager::class.java).newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, WAKE_LOCK_TAG).apply { setReferenceCounted(false) }
    }
    private var shell: Context? = null

    private fun named(name: String) = states[name] ?: throw Failure(Failure.Name.TypeError, "unknown state $name")

    internal fun checkDisplay(id: Int) {
        if (displayManager.getDisplay(id) == null) throw Failure(Failure.Name.NotFoundError, "no display $id")
    }

    override fun info(): String = Failure.guard("device.info") {
        JSONObject()
            .put("manufacturer", Build.MANUFACTURER)
            .put("model", Build.MODEL)
            .put("os", Build.VERSION.RELEASE)
            .put("sdk", Build.VERSION.SDK_INT)
            .put("build", Build.ID)
            .put("language", activities.configuration.locales[0].toLanguageTag())
            .put("mdnsHostname", Advertiser.supportsHostname)
            .toString()
    }

    override fun languages(): String = Failure.guard("device.languages") { activities.configuration.locales.toLanguageTags() }

    override fun state(name: String): String = Failure.guard("device.state") { named(name).get() }

    override fun watchState(name: String, observer: Observer): Watch = Failure.guard("device.watchState") { named(name).watch(observer) }

    override fun watchEvents(name: String, observer: Observer): Watch = Failure.guard("$name.events") {
        when (name) {
            "applications" -> applications.watch(observer)
            "notifications" -> notifications.watch(observer)
            else -> throw Failure(Failure.Name.TypeError, "unknown events $name")
        }
    }

    override fun setAwake(awake: Boolean) {
        try {
            if (awake) wakeLock.acquire() else wakeLock.release()
        } catch (e: RuntimeException) {
            Log.w(TAG, "keep the device awake", e)
        }
    }

    override fun screenshot(display: Int, consumer: ScreenshotConsumer) = Failure.guard("device.screenshot") { displays.screenshot(display, consumer) }

    override fun unlock(call: Long, unlockType: Int, credential: String) = Failure.guard("device.unlock") {
        Call.run(call) { keyguard.unlock(unlockType, credential) }
    }

    override fun lock(call: Long) = Failure.guard("device.lock") { Call.run(call) { keyguard.lock() } }

    override fun createVirtualDisplay(width: Int, height: Int, densityDpi: Int, name: String): String = Failure.guard("device.createVirtualDisplay") {
        displays.create(width, height, densityDpi, name)
    }

    override fun releaseVirtualDisplay(display: Int) = Failure.guard("device.releaseVirtualDisplay") { displays.release(display) }

    override fun installedApplications(): String = Failure.guard("applications.installed") { applications.installed() }

    override fun applicationIcon(packageName: String): ByteArray = Failure.guard("applications.icon") { applications.icon(packageName) }

    override fun applicationUpdated(packageName: String): Long = Failure.guard("applications.updated") { applications.updated(packageName) }

    override fun launchApplication(call: Long, packageName: String, data: String, restart: Boolean, display: Int) = Failure.guard("applications.launch") {
        Call.run(call) { applications.launch(packageName, data, restart, display) }
    }

    override fun notifications(): String = Failure.guard("notifications.list") { notifications.list() }

    override fun openNotification(key: String) = Failure.guard("notifications.open") { notifications.open(key) }

    override fun dismissNotification(key: String) = Failure.guard("notifications.dismiss") { notifications.dismiss(key) }

    override fun performNotificationAction(request: String) = Failure.guard("notifications.perform") { notifications.perform(request) }

    override fun touch(call: Long, display: Int, fingers: String) = Failure.guard("input.touch") {
        Call.run(call) {
            checkDisplay(display)
            input.touch(display, JSONArray(fingers))
        }
    }

    override fun pressKeys(display: Int, keys: String) = Failure.guard("input.key") {
        checkDisplay(display)
        input.key(display, JSONArray(keys))
    }

    override fun accessibilitySnapshot(call: Long, display: Int): String = Failure.guard("accessibility.snapshot") {
        Call.run(call) {
            if (display >= 0) checkDisplay(display)
            accessibility.snapshot(display)
        }
    }

    override fun waitAccessibilityChange(call: Long, seen: Long, timeoutMs: Long) = Failure.guard("accessibility.changed") {
        Call.run(call) { accessibility.waitForChange(seen, timeoutMs) }
    }

    override fun waitAccessibilityIdle(call: Long, display: Int, idleMs: Long, timeoutMs: Long): Boolean = Failure.guard("accessibility.waitForIdle") {
        Call.run(call) {
            if (display >= 0) checkDisplay(display)
            accessibility.waitForIdle(display, idleMs, timeoutMs)
        }
    }

    override fun aimElement(call: Long, element: String): String = Failure.guard("accessibility.aim") { Call.run(call) { accessibility.aim(element) } }

    override fun setElementText(call: Long, element: String, text: String): String = Failure.guard("accessibility.setText") {
        Call.run(call) { accessibility.setText(element, text) }
    }

    override fun performElementAction(call: Long, element: String, action: String, arguments: String) = Failure.guard("accessibility.perform") {
        Call.run(call) { accessibility.perform(element, action, arguments) }
    }

    override fun interrupt(call: Long) = Call.interrupt(call)

    override fun clipboard(): String = Failure.guard("device.clipboard") { clipboard.get() }

    override fun setClipboard(hasText: Boolean, text: String) = Failure.guard("device.setClipboard") { clipboard.set(text.takeIf { hasText }) }

    override fun advertise(name: String, type: String, port: Int, hostname: String, attributes: String, observer: Observer): Watch = Failure.guard("advertise") {
        val list = if (attributes.isEmpty() || attributes == "null") emptyList() else JSONArray(attributes).let { parsed -> List(parsed.length(), parsed::getString) }
        val advertiser = Advertiser(shellContext(), name, type, port, hostname, list, observer::next, observer::failed)
        advertiser.start()
        Watch(advertiser::stop)
    }

    @Synchronized
    private fun shellContext(): Context = shell ?: Advertiser.shellContext(context).also { shell = it }

    override fun openScreen(display: Int, bitrate: Int, consumer: ScreenConsumer): ScreenCapture = Failure.guard("device.openScreenStream") {
        checkDisplay(display)
        screen.open(display, bitrate, consumer)
    }

    internal fun watchDisplays(handler: Handler, changed: () -> Unit): () -> Unit {
        val listener = object : DisplayManager.DisplayListener {
            override fun onDisplayAdded(displayId: Int) = changed()

            override fun onDisplayRemoved(displayId: Int) = changed()

            override fun onDisplayChanged(displayId: Int) = changed()
        }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.BAKLAVA) {
            displayManager.registerDisplayListener(
                Executor { handler.post(it) },
                DisplayManager.EVENT_TYPE_DISPLAY_ADDED or DisplayManager.EVENT_TYPE_DISPLAY_REMOVED or DisplayManager.EVENT_TYPE_DISPLAY_CHANGED or
                    DisplayManager.EVENT_TYPE_DISPLAY_STATE,
                listener,
            )
        } else {
            displayManager.registerDisplayListener(listener, handler)
        }
        handler.post(changed)
        return { displayManager.unregisterDisplayListener(listener) }
    }

    private class SystemApplication(base: Context) : Application() {
        init {
            attachBaseContext(base)
        }
    }

    companion object {
        private const val WAKE_LOCK_TAG = "swipt:core"

        // Drawables such as AdaptiveIconDrawable read their resources from the application of the ActivityThread.
        internal fun systemContext(): Context {
            val thread = ActivityThread.currentActivityThread() ?: run {
                Looper.prepareMainLooper()
                ActivityThread::class.java.getDeclaredConstructor().apply { isAccessible = true }.newInstance().also { created ->
                    ActivityThread::class.java.getDeclaredField("sCurrentActivityThread").apply { isAccessible = true }.set(null, created)
                    ActivityThread::class.java.getDeclaredField("mSystemThread").apply { isAccessible = true }.setBoolean(created, true)
                }
            }
            val context = thread.systemContext
            if (thread.application == null) {
                ActivityThread::class.java.getDeclaredField("mInitialApplication").apply { isAccessible = true }.set(thread, SystemApplication(context))
            }
            return context
        }
    }
}
