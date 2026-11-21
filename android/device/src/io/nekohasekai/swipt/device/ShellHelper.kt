package io.nekohasekai.swipt.device

import android.app.IActivityManager
import android.content.Context
import android.os.Looper
import android.os.ServiceManager
import java.io.PrintStream
import kotlin.concurrent.thread
import kotlin.system.exitProcess
import org.json.JSONObject
import org.json.JSONTokener

class ShellHelper private constructor(private val system: Context, activities: IActivityManager, private val output: PrintStream) {
    private val clipboard = Clipboard(system, activities)
    private var shell: Context? = null
    private var clipboardWatch: AutoCloseable? = null
    private var advertiseID = 0L
    private var advertiser: Advertiser? = null

    private fun emit(message: JSONObject) {
        synchronized(output) { output.println(message) }
    }

    @Synchronized
    private fun shellContext(): Context = shell ?: Advertiser.shellContext(system).also { shell = it }

    private fun readLoop() {
        try {
            System.`in`.bufferedReader().forEachLine { line ->
                val request = JSONObject(line)
                val response = JSONObject().put("id", request.getLong("id"))
                try {
                    handle(request, response)
                } catch (e: Exception) {
                    response.put("error", Failure.of(request.optString("method", "shell"), e).message)
                }
                emit(response)
            }
        } catch (e: Exception) {
            System.err.println(e)
            exitProcess(1)
        }
        stopAdvertiser(0)
        exitProcess(0)
    }

    @Synchronized
    private fun handle(request: JSONObject, response: JSONObject) {
        when (val method = request.getString("method")) {
            "clipboard.get" -> response.put("value", JSONTokener(clipboard.get()).nextValue())
            "clipboard.set" -> clipboard.set(if (request.isNull("text")) null else request.getString("text"))
            "clipboard.watch" -> if (clipboardWatch == null) {
                clipboardWatch = clipboard.listen { emit(JSONObject().put("event", "clipboard")) }
            }
            "clipboard.unwatch" -> {
                clipboardWatch?.close()
                clipboardWatch = null
            }
            "advertise.start" -> {
                stopAdvertiser(0)
                val session = request.getLong("session")
                advertiseID = session
                val attributes = request.optJSONArray("attributes")?.let { given -> List(given.length(), given::getString) }.orEmpty()
                advertiser = Advertiser(
                    shellContext(),
                    request.getString("name"),
                    request.getString("type"),
                    request.getInt("port"),
                    request.optString("hostname"),
                    attributes,
                    onHostname = { emit(JSONObject().put("event", "advertise.hostname").put("session", session).put("hostname", it)) },
                    onFailed = { emit(JSONObject().put("event", "advertise.error").put("session", session).put("error", it)) },
                ).apply { start() }
            }
            "advertise.stop" -> stopAdvertiser(request.getLong("session"))
            else -> throw Failure(Failure.Name.TypeError, "unknown method $method")
        }
    }

    @Synchronized
    private fun stopAdvertiser(session: Long) {
        val current = advertiser ?: return
        if (session != 0L && advertiseID != session) return
        current.stop()
        advertiser = null
    }

    companion object {
        @JvmStatic
        fun main(arguments: Array<String>) {
            Thread.setDefaultUncaughtExceptionHandler { _, e ->
                System.err.println(e)
                exitProcess(1)
            }
            val system = Device.systemContext()
            val activities = IActivityManager.Stub.asInterface(ServiceManager.getService(Context.ACTIVITY_SERVICE))
            val helper = ShellHelper(system, activities, PrintStream(System.out, true, Charsets.UTF_8))
            thread(name = "ShellHelper") { helper.readLoop() }
            Looper.loop()
        }
    }
}
