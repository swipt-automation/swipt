package io.nekohasekai.swipt.device

import android.os.ParcelFileDescriptor
import android.util.Log
import io.nekohasekai.libSwipt.Core
import io.nekohasekai.libSwipt.CoreOptions
import io.nekohasekai.libSwipt.LibSwipt

object CoreProcess {
    private var core: Core? = null

    @JvmStatic
    @Synchronized
    fun connect(apk: String): ParcelFileDescriptor {
        val core = core ?: start(apk).also { core = it }
        val (served, returned) = ParcelFileDescriptor.createSocketPair()
        core.serve(served.detachFd())
        return returned
    }

    @JvmStatic
    @Synchronized
    fun close() {
        val running = core ?: return
        core = null
        running.close()
    }

    private fun start(apk: String): Core {
        LibSwipt.setLogHandler({ level, message ->
            val priority = when (level) {
                LibSwipt.LogLevelError -> Log.ERROR
                LibSwipt.LogLevelWarn -> Log.WARN
                LibSwipt.LogLevelInfo -> Log.INFO
                else -> Log.DEBUG
            }
            Log.println(priority, TAG, message)
        }, LibSwipt.LogLevelDebug)
        val device = Device()
        val info = device.context.packageManager.getPackageArchiveInfo(apk, 0)
        val options = CoreOptions()
        options.apk = apk
        if (info != null) {
            options.appVersion = info.versionName.orEmpty()
            options.appBuild = info.longVersionCode.toString()
        }
        return LibSwipt.startCore(options, device)
    }
}
