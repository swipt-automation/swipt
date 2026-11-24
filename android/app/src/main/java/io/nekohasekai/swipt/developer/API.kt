package io.nekohasekai.swipt.developer

import android.os.Build
import com.google.protobuf.Empty
import io.nekohasekai.swipt.R
import io.nekohasekai.swipt.app.AlertState
import io.nekohasekai.swipt.app.ErrorAlert
import io.nekohasekai.swipt.app.SwiptApplication
import io.nekohasekai.swipt.core.Core
import io.nekohasekai.swipt.proto.LogInCloudflareResponse
import io.nekohasekai.swipt.proto.LogInTailscaleResponse
import io.nekohasekai.swipt.proto.PublicationStatus
import io.nekohasekai.swipt.proto.TailscaleState
import io.nekohasekai.swipt.proto.cloudflareAccount
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import java.security.SecureRandom
import io.nekohasekai.swipt.proto.CloudflareMode as CoreCloudflareMode
import io.nekohasekai.swipt.proto.Settings as CoreSettings

object API {
    const val MCP_PATH = "/mcp"

    val ports = 1..65535

    enum class CloudflareMode { QuickTunnel, Tunnel }

    data class LocalNetwork(val isEnabled: Boolean, val port: Int, val authenticates: Boolean, val token: String, val hostname: String)

    data class CloudflareAccount(val credential: String, val name: String, val zone: String)

    data class Cloudflare(
        val isEnabled: Boolean,
        val mode: CloudflareMode,
        val path: String,
        val token: String,
        val account: CloudflareAccount?,
        val hostname: String,
        val runningHostname: String,
    ) {
        val isComplete: Boolean get() = mode == CloudflareMode.QuickTunnel || (account != null && hostname.isNotEmpty())

        val pathPrefix: String get() = if (path == "/") "" else path

        val publicHostname: String
            get() = when (mode) {
                CloudflareMode.QuickTunnel -> if (isEnabled) runningHostname else ""
                CloudflareMode.Tunnel -> hostname
            }
    }

    data class TailscaleStatus(val state: State, val loginName: String, val tailnet: String, val dnsName: String, val addresses: List<String>) {
        enum class State { Starting, Stopped, NeedsLogin, NeedsMachineAuth, Running }

        val isLoggedIn: Boolean get() = state == State.Stopped || state == State.NeedsMachineAuth || state == State.Running
    }

    data class Tailscale(val isEnabled: Boolean, val authenticates: Boolean, val token: String, val status: TailscaleStatus?)

    data class State(val localNetwork: LocalNetwork, val cloudflare: Cloudflare, val tailscale: Tailscale) {
        val isPublished: Boolean get() = localNetwork.isEnabled || cloudflare.isEnabled || tailscale.isEnabled
    }

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main)

    val state: StateFlow<State?> = combine(Core.settings, Core.publication, ::stateOf)
        .stateIn(scope, SharingStarted.Eagerly, null)

    val name: String
        get() = (Core.state.value as? Core.State.Running)?.info?.deviceName ?: Build.MODEL

    val hostLabel: String
        get() = name.filter { it != '\'' && it != '’' }
            .map { if (it.code < 128 && it.isLetterOrDigit()) it else '-' }
            .joinToString("")
            .split('-')
            .filter { it.isNotEmpty() }
            .joinToString("-")
            .ifEmpty { Build.MODEL }

    fun start() {
        scope.launch {
            Core.publication.map { listOf(it.localNetworkError, it.cloudflareError, it.tailscaleError) }.distinctUntilChanged().collect { errors ->
                errors.filter(String::isNotEmpty).forEach(::failed)
            }
        }
    }

    private fun stateOf(settings: CoreSettings?, status: PublicationStatus): State? {
        val stored = settings ?: return null
        val account = stored.cloudflare.account.takeIf { stored.cloudflare.hasAccount() }
        return State(
            LocalNetwork(
                stored.localNetwork.enabled,
                stored.localNetwork.port.toInt(),
                stored.localNetwork.authenticates,
                stored.localNetwork.token,
                status.localNetworkHostname,
            ),
            Cloudflare(
                isEnabled = stored.cloudflare.enabled,
                mode = if (stored.cloudflare.mode == CoreCloudflareMode.CLOUDFLARE_MODE_TUNNEL) CloudflareMode.Tunnel else CloudflareMode.QuickTunnel,
                path = stored.cloudflare.path.ifEmpty { "/" },
                token = stored.cloudflare.token,
                account = account?.let { CloudflareAccount(it.credential, it.name, it.zone) },
                hostname = stored.cloudflare.hostname,
                runningHostname = status.cloudflareHostname,
            ),
            Tailscale(
                isEnabled = stored.tailscale.enabled,
                authenticates = stored.tailscale.authenticates,
                token = stored.tailscale.token,
                status = status.tailscale.takeIf { status.hasTailscale() }?.let {
                    TailscaleStatus(
                        when (it.state) {
                            TailscaleState.TAILSCALE_STATE_STOPPED -> TailscaleStatus.State.Stopped
                            TailscaleState.TAILSCALE_STATE_NEEDS_LOGIN -> TailscaleStatus.State.NeedsLogin
                            TailscaleState.TAILSCALE_STATE_NEEDS_MACHINE_AUTH -> TailscaleStatus.State.NeedsMachineAuth
                            TailscaleState.TAILSCALE_STATE_RUNNING -> TailscaleStatus.State.Running
                            else -> TailscaleStatus.State.Starting
                        },
                        it.loginName,
                        it.tailnet,
                        it.dnsName,
                        it.addressesList,
                    )
                },
            ),
        )
    }

    fun newToken(): String {
        val bytes = ByteArray(16)
        SecureRandom().nextBytes(bytes)
        return bytes.joinToString("") { "%02x".format(it) }
    }

    fun newPath(): String {
        val bytes = ByteArray(8)
        SecureRandom().nextBytes(bytes)
        return "/" + bytes.joinToString("") { "%02x".format(it) }
    }

    private fun change(transform: CoreSettings.Builder.() -> Unit) {
        scope.launch {
            try {
                Core.updateSettings(transform)
            } catch (e: Exception) {
                ErrorAlert.report(SwiptApplication.application.getString(R.string.couldnt_save_settings), e)
            }
        }
    }

    fun setLocalNetworkEnabled(enabled: Boolean) = change { localNetwork = localNetwork.toBuilder().setEnabled(enabled).build() }

    fun setPort(port: Int) {
        require(port in ports) { "invalid port $port" }
        change { localNetwork = localNetwork.toBuilder().setPort(port).build() }
    }

    fun setLocalAuthenticates(authenticates: Boolean) = change { localNetwork = localNetwork.toBuilder().setAuthenticates(authenticates).build() }

    fun setLocalToken(token: String) = change { localNetwork = localNetwork.toBuilder().setToken(token).build() }

    fun setCloudflareEnabled(enabled: Boolean) = change { cloudflare = cloudflare.toBuilder().setEnabled(enabled).build() }

    fun setCloudflarePath(path: String) = change { cloudflare = cloudflare.toBuilder().setPath(path).build() }

    fun setCloudflareToken(token: String) = change { cloudflare = cloudflare.toBuilder().setToken(token).build() }

    fun setCloudflareTunnel(mode: CloudflareMode, account: CloudflareAccount?, hostname: String) = change {
        val tunnel = cloudflare.toBuilder()
            .setMode(if (mode == CloudflareMode.Tunnel) CoreCloudflareMode.CLOUDFLARE_MODE_TUNNEL else CoreCloudflareMode.CLOUDFLARE_MODE_QUICK_TUNNEL)
            .setHostname(hostname)
        if (account == null) {
            tunnel.clearAccount()
        } else {
            tunnel.account = cloudflareAccount {
                credential = account.credential
                name = account.name
                zone = account.zone
            }
        }
        cloudflare = tunnel.build()
    }

    fun setTailscaleEnabled(enabled: Boolean) = change { tailscale = tailscale.toBuilder().setEnabled(enabled).build() }

    fun setTailscaleAuthenticates(authenticates: Boolean) = change { tailscale = tailscale.toBuilder().setAuthenticates(authenticates).build() }

    fun setTailscaleToken(token: String) = change { tailscale = tailscale.toBuilder().setToken(token).build() }

    suspend fun logInCloudflare(open: (String) -> Unit): CloudflareAccount {
        var account: CloudflareAccount? = null
        Core.settingsService.logInCloudflare(Empty.getDefaultInstance()).collect { step ->
            when (step.stepCase) {
                LogInCloudflareResponse.StepCase.URL -> open(step.url)
                LogInCloudflareResponse.StepCase.ACCOUNT -> account = step.account.let { CloudflareAccount(it.credential, it.name, it.zone) }
                else -> {}
            }
        }
        return checkNotNull(account)
    }

    suspend fun logInTailscale(open: (String) -> Unit) {
        Core.settingsService.logInTailscale(Empty.getDefaultInstance()).collect { step ->
            if (step.stepCase == LogInTailscaleResponse.StepCase.URL) open(step.url)
        }
    }

    suspend fun logOutTailscale() {
        Core.settingsService.logOutTailscale(Empty.getDefaultInstance())
    }

    internal fun failed(message: String) {
        val application = SwiptApplication.application
        ErrorAlert.report(AlertState.failure(application.getString(R.string.developer_couldnt_publish), message))
    }
}
