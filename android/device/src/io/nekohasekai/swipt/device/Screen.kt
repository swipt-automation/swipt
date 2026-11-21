package io.nekohasekai.swipt.device

import android.util.Log
import io.nekohasekai.libSwipt.ScreenCapture
import io.nekohasekai.libSwipt.ScreenConsumer
import java.util.concurrent.ConcurrentHashMap

internal class Screen(private val device: Device) {
    fun open(display: Int, bitrate: Int, consumer: ScreenConsumer): ScreenCapture = Streaming(display, bitrate, consumer).apply { thread.start() }

    private inner class Streaming(display: Int, @Volatile private var bitrate: Int, private val consumer: ScreenConsumer) : ScreenCapture {
        private val windows = ConcurrentHashMap.newKeySet<Long>()

        @Volatile
        private var capture: Capture? = null

        @Volatile
        private var encoding = false
        val thread = Thread({ stream(display) }, "Screen").apply { isDaemon = true }

        private fun stream(display: Int) {
            var ended = "the capture stopped"
            try {
                while (!Thread.currentThread().isInterrupted) {
                    try {
                        Capture(device, display, bitrate, consumer, ::windowFailed).use { opened ->
                            capture = opened
                            consumer.format(opened.drawn.width, opened.drawn.height, opened.drawn.scale, opened.encoded.width, opened.encoded.height, opened.encoded.scale)
                            for (window in windows) {
                                try {
                                    opened.addWindow(window)
                                } catch (e: Exception) {
                                    windowFailed(e, window)
                                }
                            }
                            opened.setEncoding(encoding)
                            opened.stream()
                            bitrate = opened.bitrate
                        }
                    } finally {
                        capture = null
                    }
                }
            } catch (_: InterruptedException) {
                return
            } catch (e: Exception) {
                ended = message(e)
                Log.w(TAG, "capture the screen", e)
            }
            consumer.ended(ended)
        }

        override fun requestKeyFrame() {
            capture?.requestKeyFrame()
        }

        override fun setBitrate(bitrate: Int) {
            this.bitrate = bitrate
            capture?.adjustBitrate(bitrate)
        }

        private fun windowFailed(e: Exception, window: Long) {
            windows.remove(window)
            consumer.windowFailed(window, message(e))
        }

        override fun addWindow(window: Long) {
            windows.add(window)
            try {
                capture?.addWindow(window)
            } catch (e: Exception) {
                windows.remove(window)
                throw e
            }
        }

        override fun removeWindow(window: Long) {
            windows.remove(window)
            try {
                capture?.removeWindow(window)
            } catch (e: Exception) {
                Log.w(TAG, "stop drawing the screen into a window", e)
            }
        }

        override fun setEncoding(encoding: Boolean) {
            this.encoding = encoding
            capture?.setEncoding(encoding)
        }

        override fun close() {
            thread.interrupt()
        }
    }

    private fun message(e: Exception) = e.message ?: e.toString()
}
