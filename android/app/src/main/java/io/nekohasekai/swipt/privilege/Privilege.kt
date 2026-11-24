package io.nekohasekai.swipt.privilege

import android.app.Activity
import android.app.Application
import android.content.ComponentName
import android.content.Intent
import android.content.ServiceConnection
import android.content.pm.PackageManager
import android.os.Bundle
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import com.topjohnwu.superuser.Shell
import com.topjohnwu.superuser.ipc.RootService
import io.nekohasekai.swipt.BuildConfig
import io.nekohasekai.swipt.app.SwiptApplication
import io.nekohasekai.swipt.model.Settings
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import rikka.shizuku.Shizuku
import java.io.IOException

object Privilege {
    enum class Mode { Shizuku, Root }

    sealed interface State {
        data object Connecting : State

        data object Connected : State

        data object ShizukuNotRunning : State

        data object ShizukuNotGranted : State

        data object ShizukuRoot : State

        data object RootDenied : State

        data class Exited(val reason: String?) : State
    }

    // su waits while its manager app asks the user, which Magisk 26.4 gives up on after 70 seconds.
    private const val ROOT_TIMEOUT = 75L

    private val main = Handler(Looper.getMainLooper())
    private val _mode = MutableStateFlow(Settings.privilegeMode)
    val mode: StateFlow<Mode> = _mode.asStateFlow()
    private val _state = MutableStateFlow<State>(State.Connecting)
    val state: StateFlow<State> = _state.asStateFlow()

    private var host: Host? = null

    private val _service = MutableStateFlow<ICoreService?>(null)
    val service: StateFlow<ICoreService?> = _service.asStateFlow()

    fun start(application: Application) {
        Shell.setDefaultBuilder(Shell.Builder.create().setTimeout(ROOT_TIMEOUT))
        Shizuku.addBinderReceivedListenerSticky {
            if (mode.value == Mode.Shizuku) {
                release()
                evaluate()
            }
        }
        Shizuku.addBinderDeadListener {
            if (mode.value == Mode.Shizuku) {
                release()
                _state.value = State.ShizukuNotRunning
            }
        }
        Shizuku.addRequestPermissionResultListener { _, result ->
            if (mode.value == Mode.Shizuku && result == PackageManager.PERMISSION_GRANTED) evaluate()
        }
        application.registerActivityLifecycleCallbacks(object : Application.ActivityLifecycleCallbacks {
            override fun onActivityResumed(activity: Activity) {
                if (state.value == State.ShizukuNotRunning || state.value == State.ShizukuNotGranted || state.value == State.ShizukuRoot) {
                    evaluate()
                }
            }

            override fun onActivityCreated(activity: Activity, savedInstanceState: Bundle?) {}

            override fun onActivityStarted(activity: Activity) {}

            override fun onActivityPaused(activity: Activity) {}

            override fun onActivityStopped(activity: Activity) {}

            override fun onActivitySaveInstanceState(activity: Activity, outState: Bundle) {}

            override fun onActivityDestroyed(activity: Activity) {}
        })
        evaluate()
    }

    fun setMode(mode: Mode) {
        if (mode == this.mode.value) return
        Settings.privilegeMode = mode
        _mode.value = mode
        reconnect()
    }

    fun requestShizukuPermission() {
        if (Shizuku.pingBinder()) Shizuku.requestPermission(0)
    }

    fun reconnect() {
        release()
        evaluate()
    }

    private fun evaluate() {
        if (host != null) return
        when (mode.value) {
            Mode.Shizuku -> when {
                !Shizuku.pingBinder() -> _state.value = State.ShizukuNotRunning
                !shizukuGranted() -> _state.value = State.ShizukuNotGranted
                shizukuRoot() -> _state.value = State.ShizukuRoot
                else -> bind(ShizukuHost())
            }

            Mode.Root -> bind(RootHost())
        }
    }

    private fun shizukuGranted(): Boolean = try {
        Shizuku.checkSelfPermission() == PackageManager.PERMISSION_GRANTED
    } catch (_: RuntimeException) {
        false
    }

    private fun shizukuRoot(): Boolean = try {
        Shizuku.getUid() == 0
    } catch (_: RuntimeException) {
        false
    }

    private fun bind(host: Host) {
        this.host = host
        _state.value = State.Connecting
        try {
            host.bind()
        } catch (e: RuntimeException) {
            exited(host, e.message ?: e.toString())
        }
    }

    private fun release() {
        val host = host ?: return
        this.host = null
        _service.value = null
        host.stop()
    }

    private fun connected(host: Host, binder: IBinder?) {
        if (this.host !== host) {
            host.discard()
            return
        }
        _service.value = ICoreService.Stub.asInterface(binder)
        _state.value = State.Connected
    }

    private fun exited(host: Host, reason: String?) {
        if (this.host !== host) return
        this.host = null
        _service.value = null
        host.unbind()
        _state.value = if (mode.value == Mode.Shizuku && !Shizuku.pingBinder()) State.ShizukuNotRunning else State.Exited(reason)
    }

    private fun denied(host: Host) {
        if (this.host !== host) return
        this.host = null
        host.unbind()
        _state.value = State.RootDenied
    }

    private abstract class Host : ServiceConnection {
        abstract fun bind()

        abstract fun stop()

        abstract fun unbind()

        abstract fun discard()

        override fun onServiceConnected(name: ComponentName?, binder: IBinder?) = connected(this, binder)

        override fun onServiceDisconnected(name: ComponentName?) = exited(this, null)
    }

    private class ShizukuHost : Host() {
        private val args = Shizuku.UserServiceArgs(ComponentName(BuildConfig.APPLICATION_ID, CoreService::class.java.name))
            .daemon(true)
            .processNameSuffix("core")
            .debuggable(BuildConfig.DEBUG)
            .version(SwiptApplication.application.run { packageManager.getPackageInfo(packageName, 0).lastUpdateTime.toInt() })

        override fun bind() {
            Shizuku.bindUserService(args, this)
        }

        override fun stop() {
            if (Shizuku.pingBinder()) {
                runCatching { Shizuku.unbindUserService(args, this, true) }
            }
        }

        override fun unbind() = stop()

        override fun discard() {}
    }

    private class RootHost : Host() {
        private val intent = Intent(SwiptApplication.application, CoreRootService::class.java).addCategory(RootService.CATEGORY_DAEMON_MODE)

        override fun bind() {
            Shell.EXECUTOR.execute {
                val shell = try {
                    Shell.getCachedShell()?.takeUnless { it.isRoot }?.close()
                    Shell.getShell()
                } catch (_: Exception) {
                    null
                }
                main.post {
                    if (shell == null || !shell.isRoot) {
                        denied(this)
                    } else if (host === this) {
                        launch(shell)
                    }
                }
            }
        }

        private fun launch(shell: Shell) {
            val task = RootService.bindOrTask(intent, SwiptApplication.application.mainExecutor, this) ?: return
            Shell.EXECUTOR.execute {
                try {
                    shell.execTask(task)
                } catch (e: IOException) {
                    main.post { exited(this, e.message ?: e.toString()) }
                }
            }
        }

        override fun stop() {
            RootService.stop(intent)
        }

        override fun unbind() {
            RootService.unbind(this)
        }

        override fun discard() {
            if (mode.value == Mode.Root) unbind() else stop()
        }
    }
}
