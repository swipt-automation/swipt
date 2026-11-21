package io.nekohasekai.swipt.device

import android.util.Log
import io.nekohasekai.libSwipt.Observer
import io.nekohasekai.libSwipt.Watch
import java.util.concurrent.CopyOnWriteArraySet
import java.util.concurrent.Semaphore
import kotlin.concurrent.thread

internal class State(
    private val name: String,
    private val reader: () -> Any?,
    private val source: (changed: () -> Unit) -> AutoCloseable,
) {
    private val watchers = CopyOnWriteArraySet<Semaphore>()
    private var watching: AutoCloseable? = null

    fun get(): String = jsonOf(reader())

    fun watch(observer: Observer): Watch {
        val changed = Semaphore(0)
        synchronized(this) {
            if (watchers.isEmpty()) {
                watching = source { watchers.forEach { it.release() } }
            }
            watchers.add(changed)
        }
        val watcher = thread(isDaemon = true, name = "State $name") {
            var last: String? = null
            try {
                while (true) {
                    val next = try {
                        Failure.guard("device.state", ::get)
                    } catch (e: Failure) {
                        if (e.name != Failure.Name.UnavailableError) {
                            observer.failed(e.message)
                            return@thread
                        }
                        null
                    }
                    if (next != null && next != last) {
                        observer.next(next)
                        last = next
                    }
                    changed.acquire()
                    changed.drainPermits()
                }
            } catch (_: InterruptedException) {
            } finally {
                unwatch(changed)
            }
        }
        return Watch(watcher::interrupt)
    }

    @Synchronized
    private fun unwatch(changed: Semaphore) {
        watchers.remove(changed)
        if (watchers.isNotEmpty()) return
        try {
            watching?.close()
        } catch (e: Exception) {
            Log.w(TAG, "stop watching the state $name", e)
        }
        watching = null
    }
}
