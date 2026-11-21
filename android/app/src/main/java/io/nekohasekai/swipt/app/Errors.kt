package io.nekohasekai.swipt.app

import io.grpc.Status
import io.grpc.StatusException

open class LocalizedException(override val message: String, cause: Throwable? = null) : Exception(message, cause)

val Throwable.localized: LocalizedException?
    get() = generateSequence(this, Throwable::cause).filterIsInstance<LocalizedException>().firstOrNull()

val Throwable.description: String
    get() {
        localized?.let { return it.message }
        if (this is StatusException) {
            if (status.code == Status.Code.UNAVAILABLE) generateSequence(cause, Throwable::cause).lastOrNull()?.message?.let { return it }
            status.description?.let { return it }
        }
        return message ?: toString()
    }
