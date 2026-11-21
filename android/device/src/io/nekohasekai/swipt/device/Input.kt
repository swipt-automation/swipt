package io.nekohasekai.swipt.device

import android.hardware.input.InputManagerGlobal
import android.os.SystemClock
import android.view.Display
import android.view.InputDevice
import android.view.InputEvent
import android.view.KeyCharacterMap
import android.view.KeyEvent
import android.view.MotionEvent
import java.util.TreeSet
import kotlin.math.roundToLong
import org.json.JSONArray
import org.json.JSONObject

internal class Input {
    private val manager = InputManagerGlobal.getInstance()
    private val keyboard = KeyCharacterMap.load(KeyCharacterMap.VIRTUAL_KEYBOARD)

    fun touch(display: Int, fingers: JSONArray) {
        touch(
            display,
            Array(fingers.length()) { i ->
                val samples = fingers.getJSONArray(i)
                Array(samples.length()) { j ->
                    val sample = samples.getJSONArray(j)
                    doubleArrayOf(sample.getDouble(0), sample.getDouble(1), sample.getDouble(2))
                }
            },
        )
    }

    fun path(points: Array<DoubleArray>, stepMs: Long, endHoldMs: Long) {
        val track = points.mapIndexed { i, point -> doubleArrayOf(point[0], point[1], i * stepMs.toDouble()) }.toMutableList()
        if (endHoldMs > 0) {
            val last = points.last()
            track += doubleArrayOf(last[0], last[1], (points.size - 1) * stepMs.toDouble() + endHoldMs)
        }
        touch(Display.DEFAULT_DISPLAY, arrayOf(track.toTypedArray()))
    }

    private fun touch(display: Int, tracks: Array<Array<DoubleArray>>) {
        if (tracks.isEmpty()) throw Failure(Failure.Name.TypeError, "a touch needs at least one finger")
        val times = TreeSet<Double>()
        tracks.forEachIndexed { i, track ->
            if (track.isEmpty()) throw Failure(Failure.Name.TypeError, "finger $i has no samples")
            track.forEachIndexed { j, sample ->
                if (j > 0 && sample[2] < track[j - 1][2]) throw Failure(Failure.Name.TypeError, "finger $i goes back in time")
                times += sample[2]
            }
        }
        val origin = times.first()
        var frame = origin + FRAME_MS
        while (frame < times.last()) {
            times += frame
            frame += FRAME_MS
        }
        val start = SystemClock.uptimeMillis()
        var downTime = start
        val ids = IntArray(tracks.size)
        val down = BooleanArray(tracks.size)
        val up = BooleanArray(tracks.size)
        try {
            for (time in times) {
                Call.check()
                val eventTime = start + (time - origin).roundToLong()
                val delay = eventTime - SystemClock.uptimeMillis()
                if (delay > 0) Thread.sleep(delay)
                var moved = false
                for (i in tracks.indices) {
                    if (!down[i] && tracks[i][0][2] <= time) {
                        if (active(down, up).isEmpty()) downTime = eventTime
                        ids[i] = freeId(ids, down, up)
                        down[i] = true
                        if (!inject(gesture(downTime, eventTime, MotionEvent.ACTION_POINTER_DOWN, i, tracks, ids, down, up, time), display, INJECT_WAIT_FOR_RESULT)) {
                            throw Failure(
                                Failure.Name.InvalidStateError,
                                "nothing took the touch at (${tracks[i][0][0].roundToLong()}, ${tracks[i][0][1].roundToLong()}), as happens between windows",
                            )
                        }
                    } else if (down[i] && !up[i]) {
                        moved = true
                    }
                }
                if (moved) inject(gesture(downTime, eventTime, MotionEvent.ACTION_MOVE, -1, tracks, ids, down, up, time), display, INJECT_ASYNC)
                for (i in tracks.indices) {
                    if (down[i] && !up[i] && tracks[i].last()[2] <= time) {
                        inject(gesture(downTime, eventTime, MotionEvent.ACTION_POINTER_UP, i, tracks, ids, down, up, time), display, INJECT_WAIT_FOR_RESULT)
                        up[i] = true
                    }
                }
            }
        } catch (e: Exception) {
            if (active(down, up).isNotEmpty()) {
                inject(gesture(downTime, SystemClock.uptimeMillis(), MotionEvent.ACTION_CANCEL, -1, tracks, ids, down, up, times.last()), display, INJECT_WAIT_FOR_RESULT)
            }
            throw e
        }
    }

    fun key(display: Int, names: JSONArray) {
        val keys = IntArray(names.length()) { i ->
            val name = names.getString(i)
            val key = if (name.startsWith("KEYCODE_")) KeyEvent.keyCodeFromString(name) else KeyEvent.KEYCODE_UNKNOWN
            if (key == KeyEvent.KEYCODE_UNKNOWN) throw Failure(Failure.Name.TypeError, "Android has no key ${JSONObject.quote(name)}")
            key
        }
        press(display, *keys)
    }

    fun press(display: Int, vararg keys: Int) {
        val downTimes = LongArray(keys.size)
        var pressed = 0
        try {
            while (pressed < keys.size) {
                val now = SystemClock.uptimeMillis()
                downTimes[pressed] = now
                if (!inject(keyEvent(now, now, KeyEvent.ACTION_DOWN, keys[pressed], meta(keys, pressed + 1)), display, INJECT_WAIT_FOR_RESULT)) {
                    throw Failure(Failure.Name.InvalidStateError, "nothing took ${KeyEvent.keyCodeToString(keys[pressed])}, as happens when no window has focus")
                }
                pressed++
            }
        } finally {
            while (pressed-- > 0) {
                inject(keyEvent(downTimes[pressed], SystemClock.uptimeMillis(), KeyEvent.ACTION_UP, keys[pressed], meta(keys, pressed)), display, INJECT_WAIT_FOR_RESULT)
            }
        }
    }

    fun type(text: String) {
        val events = mutableListOf<KeyEvent>()
        var offset = 0
        while (offset < text.length) {
            val codePoint = text.codePointAt(offset)
            val characters = Character.toChars(codePoint)
            events += keyboard.getEvents(characters)
                ?: throw Failure(Failure.Name.NotSupportedError, "the virtual keyboard of Android has no key for ${JSONObject.quote(String(characters))}")
            offset += characters.size
        }
        for (key in events) {
            val event = KeyEvent.changeTimeRepeat(key, SystemClock.uptimeMillis(), 0, key.flags or KeyEvent.FLAG_FROM_SYSTEM)
            if (!inject(event, Display.DEFAULT_DISPLAY, INJECT_WAIT_FOR_RESULT) && event.action == KeyEvent.ACTION_DOWN) {
                throw Failure(Failure.Name.InvalidStateError, "nothing took the text, as happens when no window has focus")
            }
        }
    }

    // The system rejects an event when no window takes it. The rest of a gesture is rejected when the window that took
    // its down goes away meanwhile.
    private fun inject(event: InputEvent, display: Int, mode: Int): Boolean {
        SET_DISPLAY_ID.invoke(event, display)
        return manager.injectInputEvent(event, mode)
    }

    companion object {
        private const val INJECT_ASYNC = 0
        private const val INJECT_WAIT_FOR_RESULT = 1
        private const val FRAME_MS = 8.0
        private val MODIFIERS = mapOf(
            KeyEvent.KEYCODE_SHIFT_LEFT to (KeyEvent.META_SHIFT_ON or KeyEvent.META_SHIFT_LEFT_ON),
            KeyEvent.KEYCODE_SHIFT_RIGHT to (KeyEvent.META_SHIFT_ON or KeyEvent.META_SHIFT_RIGHT_ON),
            KeyEvent.KEYCODE_CTRL_LEFT to (KeyEvent.META_CTRL_ON or KeyEvent.META_CTRL_LEFT_ON),
            KeyEvent.KEYCODE_CTRL_RIGHT to (KeyEvent.META_CTRL_ON or KeyEvent.META_CTRL_RIGHT_ON),
            KeyEvent.KEYCODE_ALT_LEFT to (KeyEvent.META_ALT_ON or KeyEvent.META_ALT_LEFT_ON),
            KeyEvent.KEYCODE_ALT_RIGHT to (KeyEvent.META_ALT_ON or KeyEvent.META_ALT_RIGHT_ON),
            KeyEvent.KEYCODE_META_LEFT to (KeyEvent.META_META_ON or KeyEvent.META_META_LEFT_ON),
            KeyEvent.KEYCODE_META_RIGHT to (KeyEvent.META_META_ON or KeyEvent.META_META_RIGHT_ON),
            KeyEvent.KEYCODE_FUNCTION to KeyEvent.META_FUNCTION_ON,
            KeyEvent.KEYCODE_SYM to KeyEvent.META_SYM_ON,
        )
        private val SET_DISPLAY_ID = InputEvent::class.java.getMethod("setDisplayId", Int::class.javaPrimitiveType)

        private fun active(down: BooleanArray, up: BooleanArray) = down.indices.filter { down[it] && !up[it] }

        private fun freeId(ids: IntArray, down: BooleanArray, up: BooleanArray): Int {
            val taken = active(down, up).map { ids[it] }
            return generateSequence(0) { it + 1 }.first { it !in taken }
        }

        private fun gesture(
            downTime: Long,
            eventTime: Long,
            action: Int,
            finger: Int,
            tracks: Array<Array<DoubleArray>>,
            ids: IntArray,
            down: BooleanArray,
            up: BooleanArray,
            time: Double,
        ): MotionEvent {
            val active = active(down, up)
            val properties = Array(active.size) {
                MotionEvent.PointerProperties().apply {
                    id = ids[active[it]]
                    toolType = MotionEvent.TOOL_TYPE_FINGER
                }
            }
            val coordinates = Array(active.size) {
                val position = position(tracks[active[it]], time)
                MotionEvent.PointerCoords().apply {
                    x = position[0].toFloat()
                    y = position[1].toFloat()
                    pressure = 1f
                    size = 1f
                }
            }
            val masked = when {
                (action == MotionEvent.ACTION_POINTER_DOWN || action == MotionEvent.ACTION_POINTER_UP) && active.size == 1 ->
                    if (action == MotionEvent.ACTION_POINTER_DOWN) MotionEvent.ACTION_DOWN else MotionEvent.ACTION_UP
                finger >= 0 -> action or (maxOf(0, active.indexOf(finger)) shl MotionEvent.ACTION_POINTER_INDEX_SHIFT)
                else -> action
            }
            return MotionEvent.obtain(
                downTime, eventTime, masked, active.size, properties, coordinates, 0, 0, 1f, 1f,
                KeyCharacterMap.VIRTUAL_KEYBOARD, 0, InputDevice.SOURCE_TOUCHSCREEN, 0,
            )
        }

        private fun position(track: Array<DoubleArray>, time: Double): DoubleArray {
            if (time <= track[0][2]) return track[0]
            for (i in 1 until track.size) {
                if (time <= track[i][2]) {
                    val from = track[i - 1]
                    val to = track[i]
                    val progress = if (to[2] == from[2]) 1.0 else (time - from[2]) / (to[2] - from[2])
                    return doubleArrayOf(from[0] + (to[0] - from[0]) * progress, from[1] + (to[1] - from[1]) * progress)
                }
            }
            return track.last()
        }

        private fun meta(keys: IntArray, count: Int) = (0 until count).fold(0) { meta, i -> meta or (MODIFIERS[keys[i]] ?: 0) }

        private fun keyEvent(downTime: Long, eventTime: Long, action: Int, keyCode: Int, metaState: Int) =
            KeyEvent(downTime, eventTime, action, keyCode, 0, metaState, KeyCharacterMap.VIRTUAL_KEYBOARD, 0, KeyEvent.FLAG_FROM_SYSTEM, InputDevice.SOURCE_KEYBOARD)
    }
}
