package io.nekohasekai.swipt.device

import android.util.Log
import java.lang.reflect.InvocationTargetException
import java.util.concurrent.ExecutionException
import org.json.JSONException

// gomobile hands Go only the message of an exception.
internal class Failure(val name: Name, message: String) : Exception() {
    enum class Name {
        TypeError,
        NotFoundError,
        NotAllowedError,
        NotSupportedError,
        InvalidStateError,
        UnavailableError,
        TimeoutError,
        AbortError,
        Error,
    }

    override val message = "$name: $message"

    companion object {
        inline fun <T> guard(method: String, body: () -> T): T = try {
            body()
        } catch (thrown: Throwable) {
            throw of(method, thrown)
        }

        fun of(method: String, error: Throwable): Failure {
            var cause = error
            while (cause is InvocationTargetException || cause is ExecutionException) {
                cause = cause.cause ?: break
            }
            return when (cause) {
                is Failure -> cause
                is InterruptedException -> Failure(Name.AbortError, "$method was canceled")
                is SecurityException -> Failure(Name.NotAllowedError, cause.message.toString())
                is JSONException -> Failure(Name.TypeError, "$method got malformed parameters: ${cause.message}")
                is IllegalArgumentException -> Failure(Name.TypeError, cause.message.toString())
                else -> {
                    Log.w(TAG, "$method failed", cause)
                    Failure(Name.Error, "$method failed with $cause")
                }
            }
        }
    }
}
