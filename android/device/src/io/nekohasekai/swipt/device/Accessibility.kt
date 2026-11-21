package io.nekohasekai.swipt.device

import android.accessibilityservice.AccessibilityServiceInfo
import android.app.IUiAutomationConnection
import android.app.UiAutomation
import android.app.UiAutomationConnection
import android.graphics.Rect
import android.graphics.Region
import android.graphics.RegionIterator
import android.os.Bundle
import android.os.HandlerThread
import android.os.Looper
import android.os.SystemClock
import android.util.SparseLongArray
import android.view.accessibility.AccessibilityEvent
import android.view.accessibility.AccessibilityNodeInfo
import android.view.accessibility.AccessibilityNodeInfo.AccessibilityAction
import android.view.accessibility.AccessibilityWindowInfo
import java.util.concurrent.TimeUnit
import java.util.concurrent.locks.ReentrantLock
import kotlin.concurrent.withLock
import kotlin.math.max
import kotlin.math.min
import org.json.JSONArray
import org.json.JSONObject

internal class Accessibility {
    private val changes = ReentrantLock()
    private val changed = changes.newCondition()
    private var lastChange = 0L
    private val lastChanges = SparseLongArray()
    private var automation: UiAutomation? = null

    @Volatile
    private var nodes: Map<String, AccessibilityNodeInfo> = emptyMap()

    @Synchronized
    fun automation(): UiAutomation {
        automation?.let { return it }
        val thread = HandlerThread("UiAutomation").apply { start() }
        try {
            val connecting = CREATE.newInstance(thread.looper, UiAutomationConnection())
            CONNECT.invoke(connecting, UiAutomation.FLAG_DONT_SUPPRESS_ACCESSIBILITY_SERVICES)
            connecting.serviceInfo = connecting.serviceInfo.apply {
                flags = flags or AccessibilityServiceInfo.FLAG_RETRIEVE_INTERACTIVE_WINDOWS or
                    AccessibilityServiceInfo.FLAG_INCLUDE_NOT_IMPORTANT_VIEWS or
                    AccessibilityServiceInfo.FLAG_REPORT_VIEW_IDS
            }
            connecting.setOnAccessibilityEventListener { event ->
                if ((event.eventType and SCREEN_CHANGES) != 0) {
                    changes.withLock {
                        lastChange = System.nanoTime() / 1000
                        lastChanges.put(event.displayId, lastChange)
                        changed.signalAll()
                    }
                }
            }
            automation = connecting
            return connecting
        } catch (e: Exception) {
            thread.quitSafely()
            throw e
        }
    }

    fun snapshot(display: Int): String = snapshot(automation(), display).toString()

    fun waitForChange(seen: Long, timeoutMs: Long) {
        automation()
        val start = SystemClock.uptimeMillis()
        val deadline = if (timeoutMs < Long.MAX_VALUE - start) start + timeoutMs else Long.MAX_VALUE
        changes.withLock {
            var now = start
            while (lastChange <= seen && now < deadline) {
                if (deadline == Long.MAX_VALUE) changed.await() else changed.await(deadline - now, TimeUnit.MILLISECONDS)
                now = SystemClock.uptimeMillis()
            }
        }
    }

    fun waitForIdle(display: Int, idleMs: Long, timeoutMs: Long): Boolean {
        automation()
        val start = System.nanoTime() / 1000
        val deadline = if (timeoutMs < (Long.MAX_VALUE - start) / 1000) start + timeoutMs * 1000 else Long.MAX_VALUE
        changes.withLock {
            while (true) {
                val now = System.nanoTime() / 1000
                val last = if (display < 0) lastChange else lastChanges.get(display)
                val quiet = max(last, start) + idleMs * 1000
                if (now >= quiet) return true
                if (now >= deadline) return false
                changed.await(min(quiet, deadline) - now, TimeUnit.MICROSECONDS)
            }
        }
    }

    fun aim(element: String): String = aim(automation(), JSONObject(element)).toString()

    fun setText(element: String, text: String): String = jsonOf(setText(automation(), JSONObject(element), text))

    fun perform(element: String, action: String, arguments: String) {
        perform(automation(), JSONObject(element), action, if (arguments.isEmpty()) null else JSONObject(arguments))
    }

    private fun snapshot(automation: UiAutomation, display: Int): JSONObject {
        val seen = System.nanoTime() / 1000
        val captured = HashMap<String, AccessibilityNodeInfo>()
        val windows = JSONArray()
        for (window in windows(automation, display)) {
            Call.check()
            val root = window.root
            val entry = JSONObject()
                .put("type", WINDOW_TYPES[window.type] ?: "system")
                .put("display", window.displayId)
                .put("layer", window.layer)
                .put("frame", rect(Rect().also(window::getBoundsInScreen)))
                .putText("title", window.title)
                .putText("packageName", root?.packageName)
                .putFlag("focused", window.isFocused)
                .putFlag("active", window.isActive)
                .putFlag("pictureInPicture", window.isInPictureInPictureMode)
            windows.put(entry.put("root", if (root == null) JSONObject.NULL else element(root, captured)))
        }
        nodes = captured
        return JSONObject().put("time", System.currentTimeMillis()).put("seen", seen).put("windows", windows)
    }

    private fun element(info: AccessibilityNodeInfo, captured: MutableMap<String, AccessibilityNodeInfo>): JSONObject {
        Call.check()
        val handle = "${info.windowId}:${(SOURCE_NODE_ID.invoke(info) as Long).toULong().toString(16)}"
        captured[handle] = info
        val element = JSONObject()
            .put("handle", handle)
            .putText("class", info.className)
            .putText("packageName", info.packageName)
            .putText("identifier", info.viewIdResourceName)
            .putText("text", if (info.isShowingHintText) null else info.text)
            .putText("contentDescription", info.contentDescription)
            .putText("hint", info.hintText)
            .putText("stateDescription", info.stateDescription)
            .putText("error", info.error)
            .putText("paneTitle", info.paneTitle)
            .putText("tooltipText", info.tooltipText)
            .put("frame", rect(Rect().also(info::getBoundsInScreen)))
            .putFlag("offScreen", !info.isVisibleToUser)
            .putFlag("disabled", !info.isEnabled)
            .putFlag("focused", info.isFocused)
            .putFlag("selected", info.isSelected)
        if (info.isCheckable) element.put("checked", info.isChecked)
        element.putFlag("secure", info.isPassword).putFlag("heading", info.isHeading).putFlag("editable", info.isEditable)
        info.rangeInfo?.let { range ->
            element.put(
                "range",
                JSONObject()
                    .put("type", RANGE_TYPES[range.type] ?: "float")
                    .put("min", range.min.toDouble())
                    .put("max", range.max.toDouble())
                    .put("current", range.current.toDouble()),
            )
        }
        info.collectionInfo?.takeIf { it.rowCount >= 0 && it.columnCount >= 0 }?.let {
            element.put("collection", JSONObject().put("rows", it.rowCount).put("columns", it.columnCount))
        }
        info.collectionItemInfo?.takeIf { it.rowIndex >= 0 && it.columnIndex >= 0 }?.let {
            element.put("collectionItem", JSONObject().put("row", it.rowIndex).put("column", it.columnIndex))
        }
        if (info.isEditable && info.textSelectionStart >= 0) {
            element.put("selection", JSONArray().put(info.textSelectionStart).put(info.textSelectionEnd))
        }
        element.put("actions", JSONArray(actionsOf(info)))
        val children = JSONArray()
        for (i in 0 until info.childCount) {
            info.getChild(i)?.let { children.put(element(it, captured)) }
        }
        return element.put("children", children)
    }

    // A handle names the view by its window and source node, which a later read of the same view gives again; a view
    // that an app reuses for other content keeps them.
    private fun resolve(automation: UiAutomation, element: JSONObject): AccessibilityNodeInfo {
        val handle = element.getString("handle")
        val node = nodes[handle] ?: run {
            snapshot(automation, -1)
            nodes[handle]
        }
        if (node == null || !node.refresh()) {
            throw Failure(Failure.Name.NotFoundError, "the element is no longer on the screen; read the screen again")
        }
        val shown = mutableListOf<Pair<String, CharSequence?>>(
            "class" to node.className,
            "identifier" to node.viewIdResourceName,
            "contentDescription" to node.contentDescription,
        )
        if (!node.isEditable) shown += "text" to if (node.isShowingHintText) null else node.text
        for ((key, value) in shown) {
            if (!element.optString(key, "").contentEquals(value ?: "")) {
                throw Failure(Failure.Name.NotFoundError, "the element is no longer on the screen: its view now shows ${nameOf(node)}; read the screen again")
            }
        }
        return node
    }

    private fun aim(automation: UiAutomation, element: JSONObject): JSONObject {
        val seen = System.nanoTime() / 1000
        val node = resolve(automation, element)
        if (!node.isEnabled) return pending(seen, "${nameOf(node)} is disabled")
        val visible = Region(Rect().also(node::getBoundsInScreen))
        val own = windows(automation, -1).firstOrNull { it.id == node.windowId }
            ?: throw Failure(Failure.Name.NotFoundError, "the element is no longer on the screen; read the screen again")
        val region = Region()
        own.getRegionInScreen(region)
        visible.op(region, Region.Op.INTERSECT)
        if (visible.isEmpty) return pending(seen, "${nameOf(node)} is off the screen")
        for (window in windows(automation, own.displayId)) {
            if (window.layer > own.layer) {
                window.getRegionInScreen(region)
                visible.op(region, Region.Op.DIFFERENCE)
            }
        }
        if (visible.isEmpty) return pending(seen, "${nameOf(node)} is covered by other windows")
        val target = visible.bounds
        if (!visible.contains(target.centerX(), target.centerY())) {
            val parts = RegionIterator(visible)
            target.setEmpty()
            val part = Rect()
            while (parts.next(part)) {
                if (part.width().toLong() * part.height() > target.width().toLong() * target.height()) target.set(part)
            }
        }
        return JSONObject()
            .put("point", JSONArray().put(target.exactCenterX().toDouble()).put(target.exactCenterY().toDouble()))
            .put("display", own.displayId)
    }

    private fun setText(automation: UiAutomation, element: JSONObject, text: String): JSONObject? {
        val seen = System.nanoTime() / 1000
        val node = resolve(automation, element)
        if (!node.isEditable) throw Failure(Failure.Name.InvalidStateError, "${nameOf(node)} is not a text field")
        if (!node.isEnabled) return pending(seen, "${nameOf(node)} is disabled")
        if (!node.isFocused && !node.performAction(AccessibilityNodeInfo.ACTION_FOCUS)) return pending(seen, "${nameOf(node)} declined the focus")
        val arguments = Bundle().apply { putCharSequence(AccessibilityNodeInfo.ACTION_ARGUMENT_SET_TEXT_CHARSEQUENCE, text) }
        return if (node.performAction(AccessibilityNodeInfo.ACTION_SET_TEXT, arguments)) null else pending(seen, "${nameOf(node)} declined the text")
    }

    private fun perform(automation: UiAutomation, element: JSONObject, name: String, given: JSONObject?) {
        val node = resolve(automation, element)
        val action = ACTIONS[name]
            ?: offered(node).firstOrNull { !ACTION_NAMES.containsKey(it.id) && name.contentEquals(it.label) }?.id
            ?: throw Failure(Failure.Name.TypeError, "${nameOf(node)} has no action ${JSONObject.quote(name)}; its actions are ${actionsOf(node).joinToString(", ")}")
        val arguments = Bundle()
        if (given != null) {
            for (key in given.keys()) {
                val argument = ARGUMENTS[key] ?: throw Failure(Failure.Name.TypeError, "unknown argument $key")
                if (key == "value") arguments.putFloat(argument, given.getDouble(key).toFloat()) else arguments.putInt(argument, given.getInt(key))
            }
        }
        if (!node.performAction(action, arguments)) {
            throw Failure(Failure.Name.InvalidStateError, "${nameOf(node)} declined the action ${JSONObject.quote(name)}")
        }
    }

    companion object {
        private const val SCREEN_CHANGES = AccessibilityEvent.TYPE_WINDOWS_CHANGED or AccessibilityEvent.TYPE_WINDOW_STATE_CHANGED or
            AccessibilityEvent.TYPE_WINDOW_CONTENT_CHANGED or AccessibilityEvent.TYPE_VIEW_SCROLLED or AccessibilityEvent.TYPE_VIEW_TEXT_CHANGED or
            AccessibilityEvent.TYPE_VIEW_TEXT_SELECTION_CHANGED or AccessibilityEvent.TYPE_VIEW_FOCUSED or AccessibilityEvent.TYPE_VIEW_SELECTED
        private val NAMED = listOf(
            AccessibilityAction.ACTION_CLICK to "click",
            AccessibilityAction.ACTION_LONG_CLICK to "longClick",
            AccessibilityAction.ACTION_CONTEXT_CLICK to "contextClick",
            AccessibilityAction.ACTION_FOCUS to "focus",
            AccessibilityAction.ACTION_CLEAR_FOCUS to "clearFocus",
            AccessibilityAction.ACTION_SELECT to "select",
            AccessibilityAction.ACTION_CLEAR_SELECTION to "clearSelection",
            AccessibilityAction.ACTION_ACCESSIBILITY_FOCUS to "accessibilityFocus",
            AccessibilityAction.ACTION_CLEAR_ACCESSIBILITY_FOCUS to "clearAccessibilityFocus",
            AccessibilityAction.ACTION_SCROLL_FORWARD to "scrollForward",
            AccessibilityAction.ACTION_SCROLL_BACKWARD to "scrollBackward",
            AccessibilityAction.ACTION_SCROLL_UP to "scrollUp",
            AccessibilityAction.ACTION_SCROLL_DOWN to "scrollDown",
            AccessibilityAction.ACTION_SCROLL_LEFT to "scrollLeft",
            AccessibilityAction.ACTION_SCROLL_RIGHT to "scrollRight",
            AccessibilityAction.ACTION_PAGE_UP to "pageUp",
            AccessibilityAction.ACTION_PAGE_DOWN to "pageDown",
            AccessibilityAction.ACTION_PAGE_LEFT to "pageLeft",
            AccessibilityAction.ACTION_PAGE_RIGHT to "pageRight",
            AccessibilityAction.ACTION_SHOW_ON_SCREEN to "showOnScreen",
            AccessibilityAction.ACTION_SCROLL_TO_POSITION to "scrollToPosition",
            AccessibilityAction.ACTION_COPY to "copy",
            AccessibilityAction.ACTION_PASTE to "paste",
            AccessibilityAction.ACTION_CUT to "cut",
            AccessibilityAction.ACTION_SET_SELECTION to "setSelection",
            AccessibilityAction.ACTION_SET_PROGRESS to "setProgress",
            AccessibilityAction.ACTION_EXPAND to "expand",
            AccessibilityAction.ACTION_COLLAPSE to "collapse",
            AccessibilityAction.ACTION_DISMISS to "dismiss",
            AccessibilityAction.ACTION_IME_ENTER to "imeEnter",
            AccessibilityAction.ACTION_PRESS_AND_HOLD to "pressAndHold",
            AccessibilityAction.ACTION_SHOW_TOOLTIP to "showTooltip",
            AccessibilityAction.ACTION_HIDE_TOOLTIP to "hideTooltip",
            AccessibilityAction.ACTION_MOVE_WINDOW to "moveWindow",
            AccessibilityAction.ACTION_DRAG_START to "dragStart",
            AccessibilityAction.ACTION_DRAG_DROP to "dragDrop",
            AccessibilityAction.ACTION_DRAG_CANCEL to "dragCancel",
            AccessibilityAction.ACTION_SHOW_TEXT_SUGGESTIONS to "showTextSuggestions",
        )
        private val ACTION_NAMES = NAMED.associate { (action, name) -> action.id to name }
        private val ACTIONS = NAMED.associate { (action, name) -> name to action.id }
        private val WINDOW_TYPES = mapOf(
            AccessibilityWindowInfo.TYPE_APPLICATION to "application",
            AccessibilityWindowInfo.TYPE_INPUT_METHOD to "inputMethod",
            AccessibilityWindowInfo.TYPE_SYSTEM to "system",
            AccessibilityWindowInfo.TYPE_ACCESSIBILITY_OVERLAY to "accessibilityOverlay",
            AccessibilityWindowInfo.TYPE_SPLIT_SCREEN_DIVIDER to "splitScreenDivider",
            AccessibilityWindowInfo.TYPE_MAGNIFICATION_OVERLAY to "magnificationOverlay",
            AccessibilityWindowInfo.TYPE_WINDOW_CONTROL to "windowControl",
        )
        private val ARGUMENTS = mapOf(
            "start" to AccessibilityNodeInfo.ACTION_ARGUMENT_SELECTION_START_INT,
            "end" to AccessibilityNodeInfo.ACTION_ARGUMENT_SELECTION_END_INT,
            "value" to AccessibilityNodeInfo.ACTION_ARGUMENT_PROGRESS_VALUE,
            "row" to AccessibilityNodeInfo.ACTION_ARGUMENT_ROW_INT,
            "column" to AccessibilityNodeInfo.ACTION_ARGUMENT_COLUMN_INT,
            "x" to AccessibilityNodeInfo.ACTION_ARGUMENT_MOVE_WINDOW_X,
            "y" to AccessibilityNodeInfo.ACTION_ARGUMENT_MOVE_WINDOW_Y,
        )
        private val RANGE_TYPES = mapOf(
            AccessibilityNodeInfo.RangeInfo.RANGE_TYPE_INT to "integer",
            AccessibilityNodeInfo.RangeInfo.RANGE_TYPE_FLOAT to "float",
            AccessibilityNodeInfo.RangeInfo.RANGE_TYPE_PERCENT to "percent",
        )
        private val CREATE = UiAutomation::class.java.getDeclaredConstructor(Looper::class.java, IUiAutomationConnection::class.java)
        private val CONNECT = UiAutomation::class.java.getDeclaredMethod("connect", Int::class.javaPrimitiveType)
        private val SOURCE_NODE_ID = AccessibilityNodeInfo::class.java.getMethod("getSourceNodeId")

        private fun windows(automation: UiAutomation, display: Int): List<AccessibilityWindowInfo> {
            val displays = automation.windowsOnAllDisplays
            return (0 until displays.size()).filter { display < 0 || displays.keyAt(it) == display }.flatMap { displays.valueAt(it) }
        }

        private fun pending(seen: Long, reason: String): JSONObject = JSONObject().put("pending", reason).put("seen", seen)

        private fun offered(node: AccessibilityNodeInfo) = node.actionList.filter { ACTION_NAMES.containsKey(it.id) || it.label != null && !isStandard(it.id) }

        // AccessibilityNodeInfo gives the standard actions up to ACTION_SET_TEXT one bit each, and the later ones the ids of
        // the android package.
        private fun isStandard(id: Int) = id.countOneBits() == 1 && id <= AccessibilityNodeInfo.ACTION_SET_TEXT || (id ushr 24) == 0x01

        private fun actionsOf(node: AccessibilityNodeInfo) = offered(node).map { ACTION_NAMES[it.id] ?: it.label.toString() }

        private fun nameOf(node: AccessibilityNodeInfo): String {
            val label = listOf(node.text, node.contentDescription, node.viewIdResourceName).firstOrNull { !it.isNullOrEmpty() }
                ?: return "an unlabeled ${node.className}"
            return "${node.className} ${JSONObject.quote(label.toString())}"
        }

        // The bounds of a node outside its window are clipped past themselves, which leaves them inverted.
        private fun rect(rect: Rect) = JSONArray().put(rect.left).put(rect.top).put(max(0, rect.width())).put(max(0, rect.height()))
    }
}
