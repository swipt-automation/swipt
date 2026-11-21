package io.nekohasekai.swipt.device

import android.Manifest
import android.app.IActivityManager
import android.content.Context
import android.content.pm.PackageManager
import android.os.Binder
import android.os.IBinder
import android.os.RemoteException
import android.os.ServiceManager
import android.os.UserHandle
import java.lang.reflect.InvocationTargetException

object CoreClient {
    private const val FLAG_PERMISSION_USER_SET = 1
    private const val FLAG_PERMISSION_USER_FIXED = 1 shl 1
    private const val FLAG_PERMISSION_POLICY_FIXED = 1 shl 2
    private const val FLAG_PERMISSION_SYSTEM_FIXED = 1 shl 4
    private const val DECIDED = FLAG_PERMISSION_USER_SET or FLAG_PERMISSION_USER_FIXED or FLAG_PERMISSION_POLICY_FIXED or FLAG_PERMISSION_SYSTEM_FIXED
    private val activities: IActivityManager = IActivityManager.Stub.asInterface(ServiceManager.getService(Context.ACTIVITY_SERVICE))
    private val holders = HashMap<IBinder, Importance>()

    fun allowNotifications(context: Context, pid: Int, uid: Int) {
        if (context.checkPermission(Manifest.permission.POST_NOTIFICATIONS, pid, uid) == PackageManager.PERMISSION_GRANTED) return
        val packages = context.packageManager
        val user = UserHandle.getUserHandleForUid(uid)
        try {
            val flags = PackageManager::class.java.getMethod("getPermissionFlags", String::class.java, String::class.java, UserHandle::class.java)
                .invoke(packages, Manifest.permission.POST_NOTIFICATIONS, context.packageName, user) as Int
            if ((flags and DECIDED) == 0) {
                PackageManager::class.java.getMethod("grantRuntimePermission", String::class.java, String::class.java, UserHandle::class.java)
                    .invoke(packages, context.packageName, Manifest.permission.POST_NOTIFICATIONS, user)
            }
        } catch (e: InvocationTargetException) {
            throw e.cause as? Exception ?: e
        }
    }

    // ContentProviderHelper and OomAdjuster of Android 17 raise the process hosting a provider to the foreground while a
    // caller without a process record holds the provider through getContentProviderExternal, until the caller removes its
    // token or the token dies.
    fun setImportant(authority: String, uid: Int, client: IBinder, important: Boolean) {
        synchronized(holders) {
            val current = holders[client]
            if (important == (current != null)) return
            if (current != null) {
                holders.remove(client)
                client.unlinkToDeath(current, 0)
                activities.removeContentProviderExternalAsUser(current.authority, current, current.user)
                return
            }
            // UserHandle.hashCode() returns mHandle, the user ID.
            val importance = Importance(authority, client, UserHandle.getUserHandleForUid(uid).hashCode())
            client.linkToDeath(importance, 0)
            val holder = try {
                activities.getContentProviderExternal(authority, importance.user, importance, null)
            } catch (e: Exception) {
                client.unlinkToDeath(importance, 0)
                throw e
            }
            if (holder == null) {
                client.unlinkToDeath(importance, 0)
                return
            }
            holders[client] = importance
        }
    }

    private class Importance(val authority: String, val client: IBinder, val user: Int) : Binder(), IBinder.DeathRecipient {
        override fun binderDied() {
            synchronized(holders) {
                if (!holders.remove(client, this)) return
                try {
                    activities.removeContentProviderExternalAsUser(authority, this, user)
                } catch (_: RemoteException) {
                }
            }
        }
    }
}
