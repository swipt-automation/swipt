package io.nekohasekai.swipt.app

import io.nekohasekai.swipt.R

data class AlertState(
    val title: String,
    val message: String = "",
    val confirm: Button = Button(SwiptApplication.application.getString(R.string.ok)),
    val dismiss: Button? = null,
    val onDismiss: () -> Unit = {},
    val copyText: String? = null,
) {
    class Button(val label: String, val destructive: Boolean = false, val onClick: () -> Unit = {})

    companion object {
        fun failure(failure: String, error: Throwable?): AlertState {
            val localized = error?.localized ?: return failure(failure, error?.description.orEmpty())
            return AlertState(failure, localized.message)
        }

        fun failure(failure: String, errorMessage: String, onClose: () -> Unit = {}): AlertState {
            val message = errorMessage.trim()
            return AlertState(
                failure,
                message,
                confirm = Button(SwiptApplication.application.getString(R.string.ok), onClick = onClose),
                onDismiss = onClose,
                copyText = if (message.isEmpty()) null else "$failure\n$message",
            )
        }
    }
}
