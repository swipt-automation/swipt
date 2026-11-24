package io.nekohasekai.swipt.automation

import android.app.Application
import android.content.MutableContextWrapper
import android.net.Uri
import android.os.Bundle
import android.util.Log
import com.facebook.react.ReactInstanceEventListener
import com.facebook.react.ReactPackage
import com.facebook.react.ReactPackageTurboModuleManagerDelegate
import com.facebook.react.bridge.JSBundleLoader
import com.facebook.react.bridge.ReactContext
import com.facebook.react.common.annotations.UnstableReactNativeAPI
import com.facebook.react.defaults.DefaultComponentsRegistry
import com.facebook.react.defaults.DefaultTurboModuleManagerDelegate
import com.facebook.react.fabric.ComponentFactory
import com.facebook.react.modules.blob.BlobModule
import com.facebook.react.runtime.BindingsInstaller
import com.facebook.react.runtime.JSRuntimeFactory
import com.facebook.react.runtime.ReactHostDelegate
import com.facebook.react.runtime.ReactHostImpl
import com.facebook.react.runtime.hermes.HermesInstance
import io.nekohasekai.swipt.R
import io.nekohasekai.swipt.app.AlertState
import io.nekohasekai.swipt.app.ErrorAlert
import io.nekohasekai.swipt.core.Core
import io.nekohasekai.swipt.proto.LogLevel
import io.nekohasekai.swipt.proto.writeLogRequest
import io.nekohasekai.swipt.runtime.JavaScriptInterrupter
import io.nekohasekai.swipt.runtime.SwiptHostDelegate
import io.nekohasekai.swipt.runtime.SwiptHostModule
import kotlinx.coroutines.Job
import kotlinx.coroutines.MainScope
import kotlinx.coroutines.async
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch
import java.io.File
import java.util.UUID
import java.util.concurrent.TimeUnit
import kotlin.concurrent.thread

@OptIn(UnstableReactNativeAPI::class)
class SwipletRuntime(private val application: Application, val entry: SwipletEntry) : SwiptHostDelegate {
    override val swiplet: String = entry.id
    internal val host: ReactHostImpl
    private val surfaces = HashMap<String, SwipletSurface>()
    private val scope = MainScope()
    private val calls = HashMap<String, Job>()
    private var terminated = false
    internal var attached = 0
    internal var broken = false

    init {
        val delegate = object : ReactHostDelegate {
            override val jsMainModulePath = "index"
            override val bindingsInstaller: BindingsInstaller? = null

            // ModuleRegistryAdapter keeps the modules of the
            // last React context that it created them for (expo-modules-core 58.0.29,
            // android/src/main/java/expo/modules/adapters/react/ModuleRegistryAdapter.java).
            override val reactPackages: List<ReactPackage> = SwipletSandbox.packages(application)
            override val jsRuntimeFactory: JSRuntimeFactory = HermesInstance()
            override val jsBundleLoader: JSBundleLoader =
                JSBundleLoader.createAssetLoader(application, "assets://main.jsbundle", true)
            override val turboModuleManagerDelegateBuilder: ReactPackageTurboModuleManagerDelegate.Builder =
                DefaultTurboModuleManagerDelegate.Builder()

            override fun handleInstanceException(error: Exception) {
                Log.e(TAG, "$swiplet: the React Native instance failed", error)
                report(error.toString())
            }
        }
        val componentFactory = ComponentFactory()
        DefaultComponentsRegistry.register(componentFactory)
        host = ReactHostImpl(application, delegate, componentFactory, allowPackagerServerAccess = false, useDevSupport = false)
        host.addReactInstanceEventListener(object : ReactInstanceEventListener {
            override fun onReactContextInitialized(context: ReactContext) {
                SwiptHostModule.bind(context, this@SwipletRuntime)
            }
        })
    }

    internal fun createSurface(): SwipletSurface {
        val id = UUID.randomUUID().toString()
        val source = entry.source
        val service = Core.services.value[swiplet]
        val props = Bundle().apply {
            putString("surface", id)
            putString("name", swiplet)
            putString("file", source.file.substringAfterLast('/'))
            putString("source", source.source)
            putString("dataDirectory", directoryURI(source.dataDirectory))
            putString("assetsDirectory", directoryURI(source.assetsDirectory))
            putString("cacheDirectory", directoryURI(source.cacheDirectory))
            putStringArray("streams", entry.swiplet.streams.toTypedArray())
            putBundle(
                "service",
                Bundle().apply {
                    putBoolean("enabled", service?.enabled ?: false)
                    putString("status", service?.statusName ?: "stopped")
                    putString("error", service?.error.orEmpty())
                    putDouble("since", service?.since?.toDouble() ?: 0.0)
                },
            )
        }
        val context = MutableContextWrapper(application)
        val reactSurface = host.createSurface(context, "main", props)
        val surface = SwipletSurface(id, reactSurface, context, this)
        surfaces[id] = surface
        reactSurface.start()
        return surface
    }

    internal fun removeSurface(id: String) {
        surfaces.remove(id)
    }

    internal fun terminate() {
        if (terminated) return
        terminated = true
        scope.cancel()
        SwiptHostModule.unbind(this)
        thread(name = "swiplet-terminate") {
            host.currentReactContext?.let { context ->
                val interruptions = JavaScriptInterrupter.interruptUntilIdle(context)
                Log.i(TAG, "$swiplet: interrupted its JavaScript $interruptions times")
            }
            host.destroy("the swiplet exited", null).waitForCompletion(10, TimeUnit.SECONDS)
        }
    }

    override suspend fun call(id: String, action: String, input: String, value: (String) -> Unit): String {
        val call = scope.async { Core.callAction(swiplet, action, input, value) }
        calls[id] = call
        try {
            return call.await()
        } finally {
            calls.remove(id)
        }
    }

    override fun cancelCall(id: String) {
        calls[id]?.cancel()
    }

    override suspend fun setServiceEnabled(enabled: Boolean) = Core.setServiceEnabled(swiplet, enabled)

    override fun surfaceDidMount(surface: String) {
        surfaces[surface]?.didMount()
    }

    override fun exit() {
        SwipletHost.exit(swiplet)
    }

    override fun log(level: String, message: String) {
        val request = writeLogRequest {
            swipletId = swiplet
            this.level = when (level) {
                "error" -> LogLevel.LOG_LEVEL_ERROR
                "warn" -> LogLevel.LOG_LEVEL_WARN
                "debug" -> LogLevel.LOG_LEVEL_DEBUG
                else -> LogLevel.LOG_LEVEL_INFO
            }
            this.message = message
        }
        scope.launch {
            try {
                Core.interfaces.writeLog(request)
            } catch (e: Exception) {
                Log.w(TAG, "$swiplet: $message", e)
            }
        }
    }

    override fun fail(surface: String, message: String) {
        surfaces[surface]?.fail(message)
    }

    override fun report(message: String) {
        ErrorAlert.report(AlertState.failure(application.getString(R.string.swiplet_error, entry.swiplet.title), message), swiplet)
    }

    override fun storeBlob(data: ByteArray, id: String) {
        blobs()?.store(data, id)
    }

    override fun removeBlob(id: String) {
        blobs()?.remove(id)
    }

    private fun blobs(): BlobModule? = host.currentReactContext?.getNativeModule(BlobModule::class.java)

    private companion object {
        const val TAG = "SwipletRuntime"

        fun directoryURI(directory: String): String = Uri.fromFile(File(directory)).toString()
    }
}
