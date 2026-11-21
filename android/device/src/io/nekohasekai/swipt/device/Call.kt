package io.nekohasekai.swipt.device

import io.nekohasekai.libSwipt.LibSwipt

internal class Call private constructor() {
    private val thread = Thread.currentThread()

    @Volatile
    private var canceled = false

    companion object {
        private val running = HashMap<Long, Call>()
        private val current = ThreadLocal<Call>()

        fun <T> run(id: Long, body: () -> T): T {
            val call = Call()
            synchronized(running) { running[id] = call }
            current.set(call)
            try {
                if (LibSwipt.callCanceled(id)) throw InterruptedException()
                return body()
            } finally {
                current.remove()
                synchronized(running) { running.remove(id) }
                Thread.interrupted()
            }
        }

        fun interrupt(id: Long) {
            synchronized(running) {
                running[id]?.let {
                    it.canceled = true
                    it.thread.interrupt()
                }
            }
        }

        // UiAutomation and AccessibilityInteractionClient catch the interrupt of a thread that waits in them, and clear it.
        fun check() {
            if (current.get()?.canceled == true) throw InterruptedException()
        }
    }
}
