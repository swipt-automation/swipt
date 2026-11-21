package io.nekohasekai.swipt.core

import android.os.Binder
import android.util.Log
import com.google.protobuf.Empty
import io.grpc.ManagedChannel
import io.grpc.okhttp.OkHttpChannelBuilder
import io.nekohasekai.swipt.R
import io.nekohasekai.swipt.app.AlertState
import io.nekohasekai.swipt.app.ErrorAlert
import io.nekohasekai.swipt.app.SwiptApplication
import io.nekohasekai.swipt.app.description
import io.nekohasekai.swipt.privilege.Privilege
import io.nekohasekai.swipt.proto.Alert
import io.nekohasekai.swipt.proto.CallActionResponse
import io.nekohasekai.swipt.proto.GetInfoResponse
import io.nekohasekai.swipt.proto.InterfaceServiceGrpcKt.InterfaceServiceCoroutineStub
import io.nekohasekai.swipt.proto.PublicationStatus
import io.nekohasekai.swipt.proto.ServiceState
import io.nekohasekai.swipt.proto.ServiceStatus
import io.nekohasekai.swipt.proto.Settings as CoreSettings
import io.nekohasekai.swipt.proto.SettingsServiceGrpcKt.SettingsServiceCoroutineStub
import io.nekohasekai.swipt.proto.Swiplet as CoreSwiplet
import io.nekohasekai.swipt.proto.SwipletServiceGrpcKt.SwipletServiceCoroutineStub
import io.nekohasekai.swipt.proto.SystemServiceGrpcKt.SystemServiceCoroutineStub
import io.nekohasekai.swipt.proto.callActionRequest
import io.nekohasekai.swipt.proto.setServiceEnabledRequest
import io.nekohasekai.swipt.proto.watchServicesRequest
import java.io.IOException
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.takeWhile
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext

object Core {
    sealed interface State {
        data object Waiting : State

        data object Starting : State

        data class Running(val info: GetInfoResponse) : State

        data class Failed(val message: String) : State
    }

    data class Service(val enabled: Boolean, val status: ServiceStatus, val error: String?, val since: Long) {
        val statusName: String
            get() = when (status) {
                ServiceStatus.SERVICE_STATUS_STARTING -> "starting"
                ServiceStatus.SERVICE_STATUS_RUNNING -> "running"
                ServiceStatus.SERVICE_STATUS_FAILED -> "failed"
                else -> "stopped"
            }

        internal constructor(state: ServiceState) : this(
            state.enabled,
            state.status,
            if (state.hasError()) "${state.error.name}: ${state.error.message}" else null,
            if (state.hasSince()) state.since.seconds * 1000 + state.since.nanos / 1_000_000 else 0,
        )
    }

    class ActionException(val name: String, message: String) : Exception("$name: $message")

    private const val TAG = "Core"

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)

    private val channel: ManagedChannel = OkHttpChannelBuilder.forAddress("localhost", 1)
        .usePlaintext()
        .socketFactory(CoreSocketFactory { Privilege.service.value?.connect() ?: throw unavailable() })
        .build()

    val system = SystemServiceCoroutineStub(channel)
    val swiplets = SwipletServiceCoroutineStub(channel)
    val interfaces = InterfaceServiceCoroutineStub(channel)
    val settingsService = SettingsServiceCoroutineStub(channel)

    private val _state = MutableStateFlow<State>(State.Waiting)
    val state: StateFlow<State> = _state.asStateFlow()

    private val _installed = MutableStateFlow<List<CoreSwiplet>?>(null)
    val installed: StateFlow<List<CoreSwiplet>?> = _installed.asStateFlow()

    private val _services = MutableStateFlow<Map<String, Service>>(emptyMap())
    val services: StateFlow<Map<String, Service>> = _services.asStateFlow()

    private val _settings = MutableStateFlow<CoreSettings?>(null)
    val settings: StateFlow<CoreSettings?> = _settings.asStateFlow()

    private val _publication = MutableStateFlow(PublicationStatus.getDefaultInstance())
    val publication: StateFlow<PublicationStatus> = _publication.asStateFlow()

    private val actions = MutableStateFlow(0)
    private val importance = Binder()
    private val importanceAccess = Mutex()
    private var setupJob: Job? = null

    fun start() {
        scope.launch {
            combine(Privilege.service, actions.map { it > 0 }.distinctUntilChanged()) { _, _ -> }.collect { applyImportance() }
        }
        scope.launch {
            Privilege.service.collectLatest { service ->
                if (service == null) {
                    disconnect(State.Waiting)
                    return@collectLatest
                }
                var backoffMs = 500L
                while (true) {
                    channel.enterIdle()
                    if (_state.value !is State.Failed) {
                        disconnect(State.Starting)
                    }
                    try {
                        val info = system.getInfo(Empty.getDefaultInstance())
                        _settings.value = settingsService.getSettings(Empty.getDefaultInstance())
                        _state.value = State.Running(info)
                        backoffMs = 500L
                        watch()
                        disconnect(State.Starting)
                    } catch (e: CancellationException) {
                        throw e
                    } catch (e: Exception) {
                        Log.w(TAG, "connect to the core", e)
                        disconnect(State.Failed(e.description))
                    }
                    delay(backoffMs)
                    backoffMs = (backoffMs * 2).coerceAtMost(5_000L)
                }
            }
        }
    }

    private fun disconnect(state: State) {
        _state.value = state
        _installed.value = null
        _services.value = emptyMap()
        _settings.value = null
        _publication.value = PublicationStatus.getDefaultInstance()
    }

    private fun unavailable() = IOException("the device service isn't connected")

    private suspend fun watch() = coroutineScope {
        launch {
            swiplets.watchSwiplets(Empty.getDefaultInstance()).collect { _installed.value = it.swipletsList }
        }
        launch {
            swiplets.watchServices(watchServicesRequest {}).collect { changed ->
                _services.update { it + (changed.swipletId to Service(changed.service)) }
            }
        }
        launch {
            settingsService.watchSettings(Empty.getDefaultInstance()).collect { _settings.value = it }
        }
        launch {
            settingsService.watchPublication(Empty.getDefaultInstance()).collect { _publication.value = it }
        }
        launch {
            system.watchAlerts(Empty.getDefaultInstance()).collect(::alert)
        }
    }

    private fun alert(alert: Alert) {
        val application = SwiptApplication.application
        when (alert.kindCase) {
            Alert.KindCase.RECORDING_FAILURE ->
                ErrorAlert.report(AlertState.failure(application.getString(R.string.recording_failed), alert.recordingFailure.message))
            else -> Log.w(TAG, "unknown alert $alert")
        }
    }

    private suspend fun applyImportance() = importanceAccess.withLock {
        val service = Privilege.service.value ?: return@withLock
        try {
            withContext(Dispatchers.IO) { service.setImportant(importance, actions.value > 0) }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            Log.w(TAG, "set the importance of the app", e)
        }
    }

    suspend fun callAction(swiplet: String, action: String, input: String, value: (String) -> Unit = {}): String {
        actions.update { it + 1 }
        try {
            applyImportance()
            val request = callActionRequest {
                swipletId = swiplet
                this.action = action
                if (input.isNotEmpty()) this.input = ProtoJson.value(input)
            }
            var output = "null"
            swiplets.callAction(request).collect { part ->
                when (part.partCase) {
                    CallActionResponse.PartCase.VALUE -> value(ProtoJson.json(part.value))
                    CallActionResponse.PartCase.RESULT -> {
                        val result = part.result
                        if (result.hasError()) throw ActionException(result.error.name, result.error.message)
                        if (result.hasOutput()) output = ProtoJson.json(result.output)
                    }
                    else -> {}
                }
            }
            return output
        } finally {
            actions.update { it - 1 }
        }
    }

    suspend fun setServiceEnabled(swiplet: String, enabled: Boolean) {
        val response = swiplets.setServiceEnabled(setServiceEnabledRequest {
            swipletId = swiplet
            this.enabled = enabled
        })
        _services.update { it + (swiplet to Service(response.service)) }
    }

    suspend fun updateSettings(transform: CoreSettings.Builder.() -> Unit) {
        val current = settings.value ?: throw unavailable()
        _settings.value = settingsService.updateSettings(current.toBuilder().apply(transform).build())
    }

    fun applySetup(preferredMode: Privilege.Mode?) {
        setupJob?.cancel()
        setupJob = scope.launch {
            withContext(Dispatchers.Main) {
                if (Privilege.state.value != Privilege.State.Connected) {
                    if (preferredMode != null && preferredMode != Privilege.mode.value) {
                        Privilege.setMode(preferredMode)
                    } else if (Privilege.state.value == Privilege.State.RootDenied || Privilege.state.value is Privilege.State.Exited) {
                        Privilege.reconnect()
                    }
                    var requestedShizuku = false
                    Privilege.state.takeWhile { it != Privilege.State.Connected }.collect { privilege ->
                        when (privilege) {
                            Privilege.State.ShizukuNotGranted -> if (!requestedShizuku) {
                                requestedShizuku = true
                                Privilege.requestShizukuPermission()
                            }
                            Privilege.State.ShizukuRoot -> Privilege.setMode(Privilege.Mode.Root)
                            else -> {}
                        }
                    }
                }
            }
            state.first { it is State.Running }
            try {
                settingsService.applySetup(Empty.getDefaultInstance())
                _settings.value = settingsService.getSettings(Empty.getDefaultInstance())
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                Log.w(TAG, "apply setup", e)
            }
        }
    }
}
