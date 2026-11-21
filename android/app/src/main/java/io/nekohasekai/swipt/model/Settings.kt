package io.nekohasekai.swipt.model

import android.content.Context
import android.content.SharedPreferences
import androidx.compose.runtime.mutableStateOf
import androidx.core.content.edit
import io.nekohasekai.swipt.app.SwiptApplication
import io.nekohasekai.swipt.privilege.Privilege
import kotlin.properties.ReadWriteProperty
import kotlin.reflect.KProperty

object Settings {
    private val preferences: SharedPreferences = SwiptApplication.application.getSharedPreferences("settings", Context.MODE_PRIVATE)

    var privilegeMode: Privilege.Mode by Stored(
        "privilegeMode",
        Privilege.Mode.Shizuku,
        { key -> getString(key, null)?.let { name -> Privilege.Mode.entries.firstOrNull { it.name == name } } },
        { key, mode -> putString(key, mode.name) },
    )

    private class Stored<T>(
        private val key: String,
        initial: T,
        read: SharedPreferences.(String) -> T?,
        private val write: SharedPreferences.Editor.(String, T) -> Unit,
    ) : ReadWriteProperty<Any, T> {
        private val state = mutableStateOf(preferences.read(key) ?: initial)

        override fun getValue(thisRef: Any, property: KProperty<*>): T = state.value

        override fun setValue(thisRef: Any, property: KProperty<*>, value: T) {
            state.value = value
            preferences.edit { write(key, value) }
        }
    }
}
