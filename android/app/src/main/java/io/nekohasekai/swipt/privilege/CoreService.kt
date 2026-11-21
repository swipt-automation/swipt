package io.nekohasekai.swipt.privilege

import android.content.Context
import android.content.Intent
import android.os.Binder
import android.os.FileObserver
import android.os.IBinder
import android.os.ParcelFileDescriptor
import android.util.Log
import com.topjohnwu.superuser.ipc.RootService
import io.nekohasekai.swipt.device.CoreBootstrap
import io.nekohasekai.swipt.device.CoreClient
import java.io.File
import kotlin.system.exitProcess

private const val TAG = "CoreService"

class CoreService(private val context: Context) : ICoreService.Stub() {
    private val observer = if (context is RootService) {
        null
    } else {
        val apk = File(context.packageCodePath)
        object : FileObserver(
            apk.parentFile!!,
            FileObserver.ATTRIB or FileObserver.CLOSE_WRITE or FileObserver.MOVED_FROM or FileObserver.MOVED_TO or
                FileObserver.DELETE or FileObserver.DELETE_SELF or FileObserver.MOVE_SELF,
        ) {
            override fun onEvent(event: Int, path: String?) {
                if (event and (DELETE_SELF or MOVE_SELF) != 0 || path == apk.name) destroy()
            }
        }.apply { startWatching() }
    }

    override fun destroy() {
        CoreBootstrap.close()
        exitProcess(0)
    }

    override fun connect(): ParcelFileDescriptor {
        try {
            CoreClient.allowNotifications(context, Binder.getCallingPid(), Binder.getCallingUid())
        } catch (e: Exception) {
            Log.w(TAG, "allow notifications", e)
        }
        return try {
            CoreBootstrap.connect(context.packageCodePath)
        } catch (e: Exception) {
            throw IllegalStateException(e.message ?: e.toString())
        }
    }

    override fun setImportant(token: IBinder, important: Boolean) = CoreClient.setImportant(ImportanceProvider.AUTHORITY, Binder.getCallingUid(), token, important)
}

class CoreRootService : RootService() {
    override fun onBind(intent: Intent): IBinder = CoreService(this)

    override fun onDestroy() {
        CoreBootstrap.close()
    }
}
