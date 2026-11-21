package io.nekohasekai.swipt.device

import android.app.ActivityOptions
import android.app.IProcessObserver
import android.app.TaskStackListener
import android.content.ComponentName
import android.content.Intent
import android.content.pm.ApplicationInfo
import android.content.pm.IPackageManager
import android.content.pm.PackageInfo
import android.content.pm.PackageManager
import android.content.pm.ParceledListSlice
import android.graphics.Bitmap
import android.graphics.Canvas
import android.net.Uri
import android.os.RemoteException
import android.util.Log
import android.view.Display
import io.nekohasekai.libSwipt.Observer
import io.nekohasekai.libSwipt.Watch
import java.io.ByteArrayOutputStream
import java.util.concurrent.ConcurrentHashMap
import org.json.JSONArray
import org.json.JSONObject

internal class Applications(private val device: Device) {
    private val labels = device.context.packageManager
    val foreground = State(
        "foreground",
        {
            device.tasks.getAllRootTaskInfosOnDisplay(Display.DEFAULT_DISPLAY)
                .firstOrNull { it.isVisible && it.topActivity != null && it.windowingMode != WINDOWING_MODE_PINNED }
                ?.topActivity?.packageName
                ?: throw Failure(Failure.Name.UnavailableError, "no activity shows on the default display")
        },
    ) { changed ->
        val listener = object : TaskStackListener() {
            override fun onTaskStackChanged() = changed()

            override fun onTaskFocusChanged(taskId: Int, focused: Boolean) = changed()
        }
        device.tasks.registerTaskStackListener(listener)
        AutoCloseable { device.tasks.unregisterTaskStackListener(listener) }
    }

    fun installed(): String {
        val installed = GET_INSTALLED_PACKAGES.invoke(device.packages, PackageManager.MATCH_DISABLED_COMPONENTS.toLong(), device.activities.currentUserId) as ParceledListSlice<*>
        return JSONArray(installed.list.map { describe(it as PackageInfo) }).toString()
    }

    private fun describe(info: PackageInfo): JSONObject {
        val application = info.applicationInfo!!
        return JSONObject()
            .put("packageName", info.packageName)
            .put("name", application.loadLabel(labels).toString())
            .put("versionCode", info.longVersionCode)
            .apply { info.versionName?.let { put("version", it) } }
            .putFlag("system", (application.flags and ApplicationInfo.FLAG_SYSTEM) != 0)
    }

    private fun find(name: String): PackageInfo =
        device.packages.getPackageInfo(name, PackageManager.MATCH_DISABLED_COMPONENTS.toLong(), device.activities.currentUserId)
            ?: throw Failure(Failure.Name.NotFoundError, "no package $name")

    fun updated(name: String) = find(name).lastUpdateTime

    fun icon(name: String): ByteArray {
        val icon = find(name).applicationInfo!!.loadIcon(labels)
        val width = icon.intrinsicWidth.takeIf { it > 0 } ?: ICON_SIZE
        val height = icon.intrinsicHeight.takeIf { it > 0 } ?: ICON_SIZE
        val bitmap = Bitmap.createBitmap(width, height, Bitmap.Config.ARGB_8888)
        icon.setBounds(0, 0, width, height)
        icon.draw(Canvas(bitmap))
        val output = ByteArrayOutputStream()
        bitmap.compress(Bitmap.CompressFormat.PNG, 100, output)
        bitmap.recycle()
        return output.toByteArray()
    }

    fun launch(packageName: String, data: String, restart: Boolean, display: Int) {
        device.checkDisplay(display)
        val name = find(packageName).packageName
        val user = device.activities.currentUserId
        if (restart) device.activities.forceStopPackage(name, user)
        val intent = if (data.isNotEmpty()) {
            Intent(Intent.ACTION_VIEW, Uri.parse(data)).setPackage(name)
        } else {
            val launcher = Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_LAUNCHER).setPackage(name)
            val resolved = device.packages.queryIntentActivities(launcher, null, 0, user).list
            if (resolved.isEmpty()) throw Failure(Failure.Name.NotFoundError, "$name has no launcher activity")
            launcher.setComponent(ComponentName(name, resolved[0].activityInfo.name))
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_RESET_TASK_IF_NEEDED)
        }
        val options = if (display == Display.DEFAULT_DISPLAY) null else ActivityOptions.makeBasic().setLaunchDisplayId(display).toBundle()
        Call.check()
        val started = device.tasks.startActivityAndWait(null, device.callingPackage, null, intent, intent.type, null, null, 0, 0, null, options, user)
        when (started.result) {
            START_SUCCESS, START_TASK_TO_FRONT, START_DELIVERED_TO_TOP -> {}
            START_INTENT_NOT_RESOLVED, START_CLASS_NOT_FOUND -> throw Failure(Failure.Name.NotFoundError, "no activity handles $intent")
            START_PERMISSION_DENIED -> throw Failure(Failure.Name.NotAllowedError, "no permission to start $intent")
            START_RETURN_LOCK_TASK_MODE_VIOLATION -> throw Failure(Failure.Name.NotAllowedError, "the device is pinned to an app, and did not start $intent")
            START_ABORTED -> throw Failure(Failure.Name.NotAllowedError, "the system refused to start $intent")
            else -> throw Failure(Failure.Name.InvalidStateError, "starting $intent failed with ${started.result}")
        }
    }

    fun watch(observer: Observer): Watch {
        val processes = ConcurrentHashMap<Int, String>()
        val watching = object : IProcessObserver.Stub() {
            override fun onProcessStarted(pid: Int, processUid: Int, packageUid: Int, packageName: String, processName: String) {
                processes[pid] = packageName
                publish("start", packageName, pid)
            }

            override fun onForegroundActivitiesChanged(pid: Int, uid: Int, foregroundActivities: Boolean) {
                val packageName = processes[pid] ?: running(pid) ?: owner(uid) ?: return
                processes[pid] = packageName
                publish(if (foregroundActivities) "foreground" else "background", packageName, pid)
            }

            override fun onForegroundServicesChanged(pid: Int, uid: Int, serviceTypes: Int) {}

            override fun onProcessDied(pid: Int, uid: Int) {
                publish("exit", processes.remove(pid) ?: owner(uid) ?: return, pid)
            }

            private fun publish(type: String, packageName: String, pid: Int) {
                observer.next(
                    JSONObject()
                        .put("type", type)
                        .put("packageName", packageName)
                        .put("pid", pid)
                        .put("time", System.currentTimeMillis())
                        .toString(),
                )
            }
        }
        for (process in device.activities.runningAppProcesses) {
            process.pkgList?.firstOrNull()?.let { processes[process.pid] = it }
        }
        device.activities.registerProcessObserver(watching)
        return Watch {
            try {
                device.activities.unregisterProcessObserver(watching)
            } catch (e: RemoteException) {
                Log.w(TAG, "stop watching the processes", e)
            }
        }
    }

    private fun running(pid: Int) = device.activities.runningAppProcesses.firstOrNull { it.pid == pid }?.pkgList?.firstOrNull()

    private fun owner(uid: Int) = labels.getPackagesForUid(uid)?.firstOrNull()

    companion object {
        private const val ICON_SIZE = 192

        // Android 17 returns a PackageInfoList, a ParceledListSlice that only the descriptor of the method tells apart.
        private val GET_INSTALLED_PACKAGES = IPackageManager::class.java.getMethod("getInstalledPackages", Long::class.javaPrimitiveType, Int::class.javaPrimitiveType)

        // WindowConfiguration.WINDOWING_MODE_PINNED
        private const val WINDOWING_MODE_PINNED = 2

        // ActivityManager.START_*
        private const val START_SUCCESS = 0
        private const val START_TASK_TO_FRONT = 2
        private const val START_DELIVERED_TO_TOP = 3
        private const val START_RETURN_LOCK_TASK_MODE_VIOLATION = 101
        private const val START_ABORTED = 102
        private const val START_INTENT_NOT_RESOLVED = -91
        private const val START_CLASS_NOT_FOUND = -92
        private const val START_PERMISSION_DENIED = -94
    }
}
