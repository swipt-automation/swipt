package io.nekohasekai.swipt.device

import android.content.pm.PackageManager
import android.graphics.Point
import android.graphics.SurfaceTexture
import android.hardware.display.DisplayManager
import android.hardware.display.IVirtualDisplayCallback
import android.hardware.display.VirtualDisplayConfig
import android.media.MediaCodec
import android.media.MediaCodecInfo
import android.media.MediaCodecList
import android.media.MediaFormat
import android.opengl.EGL14
import android.opengl.EGLConfig
import android.opengl.EGLContext
import android.opengl.EGLDisplay
import android.opengl.EGLExt
import android.opengl.EGLSurface
import android.opengl.GLES11Ext
import android.opengl.GLES20
import android.os.Bundle
import android.os.Handler
import android.os.HandlerThread
import android.os.RemoteException
import android.util.Log
import android.util.Range
import android.view.Display
import android.view.Surface
import io.nekohasekai.libSwipt.ScreenConsumer
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.nio.FloatBuffer
import java.util.concurrent.Callable
import java.util.concurrent.ConcurrentSkipListMap
import java.util.concurrent.ExecutionException
import java.util.concurrent.FutureTask
import java.util.concurrent.locks.ReentrantLock
import kotlin.concurrent.withLock
import kotlin.math.max
import kotlin.math.min
import kotlin.math.roundToInt

internal class Capture(
    private val device: Device,
    private val displayId: Int,
    bitrate: Int,
    private val consumer: ScreenConsumer,
    private val windowFailed: (Exception, Long) -> Unit,
) : AutoCloseable {
    data class Picture(val width: Int, val height: Int, val codedWidth: Int, val codedHeight: Int, val scale: Double)

    private class Program(val name: Int, val coordinate: Int, val transformation: Int)

    private class Target(val surface: EGLSurface, val window: Surface, val x: Int, val y: Int, val width: Int, val height: Int)

    val drawn: Picture
    val encoded: Picture
    private val own = device.displays.owns(displayId)
    private val sides: Point
    private val encoder: MediaCodecInfo
    private val bitrates: Range<Int>

    @Volatile
    var bitrate: Int
        private set

    init {
        val display = display()
        val rotation = display.rotation
        val size = Point().also(display::getRealSize)
        sides = sides(display)
        val naturalWidth = if (rotation % 2 == 0) size.x else size.y
        val naturalHeight = if (rotation % 2 == 0) size.y else size.x
        drawn = Picture(naturalWidth, naturalHeight, naturalWidth, naturalHeight, 1.0)
        encoder = encoder()
        val video = encoder.getCapabilitiesForType(MediaFormat.MIMETYPE_VIDEO_HEVC).videoCapabilities!!
        bitrates = video.bitrateRange
        this.bitrate = bitrates.clamp(bitrate)
        encoded = fit(video, naturalWidth, naturalHeight)
    }

    private val turns = ConcurrentSkipListMap<Long, Int>()
    private val changes = ReentrantLock()
    private val changed = changes.newCondition()
    private val codecs = Any()
    private var codec: MediaCodec? = null
    private var input: Surface? = null

    @Volatile
    private var failure: Exception? = null

    @Volatile
    private var resized = false

    @Volatile
    private var encoding = false
    private var eglDisplay: EGLDisplay = EGL14.EGL_NO_DISPLAY
    private lateinit var eglConfig: EGLConfig
    private var eglContext: EGLContext = EGL14.EGL_NO_CONTEXT
    private var idle: EGLSurface = EGL14.EGL_NO_SURFACE
    private lateinit var plain: Program
    private lateinit var swapped: Program
    private var recorded: Target? = null
    private val windows = HashMap<Long, Target>()
    private lateinit var surfaceTexture: SurfaceTexture
    private lateinit var output: Surface
    private var mirror: IVirtualDisplayCallback? = null
    private var watch: (() -> Unit)? = null
    private val transform = FloatArray(16)
    private lateinit var coordinates: FloatBuffer
    private var rotation = -1
    private var rendered = false
    private var released = false
    private val thread = HandlerThread("Capture").apply { start() }
    private val handler = Handler(thread.looper)

    init {
        try {
            onHandler { prepare() }
        } catch (e: Throwable) {
            close()
            throw e
        }
    }

    private fun display(): Display = device.displayManager.getDisplay(displayId) ?: throw Failure(Failure.Name.NotFoundError, "no display $displayId")

    private fun <T> onHandler(work: () -> T): T? {
        val task = FutureTask(Callable { work() })
        if (!handler.post(task)) return null
        try {
            return task.get()
        } catch (e: ExecutionException) {
            throw e.cause as? Exception ?: e
        }
    }

    private fun prepare() {
        eglDisplay = EGL14.eglGetDisplay(EGL14.EGL_DEFAULT_DISPLAY)
        val version = IntArray(2)
        checkEgl(EGL14.eglInitialize(eglDisplay, version, 0, version, 1), "eglInitialize")
        val configs = arrayOfNulls<EGLConfig>(1)
        val count = IntArray(1)
        val attributes = intArrayOf(
            EGL14.EGL_RED_SIZE, 8,
            EGL14.EGL_GREEN_SIZE, 8,
            EGL14.EGL_BLUE_SIZE, 8,
            EGL14.EGL_ALPHA_SIZE, 8,
            EGL14.EGL_RENDERABLE_TYPE, EGL14.EGL_OPENGL_ES2_BIT,
            EGL14.EGL_SURFACE_TYPE, EGL14.EGL_WINDOW_BIT or EGL14.EGL_PBUFFER_BIT,
            EGLExt.EGL_RECORDABLE_ANDROID, 1,
            EGL14.EGL_NONE,
        )
        checkEgl(EGL14.eglChooseConfig(eglDisplay, attributes, 0, configs, 0, 1, count, 0) && count[0] > 0, "eglChooseConfig")
        eglConfig = configs[0]!!
        eglContext = EGL14.eglCreateContext(eglDisplay, eglConfig, EGL14.EGL_NO_CONTEXT, intArrayOf(EGL14.EGL_CONTEXT_CLIENT_VERSION, 2, EGL14.EGL_NONE), 0)
        checkEgl(eglContext != EGL14.EGL_NO_CONTEXT, "eglCreateContext")
        idle = EGL14.eglCreatePbufferSurface(eglDisplay, eglConfig, intArrayOf(EGL14.EGL_WIDTH, 1, EGL14.EGL_HEIGHT, 1, EGL14.EGL_NONE), 0)
        checkEgl(idle != EGL14.EGL_NO_SURFACE, "eglCreatePbufferSurface")
        checkEgl(EGL14.eglMakeCurrent(eglDisplay, idle, idle, eglContext), "eglMakeCurrent")

        plain = program("")
        swapped = program(".bgra")
        val texture = IntArray(1)
        GLES20.glGenTextures(1, texture, 0)
        GLES20.glBindTexture(GLES11Ext.GL_TEXTURE_EXTERNAL_OES, texture[0])
        GLES20.glTexParameteri(GLES11Ext.GL_TEXTURE_EXTERNAL_OES, GLES20.GL_TEXTURE_MIN_FILTER, GLES20.GL_LINEAR)
        GLES20.glTexParameteri(GLES11Ext.GL_TEXTURE_EXTERNAL_OES, GLES20.GL_TEXTURE_MAG_FILTER, GLES20.GL_LINEAR)
        GLES20.glTexParameteri(GLES11Ext.GL_TEXTURE_EXTERNAL_OES, GLES20.GL_TEXTURE_WRAP_S, GLES20.GL_CLAMP_TO_EDGE)
        GLES20.glTexParameteri(GLES11Ext.GL_TEXTURE_EXTERNAL_OES, GLES20.GL_TEXTURE_WRAP_T, GLES20.GL_CLAMP_TO_EDGE)
        GLES20.glClearColor(0f, 0f, 0f, 1f)

        surfaceTexture = SurfaceTexture(texture[0])
        surfaceTexture.setOnFrameAvailableListener({ render(true) }, handler)
        output = Surface(surfaceTexture)
        orient()
        if (own) {
            device.displays.attach(displayId, output)
            watch = device.watchDisplays(handler, ::displayChanged)
            return
        }
        var flags = DisplayManager.VIRTUAL_DISPLAY_FLAG_AUTO_MIRROR
        if (device.context.checkSelfPermission(CAPTURE_SECURE_VIDEO_OUTPUT) == PackageManager.PERMISSION_GRANTED) {
            flags = flags or DisplayManager.VIRTUAL_DISPLAY_FLAG_SECURE
        }
        val created = VirtualDisplayToken()
        val config = VirtualDisplayConfig.Builder("swipt", if (rotation % 2 == 0) drawn.width else drawn.height, if (rotation % 2 == 0) drawn.height else drawn.width, 1)
            .setFlags(flags)
            .setSurface(output)
        if (displayId != Display.DEFAULT_DISPLAY) SET_DISPLAY_ID_TO_MIRROR.invoke(config, displayId)
        check(device.displayService.createVirtualDisplay(config.build(), created, null, if (device.root) null else device.callingPackage) >= 0) {
            "the mirror of the display could not be created"
        }
        mirror = created
        watch = device.watchDisplays(handler, ::displayChanged)
    }

    private fun target(window: Surface, coded: Boolean): Target {
        val surface = EGL14.eglCreateWindowSurface(eglDisplay, eglConfig, window, intArrayOf(EGL14.EGL_NONE), 0)
        checkEgl(surface != EGL14.EGL_NO_SURFACE, "eglCreateWindowSurface")
        if (coded) return Target(surface, window, 0, encoded.codedHeight - encoded.height, encoded.width, encoded.height)
        val size = IntArray(2)
        EGL14.eglQuerySurface(eglDisplay, surface, EGL14.EGL_WIDTH, size, 0)
        EGL14.eglQuerySurface(eglDisplay, surface, EGL14.EGL_HEIGHT, size, 1)
        return Target(surface, window, 0, 0, size[0], size[1])
    }

    private fun destroy(target: Target) {
        EGL14.eglMakeCurrent(eglDisplay, idle, idle, eglContext)
        EGL14.eglDestroySurface(eglDisplay, target.surface)
    }

    private fun displayChanged() {
        if (released) return
        try {
            if (sides(display()) != sides) {
                resized = true
                wake()
                return
            }
            orient()
        } catch (e: Exception) {
            failure = e
            wake()
        }
    }

    private fun orient() {
        val rotation = display().rotation
        if (rotation == this.rotation) return
        this.rotation = rotation
        val corners = floatArrayOf(0f, 0f, 1f, 0f, 0f, 1f, 1f, 1f)
        repeat(rotation) {
            for (i in corners.indices step 2) {
                val x = corners[i]
                corners[i] = 1 - corners[i + 1]
                corners[i + 1] = x
            }
        }
        coordinates = floats(corners)
        val width = if (rotation % 2 == 0) drawn.width else drawn.height
        val height = if (rotation % 2 == 0) drawn.height else drawn.width
        surfaceTexture.setDefaultBufferSize(width, height)
        mirror?.let {
            try {
                device.displayService.resizeVirtualDisplay(it, width, height, 1)
            } catch (e: RemoteException) {
                throw e.rethrowFromSystemServer()
            }
        }
    }

    private fun draw(target: Target, program: Program, time: Long) {
        checkEgl(EGL14.eglMakeCurrent(eglDisplay, target.surface, target.surface, eglContext), "eglMakeCurrent")
        GLES20.glViewport(target.x, target.y, target.width, target.height)
        GLES20.glUseProgram(program.name)
        GLES20.glClear(GLES20.GL_COLOR_BUFFER_BIT)
        GLES20.glUniformMatrix4fv(program.transformation, 1, false, transform, 0)
        GLES20.glVertexAttribPointer(program.coordinate, 2, GLES20.GL_FLOAT, false, 0, coordinates)
        GLES20.glDrawArrays(GLES20.GL_TRIANGLE_STRIP, 0, 4)
        EGLExt.eglPresentationTimeANDROID(eglDisplay, target.surface, time)
    }

    private fun render(update: Boolean) {
        if (released) return
        try {
            if (update) {
                surfaceTexture.updateTexImage()
                surfaceTexture.getTransformMatrix(transform)
                rendered = true
            } else if (!rendered) {
                return
            }
            val time = System.nanoTime()
            val turn = (4 - rotation) % 4
            if (windows.isNotEmpty()) consumer.drawn(time, turn)
            val drawing = windows.entries.iterator()
            while (drawing.hasNext()) {
                val (window, target) = drawing.next()
                try {
                    draw(target, swapped, time)
                    checkEgl(EGL14.eglSwapBuffers(eglDisplay, target.surface), "eglSwapBuffers")
                } catch (e: RuntimeException) {
                    drawing.remove()
                    destroy(target)
                    target.window.release()
                    windowFailed(e, window)
                }
            }
            recorded?.let {
                turns[time / 1000] = turn
                draw(it, plain, time)
                checkEgl(EGL14.eglSwapBuffers(eglDisplay, it.surface), "eglSwapBuffers")
            }
        } catch (e: Exception) {
            failure = e
            wake()
        }
    }

    fun addWindow(window: Long) {
        onHandler {
            if (!released && !windows.containsKey(window)) {
                val surface = surfaceOf(window)
                try {
                    windows[window] = target(surface, false)
                } catch (e: RuntimeException) {
                    surface.release()
                    throw e
                }
                render(false)
            }
        }
    }

    fun removeWindow(window: Long) {
        onHandler {
            windows.remove(window)?.let {
                destroy(it)
                it.window.release()
            }
        }
    }

    fun setEncoding(encoding: Boolean) {
        this.encoding = encoding
        wake()
    }

    private fun wake() {
        changes.withLock { changed.signalAll() }
    }

    private fun startEncoder() {
        val format = MediaFormat.createVideoFormat(MediaFormat.MIMETYPE_VIDEO_HEVC, encoded.codedWidth, encoded.codedHeight)
        format.setInteger(MediaFormat.KEY_COLOR_FORMAT, MediaCodecInfo.CodecCapabilities.COLOR_FormatSurface)
        format.setInteger(MediaFormat.KEY_BIT_RATE, bitrate)
        format.setInteger(MediaFormat.KEY_FRAME_RATE, 60)
        format.setInteger(MediaFormat.KEY_I_FRAME_INTERVAL, 10)
        format.setLong(MediaFormat.KEY_REPEAT_PREVIOUS_FRAME_AFTER, 100_000)
        val created = MediaCodec.createByCodecName(encoder.name)
        try {
            created.configure(format, null, null, MediaCodec.CONFIGURE_FLAG_ENCODE)
            val surface = created.createInputSurface()
            input = surface
            created.start()
            synchronized(codecs) { codec = created }
            onHandler {
                recorded = target(surface, true)
                render(false)
            }
        } catch (e: Throwable) {
            stopEncoder(created)
            throw e
        }
    }

    private fun stopEncoder(stopping: MediaCodec) {
        onHandler {
            recorded?.let {
                destroy(it)
                recorded = null
            }
        }
        synchronized(codecs) { codec = null }
        turns.clear()
        stopping.release()
        input?.release()
        input = null
    }

    fun stream() {
        val info = MediaCodec.BufferInfo()
        var configuration = ByteArray(0)
        var current: MediaCodec? = null
        try {
            while (!resized) {
                if (Thread.interrupted()) throw InterruptedException()
                failure?.let { throw it }
                if (encoding && current == null) {
                    startEncoder()
                    current = codec
                    configuration = ByteArray(0)
                } else if (!encoding && current != null) {
                    val stopping = current
                    current = null
                    stopEncoder(stopping)
                }
                if (current == null) {
                    changes.withLock {
                        if (!encoding && !resized && failure == null) changed.await()
                    }
                    continue
                }
                val index = current.dequeueOutputBuffer(info, ENCODER_WAIT_MS * 1000)
                if (index < 0) continue
                val parameters = (info.flags and MediaCodec.BUFFER_FLAG_CODEC_CONFIG) != 0
                val prefix = if (!parameters && (info.flags and MediaCodec.BUFFER_FLAG_KEY_FRAME) != 0) configuration.size else 0
                val unit = ByteArray(prefix + info.size)
                configuration.copyInto(unit, 0, 0, prefix)
                current.getOutputBuffer(index)!!.get(unit, prefix, info.size)
                current.releaseOutputBuffer(index, false)
                if (parameters) {
                    configuration = unit
                    continue
                }
                if (info.size == 0) continue
                val frame = turns.floorEntry(info.presentationTimeUs) ?: turns.firstEntry() ?: continue
                turns.headMap(frame.key).clear()
                consumer.unit(unit, info.presentationTimeUs, frame.value)
            }
        } finally {
            current?.let { stopEncoder(it) }
        }
    }

    fun requestKeyFrame() {
        synchronized(codecs) {
            val codec = codec ?: return
            codec.setParameters(Bundle().apply { putInt(MediaCodec.PARAMETER_KEY_REQUEST_SYNC_FRAME, 0) })
        }
        handler.post { render(false) }
    }

    fun adjustBitrate(requested: Int) {
        val bitrate = bitrates.clamp(requested)
        this.bitrate = bitrate
        synchronized(codecs) {
            codec?.setParameters(Bundle().apply { putInt(MediaCodec.PARAMETER_KEY_VIDEO_BITRATE, bitrate) })
        }
    }

    private fun release() {
        released = true
        watch?.invoke()
        mirror?.let {
            try {
                device.displayService.releaseVirtualDisplay(it)
            } catch (e: RemoteException) {
                Log.w(TAG, "release the mirror of the display", e)
            }
        }
        if (own) {
            try {
                device.displays.detach(displayId)
            } catch (e: RemoteException) {
                Log.w(TAG, "detach the capture from the display", e)
            }
        }
        if (::output.isInitialized) output.release()
        if (::surfaceTexture.isInitialized) surfaceTexture.release()
        if (eglDisplay != EGL14.EGL_NO_DISPLAY) {
            for (target in windows.values) {
                destroy(target)
                target.window.release()
            }
            windows.clear()
            recorded?.let {
                destroy(it)
                recorded = null
            }
            EGL14.eglMakeCurrent(eglDisplay, EGL14.EGL_NO_SURFACE, EGL14.EGL_NO_SURFACE, EGL14.EGL_NO_CONTEXT)
            if (idle != EGL14.EGL_NO_SURFACE) EGL14.eglDestroySurface(eglDisplay, idle)
            if (eglContext != EGL14.EGL_NO_CONTEXT) EGL14.eglDestroyContext(eglDisplay, eglContext)
            EGL14.eglReleaseThread()
        }
    }

    override fun close() {
        handler.post { release() }
        thread.quitSafely()
        var interrupted = false
        while (true) {
            try {
                thread.join()
                break
            } catch (_: InterruptedException) {
                interrupted = true
            }
        }
        if (interrupted) Thread.currentThread().interrupt()
    }

    companion object {
        private val VERTEX_SHADER = """
            attribute vec4 position;
            attribute vec4 coordinate;
            uniform mat4 transform;
            varying vec2 point;
            void main() {
                gl_Position = position;
                point = (transform * coordinate).xy;
            }
        """.trimIndent()
        private val POSITIONS = floats(floatArrayOf(-1f, -1f, 1f, -1f, -1f, 1f, 1f, 1f))
        private const val CAPTURE_SECURE_VIDEO_OUTPUT = "android.permission.CAPTURE_SECURE_VIDEO_OUTPUT"
        private const val ENCODER_WAIT_MS = 100L
        private val SET_DISPLAY_ID_TO_MIRROR = VirtualDisplayConfig.Builder::class.java.getMethod("setDisplayIdToMirror", Int::class.javaPrimitiveType)

        @JvmStatic
        private external fun surfaceOf(window: Long): Surface

        private fun fragmentShader(swizzle: String) = """
            #extension GL_OES_EGL_image_external : require
            precision mediump float;
            uniform samplerExternalOES picture;
            varying vec2 point;
            void main() {
                gl_FragColor = texture2D(picture, point)$swizzle;
            }
        """.trimIndent()

        private fun sides(display: Display): Point {
            val size = Point().also(display::getRealSize)
            return Point(min(size.x, size.y), max(size.x, size.y))
        }

        private fun encoder(): MediaCodecInfo {
            var chosen: MediaCodecInfo? = null
            for (info in MediaCodecList(MediaCodecList.REGULAR_CODECS).codecInfos) {
                if (!info.isEncoder || chosen != null && (chosen.isHardwareAccelerated || !info.isHardwareAccelerated)) continue
                for (type in info.supportedTypes) {
                    if (type.equals(MediaFormat.MIMETYPE_VIDEO_HEVC, ignoreCase = true) &&
                        MediaCodecInfo.CodecCapabilities.COLOR_FormatSurface in info.getCapabilitiesForType(type).colorFormats
                    ) {
                        chosen = info
                    }
                }
            }
            return chosen ?: throw Failure(Failure.Name.NotSupportedError, "no HEVC encoder takes a surface as input")
        }

        private fun fit(video: MediaCodecInfo.VideoCapabilities, naturalWidth: Int, naturalHeight: Int): Picture {
            val limit = minOf(1.0, video.supportedWidths.upper / naturalWidth.toDouble(), video.supportedHeights.upper / naturalHeight.toDouble())
            val largest = (naturalWidth * limit).toInt() and 1.inv()
            for (width in largest downTo largest * 3 / 4 step 2) {
                val height = naturalHeight.toLong() * width
                if (height % naturalWidth == 0L && height / naturalWidth % 2 == 0L) {
                    picture(video, naturalWidth, width, (height / naturalWidth).toInt())?.let { return it }
                }
            }
            for (width in largest downTo 1 step 2) {
                picture(video, naturalWidth, width, (naturalHeight * (width / naturalWidth.toDouble())).roundToInt() and 1.inv())?.let { return it }
            }
            throw Failure(Failure.Name.NotSupportedError, "the HEVC encoder takes no picture size of a ${naturalWidth}x$naturalHeight display")
        }

        private fun picture(video: MediaCodecInfo.VideoCapabilities, naturalWidth: Int, width: Int, height: Int): Picture? {
            val codedWidth = (width + video.widthAlignment - 1) / video.widthAlignment * video.widthAlignment
            val codedHeight = (height + video.heightAlignment - 1) / video.heightAlignment * video.heightAlignment
            return Picture(width, height, codedWidth, codedHeight, width / naturalWidth.toDouble()).takeIf { video.isSizeSupported(codedWidth, codedHeight) }
        }

        private fun program(swizzle: String): Program {
            val program = GLES20.glCreateProgram()
            GLES20.glAttachShader(program, shader(GLES20.GL_VERTEX_SHADER, VERTEX_SHADER))
            GLES20.glAttachShader(program, shader(GLES20.GL_FRAGMENT_SHADER, fragmentShader(swizzle)))
            GLES20.glLinkProgram(program)
            val status = IntArray(1)
            GLES20.glGetProgramiv(program, GLES20.GL_LINK_STATUS, status, 0)
            check(status[0] != 0) { "linking the capture shaders failed: ${GLES20.glGetProgramInfoLog(program)}" }
            val position = GLES20.glGetAttribLocation(program, "position")
            GLES20.glEnableVertexAttribArray(position)
            GLES20.glVertexAttribPointer(position, 2, GLES20.GL_FLOAT, false, 0, POSITIONS)
            val coordinate = GLES20.glGetAttribLocation(program, "coordinate")
            GLES20.glEnableVertexAttribArray(coordinate)
            return Program(program, coordinate, GLES20.glGetUniformLocation(program, "transform"))
        }

        private fun shader(type: Int, source: String): Int {
            val shader = GLES20.glCreateShader(type)
            GLES20.glShaderSource(shader, source)
            GLES20.glCompileShader(shader)
            val status = IntArray(1)
            GLES20.glGetShaderiv(shader, GLES20.GL_COMPILE_STATUS, status, 0)
            check(status[0] != 0) { "compiling a capture shader failed: ${GLES20.glGetShaderInfoLog(shader)}" }
            return shader
        }

        private fun checkEgl(succeeded: Boolean, operation: String) {
            check(succeeded) { "$operation failed with EGL error 0x${EGL14.eglGetError().toString(16)}" }
        }

        private fun floats(values: FloatArray): FloatBuffer =
            ByteBuffer.allocateDirect(values.size * Float.SIZE_BYTES).order(ByteOrder.nativeOrder()).asFloatBuffer().apply { put(values).position(0) }
    }
}
