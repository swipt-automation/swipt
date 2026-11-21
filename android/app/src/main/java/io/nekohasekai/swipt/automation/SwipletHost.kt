package io.nekohasekai.swipt.automation

import android.app.Activity
import android.app.Application
import android.content.ComponentCallbacks2
import android.content.MutableContextWrapper
import android.content.res.Configuration
import android.util.Log
import android.view.View
import android.view.ViewGroup
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import com.facebook.react.interfaces.fabric.ReactSurface
import io.nekohasekai.swipt.core.Core
import io.nekohasekai.swipt.proto.GetSwipletInterfaceResponse
import io.nekohasekai.swipt.proto.getSwipletInterfaceRequest
import kotlinx.coroutines.MainScope
import kotlinx.coroutines.launch

class SwipletEntry(val swiplet: Swiplet, val source: GetSwipletInterfaceResponse) {
    val id: String get() = swiplet.id

    override fun equals(other: Any?): Boolean = other is SwipletEntry && other.id == id && other.swiplet.installed == swiplet.installed

    override fun hashCode(): Int = 31 * id.hashCode() + swiplet.installed.hashCode()
}

class SwipletSurface internal constructor(
    private val id: String,
    private val surface: ReactSurface,
    private val context: MutableContextWrapper,
    val runtime: SwipletRuntime,
) {
    private var stopped = false
    private var mounted = false
    private val pending = ArrayList<() -> Unit>()
    private var failure: String? = null
    private var onFailure: ((String) -> Unit)? = null

    val entry: SwipletEntry get() = runtime.entry

    val view: View get() = surface.view!!

    fun attach(activity: Activity): View {
        context.baseContext = activity
        (view.parent as? ViewGroup)?.removeView(view)
        return view
    }

    fun whenMounted(action: () -> Unit) {
        if (mounted) action() else pending += action
    }

    internal fun didMount() {
        mounted = true
        val actions = pending.toList()
        pending.clear()
        actions.forEach { it() }
    }

    fun whenFailed(action: (String) -> Unit) {
        val failure = failure
        if (failure != null) action(failure) else onFailure = action
    }

    internal fun fail(message: String) {
        runtime.broken = true
        if (failure != null) return
        failure = message
        onFailure?.invoke(message)
    }

    fun stop() {
        if (stopped) return
        stopped = true
        surface.stop()
        runtime.removeSurface(id)
        SwipletHost.detach(runtime)
    }
}

object SwipletHost {
    private const val TAG = "SwipletHost"
    private const val PREPARED_LIMIT = 2

    private lateinit var application: Application
    private val runtimes = HashMap<String, SwipletRuntime>()
    private val recent = ArrayList<String>()
    private val prepared = HashMap<String, SwipletSurface>()
    private val presented = ArrayList<Pair<String, () -> Unit>>()

    var interfaces by mutableStateOf(emptyMap<String, SwipletEntry>())
        private set

    fun start(application: Application) {
        this.application = application
        application.registerComponentCallbacks(object : ComponentCallbacks2 {
            override fun onTrimMemory(level: Int) {
                if (level >= ComponentCallbacks2.TRIM_MEMORY_BACKGROUND) releasePrepared()
            }

            override fun onConfigurationChanged(newConfig: Configuration) {}

            @Deprecated("Deprecated in Java")
            override fun onLowMemory() = releasePrepared()
        })
        MainScope().launch {
            snapshotFlow { SwipletStore.swiplets }.collect { swiplets ->
                interfaces = swiplets.filter(Swiplet::hasInterface).mapNotNull { swiplet ->
                    interfaces[swiplet.id]?.takeIf { it.swiplet.installed == swiplet.installed }?.let { SwipletEntry(swiplet, it.source) }
                        ?: try {
                            SwipletEntry(swiplet, Core.swiplets.getSwipletInterface(getSwipletInterfaceRequest { swipletId = swiplet.id }))
                        } catch (e: Exception) {
                            Log.w(TAG, "read the interface of ${swiplet.id}", e)
                            null
                        }
                }.associateBy(SwipletEntry::id)
                prepare()
            }
        }
    }

    fun take(entry: SwipletEntry): SwipletSurface {
        prepared.remove(entry.id)?.let { surface ->
            if (surface.entry == entry) return surface
            surface.stop()
        }
        return makeSurface(entry)
    }

    fun makeSurface(entry: SwipletEntry): SwipletSurface {
        val runtime = runtimes[entry.id]?.takeIf { it.entry == entry && !it.broken }
            ?: SwipletRuntime(application, entry).also { runtimes[entry.id] = it }
        runtime.attached++
        return runtime.createSurface()
    }

    internal fun detach(runtime: SwipletRuntime) {
        runtime.attached--
        if (runtime.attached > 0) return
        if (runtimes[runtime.swiplet] === runtime) runtimes.remove(runtime.swiplet)
        runtime.terminate()
    }

    fun didPresent(name: String, exit: () -> Unit) {
        presented.removeAll { it.first == name }
        presented += name to exit
        recent.remove(name)
        recent += name
        prepare()
    }

    fun didDismiss(name: String) {
        presented.removeAll { it.first == name }
        prepare()
    }

    fun exit(swiplet: String) {
        presented.lastOrNull { it.first == swiplet }?.second?.invoke()
    }

    fun isPresented(name: String): Boolean = presented.any { it.first == name }

    private fun prepare() {
        val wanted = recent.filter { interfaces[it] != null && !isPresented(it) }.takeLast(PREPARED_LIMIT)
        val iterator = prepared.iterator()
        while (iterator.hasNext()) {
            val (name, surface) = iterator.next()
            if (name !in wanted && !isPresented(name) || surface.entry != interfaces[name]) {
                iterator.remove()
                surface.stop()
            }
        }
        for (name in wanted) {
            if (name !in prepared) prepared[name] = makeSurface(interfaces.getValue(name))
        }
    }

    private fun releasePrepared() {
        val surfaces = prepared.values.toList()
        prepared.clear()
        surfaces.forEach(SwipletSurface::stop)
    }
}
