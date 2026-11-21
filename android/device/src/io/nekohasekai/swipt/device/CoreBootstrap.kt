package io.nekohasekai.swipt.device

import android.os.Build
import android.os.ParcelFileDescriptor
import android.util.Log
import dalvik.system.PathClassLoader
import java.lang.reflect.InvocationTargetException
import java.lang.reflect.Method

object CoreBootstrap {
    private var connectCore: Method? = null
    private var closeCore: Method? = null

    @Synchronized
    fun connect(apk: String): ParcelFileDescriptor {
        val connect = connectCore ?: run {
            val loader = PathClassLoader(apk, "$apk!/lib/${Build.SUPPORTED_64_BIT_ABIS[0]}", ClassLoader.getSystemClassLoader().parent)
            val coreProcess = loader.loadClass("io.nekohasekai.swipt.device.CoreProcess")
            closeCore = coreProcess.getMethod("close")
            coreProcess.getMethod("connect", String::class.java).also { connectCore = it }
        }
        try {
            return connect.invoke(null, apk) as ParcelFileDescriptor
        } catch (e: InvocationTargetException) {
            throw e.cause as? Exception ?: e
        }
    }

    @Synchronized
    fun close() {
        val close = closeCore ?: return
        try {
            close.invoke(null)
        } catch (e: ReflectiveOperationException) {
            Log.w(TAG, "close the core", if (e is InvocationTargetException) e.cause else e)
        }
    }
}
