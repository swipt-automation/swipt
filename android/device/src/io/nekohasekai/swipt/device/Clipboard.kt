package io.nekohasekai.swipt.device

import android.app.IActivityManager
import android.content.ClipData
import android.content.Context
import android.content.IClipboard
import android.content.IOnPrimaryClipChangedListener
import android.os.ServiceManager

// ClipboardService lets a caller read in the background only by a package that holds READ_CLIPBOARD_IN_BACKGROUND, which
// no package of uid 0 does, so under root the clipboard is read in ShellHelper.
internal class Clipboard(private val context: Context, private val activities: IActivityManager) {
    private val clipboard = IClipboard.Stub.asInterface(ServiceManager.getService(Context.CLIPBOARD_SERVICE))
    val state = State("clipboard", ::read, ::listen)

    private fun read(): String? {
        val clip = clipboard.getPrimaryClip(SHELL_PACKAGE, null, activities.currentUserId, DEFAULT_DEVICE)
        return clip?.takeIf { it.itemCount > 0 }?.getItemAt(0)?.coerceToText(context)?.toString()
    }

    fun get(): String = state.get()

    fun set(text: String?) {
        val user = activities.currentUserId
        if (text == null) {
            clipboard.clearPrimaryClip(SHELL_PACKAGE, null, user, DEFAULT_DEVICE)
        } else {
            clipboard.setPrimaryClip(ClipData.newPlainText(null, text), SHELL_PACKAGE, null, user, DEFAULT_DEVICE)
        }
    }

    fun listen(changed: () -> Unit): AutoCloseable {
        val user = activities.currentUserId
        val listener = object : IOnPrimaryClipChangedListener.Stub() {
            override fun dispatchPrimaryClipChanged() = changed()
        }
        clipboard.addPrimaryClipChangedListener(listener, SHELL_PACKAGE, null, user, DEFAULT_DEVICE)
        return AutoCloseable { clipboard.removePrimaryClipChangedListener(listener, SHELL_PACKAGE, null, user, DEFAULT_DEVICE) }
    }

    companion object {
        const val SHELL_PACKAGE = "com.android.shell"
        private const val DEFAULT_DEVICE = 0
    }
}
