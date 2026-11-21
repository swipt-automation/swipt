package io.nekohasekai.swipt.automation

import com.google.protobuf.ByteString
import io.nekohasekai.swipt.app.description
import io.nekohasekai.swipt.core.Core
import io.nekohasekai.swipt.proto.InterfaceEvent
import io.nekohasekai.swipt.proto.StartWebResponse
import io.nekohasekai.swipt.proto.cancelWebRequest
import io.nekohasekai.swipt.proto.openInterfaceRequest
import io.nekohasekai.swipt.proto.openWebCompressionRequest
import io.nekohasekai.swipt.proto.openWebFileRequest
import io.nekohasekai.swipt.proto.putWebRequest
import io.nekohasekai.swipt.proto.queryDatabaseRequest
import io.nekohasekai.swipt.proto.releaseWebRequest
import io.nekohasekai.swipt.proto.startWebRequest
import io.nekohasekai.swipt.runtime.SwiptHostSession
import io.nekohasekai.swipt.runtime.SwiptHostWebResult
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive

class SwipletSession(swiplet: String, event: (String, Map<String, Any?>) -> Unit) : SwiptHostSession {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val session = MutableStateFlow<Result<String>?>(null)

    init {
        scope.launch {
            var backoffMs = 250L
            while (isActive) {
                session.value = null
                try {
                    Core.interfaces.openInterface(openInterfaceRequest { swipletId = swiplet }).collect {
                        when (it.eventCase) {
                            InterfaceEvent.EventCase.SESSION -> {
                                backoffMs = 250L
                                session.value = Result.success(it.session)
                            }
                            InterfaceEvent.EventCase.DATABASE_CHANGE -> event(
                                "sqlite",
                                mapOf(
                                    "databaseName" to it.databaseChange.database,
                                    "databaseFilePath" to it.databaseChange.path,
                                    "tableName" to it.databaseChange.table,
                                    "rowId" to it.databaseChange.rowId.toDouble(),
                                ),
                            )
                            InterfaceEvent.EventCase.SERVICE -> {
                                val service = Core.Service(it.service)
                                event(
                                    "service",
                                    mapOf(
                                        "enabled" to service.enabled,
                                        "status" to service.statusName,
                                        "error" to service.error.orEmpty(),
                                        "since" to service.since.toDouble(),
                                    ),
                                )
                            }
                            else -> {}
                        }
                    }
                } catch (e: CancellationException) {
                    throw e
                } catch (e: Exception) {
                    session.value = Result.failure(e)
                }
                delay(backoffMs)
                backoffMs = (backoffMs * 2).coerceAtMost(5_000L)
            }
        }
    }

    private suspend fun awaitSession(): String = session.filterNotNull().first().getOrThrow()

    private fun <T> call(body: suspend (session: String) -> T): T = runBlocking(Dispatchers.IO) { body(awaitSession()) }

    override fun start(id: String, name: String, arguments: String, completion: (SwiptHostWebResult) -> Unit) {
        scope.launch {
            val result = try {
                val response = Core.interfaces.startWeb(startWebRequest {
                    this.session = awaitSession()
                    this.id = id
                    this.name = name
                    this.arguments = arguments
                })
                when (response.resultCase) {
                    StartWebResponse.ResultCase.DATA -> SwiptHostWebResult.Bytes(response.data.toByteArray())
                    StartWebResponse.ResultCase.FAILURE -> SwiptHostWebResult.Failure(response.failure)
                    else -> SwiptHostWebResult.Json(response.json.ifEmpty { "null" })
                }
            } catch (e: Exception) {
                SwiptHostWebResult.Failure(failure(e))
            }
            completion(result)
        }
    }

    override fun cancel(id: String) {
        scope.launch {
            runCatching {
                Core.interfaces.cancelWeb(cancelWebRequest {
                    session = awaitSession()
                    this.id = id
                })
            }
        }
    }

    override fun put(data: ByteArray): String = call { session ->
        Core.interfaces.putWeb(putWebRequest {
            this.session = session
            this.data = ByteString.copyFrom(data)
        }).id
    }

    override fun release(id: String, reason: String) {
        scope.launch {
            runCatching {
                Core.interfaces.releaseWeb(releaseWebRequest {
                    session = awaitSession()
                    this.id = id
                    this.reason = reason
                })
            }
        }
    }

    override fun openFile(root: String, path: String): String = call { session ->
        Core.interfaces.openWebFile(openWebFileRequest {
            this.session = session
            this.root = root
            this.path = path
        }).id
    }

    override fun compression(format: String, mode: String): List<String> = call { session ->
        Core.interfaces.openWebCompression(openWebCompressionRequest {
            this.session = session
            this.format = format
            this.mode = mode
        }).idsList
    }

    override fun database(request: String): String = call { session ->
        Core.interfaces.queryDatabase(queryDatabaseRequest {
            this.session = session
            this.request = request
        }).result
    }

    override fun close() {
        scope.cancel()
    }

    private companion object {
        fun failure(error: Exception): String =
            JsonObject(mapOf("name" to JsonPrimitive("Error"), "message" to JsonPrimitive(error.description))).toString()
    }
}
