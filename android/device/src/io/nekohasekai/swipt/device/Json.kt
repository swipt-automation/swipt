package io.nekohasekai.swipt.device

import org.json.JSONObject

internal fun jsonOf(value: Any?): String = when (value) {
    null -> "null"
    is String -> JSONObject.quote(value)
    else -> value.toString()
}

internal fun JSONObject.putText(key: String, value: CharSequence?): JSONObject = apply {
    if (!value.isNullOrEmpty()) put(key, value.toString())
}

internal fun JSONObject.putFlag(key: String, value: Boolean): JSONObject = apply {
    if (value) put(key, true)
}
