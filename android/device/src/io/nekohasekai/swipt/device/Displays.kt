package io.nekohasekai.swipt.device

import android.content.pm.PackageManager
import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.ColorSpace
import android.graphics.PixelFormat
import android.graphics.Point
import android.hardware.HardwareBuffer
import android.hardware.display.DisplayManager
import android.hardware.display.IVirtualDisplayCallback
import android.hardware.display.VirtualDisplayConfig
import android.media.ImageReader
import android.os.Build
import android.os.Handler
import android.os.HandlerThread
import android.os.SystemClock
import android.util.DisplayMetrics
import android.view.Display
import android.view.Surface
import android.window.ScreenCapture
import android.window.ScreenCaptureInternal
import com.android.internal.policy.IKeyguardLockedStateListener
import io.nekohasekai.libSwipt.ScreenshotConsumer
import java.nio.ByteBuffer
import java.util.concurrent.ConcurrentHashMap
import org.json.JSONArray
import org.json.JSONObject

internal class VirtualDisplayToken : IVirtualDisplayCallback.Stub() {
    override fun onPaused() {}

    override fun onResumed() {}

    override fun onStopped() {}
}

internal class Displays(private val device: Device) {
    // A virtual display without a surface is off, so one that no capture reads draws into a reader that drops its frames.
    private class Virtual(val token: IVirtualDisplayCallback, val idle: ImageReader, val thread: HandlerThread)

    private val created = ConcurrentHashMap<Int, Virtual>()
    val all = State("displays", ::list) { changed ->
        val thread = HandlerThread("Displays").apply { start() }
        val unwatch = device.watchDisplays(Handler(thread.looper), changed)
        AutoCloseable {
            unwatch()
            thread.quitSafely()
        }
    }
    val locked = State("locked", device.windows::isKeyguardLocked) { changed ->
        val listener = object : IKeyguardLockedStateListener.Stub() {
            override fun onKeyguardLockedStateChanged(isKeyguardLocked: Boolean) = changed()
        }
        device.windows.addKeyguardLockedStateListener(listener)
        AutoCloseable { device.windows.removeKeyguardLockedStateListener(listener) }
    }

    fun owns(id: Int) = created.containsKey(id)

    @Synchronized
    fun attach(id: Int, surface: Surface) {
        created[id]?.let { device.displayService.setVirtualDisplaySurface(it.token, surface) }
    }

    @Synchronized
    fun detach(id: Int) {
        created[id]?.let { device.displayService.setVirtualDisplaySurface(it.token, it.idle.surface) }
    }

    private fun list(): JSONArray {
        val displays = device.displayManager.displays.sortedBy { if (it.displayId == Display.DEFAULT_DISPLAY) Int.MIN_VALUE else it.displayId }
        return JSONArray(displays.map { describe(it) })
    }

    fun create(width: Int, height: Int, densityDpi: Int, name: String): String {
        var flags = DisplayManager.VIRTUAL_DISPLAY_FLAG_PUBLIC or DisplayManager.VIRTUAL_DISPLAY_FLAG_OWN_CONTENT_ONLY or
            VIRTUAL_DISPLAY_FLAG_TRUSTED or VIRTUAL_DISPLAY_FLAG_SUPPORTS_TOUCH
        if (device.root || device.context.checkSelfPermission(ADD_ALWAYS_UNLOCKED_DISPLAY) == PackageManager.PERMISSION_GRANTED) {
            // DisplayManagerService drops ALWAYS_UNLOCKED from a display in the default display group.
            flags = flags or VIRTUAL_DISPLAY_FLAG_OWN_DISPLAY_GROUP or VIRTUAL_DISPLAY_FLAG_ALWAYS_UNLOCKED
        }
        val thread = HandlerThread("VirtualDisplay").apply { start() }
        val idle = ImageReader.newInstance(width, height, PixelFormat.RGBA_8888, 2, HardwareBuffer.USAGE_GPU_SAMPLED_IMAGE)
        idle.setOnImageAvailableListener({ it.acquireLatestImage()?.close() }, Handler(thread.looper))
        val token = VirtualDisplayToken()
        val id = try {
            val config = VirtualDisplayConfig.Builder(name, width, height, densityDpi)
                .setFlags(flags)
                .setSurface(idle.surface)
                .build()
            device.displayService.createVirtualDisplay(config, token, null, if (device.root) null else device.callingPackage).also {
                if (it < 0) throw Failure(Failure.Name.Error, "the system did not create the display")
            }
        } catch (e: Exception) {
            idle.close()
            thread.quitSafely()
            throw e
        }
        created[id] = Virtual(token, idle, thread)
        // DisplayManagerService lists a virtual display before it powers it on, and after it releases it.
        var display = device.displayManager.getDisplay(id)
        val deadline = SystemClock.uptimeMillis() + SETTLE_LIMIT_MS
        while (display != null && display.state == Display.STATE_UNKNOWN && SystemClock.uptimeMillis() < deadline) {
            Thread.sleep(POLL_MS)
            display = device.displayManager.getDisplay(id)
        }
        if (display == null) throw Failure(Failure.Name.InvalidStateError, "display $id was removed as it was created")
        return describe(display).toString()
    }

    @Synchronized
    fun release(id: Int) {
        val virtual = created.remove(id) ?: run {
            device.checkDisplay(id)
            throw Failure(Failure.Name.NotAllowedError, "display $id was not created by Swipt")
        }
        try {
            device.displayService.releaseVirtualDisplay(virtual.token)
        } finally {
            virtual.idle.close()
            virtual.thread.quitSafely()
        }
        val deadline = SystemClock.uptimeMillis() + SETTLE_LIMIT_MS
        while (device.displayManager.getDisplay(id) != null && SystemClock.uptimeMillis() < deadline) {
            Thread.sleep(POLL_MS)
        }
    }

    fun screenshot(display: Int, consumer: ScreenshotConsumer) {
        device.checkDisplay(display)
        val buffer: HardwareBuffer
        val colorSpace: ColorSpace
        if (CAPTURE_INTERNAL) {
            val listener = ScreenCaptureInternal.createSyncCaptureListener()
            device.windows.captureDisplay(display, ScreenCaptureInternal.CaptureArgs.Builder().setSecureContentPolicy(SECURE_CONTENT_POLICY_CAPTURE).build(), listener)
            val captured = listener.buffer ?: throw Failure(Failure.Name.UnavailableError, "the display has nothing to capture")
            buffer = captured.hardwareBuffer
            colorSpace = captured.colorSpace
        } else {
            val listener = ScreenCapture.createSyncCaptureListener()
            device.windows.captureDisplay(display, ScreenCapture.CaptureArgs.Builder().setCaptureSecureLayers(true).build(), listener)
            val captured = listener.buffer ?: throw Failure(Failure.Name.UnavailableError, "the display has nothing to capture")
            buffer = captured.hardwareBuffer
            colorSpace = captured.colorSpace
        }
        val copied = buffer.use {
            val hardware = Bitmap.wrapHardwareBuffer(buffer, colorSpace) ?: throw IllegalStateException("the screenshot is in a format that a Bitmap can't hold")
            hardware.copy(Bitmap.Config.ARGB_8888, false).also { hardware.recycle() }
        }
        val bitmap = if (copied.colorSpace == SRGB) {
            copied
        } else {
            Bitmap.createBitmap(copied.width, copied.height, Bitmap.Config.ARGB_8888, true, SRGB).also {
                Canvas(it).drawBitmap(copied, 0f, 0f, null)
                copied.recycle()
            }
        }
        val pixels = ByteArray(bitmap.rowBytes * bitmap.height)
        bitmap.copyPixelsToBuffer(ByteBuffer.wrap(pixels))
        consumer.pixels(pixels, bitmap.width, bitmap.height, bitmap.rowBytes)
        bitmap.recycle()
    }

    companion object {
        private const val SECURE_CONTENT_POLICY_CAPTURE = 1

        // Android 16 QPR2 renamed ScreenCapture to ScreenCaptureInternal, and replaced setCaptureSecureLayers with setSecureContentPolicy.
        private val CAPTURE_INTERNAL = Build.VERSION.SDK_INT > Build.VERSION_CODES.BAKLAVA ||
            Build.VERSION.SDK_INT == Build.VERSION_CODES.BAKLAVA && Build.VERSION.SDK_INT_FULL > Build.VERSION_CODES_FULL.BAKLAVA

        // DisplayManager.VIRTUAL_DISPLAY_FLAG_*
        private const val VIRTUAL_DISPLAY_FLAG_SUPPORTS_TOUCH = 1 shl 6
        private const val VIRTUAL_DISPLAY_FLAG_TRUSTED = 1 shl 10
        private const val VIRTUAL_DISPLAY_FLAG_OWN_DISPLAY_GROUP = 1 shl 11
        private const val VIRTUAL_DISPLAY_FLAG_ALWAYS_UNLOCKED = 1 shl 12

        // Display.TYPE_*
        private const val TYPE_EXTERNAL = 2
        private const val TYPE_WIFI = 3
        private const val TYPE_OVERLAY = 4
        private const val TYPE_VIRTUAL = 5
        private const val SETTLE_LIMIT_MS = 2000L
        private const val POLL_MS = 10L
        private const val ADD_ALWAYS_UNLOCKED_DISPLAY = "android.permission.ADD_ALWAYS_UNLOCKED_DISPLAY"
        private val GET_TYPE = Display::class.java.getMethod("getType")
        private val SRGB = ColorSpace.get(ColorSpace.Named.SRGB)

        private fun describe(display: Display): JSONObject {
            val size = Point().also(display::getRealSize)
            val metrics = DisplayMetrics().also(display::getRealMetrics)
            return JSONObject()
                .put("id", display.displayId)
                .put(
                    "type",
                    when (GET_TYPE.invoke(display) as Int) {
                        TYPE_EXTERNAL -> "external"
                        TYPE_WIFI -> "wifi"
                        TYPE_OVERLAY -> "overlay"
                        TYPE_VIRTUAL -> "virtual"
                        else -> "internal"
                    },
                )
                .put("name", display.name)
                .put("width", size.x)
                .put("height", size.y)
                .put("densityDpi", metrics.densityDpi)
                .put("rotation", display.rotation * 90)
                .put(
                    "state",
                    when (display.state) {
                        Display.STATE_OFF -> "off"
                        Display.STATE_ON -> "on"
                        Display.STATE_DOZE -> "doze"
                        Display.STATE_DOZE_SUSPEND -> "dozeSuspend"
                        Display.STATE_VR -> "vr"
                        Display.STATE_ON_SUSPEND -> "onSuspend"
                        else -> "unknown"
                    },
                )
        }
    }
}
