package io.nekohasekai.swipt.app

import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.MutableState
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.runtime.key
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.ui.res.stringResource
import io.nekohasekai.swipt.R

internal data class TopBarEntry(val key: Any, val content: @Composable () -> Unit)

class TopBarController internal constructor(private val state: MutableState<List<TopBarEntry>>) {
    @Composable
    fun Content() {
        val entry = state.value.lastOrNull() ?: return
        key(entry.key) { entry.content() }
    }

    fun set(key: Any, content: @Composable () -> Unit) {
        state.value = state.value.filterNot { it.key == key } + TopBarEntry(key, content)
    }

    fun clear(key: Any) {
        state.value = state.value.filterNot { it.key == key }
    }
}

val LocalTopBarController = compositionLocalOf<TopBarController?> { null }

val LocalScaffoldPadding = compositionLocalOf { PaddingValues() }

@Composable
fun OverrideTopBar(content: @Composable () -> Unit) {
    val controller = LocalTopBarController.current
    val token = remember { Any() }
    val currentContent = rememberUpdatedState(content)
    DisposableEffect(controller, token) {
        controller?.set(token) { currentContent.value() }
        onDispose { controller?.clear(token) }
    }
}

@Composable
fun BackButton(onClick: () -> Unit) {
    IconButton(onClick = onClick) {
        Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = stringResource(R.string.back))
    }
}
