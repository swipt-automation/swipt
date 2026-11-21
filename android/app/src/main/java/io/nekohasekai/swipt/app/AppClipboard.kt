package io.nekohasekai.swipt.app

import android.content.ClipData
import android.content.ClipboardManager

object AppClipboard {
    fun write(text: String) {
        val clipboard = SwiptApplication.application.getSystemService(ClipboardManager::class.java)
        clipboard.setPrimaryClip(ClipData.newPlainText(null, text))
    }
}
