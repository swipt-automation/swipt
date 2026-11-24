package io.nekohasekai.swipt.settings

import android.content.Context
import androidx.annotation.StringRes
import io.nekohasekai.swipt.R
import io.nekohasekai.swipt.app.ErrorAlert
import io.nekohasekai.swipt.app.SwiptApplication
import io.nekohasekai.swipt.core.Core
import io.nekohasekai.swipt.proto.UnlockType
import io.nekohasekai.swipt.proto.Settings as CoreSettings

enum class AndroidUnlockType(
    val proto: UnlockType,
    @param:StringRes val titleRes: Int,
    @param:StringRes val subtitleRes: Int,
) {
    Pattern(UnlockType.UNLOCK_TYPE_PATTERN, R.string.settings_lock_pattern, R.string.settings_lock_pattern_detail),
    Pin(UnlockType.UNLOCK_TYPE_PIN, R.string.settings_lock_pin, R.string.settings_lock_pin_detail),
    Password(UnlockType.UNLOCK_TYPE_PASSWORD, R.string.settings_lock_password, R.string.settings_lock_password_detail),
    None(UnlockType.UNLOCK_TYPE_NONE, R.string.settings_lock_none, R.string.settings_lock_none_detail),
    ;

    fun problem(credential: String, context: Context): String? = when (this) {
        None -> null
        Pattern -> when {
            credential.length < AutoUnlock.MINIMUM_LENGTH -> context.getString(R.string.settings_pattern_too_short)
            credential.length > AutoUnlock.MAXIMUM_PATTERN || credential.any { it !in '1'..'9' } || credential.toSet().size != credential.length ->
                context.getString(R.string.settings_lock_pattern_detail)
            else -> null
        }

        Pin -> when {
            credential.any { it !in '0'..'9' } -> context.getString(R.string.settings_pin_digits)
            credential.length !in AutoUnlock.MINIMUM_LENGTH..AutoUnlock.MAXIMUM_LENGTH ->
                context.getString(R.string.settings_pin_length, AutoUnlock.MINIMUM_LENGTH, AutoUnlock.MAXIMUM_LENGTH)
            else -> null
        }

        Password -> when {
            credential.any { it !in ' '..'~' } -> context.getString(R.string.settings_password_characters)
            credential.length !in AutoUnlock.MINIMUM_LENGTH..AutoUnlock.MAXIMUM_LENGTH ->
                context.getString(R.string.settings_password_length, AutoUnlock.MINIMUM_LENGTH, AutoUnlock.MAXIMUM_LENGTH)
            else -> null
        }
    }
}

val CoreSettings.androidUnlockType: AndroidUnlockType?
    get() = AndroidUnlockType.entries.firstOrNull { it.proto == unlockType }

val CoreSettings.isUnlockConfigured: Boolean
    get() = when (androidUnlockType) {
        null -> false
        AndroidUnlockType.None -> true
        else -> unlockPasscode.isNotEmpty()
    }

object AutoUnlock {
    const val MINIMUM_LENGTH = 4
    const val MAXIMUM_LENGTH = 16
    const val MAXIMUM_PATTERN = 9

    suspend fun update(transform: CoreSettings.Builder.() -> Unit): Boolean = try {
        Core.updateSettings(transform)
        true
    } catch (e: Exception) {
        ErrorAlert.report(SwiptApplication.application.getString(R.string.couldnt_save_settings), e)
        false
    }
}
