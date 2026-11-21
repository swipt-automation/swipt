package io.nekohasekai.swipt.runtime

import android.content.Context
import android.icu.util.ULocale
import android.os.Build
import android.os.LocaleList
import expo.modules.kotlin.exception.CodedException
import expo.modules.kotlin.functions.Coroutine
import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition
import java.security.MessageDigest
import java.security.SecureRandom
import java.util.Collections
import java.util.WeakHashMap
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withContext

private const val RANDOM_LIMIT = 65536

sealed interface SwiptHostWebResult {
  class Bytes(val data: ByteArray) : SwiptHostWebResult
  class Json(val json: String) : SwiptHostWebResult
  class Failure(val failure: String) : SwiptHostWebResult
}

interface SwiptHostSession {
  fun start(id: String, name: String, arguments: String, completion: (SwiptHostWebResult) -> Unit)
  fun cancel(id: String)
  fun put(data: ByteArray): String
  fun release(id: String, reason: String)
  fun openFile(root: String, path: String): String
  fun compression(format: String, mode: String): List<String>
  fun database(request: String): String
  fun close()
}

interface SwiptHostDelegate {
  val swiplet: String
  suspend fun call(id: String, action: String, input: String, value: (String) -> Unit): String
  fun cancelCall(id: String)
  suspend fun setServiceEnabled(enabled: Boolean)
  fun surfaceDidMount(surface: String)
  fun exit()
  fun log(level: String, message: String)
  fun fail(surface: String, message: String)
  fun report(message: String)
  fun storeBlob(data: ByteArray, id: String)
  fun removeBlob(id: String)
}

private class SwiptHostUnavailable : CodedException("SwiptHostUnavailable", "Swipt isn't ready for swiplets", null)

private data class ParsedLocale(
  val tag: String,
  val language: String,
  val script: String,
  val country: String,
  val rawScript: String,
  val rawCountry: String,
)

private fun parseLocale(tag: String): ParsedLocale {
  val raw = ULocale.forLanguageTag(tag)
  val likely = ULocale.addLikelySubtags(raw)
  return ParsedLocale(
    tag = tag,
    language = likely.language,
    script = likely.script,
    country = likely.country,
    rawScript = raw.script,
    rawCountry = raw.country,
  )
}

private fun matchLocalizations(localizations: List<String>, preferences: LocaleList): List<String> {
  if (localizations.isEmpty()) return emptyList()
  val supported = localizations.map(::parseLocale)
  for (i in 0 until preferences.size()) {
    val preferred = ULocale.addLikelySubtags(ULocale.forLocale(preferences[i]))
    val language = preferred.language
    if (language.isEmpty() || language == "und") continue
    var bestIndex = -1
    var bestRank = 0
    for ((index, candidate) in supported.withIndex()) {
      if (candidate.language != language || candidate.script != preferred.script) continue
      val rank = when {
        preferred.country.isNotEmpty() && candidate.country == preferred.country && candidate.rawCountry.isNotEmpty() -> 4
        candidate.rawCountry.isEmpty() && candidate.rawScript.isNotEmpty() -> 3
        candidate.rawCountry.isEmpty() -> 2
        else -> 1
      }
      if (rank > bestRank) {
        bestRank = rank
        bestIndex = index
      }
    }
    if (bestIndex < 0) continue
    val chosen = supported[bestIndex]
    val result = mutableListOf(chosen.tag)
    if (chosen.rawCountry.isNotEmpty()) {
      supported.firstOrNull {
        it.language == chosen.language && it.script == chosen.script && it.rawCountry.isEmpty() && it.rawScript.isNotEmpty()
      }?.let { if (it.tag !in result) result.add(it.tag) }
    }
    if (chosen.rawCountry.isNotEmpty() || chosen.rawScript.isNotEmpty()) {
      supported.firstOrNull {
        it.language == chosen.language && it.script == chosen.script && it.rawCountry.isEmpty() && it.rawScript.isEmpty()
      }?.let { if (it.tag !in result) result.add(it.tag) }
    }
    return result
  }
  return listOf(localizations[0])
}

private fun decodeUTF8(data: ByteArray, fatal: Boolean): Map<String, Any> {
  val out = StringBuilder(if (fatal) 0 else data.size)
  var needed = 0
  var seen = 0
  var point = 0
  var lower = 0x80
  var upper = 0xbf
  var index = 0
  while (index < data.size) {
    val b = data[index].toInt() and 0xff
    if (needed == 0) {
      index++
      when {
        b < 0x80 -> out.append(b.toChar())
        b in 0xc2..0xdf -> {
          needed = 1
          point = b and 0x1f
        }
        b in 0xe0..0xef -> {
          lower = if (b == 0xe0) 0xa0 else 0x80
          upper = if (b == 0xed) 0x9f else 0xbf
          needed = 2
          point = b and 0x0f
        }
        b in 0xf0..0xf4 -> {
          lower = if (b == 0xf0) 0x90 else 0x80
          upper = if (b == 0xf4) 0x8f else 0xbf
          needed = 3
          point = b and 0x07
        }
        fatal -> return mapOf("restored" to -1)
        else -> out.append('\ufffd')
      }
      continue
    }
    if (b < lower || b > upper) {
      if (fatal) return mapOf("restored" to index)
      needed = 0
      seen = 0
      point = 0
      lower = 0x80
      upper = 0xbf
      out.append('\ufffd')
      continue
    }
    index++
    lower = 0x80
    upper = 0xbf
    point = (point shl 6) or (b and 0x3f)
    if (++seen < needed) continue
    out.appendCodePoint(point)
    needed = 0
    seen = 0
    point = 0
  }
  if (needed != 0) {
    if (fatal) return mapOf("restored" to -1)
    out.append('\ufffd')
  }
  return mapOf("text" to out.toString())
}

class SwiptHostModule : Module() {
  companion object {
    @Volatile
    var openSession: ((swiplet: String, event: (name: String, body: Map<String, Any?>) -> Unit) -> SwiptHostSession)? = null

    private val random = SecureRandom()
    private val modules = Collections.newSetFromMap(WeakHashMap<SwiptHostModule, Boolean>())
    private val delegates = WeakHashMap<Context, SwiptHostDelegate>()

    fun bind(reactContext: Context, delegate: SwiptHostDelegate) {
      synchronized(this) { delegates[reactContext] = delegate }
    }

    fun unbind(delegate: SwiptHostDelegate) {
      val unbound = synchronized(this) {
        val contexts = delegates.filterValues { it === delegate }.keys
        contexts.forEach(delegates::remove)
        modules.filter { it.appContext.reactContext in contexts }
      }
      unbound.forEach { it.closeSession() }
    }
  }

  private var session: SwiptHostSession? = null
  private var sessionClosed = false

  private fun delegate(): SwiptHostDelegate? {
    val context = appContext.reactContext ?: return null
    return synchronized(Companion) { delegates[context] }
  }

  private fun host(): SwiptHostDelegate = delegate() ?: throw SwiptHostUnavailable()

  private fun session(): SwiptHostSession = synchronized(this) {
    session ?: run {
      val open = openSession
      if (sessionClosed || open == null) {
        throw SwiptHostUnavailable()
      }
      open(host().swiplet) { name, body -> sendEvent(name, body) }.also { session = it }
    }
  }

  private fun openedSession(): SwiptHostSession? = synchronized(this) { session }

  private fun closeSession() {
    val closing = synchronized(this) {
      sessionClosed = true
      session.also { session = null }
    }
    closing?.close()
  }

  private fun onMain(body: (SwiptHostDelegate) -> Unit) {
    val delegate = delegate() ?: return
    appContext.mainQueue.launch { body(delegate) }
  }

  override fun definition() = ModuleDefinition {
    Name("SwiptHost")

    Events("service", "sqlite", "value")

    OnCreate {
      synchronized(Companion) { modules.add(this@SwiptHostModule) }
    }

    OnStartObserving {
      try {
        session()
      } catch (_: SwiptHostUnavailable) {
      }
    }

    OnDestroy {
      synchronized(Companion) { modules.remove(this@SwiptHostModule) }
      closeSession()
    }

    Constant("osVersion") { Build.VERSION.SDK_INT.toString() }

    Constant("packageName") { appContext.reactContext?.packageName.orEmpty() }

    Function("languages") {
      val locales = LocaleList.getDefault()
      List(locales.size()) { locales[it].toLanguageTag() }
    }

    AsyncFunction("webStart") Coroutine { id: String, name: String, arguments: String ->
      val session = session()
      val result = suspendCancellableCoroutine { continuation ->
        session.start(id, name, arguments) { continuation.resumeWith(Result.success(it)) }
      }
      when (result) {
        is SwiptHostWebResult.Bytes -> mapOf("data" to result.data)
        is SwiptHostWebResult.Json -> mapOf("json" to result.json)
        is SwiptHostWebResult.Failure -> mapOf("failure" to result.failure)
      }
    }

    Function("webCancel") { id: String -> openedSession()?.cancel(id) }

    Function("webPut") { data: ByteArray -> session().put(data) }

    Function("webRelease") { id: String, reason: String -> openedSession()?.release(id, reason) }

    Function("webOpenFile") { root: String, path: String -> session().openFile(root, path) }

    Function("webCompression") { format: String, mode: String -> session().compression(format, mode) }

    Function("webRandom") { length: Int ->
      if (length < 0 || length > RANDOM_LIMIT) {
        throw CodedException("TypeError", "the length of random bytes must be between 0 and $RANDOM_LIMIT", null)
      }
      ByteArray(length).also(random::nextBytes)
    }

    Function("webDigest") { algorithm: String, data: ByteArray ->
      val name = when (algorithm) {
        "SHA-1", "SHA-256", "SHA-384", "SHA-512" -> algorithm
        else -> throw CodedException("NotSupportedError", "unknown digest $algorithm", null)
      }
      MessageDigest.getInstance(name).digest(data)
    }

    Function("webDecodeText") { data: ByteArray, fatal: Boolean -> decodeUTF8(data, fatal) }

    Function("publishObjectURL") { url: String, data: ByteArray? ->
      if (!url.startsWith("blob:")) {
        throw CodedException("TypeError", "the URL must be a blob URL", null)
      }
      val id = url.removePrefix("blob:")
      if (data != null) host().storeBlob(data, id) else host().removeBlob(id)
    }

    AsyncFunction("call") Coroutine { id: String, action: String, input: String ->
      try {
        withContext(Dispatchers.Main) {
          host().call(id, action, input) { value -> sendEvent("value", mapOf("call" to id, "value" to value)) }
        }
      } finally {
        sendEvent("value", mapOf("call" to id))
      }
    }

    AsyncFunction("cancelCall") { id: String -> onMain { it.cancelCall(id) } }

    AsyncFunction("setServiceEnabled") Coroutine { enabled: Boolean ->
      withContext(Dispatchers.Main) { host().setServiceEnabled(enabled) }
    }

    Function("mounted") { surface: String -> onMain { it.surfaceDidMount(surface) } }

    Function("exit") { onMain { it.exit() } }

    Function("sqlite") { request: String -> session().database(request) }

    AsyncFunction("sqliteAsync") Coroutine { request: String ->
      withContext(Dispatchers.IO) { session().database(request) }
    }

    Function("preferredLocalizations") { localizations: List<String> ->
      matchLocalizations(localizations, LocaleList.getDefault())
    }

    Function("log") { level: String, message: String -> onMain { it.log(level, message) } }

    Function("fail") { surface: String, message: String -> onMain { it.fail(surface, message) } }

    Function("report") { message: String -> onMain { it.report(message) } }
  }
}
