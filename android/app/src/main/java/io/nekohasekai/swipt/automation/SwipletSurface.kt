package io.nekohasekai.swipt.automation

import android.app.Activity
import androidx.activity.compose.BackHandler
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.viewinterop.AndroidView
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import com.facebook.react.modules.core.DefaultHardwareBackBtnHandler
import io.nekohasekai.swipt.R
import io.nekohasekai.swipt.app.AlertState
import io.nekohasekai.swipt.app.ErrorAlert

@Composable
fun SwipletInterface(name: String, entry: SwipletEntry, exit: () -> Unit, modifier: Modifier = Modifier) {
    val activity = LocalContext.current as Activity
    val presentation = remember(name) { SwipletPresentation(activity, name, entry) }
    val current = SwipletHost.interfaces[name]
    LaunchedEffect(presentation, current) {
        if (current != null) presentation.show(current)
    }
    DisposableEffect(presentation) {
        SwipletHost.didPresent(name, exit)
        onDispose {
            SwipletHost.didDismiss(name)
            presentation.stop()
        }
    }
    val displayed = presentation.displayed
    DisposableEffect(displayed) {
        onDispose { if (presentation.displayed !== displayed) displayed.stop() }
    }
    val lifecycleOwner = LocalLifecycleOwner.current
    DisposableEffect(displayed.runtime, lifecycleOwner) {
        val host = displayed.runtime.host
        val back = DefaultHardwareBackBtnHandler { exit() }
        val observer = LifecycleEventObserver { _, event ->
            when (event) {
                Lifecycle.Event.ON_RESUME -> host.onHostResume(activity, back)
                Lifecycle.Event.ON_PAUSE -> host.onHostPause(activity)
                Lifecycle.Event.ON_DESTROY -> host.onHostDestroy(activity)
                else -> {}
            }
        }
        lifecycleOwner.lifecycle.addObserver(observer)
        onDispose { lifecycleOwner.lifecycle.removeObserver(observer) }
    }
    BackHandler {
        if (!displayed.runtime.host.onBackPressed()) exit()
    }
    key(displayed) {
        AndroidView(factory = { displayed.attach(activity) }, modifier = modifier)
    }
}

private class SwipletPresentation(private val activity: Activity, private val name: String, entry: SwipletEntry) {
    var displayed by mutableStateOf(SwipletHost.take(entry))
        private set
    private var loading: SwipletSurface? = null

    init {
        watch(displayed)
    }

    fun show(entry: SwipletEntry) {
        if (entry == (loading ?: displayed).entry) return
        loading?.stop()
        val next = SwipletHost.makeSurface(entry)
        loading = next
        next.whenMounted {
            if (loading !== next) return@whenMounted
            loading = null
            watch(next)
            displayed = next
        }
    }

    fun stop() {
        displayed.stop()
        loading?.stop()
    }

    private fun watch(surface: SwipletSurface) {
        surface.whenFailed { message ->
            val title = activity.getString(R.string.couldnt_open_swiplet, surface.entry.swiplet.title)
            ErrorAlert.report(AlertState.failure(title, message) { SwipletHost.exit(name) })
        }
    }
}
