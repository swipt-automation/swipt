package io.nekohasekai.swipt.device

import android.app.UiAutomation
import android.graphics.Rect
import android.os.PowerManager
import android.os.SystemClock
import android.view.Display
import android.view.KeyEvent
import android.view.accessibility.AccessibilityNodeInfo
import android.view.accessibility.AccessibilityWindowInfo
import com.android.internal.policy.IKeyguardDismissCallback
import io.nekohasekai.libSwipt.LibSwipt
import java.util.concurrent.CompletableFuture

internal class Keyguard(private val device: Device, private val accessibility: Accessibility, private val input: Input) {
    private enum class Dismissal { SUCCEEDED, CANCELLED, FAILED }

    private sealed interface Bouncer {
        class Pattern(val view: AccessibilityNodeInfo) : Bouncer

        class Field(val field: AccessibilityNodeInfo) : Bouncer
    }

    private val power = device.context.getSystemService(PowerManager::class.java)

    // The device turns interactive before its display finishes turning on, and the other way around.
    private fun wake() {
        if (!power.isInteractive) input.press(Display.DEFAULT_DISPLAY, KeyEvent.KEYCODE_WAKEUP)
        if (!waitUntil(WAKE_LIMIT_MS) { power.isInteractive && displayState() == Display.STATE_ON }) {
            throw Failure(Failure.Name.InvalidStateError, "the display did not turn on within $WAKE_LIMIT_MS ms")
        }
    }

    // The keyguard locks a while after the display turns off, unless the power button turned it off.
    fun lock() {
        if (power.isInteractive) input.press(Display.DEFAULT_DISPLAY, KeyEvent.KEYCODE_SLEEP)
        if (!waitUntil(WAKE_LIMIT_MS) { !power.isInteractive && displayState() != Display.STATE_ON }) {
            throw Failure(Failure.Name.InvalidStateError, "the display did not turn off within $WAKE_LIMIT_MS ms")
        }
        device.windows.lockNow(null)
    }

    private fun displayState() = device.displayManager.getDisplay(Display.DEFAULT_DISPLAY).state

    fun unlock(unlockType: Int, credential: String) {
        wake()
        if (!device.windows.isKeyguardLocked) return
        if (!device.windows.isKeyguardSecure(device.activities.currentUserId)) {
            if (!unlocked(dismiss(), UNLOCK_LIMIT_MS)) {
                throw Failure(Failure.Name.InvalidStateError, "the keyguard stayed locked for $UNLOCK_LIMIT_MS ms after it was dismissed")
            }
            return
        }
        val automation = accessibility.automation()
        val dismissal = dismiss()
        val bouncer = waitForBouncer(automation, dismissal) ?: return
        when {
            unlockType == LibSwipt.UnlockDisabled -> throw Failure(
                Failure.Name.InvalidStateError,
                "the keyguard asks for the credential of the device, and automatic unlock is disabled in Settings of the Swipt app",
            )
            unlockType == LibSwipt.UnlockNone -> throw Failure(
                Failure.Name.InvalidStateError,
                "the keyguard asks for the credential of the device, and Swipt is set to None or Swipe; select Pattern, PIN, or Password in Settings of the Swipt app",
            )
            credential.isEmpty() -> throw Failure(
                Failure.Name.InvalidStateError,
                "the keyguard asks for the credential of the device, and Swipt has no unlock credential; configure Automatic unlock in Settings of the Swipt app",
            )
        }
        when (bouncer) {
            is Bouncer.Pattern -> {
                if (unlockType != LibSwipt.UnlockPattern) {
                    throw Failure(
                        Failure.Name.InvalidStateError,
                        "the keyguard asks for a pattern, while Swipt is not configured for Pattern; update Automatic unlock in Settings of the Swipt app",
                    )
                }
                unlockPattern(bouncer.view, credential, dismissal)
            }
            is Bouncer.Field -> {
                if (unlockType == LibSwipt.UnlockPattern) {
                    throw Failure(
                        Failure.Name.InvalidStateError,
                        "the keyguard asks for a PIN or password, while Swipt is configured for Pattern; update Automatic unlock in Settings of the Swipt app",
                    )
                }
                unlockCredential(unlockType, credential, bouncer.field, dismissal)
            }
        }
    }

    private fun unlockPattern(patternView: AccessibilityNodeInfo, pattern: String, dismissal: CompletableFuture<Dismissal>) {
        val bounds = Rect().also(patternView::getBoundsInScreen)
        if (bounds.width() <= 0 || bounds.height() <= 0) {
            throw Failure(Failure.Name.InvalidStateError, "the pattern view of the keyguard has no screen bounds")
        }
        val points = Array(pattern.length) { i ->
            val cell = pattern[i] - '1'
            if (cell !in 0..8) throw Failure(Failure.Name.TypeError, "an Android pattern can contain only dots 1 to 9")
            doubleArrayOf(bounds.left + bounds.width() * (cell % 3 + 0.5) / 3.0, bounds.top + bounds.height() * (cell / 3 + 0.5) / 3.0)
        }
        input.path(points, PATTERN_STEP_MS, PATTERN_HOLD_MS)
        if (!unlocked(dismissal, UNLOCK_LIMIT_MS)) {
            throw Failure(
                Failure.Name.InvalidStateError,
                "the device stayed locked for $UNLOCK_LIMIT_MS ms after the pattern was drawn, as happens when the pattern is wrong",
            )
        }
    }

    private fun unlockCredential(unlockType: Int, credential: String, field: AccessibilityNodeInfo, dismissal: CompletableFuture<Dismissal>) {
        if (entered(field) > 0) {
            input.press(Display.DEFAULT_DISPLAY, KeyEvent.KEYCODE_MOVE_END)
            repeat(entered(field)) { input.press(Display.DEFAULT_DISPLAY, KeyEvent.KEYCODE_DEL) }
            if (!waitUntil(ENTRY_LIMIT_MS) { field.refresh() && entered(field) == 0 }) {
                throw Failure(Failure.Name.InvalidStateError, "the keyguard kept what was entered in it before")
            }
        }
        input.type(credential)
        val complete = waitUntil(ENTRY_LIMIT_MS) {
            !device.windows.isKeyguardLocked || field.refresh() && entered(field) == credential.length
        }
        val took = entered(field)
        if (complete && device.windows.isKeyguardLocked) {
            // Android's auto-confirm unlock submits 6+ digit PINs automatically after a brief delay.
            val confirmed = unlockType == LibSwipt.UnlockPIN && credential.length >= 6 && unlocked(dismissal, AUTO_CONFIRM_GRACE_MS)
            if (!confirmed) input.press(Display.DEFAULT_DISPLAY, KeyEvent.KEYCODE_ENTER)
        }
        if (!unlocked(dismissal, UNLOCK_LIMIT_MS)) {
            val outcome = if (complete) ", as happens when the credential is wrong" else "; the keyguard took $took of its ${credential.length} characters"
            throw Failure(Failure.Name.InvalidStateError, "the device stayed locked for $UNLOCK_LIMIT_MS ms after the credential was entered$outcome")
        }
    }

    private fun waitForBouncer(automation: UiAutomation, dismissal: CompletableFuture<Dismissal>): Bouncer? {
        val deadline = SystemClock.uptimeMillis() + BOUNCER_LIMIT_MS
        while (true) {
            Call.check()
            val focused = automation.findFocus(AccessibilityNodeInfo.FOCUS_INPUT)
            if (focused != null && focused.isPassword && focused.window?.type == AccessibilityWindowInfo.TYPE_SYSTEM) return Bouncer.Field(focused)
            bouncer(automation)?.let { return it }
            if (unlocked(dismissal, 0)) return null
            if (SystemClock.uptimeMillis() >= deadline) {
                throw Failure(Failure.Name.InvalidStateError, "the keyguard did not show a recognized lock screen within $BOUNCER_LIMIT_MS ms")
            }
            Thread.sleep(POLL_MS)
        }
    }

    private fun dismiss(): CompletableFuture<Dismissal> {
        val dismissal = CompletableFuture<Dismissal>()
        device.windows.dismissKeyguard(
            object : IKeyguardDismissCallback.Stub() {
                override fun onDismissError() {
                    dismissal.complete(Dismissal.FAILED)
                }

                override fun onDismissSucceeded() {
                    dismissal.complete(Dismissal.SUCCEEDED)
                }

                override fun onDismissCancelled() {
                    dismissal.complete(Dismissal.CANCELLED)
                }
            },
            null,
        )
        return dismissal
    }

    private fun unlocked(dismissal: CompletableFuture<Dismissal>, limitMs: Long): Boolean {
        val deadline = SystemClock.uptimeMillis() + limitMs
        while (true) {
            Call.check()
            if (!device.windows.isKeyguardLocked) return true
            when (dismissal.getNow(null)) {
                Dismissal.SUCCEEDED -> {
                    if (waitUntil(UNLOCK_LIMIT_MS) { !device.windows.isKeyguardLocked }) return true
                    throw Failure(Failure.Name.InvalidStateError, "the keyguard was dismissed, but stayed locked for $UNLOCK_LIMIT_MS ms")
                }
                Dismissal.CANCELLED -> throw Failure(Failure.Name.InvalidStateError, "the keyguard canceled the unlock, as it does when its bouncer closes")
                Dismissal.FAILED -> throw Failure(Failure.Name.InvalidStateError, "the keyguard refused to be dismissed")
                null -> {}
            }
            if (SystemClock.uptimeMillis() >= deadline) return false
            Thread.sleep(POLL_MS)
        }
    }

    companion object {
        private const val WAKE_LIMIT_MS = 2000L
        private const val BOUNCER_LIMIT_MS = 2000L
        private const val ENTRY_LIMIT_MS = 1000L
        private const val AUTO_CONFIRM_GRACE_MS = 500L
        private const val UNLOCK_LIMIT_MS = 5000L
        private const val PATTERN_STEP_MS = 100L
        private const val PATTERN_HOLD_MS = 40L
        private const val POLL_MS = 20L

        // The Compose bouncer of SystemUI leaves its PIN entry unfocused, with the input focus on the first key of its pad.
        private fun bouncer(automation: UiAutomation): Bouncer? {
            for (window in automation.windows) {
                if (window.type != AccessibilityWindowInfo.TYPE_SYSTEM) continue
                val stack = ArrayDeque(listOf(window.root ?: continue))
                while (stack.isNotEmpty()) {
                    Call.check()
                    val node = stack.removeLast()
                    if (node.isVisibleToUser) {
                        if (node.viewIdResourceName?.endsWith(":id/lockPatternView") == true) return Bouncer.Pattern(node)
                        if (node.isPassword) return Bouncer.Field(node)
                    }
                    for (i in node.childCount - 1 downTo 0) {
                        node.getChild(i)?.let(stack::addLast)
                    }
                }
            }
            return null
        }

        // The bouncer shows one mask character for each character entered.
        private fun entered(field: AccessibilityNodeInfo) = (if (field.isShowingHintText) null else field.text)?.length ?: 0

        private fun waitUntil(limitMs: Long, condition: () -> Boolean): Boolean {
            val deadline = SystemClock.uptimeMillis() + limitMs
            while (!condition()) {
                Call.check()
                if (SystemClock.uptimeMillis() >= deadline) return false
                Thread.sleep(POLL_MS)
            }
            return true
        }
    }
}
